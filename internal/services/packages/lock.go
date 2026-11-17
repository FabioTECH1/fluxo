// Package packages coordinates host package and PHP runtime maintenance across
// Fluxo processes. Native APT locks remain authoritative for other operators.
package packages

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	f, err := acquireFile(ctx, path, shared)
	if err != nil {
		return nil, err
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}

func acquireFile(ctx context.Context, path string, shared bool) (*os.File, error) {
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
			if path == LifecycleLockPath && !shared {
				if err := tinkerDomainsIdle(); err != nil {
					_ = syscall.Flock(fd, syscall.LOCK_UN)
					f.Close()
					return nil, err
				}
			}
			return f, nil
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

// A crashed panel drops its flock before systemd finishes cgroup cleanup.
// The process-local registry also distinguishes this panel's live sessions from
// execution domains left by a prior daemon during restart.
var tinkerRegistry = struct {
	sync.Mutex
	ids map[string]bool
}{ids: map[string]bool{}}

func RegisterTinkerDomain(id string) func() {
	tinkerRegistry.Lock()
	tinkerRegistry.ids[id] = true
	tinkerRegistry.Unlock()
	return func() { tinkerRegistry.Lock(); delete(tinkerRegistry.ids, id); tinkerRegistry.Unlock() }
}
func activeTinkerDomains() ([]string, error) {
	paths, err := filepath.Glob("/sys/fs/cgroup/system.slice/fluxo-tinker-*.service/cgroup.events")
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if strings.Contains(string(b), "populated 1") {
			name := filepath.Base(filepath.Dir(p))
			id := strings.TrimSuffix(strings.TrimPrefix(name, "fluxo-tinker-"), ".service")
			if _, err := hex.DecodeString(id); err == nil && len(id) == 32 {
				ids = append(ids, id)
			}
		}
	}
	return ids, nil
}
func UnownedTinkerDomainsIdle() error {
	ids, err := activeTinkerDomains()
	if err != nil {
		return err
	}
	tinkerRegistry.Lock()
	defer tinkerRegistry.Unlock()
	for _, id := range ids {
		if !tinkerRegistry.ids[id] {
			return errors.New("Wait for a previous panel's Tinker cleanup")
		}
	}
	return nil
}
func tinkerDomainsIdle() error {
	ids, err := activeTinkerDomains()
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		return errors.New("Wait for Tinker session cleanup before runtime maintenance")
	}
	return nil
}

// TinkerDomainsIdle observes every live interactive domain, including sessions
// owned by this process. It performs no cancellation or registration changes.
func TinkerDomainsIdle() error { return tinkerDomainsIdle() }
