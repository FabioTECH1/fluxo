package maintenancetask

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"fluxo/internal/services/packages"
)

func ownThreads(pid int) (map[string]string, error) {
	entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", pid))
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, entry := range entries {
		if _, err = strconv.Atoi(entry.Name()); err != nil {
			return nil, err
		}
		b, e := os.ReadFile(fmt.Sprintf("/proc/%d/task/%s/stat", pid, entry.Name()))
		if e != nil {
			return nil, e
		}
		i := strings.LastIndexByte(string(b), ')')
		if i < 0 {
			return nil, errors.New("Invalid helper thread identity")
		}
		fields := strings.Fields(string(b)[i+1:])
		if len(fields) < 20 {
			return nil, errors.New("Incomplete helper thread identity")
		}
		result[entry.Name()] = fields[19]
	}
	if len(result) == 0 {
		return nil, errors.New("Helper has no execution threads")
	}
	return result, nil
}

// domainDrained brackets the hierarchical kernel counter with positive
// identities for every helper thread. Go helpers are multithreaded: subtracting
// one PID from pids.current would hang; subtracting an unbound thread count
// could hide descendants. Every before-thread must still be the same after-
// thread, so the counter at the middle observation accounts only for this helper.
func domainDrained(unit string, pid int) (bool, error) {
	if !validUnit(unit) {
		return false, errors.New("Invalid job execution domain")
	}
	beforeID, err := processIdentity(pid)
	if err != nil {
		return false, err
	}
	before, err := ownThreads(pid)
	if err != nil {
		return false, err
	}
	path := filepath.Join("/sys/fs/cgroup/system.slice", unit)
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	b, err := os.ReadFile(fmt.Sprintf("/proc/self/fd/%d/pids.current", f.Fd()))
	if err != nil {
		return false, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return false, err
	}
	after, err := ownThreads(pid)
	if err != nil {
		return false, err
	}
	afterID, err := processIdentity(pid)
	if err != nil {
		return false, err
	}
	current, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if info.Sys().(*syscall.Stat_t).Ino != current.Sys().(*syscall.Stat_t).Ino || beforeID != afterID {
		return false, errors.New("Job execution domain or helper identity changed")
	}
	if len(before) != len(after) || n != len(before) {
		return false, nil
	}
	for id, key := range before {
		if after[id] != key {
			return false, nil
		}
	}
	return true, nil
}

func waitDomain(ctx context.Context, unit string) error {
	for {
		drained, err := domainDrained(unit, os.Getpid())
		if err != nil {
			return err
		}
		if drained {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func liveGuard(v invocation, p map[string]string) error {
	pid, err := strconv.Atoi(p["MainPID"])
	if err != nil || pid <= 1 {
		return errors.New("Job has no live execution guard")
	}
	before, err := processIdentity(pid)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(before, v.Boot+":") {
		return errors.New("Job guard boot changed")
	}
	a, err := os.Stat(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return err
	}
	b, err := os.Stat(v.Registration.Helper)
	if err != nil {
		return err
	}
	if !os.SameFile(a, b) {
		return errors.New("Job guard executable changed")
	}
	cgroup, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil || strings.TrimSpace(string(cgroup)) != "0::/system.slice/"+v.Unit {
		return errors.New("Job guard left its recorded execution domain")
	}
	if err = packages.VerifySharedLifecycleOwner(pid); err != nil {
		return err
	}
	after, err := processIdentity(pid)
	if err != nil {
		return err
	}
	if before != after {
		return errors.New("Job guard process identity changed")
	}
	// Reinspect the executable as well as boot/start: exec preserves start time.
	a, err = os.Stat(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return err
	}
	if !os.SameFile(a, b) {
		return errors.New("Job guard role changed during observation")
	}
	return nil
}
