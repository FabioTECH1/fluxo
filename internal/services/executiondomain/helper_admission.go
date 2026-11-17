package executiondomain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"fluxo/internal/syscmd"
	"golang.org/x/sys/unix"
)

// InvocationHelperRequirements describes the written reader format and only
// this invocation's execution/profile role. It does not ask a Scheduler helper
// to advertise package execution or backend migration authority.
func InvocationHelperRequirements(component string, adapter AdapterID) (ReaderCapabilities, error) {
	if adapter == "" {
		return ReaderCapabilities{}, errors.New("new invocation helper admission requires an explicit adapter")
	}
	if _, err := InvocationAdapter(adapter); err != nil {
		return ReaderCapabilities{}, err
	}
	need := ReaderCapabilities{Format: 1, SchedulerSupport: SchedulerReaderSupport, SystemTaskSupport: SystemTaskReaderSupport, SchedulerSchemas: []int{CurrentSchedulerSchema}, SystemTaskSchemas: []int{CurrentSystemTaskSchema}, Adapters: Capabilities().Adapters, RoleContracts: map[string][]string{}}
	switch component {
	case "scheduler":
		need.RoleContracts["scheduler-execute"] = []string{string(adapter)}
	case "system-task":
		need.RoleContracts["system-task-execute"] = []string{string(adapter)}
		need.RoleContracts["system-task-profile"] = Capabilities().RoleContracts["system-task-profile"]
	default:
		return need, errors.New("unknown invocation helper authority role")
	}
	return need, nil
}

// PersistInvocationWithHelper is the last admission boundary before new durable
// invocation/profile authority. The actual retained executable must advertise
// the needed readers and role, after protected-image verification. A marketing
// version, panel capabilities or a committed floor cannot stand in for it.
// The caller retains its journal gate through validation and the single write.
func PersistInvocationWithHelper(ctx context.Context, path, expectedSHA string, need ReaderCapabilities, persist func() error) error {
	return persistInvocationWithHelper(func() (ReaderCapabilities, error) { return ReadHelperCapabilities(ctx, path, expectedSHA) }, need, persist)
}
func persistInvocationWithHelper(inspect func() (ReaderCapabilities, error), need ReaderCapabilities, persist func() error) error {
	have, err := inspect()
	if err != nil {
		return err
	}
	if err = RequireCapabilities(have, need); err != nil {
		return err
	}
	return persist()
}

// ReadHelperCapabilities executes only the pure metadata flag of a protected
// retained image. Content-addressed callers additionally supply their recorded
// SHA. Inode, permissions, size and nanosecond mtime/ctime bracket the digest and
// metadata execution so capability claims cannot silently refer to a replacement.
func ReadHelperCapabilities(ctx context.Context, path, expectedSHA string) (ReaderCapabilities, error) {
	return readHelperCapabilities(ctx, path, expectedSHA, readProtected, func(path string) (unix.Stat_t, error) { var s unix.Stat_t; err := unix.Lstat(path, &s); return s, err }, func(ctx context.Context, path string) (string, error) {
		return syscmd.Run(ctx, 5*time.Second, path, "--execution-capabilities")
	})
}
func readHelperCapabilities(ctx context.Context, path, expectedSHA string, read func(string, int64) ([]byte, error), observe func(string) (unix.Stat_t, error), run func(context.Context, string) (string, error)) (ReaderCapabilities, error) {
	var have ReaderCapabilities
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return have, errors.New("helper capability inspection requires its canonical retained path")
	}
	before, err := observe(path)
	if err != nil {
		return have, err
	}
	code, err := read(path, 128<<20)
	if err != nil {
		return have, err
	}
	if expectedSHA != "" {
		sum := sha256.Sum256(code)
		if hex.EncodeToString(sum[:]) != expectedSHA {
			return have, errors.New("retained helper differs from its recorded content digest")
		}
	}
	out, err := run(ctx, path)
	if err != nil {
		return have, fmt.Errorf("retained helper cannot report execution readers: %w", err)
	}
	after, err := observe(path)
	if err != nil || !sameFileObservation(before, after) {
		return have, errors.New("retained helper changed during capability inspection")
	}
	if err = json.Unmarshal([]byte(out), &have); err != nil {
		return have, errors.New("retained helper returned invalid execution reader metadata")
	}
	return have, nil
}
