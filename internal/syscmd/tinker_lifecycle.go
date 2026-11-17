package syscmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// TinkerPlatform checks the kill-on-session-exit contract before a ticket is
// issued. Tinker intentionally terminates its own descendants on cancellation;
// it does not use Scheduler's persistent/natural-drain execution adapter.
func TinkerPlatform(ctx context.Context) error {
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		return errors.New("Tinker requires unified cgroup v2")
	}
	out, err := Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", "--property=Version", "--value")
	if err != nil {
		return err
	}
	version := strings.TrimSpace(out)
	end := 0
	for end < len(version) && version[end] >= '0' && version[end] <= '9' {
		end++
	}
	number, err := strconv.Atoi(version[:end])
	if err != nil || number < 249 {
		return errors.New("Tinker requires systemd 249 or newer")
	}
	return nil
}

func lockTinkerLaunch(ctx context.Context, dir string) (func(), error) {
	if filepath.Dir(dir) != "/run" || !strings.HasPrefix(filepath.Base(dir), "fluxo-tinker-config-") {
		return nil, errors.New("invalid Tinker launch directory")
	}
	for parent := dir; ; parent = filepath.Dir(parent) {
		var st unix.Stat_t
		if err := unix.Lstat(parent, &st); err != nil {
			return nil, err
		}
		if st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Mode&0022 != 0 {
			return nil, errors.New("unsafe Tinker launch directory")
		}
		if parent == "/" {
			break
		}
	}
	fd, err := unix.Open(filepath.Join(dir, "launch.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "Tinker launch gate")
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil || st.Uid != 0 || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&07777 != 0600 || st.Nlink != 1 {
		f.Close()
		return nil, errors.New("unsafe Tinker launch gate")
	}
	for {
		if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err == nil {
			var once sync.Once
			return func() { once.Do(func() { _ = unix.Flock(fd, unix.LOCK_UN); _ = f.Close() }) }, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func revokeTinkerAdmission(ctx context.Context, dir string) error {
	unlock, err := lockTinkerLaunch(ctx, dir)
	if os.IsNotExist(err) {
		return nil
	} // An absent runtime directory has no launch marker.
	if err != nil {
		return err
	}
	defer unlock()
	if err = os.Remove(filepath.Join(dir, "admission")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func waitTinkerDomainEmpty(ctx context.Context, unit string) error {
	id := strings.TrimSuffix(strings.TrimPrefix(unit, "fluxo-tinker-"), ".service")
	if !tinkerID.MatchString(id) || unit != "fluxo-tinker-"+id+".service" {
		return errors.New("invalid Tinker execution domain")
	}
	path := filepath.Join("/sys/fs/cgroup/system.slice", unit, "cgroup.events")
	return waitTinkerPopulation(ctx, func() ([]byte, error) {
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			if _, check := os.Lstat(filepath.Dir(path)); os.IsNotExist(check) {
				return []byte("populated 0\n"), nil
			}
			return nil, errors.New("Tinker execution domain lacks population evidence")
		}
		return raw, err
	})
}

func waitTinkerPopulation(ctx context.Context, read func() ([]byte, error)) error {
	for {
		raw, err := read()
		if err != nil {
			return err
		}
		found, populated := false, false
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 0 || fields[0] != "populated" {
				continue
			}
			if found || len(fields) != 2 || (fields[1] != "0" && fields[1] != "1") {
				return errors.New("invalid Tinker population evidence")
			}
			found, populated = true, fields[1] == "1"
		}
		if !found {
			return errors.New("missing Tinker population evidence")
		}
		if !populated {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("Tinker descendants have not drained: %w", ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}
