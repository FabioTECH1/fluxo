// Package packages coordinates host package and PHP runtime maintenance across
// Fluxo processes. Native APT locks remain authoritative for other operators.
package packages

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

const LockPath = "/run/lock/fluxo-packages.lock"
const LifecycleLockPath = "/run/lock/fluxo-php-lifecycle.lock"

// Acquire returns an unlock function. Lock order is lifecycle, then packages.
func Acquire(ctx context.Context) (func(), error) { return acquire(ctx, LockPath, false) }
func Lifecycle(ctx context.Context, shared bool) (func(), error) {
	return acquire(ctx, LifecycleLockPath, shared)
}

func acquire(ctx context.Context, path string, shared bool) (func(), error) {
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, fmt.Errorf("open maintenance lock: %w", err)
	}
	f := os.NewFile(uintptr(fd), path)
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil || st.Mode&syscall.S_IFMT != syscall.S_IFREG || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 || st.Mode&0022 != 0 {
		f.Close()
		return nil, errors.New("unsafe maintenance lock ownership or mode")
	}
	mode := syscall.LOCK_EX
	if shared {
		mode = syscall.LOCK_SH
	}
	for {
		if err := syscall.Flock(fd, mode|syscall.LOCK_NB); err == nil {
			return func() { _ = syscall.Flock(fd, syscall.LOCK_UN); _ = f.Close() }, nil
		} else if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, fmt.Errorf("wait for maintenance lock: %w", ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}
