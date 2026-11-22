package hostsystem

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"fluxo/internal/bridge"
	"fluxo/internal/services/packages"
	"fluxo/internal/services/phpupdate"
	"fluxo/internal/services/scheduler"
)

func (h *Host) owner() uint32 {
	if h.Root != "" {
		return uint32(os.Geteuid())
	}
	return 0
}
func (h *Host) directory(dir string) error {
	for p := dir; ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil {
			return e
		}
		s, ok := st.Sys().(*syscall.Stat_t)
		if !ok || !st.IsDir() || s.Uid != h.owner() || st.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("Unsafe system directory: %s", p)
		}
		if p == "/" || h.Root != "" && p == h.Root {
			return nil
		}
	}
}

// Gate serializes every settings/restart request across tabs and processes.
func (h *Host) Gate(ctx context.Context) (func(), error) {
	path := h.path("/run/fluxo-host-system.lock")
	if e := h.directory(filepath.Dir(path)); e != nil {
		return nil, e
	}
	fd, e := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), path)
	var s syscall.Stat_t
	if e = syscall.Fstat(fd, &s); e != nil || s.Uid != h.owner() || s.Mode&syscall.S_IFMT != syscall.S_IFREG || s.Mode&0022 != 0 || s.Nlink != 1 {
		f.Close()
		return nil, errors.New("Unsafe system-management lock")
	}
	for {
		if e = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); e == nil {
			return func() { syscall.Flock(fd, syscall.LOCK_UN); f.Close() }, nil
		}
		if !errors.Is(e, syscall.EWOULDBLOCK) && !errors.Is(e, syscall.EAGAIN) {
			f.Close()
			return nil, e
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, errors.New("Server maintenance or restart is in progress")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Admission uses the existing maintenance locks, never a check-then-reboot
// gap. A separate systemd helper retains them after the HTTP response.
func Admission(ctx context.Context) (func(), error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("System management requires the installed root service")
	}
	a, e := packages.Lifecycle(ctx, false)
	if e != nil {
		return nil, e
	}
	b, e := packages.Acquire(ctx)
	if e != nil {
		a()
		return nil, e
	}
	c, e := packages.NativeDPKG(ctx)
	if e != nil {
		b()
		a()
		return nil, e
	}
	release := func() { c(); b(); a() }
	status, e := bridge.InstallerExecutionStatus("/var/lib/fluxo")
	if e != nil {
		release()
		return nil, e
	}
	if status.(map[string]any)["pending"] == true {
		release()
		return nil, errors.New("Complete the exact interrupted Fluxo installation before server maintenance")
	}
	busy, e := scheduler.UpgradeBusy()
	if e != nil || busy {
		release()
		return nil, errors.New("Finish the current Fluxo installation before server maintenance")
	}
	if e = phpupdate.CheckInstallerUpgradeDatabase(ctx, "/var/lib/fluxo"); e != nil {
		release()
		return nil, e
	}
	// Existing package/runtime checks plus durable application operations; no
	// queued deployment or pending deletion is silently interrupted by a reboot.
	db, e := sql.Open("sqlite", "file:/var/lib/fluxo/fluxo.db?mode=ro&_pragma=busy_timeout(1000)")
	if e != nil {
		release()
		return nil, e
	}
	defer db.Close()
	for _, q := range []string{"SELECT count(*) FROM deployments WHERE status IN ('pending','running')", "SELECT count(*) FROM sites WHERE COALESCE(deletion_status,'') != ''"} {
		var n int
		if e = db.QueryRowContext(ctx, q).Scan(&n); e != nil || n != 0 {
			release()
			return nil, errors.New("Finish active deployments or site deletion before server maintenance")
		}
	}
	return release, nil
}
func sameInstalledImage() error {
	if e := New().installedTool("/usr/local/bin/fluxo"); e != nil {
		return e
	}
	hash := func(p string) ([32]byte, error) {
		f, e := os.Open(p)
		if e != nil {
			return [32]byte{}, e
		}
		defer f.Close()
		h := sha256.New()
		_, e = io.Copy(h, f)
		var v [32]byte
		copy(v[:], h.Sum(nil))
		return v, e
	}
	a, e := hash("/proc/self/exe")
	if e != nil {
		return e
	}
	b, e := hash("/usr/local/bin/fluxo")
	if e != nil || a != b {
		return errors.New("Installed Fluxo image changed; restart Fluxo before requesting a server restart")
	}
	return nil
}
func ExecuteInstalledReboot(ctx context.Context, id string) error {
	if os.Getenv("FLUXO_ENV") != "prod" || os.Getenv("FLUXO_DATA_DIR") != "/var/lib/fluxo" || os.Geteuid() != 0 {
		return errors.New("Restart helper requires installed root scope")
	}
	if e := sameInstalledImage(); e != nil {
		return e
	}
	h := New()
	unlock, e := h.Gate(ctx)
	if e != nil {
		return e
	}
	defer unlock()
	return h.Execute(ctx, id, func(c context.Context) (func(), error) {
		release, e := Admission(c)
		if e != nil {
			return nil, e
		}
		if e = sameInstalledImage(); e != nil {
			release()
			return nil, e
		}
		return release, nil
	})
}
