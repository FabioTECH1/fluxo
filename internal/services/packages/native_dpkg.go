package packages

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// NativeDPKG excludes APT/dpkg, including operators outside Fluxo. These are
// POSIX record locks, not flock locks. Caller holds lifecycle then Fluxo locks;
// no package-manager command may run while these native locks are held.
func NativeDPKG(ctx context.Context) (func(), error) {
	return nativeDPKGLocks(ctx, []string{"/var/lib/dpkg/lock-frontend", "/var/lib/dpkg/lock"})
}

func nativeDPKGLocks(ctx context.Context, paths []string) (func(), error) {
	files := []*os.File{}
	release := func() {
		for i := len(files) - 1; i >= 0; i-- {
			_ = files[i].Close()
		}
	}
	for _, path := range paths {
		for p := filepath.Dir(path); ; p = filepath.Dir(p) {
			info, e := os.Lstat(p)
			if e != nil {
				release()
				return nil, e
			}
			st, ok := info.Sys().(*syscall.Stat_t)
			if !ok || st.Uid != 0 || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
				release()
				return nil, errors.New("Unsafe native package lock directory")
			}
			if p == "/" {
				break
			}
		}
		fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
		if err != nil {
			release()
			return nil, fmt.Errorf("Open native package lock: %w", err)
		}
		f := os.NewFile(uintptr(fd), path)
		files = append(files, f)
		var st syscall.Stat_t
		if err = syscall.Fstat(fd, &st); err != nil || st.Uid != 0 || st.Mode&syscall.S_IFMT != syscall.S_IFREG || st.Mode&0022 != 0 || st.Nlink != 1 {
			release()
			return nil, errors.New("Unsafe native package lock")
		}
		lock := syscall.Flock_t{Type: syscall.F_WRLCK, Whence: 0, Start: 0, Len: 0}
		for {
			err = syscall.FcntlFlock(uintptr(fd), syscall.F_SETLK, &lock)
			if err == nil {
				break
			}
			if !errors.Is(err, syscall.EACCES) && !errors.Is(err, syscall.EAGAIN) {
				release()
				return nil, err
			}
			select {
			case <-ctx.Done():
				release()
				return nil, fmt.Errorf("APT/dpkg is busy; no applications were stopped: %w", ctx.Err())
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	return release, nil
}
