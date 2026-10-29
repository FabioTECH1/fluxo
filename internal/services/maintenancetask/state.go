package maintenancetask

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"fluxo/internal/services/scheduler"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

var stateRoot = "/var/lib/fluxo-system-tasks"

type registration struct {
	Contract          Contract                      `json:"contract"`
	Database          string                        `json:"database"`
	Identity          string                        `json:"identity"`
	Boot              string                        `json:"boot"`
	Backend           scheduler.CronBackendIdentity `json:"backend"`
	Nonce             string                        `json:"nonce"`
	Received          bool                          `json:"received"`
	Published         bool                          `json:"published"`
	Enabled           bool                          `json:"enabled"`
	Predecessor       string                        `json:"predecessor"`
	PredecessorAbsent bool                          `json:"predecessor_absent"`
	Helper            string                        `json:"helper"`
}

type invocation struct {
	Unit               string       `json:"unit"`
	Registration       registration `json:"registration"`
	Boot               string       `json:"boot"`
	Status             string       `json:"status"`
	Profile            profile      `json:"profile"`
	Error              string       `json:"error"`
	ResultProfile      *profile     `json:"result_profile"`
	ReconciledFrom     string       `json:"reconciled_from,omitempty"`
	ReconciledBoot     string       `json:"reconciled_boot,omitempty"`
	ApplicationStarted bool         `json:"application_started"`
}

type journal struct {
	Schema      int                   `json:"schema"`
	Entries     map[int]registration  `json:"entries"`
	Invocations map[string]invocation `json:"invocations"`
}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func bootID() (string, error) {
	b, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(b))
	_, err = uuid.Parse(s)
	return s, err
}

// readRegular verifies the complete immutable ancestor chain before opening.
// The descriptor and path must still identify the same single-link regular file.
func readRegular(path string, limit int64) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("Unsafe task path")
	}
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		st, err := os.Lstat(p)
		if err != nil {
			return nil, err
		}
		s, ok := st.Sys().(*syscall.Stat_t)
		if !ok || s.Uid != 0 || !st.IsDir() || st.Mode().Perm()&0022 != 0 {
			return nil, fmt.Errorf("Unsafe task ancestor: %s", p)
		}
		if p == "/" {
			break
		}
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var before, after, current unix.Stat_t
	if err = unix.Fstat(fd, &before); err != nil {
		return nil, err
	}
	if before.Uid != 0 || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&0022 != 0 || before.Nlink != 1 || before.Size > limit {
		return nil, fmt.Errorf("Unsafe task file: %s", path)
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if err = unix.Fstat(fd, &after); err != nil {
		return nil, err
	}
	if err = unix.Lstat(path, &current); err != nil {
		return nil, err
	}
	stable := func(a, b unix.Stat_t) bool {
		return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
	}
	if !stable(before, after) || !stable(before, current) || int64(len(b)) != before.Size {
		return nil, errors.New("Task file changed during read")
	}
	return b, nil
}

func syncDir(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func atomicFile(path string, b []byte, mode os.FileMode) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".task-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	return syncDir(filepath.Dir(path))
}

func gate(ctx context.Context, initialize bool) (func(), error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("System maintenance task management requires root")
	}
	// The anchor is outside the journal directory; losing state cannot reset authority.
	parent := filepath.Dir(stateRoot)
	st, err := os.Lstat(parent)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() || st.Mode().Perm()&0022 != 0 || st.Sys().(*syscall.Stat_t).Uid != 0 {
		return nil, errors.New("Unsafe system-task state parent")
	}
	anchor, anchorReadErr := readRegular(stateRoot+".initialized", 32)
	if anchorReadErr == nil && string(anchor) != "1\n" {
		return nil, errors.New("Invalid system-task initialization anchor")
	}
	if !initialize && anchorReadErr != nil {
		return nil, anchorReadErr
	}
	flags := unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW
	if initialize && os.IsNotExist(anchorReadErr) {
		flags |= unix.O_CREAT
	}
	fd, err := unix.Open(stateRoot+".lock", flags, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), stateRoot+".lock")
	var stat unix.Stat_t
	if err = unix.Fstat(fd, &stat); err != nil || stat.Uid != 0 || stat.Nlink != 1 || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0022 != 0 {
		f.Close()
		return nil, errors.New("Unsafe system-task gate")
	}
	for {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if err != unix.EAGAIN && err != unix.EWOULDBLOCK {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	unlock := func() { unix.Flock(fd, unix.LOCK_UN); f.Close() }
	var pathStat unix.Stat_t
	if err = unix.Lstat(stateRoot+".lock", &pathStat); err != nil || pathStat.Dev != stat.Dev || pathStat.Ino != stat.Ino {
		unlock()
		return nil, errors.New("System-task gate replaced")
	}
	_, anchorErr := readRegular(stateRoot+".initialized", 32)
	_, stateErr := os.Lstat(filepath.Join(stateRoot, "state.json"))
	if err = finishInitialization(f, initialize, anchorErr, stateErr); err != nil {
		unlock()
		return nil, err
	}

	return unlock, nil
}

func save(j journal) error {
	b, e := json.Marshal(j)
	if e != nil {
		return e
	}
	if len(b) > 8<<20 {
		return errors.New("Scheduled-job journal budget reached; finish or reconcile outstanding jobs before starting more")
	}
	return atomicFile(filepath.Join(stateRoot, "state.json"), b, 0600)
}
func read() (journal, error) {
	var j journal
	b, e := readRegular(filepath.Join(stateRoot, "state.json"), 8<<20)
	if e != nil {
		return j, e
	}
	if e = json.Unmarshal(b, &j); e != nil {
		return j, e
	}
	var raw struct {
		Entries     map[string]json.RawMessage `json:"entries"`
		Invocations map[string]json.RawMessage `json:"invocations"`
	}
	if e = json.Unmarshal(b, &raw); e != nil {
		return j, e
	}
	required := func(data json.RawMessage, keys ...string) error {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return err
		}
		for _, key := range keys {
			if _, ok := fields[key]; !ok {
				return errors.New("Incomplete scheduled-job evidence: " + key)
			}
		}
		return nil
	}
	registrationFields := []string{"contract", "database", "identity", "boot", "backend", "nonce", "received", "published", "enabled", "predecessor", "predecessor_absent", "helper"}
	for _, r := range raw.Entries {
		if e = required(r, registrationFields...); e != nil {
			return j, e
		}
	}
	for _, v := range raw.Invocations {
		if e = required(v, "unit", "registration", "boot", "status", "profile", "error", "result_profile", "application_started"); e != nil {
			return j, e
		}
		var nested map[string]json.RawMessage
		if e = json.Unmarshal(v, &nested); e != nil {
			return j, e
		}
		if e = required(nested["registration"], registrationFields...); e != nil {
			return j, e
		}
	}
	if j.Schema != 1 || j.Entries == nil || j.Invocations == nil {
		return j, errors.New("Incomplete system-task journal")
	}
	for id, r := range j.Entries {
		if id != r.Contract.ID || !validContract(r.Contract) || !filepath.IsAbs(r.Database) || r.Boot == "" || r.Nonce == "" || r.Helper == "" || r.Predecessor == "" {
			return j, errors.New("Invalid system-task registration")
		}
		if _, e = uuid.Parse(r.Identity); e != nil {
			return j, e
		}
		if _, e = uuid.Parse(r.Boot); e != nil {
			return j, e
		}
		if _, e = uuid.Parse(r.Nonce); e != nil {
			return j, e
		}
		if r.Predecessor != LegacyContent(r.Contract) || filepath.Dir(r.Helper) != filepath.Join(stateRoot, "helpers") || len(filepath.Base(r.Helper)) != 64 {
			return j, errors.New("Incomplete scheduled-job provenance")
		}
	}
	for name, v := range j.Invocations {
		if name != v.Unit || !validUnit(name) || v.Registration.Identity == "" || v.Boot == "" {
			return j, errors.New("Invalid system-task invocation")
		}
		if !validContract(v.Registration.Contract) || v.Profile.Kind != v.Registration.Contract.Kind || v.Profile.Files == nil || v.Profile.Directories == nil || v.Profile.External == nil {
			return j, errors.New("Incomplete scheduled-job invocation contract")
		}
		if (v.Status == "completed" || v.Status == "interrupted") && (v.ResultProfile == nil || v.ResultProfile.Kind != v.Registration.Contract.Kind) {
			return j, errors.New("System-task completion evidence is missing")
		}
		if v.Status == "interrupted" {
			if v.ReconciledFrom != "started" && v.ReconciledFrom != "failed" {
				return j, errors.New("Interrupted task has no retained original outcome")
			}
			if _, e = uuid.Parse(v.ReconciledBoot); e != nil {
				return j, e
			}
		}
		switch v.Status {
		case "submitted", "started", "completed", "blocked", "failed", "interrupted":
		default:
			return j, errors.New("Unknown system-task invocation state")
		}
	}
	return j, nil
}

func validUnit(s string) bool {
	if !strings.HasPrefix(s, "fluxo-system-task-") || !strings.HasSuffix(s, ".service") {
		return false
	}
	_, e := uuid.Parse(strings.TrimSuffix(strings.TrimPrefix(s, "fluxo-system-task-"), ".service"))
	return e == nil
}
