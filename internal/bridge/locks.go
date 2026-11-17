package bridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// Lock order matches registration callers: lifecycle, packages, native dpkg,
// Scheduler gate, system-task gate, then SQLite admission. No lock is created.
func lockCoordination(ctx context.Context) (func(), error) {
	var files []*os.File
	release := func() {
		for i := len(files) - 1; i >= 0; i-- {
			unix.Flock(int(files[i].Fd()), unix.LOCK_UN)
			files[i].Close()
		}
	}
	for _, path := range []string{"/var/lib/fluxo-scheduler/gate.lock", "/var/lib/fluxo-system-tasks.lock"} {
		before, err := protectedInfo(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			release()
			return nil, err
		}
		if err = trustedDirectory(filepath.Dir(path)); err != nil {
			release()
			return nil, err
		}
		fd, err := unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			release()
			return nil, err
		}
		f := os.NewFile(uintptr(fd), path)
		files = append(files, f)
		for {
			err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
			if err == nil {
				break
			}
			if err != unix.EAGAIN && err != unix.EWOULDBLOCK {
				release()
				return nil, err
			}
			select {
			case <-ctx.Done():
				release()
				return nil, ctx.Err()
			case <-time.After(25 * time.Millisecond):
			}
		}
		after, err := f.Stat()
		if err != nil || !os.SameFile(before, after) {
			release()
			return nil, errors.New("coordination lock identity changed")
		}
	}
	return release, nil
}
