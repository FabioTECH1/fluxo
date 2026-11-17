package nginx

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const httpDefaultsTransactionName = ".fluxo-http-defaults-transaction"

// HTTPDefaultsRecoveryReady is a strictly read-only admission check. A pending
// quarantine may contain an operator replacement and must never disappear from
// upgrade/certification readiness merely because Nginx no longer includes it.
func HTTPDefaultsRecoveryReady() error {
	parent := filepath.Dir(httpDefaultsPath)
	for path := parent; ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return err
			}
		} else {
			st, ok := info.Sys().(*syscall.Stat_t)
			if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || st.Uid != 0 || info.Mode().Perm()&0022 != 0 {
				return errors.New("unsafe Nginx HTTP defaults ancestry")
			}
		}
		if path == "/" {
			break
		}
	}
	return inspectHTTPDefaultsRecovery(filepath.Join(parent, httpDefaultsTransactionName), 0)
}

func inspectHTTPDefaultsRecovery(directory string, owner uint32) error {
	if _, err := os.Lstat(directory); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	tx := httpDefaultsTransaction{directory: directory, owner: owner, env: httpDefaultsEnvironment{path: filepath.Join(filepath.Dir(directory), filepath.Base(httpDefaultsPath))}}
	if err := tx.privateDirectory(directory); err != nil {
		return err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		switch entry.Name() {
		case "evidence":
			if err = tx.privateDirectory(tx.path("evidence")); err != nil {
				return err
			}
		case "lock":
			info, e := os.Lstat(tx.path("lock"))
			if e != nil {
				return e
			}
			st, ok := info.Sys().(*syscall.Stat_t)
			if !ok || !info.Mode().IsRegular() || st.Uid != owner || st.Nlink != 1 || info.Mode().Perm() != 0600 {
				return errors.New("unsafe Nginx HTTP defaults lock")
			}
		default:
			return tx.retained(errors.New("finish Nginx HTTP defaults recovery before upgrading or certifying"))
		}
	}
	return nil
}

type httpDefaultsIdentity struct {
	Device   uint64 `json:"device"`
	Inode    uint64 `json:"inode"`
	Mode     uint32 `json:"mode"`
	UID      uint32 `json:"uid"`
	GID      uint32 `json:"gid"`
	Links    uint64 `json:"links"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
}

type httpDefaultsRollbackIntent struct {
	Format   int                  `json:"format"`
	Target   string               `json:"target"`
	Expected httpDefaultsIdentity `json:"expected"`
}

// This directory is deliberately outside the *.conf include namespace. Only
// this controller writes inside it; operator replacements at the public path
// are atomically captured and restored, never inspected then unlinked there.
type httpDefaultsTransaction struct {
	env       httpDefaultsEnvironment
	directory string
	owner     uint32
	lock      *os.File
}

func openHTTPDefaultsTransaction(ctx context.Context, env httpDefaultsEnvironment) (*httpDefaultsTransaction, error) {
	parent := filepath.Dir(env.path)
	info, err := os.Stat(parent)
	if err != nil {
		return nil, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, errors.New("missing Nginx directory identity")
	}
	tx := &httpDefaultsTransaction{env: env, directory: filepath.Join(parent, httpDefaultsTransactionName), owner: st.Uid}
	if err = os.Mkdir(tx.directory, 0700); err == nil {
		if err = tx.sync(parent); err != nil {
			return nil, err
		}
	} else if !os.IsExist(err) {
		return nil, err
	}
	if err = tx.privateDirectory(tx.directory); err != nil {
		return nil, err
	}
	fd, err := unix.Open(tx.path("lock"), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	tx.lock = os.NewFile(uintptr(fd), "Nginx HTTP defaults lock")
	if _, err = tx.privateRegular(tx.lock, 0600, 1<<20); err != nil {
		tx.lock.Close()
		return nil, err
	}
	for {
		if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			tx.lock.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			tx.lock.Close()
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	if err = tx.checkEntries(); err != nil {
		tx.close()
		return nil, err
	}
	return tx, nil
}

func (t *httpDefaultsTransaction) close() {
	_ = unix.Flock(int(t.lock.Fd()), unix.LOCK_UN)
	_ = t.lock.Close()
}
func (t *httpDefaultsTransaction) path(name string) string { return filepath.Join(t.directory, name) }
func (t *httpDefaultsTransaction) sync(path string) error {
	if t.env.syncDirectory != nil {
		return t.env.syncDirectory(path)
	}
	return syncHTTPDefaultsDirectory(path)
}
func (t *httpDefaultsTransaction) point(name string) {
	if t.env.checkpoint != nil {
		t.env.checkpoint(name)
	}
}
func (t *httpDefaultsTransaction) syncMovement() error {
	return errors.Join(t.sync(t.directory), t.sync(filepath.Dir(t.env.path)))
}
func (t *httpDefaultsTransaction) retained(err error) error {
	return fmt.Errorf("Nginx HTTP defaults recovery retained at %s; public path %s was not overwritten: %w", t.directory, t.env.path, err)
}

func (t *httpDefaultsTransaction) privateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || st.Uid != t.owner || info.Mode().Perm() != 0700 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("unsafe private Nginx recovery directory: %s", path)
	}
	return nil
}

func (t *httpDefaultsTransaction) checkEntries() error {
	entries, err := os.ReadDir(t.directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		switch entry.Name() {
		case "lock", "intent.json", "staged", "held":
		case "evidence":
			if err = t.privateDirectory(t.path("evidence")); err != nil {
				return err
			}
		default:
			return t.retained(errors.New("unknown recovery directory member"))
		}
	}
	return nil
}

func httpIdentity(info os.FileInfo) (httpDefaultsIdentity, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return httpDefaultsIdentity{}, errors.New("missing file identity")
	}
	return httpDefaultsIdentity{uint64(st.Dev), st.Ino, st.Mode, st.Uid, st.Gid, uint64(st.Nlink), st.Size, info.ModTime().UnixNano()}, nil
}

func (t *httpDefaultsTransaction) privateRegular(file *os.File, mode os.FileMode, limit int64) (httpDefaultsIdentity, error) {
	info, err := file.Stat()
	if err != nil {
		return httpDefaultsIdentity{}, err
	}
	id, err := httpIdentity(info)
	if err != nil {
		return id, err
	}
	if !info.Mode().IsRegular() || id.UID != t.owner || id.Links != 1 || info.Mode().Perm() != mode || id.Size < 0 || id.Size > limit || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return id, errors.New("unsafe private Nginx recovery file")
	}
	return id, nil
}

// A candidate can be removed only after it is inside the closed quarantine,
// has the recorded inode/metadata and exact managed bytes, and baseline passed.
func (t *httpDefaultsTransaction) candidate(path string, expected *httpDefaultsIdentity) (httpDefaultsIdentity, bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return httpDefaultsIdentity{}, false, err
	}
	id, err := httpIdentity(info)
	if err != nil {
		return id, false, err
	}
	if !info.Mode().IsRegular() || id.UID != t.owner || id.Links != 1 || info.Mode().Perm()&0022 != 0 || id.Size != int64(len(httpDefaultsConfig)) || expected != nil && id != *expected {
		return id, false, nil
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return id, false, err
	}
	f := os.NewFile(uintptr(fd), "Nginx owned candidate")
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return id, false, err
	}
	opened, err := httpIdentity(before)
	if err != nil || opened != id {
		return id, false, errors.New("Nginx candidate changed before read")
	}
	raw, err := io.ReadAll(io.LimitReader(f, int64(len(httpDefaultsConfig)+1)))
	if err != nil {
		return id, false, err
	}
	after, err := f.Stat()
	if err != nil {
		return id, false, err
	}
	closed, err := httpIdentity(after)
	return id, err == nil && closed == id && bytes.Equal(raw, []byte(httpDefaultsConfig)), err
}

func (t *httpDefaultsTransaction) publish() (identity httpDefaultsIdentity, published bool, err error) {
	fd, err := unix.Open(t.path("staged"), unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return identity, false, err
	}
	f := os.NewFile(uintptr(fd), "Nginx staged HTTP default")
	defer f.Close()
	if err = f.Chmod(0644); err != nil {
		return identity, false, err
	}
	if _, err = f.WriteString(httpDefaultsConfig); err != nil {
		return identity, false, err
	}
	if err = f.Sync(); err != nil {
		return identity, false, err
	}
	info, err := f.Stat()
	if err != nil {
		return identity, false, err
	}
	identity, err = httpIdentity(info)
	if err != nil {
		return identity, false, err
	}
	if err = f.Close(); err != nil {
		return identity, false, err
	}
	if err = t.sync(t.directory); err != nil {
		return identity, false, err
	}
	t.point("before-publish")
	// Never replace a public file, including one appearing after validation.
	if err = unix.Renameat2(unix.AT_FDCWD, t.path("staged"), unix.AT_FDCWD, t.env.path, unix.RENAME_NOREPLACE); err != nil {
		return identity, false, err
	}
	published = true
	t.point("after-publish-rename")
	if err = t.syncMovement(); err != nil {
		return identity, true, err
	}
	t.point("after-publish")
	return identity, true, nil
}

func (t *httpDefaultsTransaction) writeIntent(expected httpDefaultsIdentity) error {
	fd, err := unix.Open(t.path("intent.json"), unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), "Nginx HTTP rollback intent")
	defer f.Close()
	raw, err := json.Marshal(httpDefaultsRollbackIntent{1, t.env.path, expected})
	if err != nil {
		return err
	}
	if _, err = f.Write(append(raw, '\n')); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = t.sync(t.directory); err != nil {
		return err
	}
	t.point("after-rollback-intent")
	return nil
}

func (t *httpDefaultsTransaction) readIntent() (httpDefaultsRollbackIntent, error) {
	var intent httpDefaultsRollbackIntent
	fd, err := unix.Open(t.path("intent.json"), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return intent, err
	}
	f := os.NewFile(uintptr(fd), "Nginx HTTP rollback intent")
	defer f.Close()
	if _, err = t.privateRegular(f, 0600, 64<<10); err != nil {
		return intent, err
	}
	raw, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return intent, errors.New("unreadable Nginx rollback intent")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(&intent); err != nil {
		return intent, err
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return intent, errors.New("trailing Nginx rollback intent")
	}
	if intent.Format != 1 || intent.Target != t.env.path || intent.Expected.Inode == 0 || intent.Expected.Mode&syscall.S_IFMT != syscall.S_IFREG || intent.Expected.UID != t.owner || intent.Expected.Links != 1 || intent.Expected.Size != int64(len(httpDefaultsConfig)) {
		return intent, errors.New("invalid Nginx rollback intent binding")
	}
	return intent, nil
}

func (t *httpDefaultsTransaction) clearIntent() error {
	if err := os.Remove(t.path("intent.json")); err != nil {
		return err
	}
	return t.sync(t.directory)
}

func existsHTTPPath(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (t *httpDefaultsTransaction) capture() error {
	t.point("before-quarantine")
	if err := unix.Renameat2(unix.AT_FDCWD, t.env.path, unix.AT_FDCWD, t.path("held"), unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return t.retained(err)
	}
	t.point("after-quarantine-rename")
	if err := t.syncMovement(); err != nil {
		return t.retained(err)
	}
	t.point("after-quarantine")
	return nil
}

func (t *httpDefaultsTransaction) rollback(ctx context.Context, expected httpDefaultsIdentity) error {
	if err := t.writeIntent(expected); err != nil {
		return t.retained(err)
	}
	if err := t.capture(); err != nil {
		return err
	}
	return t.finishRollback(ctx, expected)
}

func (t *httpDefaultsTransaction) restoreHeld() error {
	t.point("before-restore")
	if err := unix.Renameat2(unix.AT_FDCWD, t.path("held"), unix.AT_FDCWD, t.env.path, unix.RENAME_NOREPLACE); err != nil {
		return t.retained(err)
	}
	if err := t.syncMovement(); err != nil {
		return t.retained(err)
	}
	t.point("after-restore")
	return t.clearIntent()
}

func (t *httpDefaultsTransaction) finishRollback(ctx context.Context, expected httpDefaultsIdentity) error {
	held, err := existsHTTPPath(t.path("held"))
	if err != nil {
		return t.retained(err)
	}
	live, err := existsHTTPPath(t.env.path)
	if err != nil {
		return t.retained(err)
	}
	if held && live {
		return t.retained(errors.New("both quarantined and public files exist; preserve both for review"))
	}
	if !held {
		if live {
			_, owned, e := t.candidate(t.env.path, &expected)
			if e != nil {
				return t.retained(e)
			}
			if !owned {
				if err = t.clearIntent(); err != nil {
					return t.retained(err)
				}
				return errors.New("operator replacement was preserved at the Nginx HTTP defaults path")
			}
			if err = t.capture(); err != nil {
				return err
			}
			return t.finishRollback(ctx, expected)
		}
		if err = t.env.validate(ctx); err != nil {
			return t.retained(fmt.Errorf("configuration without candidate does not validate: %w", err))
		}
		return t.clearIntent()
	}
	_, owned, err := t.candidate(t.path("held"), &expected)
	if err != nil {
		return t.retained(err)
	}
	if !owned {
		if err = t.restoreHeld(); err != nil {
			return err
		}
		return errors.New("operator replacement was restored unchanged; review its custom Nginx HTTP defaults")
	}
	if err = t.env.validate(ctx); err != nil {
		if restoreErr := t.restoreHeld(); restoreErr != nil {
			return errors.Join(err, restoreErr)
		}
		return fmt.Errorf("configuration without candidate failed validation; original candidate restored: %w", err)
	}
	// The public path may have changed while Nginx validated. Never discard
	// the held original while another public file occupies its destination.
	if present, e := existsHTTPPath(t.env.path); e != nil || present {
		return t.retained(errors.New("public path changed during rollback validation"))
	}
	if _, owned, err = t.candidate(t.path("held"), &expected); err != nil || !owned {
		return t.retained(errors.New("quarantined candidate identity changed"))
	}
	t.point("before-discard")
	if err = os.Remove(t.path("held")); err != nil {
		return t.retained(err)
	}
	if err = t.sync(t.directory); err != nil {
		return t.retained(err)
	}
	t.point("after-discard")
	return t.clearIntent()
}

func (t *httpDefaultsTransaction) recover(ctx context.Context) error {
	intent, err := t.readIntent()
	if err == nil {
		if err = t.finishRollback(ctx, intent.Expected); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		if held, e := existsHTTPPath(t.path("held")); e != nil || held {
			return t.retained(errors.New("invalid intent with quarantined data; preserve both for review"))
		}
		// No rename can precede a fully synced intent. An incomplete record
		// with no held data is pre-mutation metadata; retain it as evidence.
		if err = t.archiveRecoveryFile("intent.json", "incomplete-intent"); err != nil {
			return t.retained(err)
		}
	}
	if held, e := existsHTTPPath(t.path("held")); e != nil || held {
		return t.retained(errors.New("quarantined file has no valid rollback intent"))
	}
	if staged, e := existsHTTPPath(t.path("staged")); e != nil {
		return e
	} else if staged {
		_, owned, e := t.candidate(t.path("staged"), nil)
		if e != nil {
			return t.retained(e)
		}
		if !owned {
			// A staged file has never occupied the public namespace. Preserve
			// incomplete writes as evidence without granting them authority.
			if err = t.archiveRecoveryFile("staged", "unpublished-candidate"); err != nil {
				return t.retained(err)
			}
		} else {
			if err = os.Remove(t.path("staged")); err != nil {
				return err
			}
			if err = t.sync(t.directory); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *httpDefaultsTransaction) archiveRecoveryFile(name, prefix string) error {
	evidence := t.path("evidence")
	if err := os.Mkdir(evidence, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	if err := t.privateDirectory(evidence); err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	target := filepath.Join(evidence, fmt.Sprintf("%s-%x", prefix, nonce))
	if err := unix.Renameat2(unix.AT_FDCWD, t.path(name), unix.AT_FDCWD, target, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	return errors.Join(t.sync(evidence), t.sync(t.directory))
}
