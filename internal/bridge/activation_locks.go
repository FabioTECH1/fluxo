package bridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// Initialization and execution gates are inode-stable. New empty coordination
// locks grant no execution authority. Their original absence is recorded in the
// creation plan, and later publication never renames an actively held lock.
// Order: lifecycle/package/native, Scheduler initializer, Scheduler execution,
// system-task initializer/execution gate. Late historical initializers wait on
// these same locks and then encounter the operation fence/current core.
type activationLocks struct {
	files []*os.File
	paths map[string]bool
}

func acquireActivationLocks(ctx context.Context) (*activationLocks, error) {
	locks := &activationLocks{paths: map[string]bool{}}
	if _, err := locks.acquire(ctx, "/var/lib/fluxo-scheduler.init.lock", true); err != nil {
		locks.release()
		return nil, err
	}
	if _, err := os.Lstat("/var/lib/fluxo-scheduler/gate.lock"); err == nil {
		if _, err = locks.acquire(ctx, "/var/lib/fluxo-scheduler/gate.lock", false); err != nil {
			locks.release()
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		locks.release()
		return nil, err
	}
	if _, err := os.Lstat("/var/lib/fluxo-system-tasks.lock"); err == nil {
		if _, err = locks.acquire(ctx, "/var/lib/fluxo-system-tasks.lock", false); err != nil {
			locks.release()
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		locks.release()
		return nil, err
	}
	return locks, nil
}
func (a *activationLocks) release() {
	for i := len(a.files) - 1; i >= 0; i-- {
		unix.Flock(int(a.files[i].Fd()), unix.LOCK_UN)
		a.files[i].Close()
	}
}
func (a *activationLocks) acquire(ctx context.Context, path string, create bool) (bool, error) {
	if a.paths[path] {
		return false, nil
	}
	if err := trustedDirectory(filepath.Dir(path)); err != nil {
		return false, err
	}
	created := false
	flags := unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW
	fd, err := unix.Open(path, flags, 0)
	if os.IsNotExist(err) && create {
		fd, err = unix.Open(path, flags|unix.O_CREAT|unix.O_EXCL, 0600)
		created = err == nil
		if os.IsExist(err) {
			fd, err = unix.Open(path, flags, 0)
		}
	}
	if err != nil {
		return false, err
	}
	file := os.NewFile(uintptr(fd), path)
	before, err := protectedInfo(path)
	if err != nil {
		file.Close()
		return false, err
	}
	for {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if err != unix.EAGAIN && err != unix.EWOULDBLOCK {
			file.Close()
			return false, err
		}
		select {
		case <-ctx.Done():
			file.Close()
			return false, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	named, err := protectedInfo(path)
	opened, e := file.Stat()
	if err != nil || e != nil || !os.SameFile(before, named) || !os.SameFile(before, opened) {
		unix.Flock(fd, unix.LOCK_UN)
		file.Close()
		return false, errors.New("activation coordination gate identity changed")
	}
	if created {
		if err = file.Sync(); err == nil {
			err = syncActivationDirectory(filepath.Dir(path))
		}
		if err != nil {
			unix.Flock(fd, unix.LOCK_UN)
			file.Close()
			return false, err
		}
	}
	a.files = append(a.files, file)
	a.paths[path] = true
	return created, nil
}

func (a *activationLocks) verify() error {
	for _, file := range a.files {
		opened, err := file.Stat()
		if err != nil {
			return err
		}
		named, err := protectedInfo(file.Name())
		if err != nil || !os.SameFile(opened, named) {
			return errors.New("activation held coordination gate was replaced before commit")
		}
	}
	return nil
}
