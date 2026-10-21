package phpupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/syscmd"
)

// Observation must inspect descendants even after a fast-terminating Horizon
// master exits. MainPID=0 or an inactive unit is not proof that jobs finished.
func observeUnit(ctx context.Context, name, previousGroup string) (unitSnapshot, error) {
	var snapshot unitSnapshot
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		snapshot, err = observeUnitOnce(ctx, name, previousGroup)
		if !errors.Is(err, errIncoherentActive) {
			return snapshot, err
		}
		// systemctl show and /sys/fs/cgroup are separate reads. A naturally
		// exiting master may disappear between them; retry a complete observation.
		// Persistent uncertainty remains an error and never counts as drainage.
		select {
		case <-ctx.Done():
			return snapshot, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return snapshot, err
}

var errIncoherentActive = errors.New("Active service has no inspectable main process")

func observeUnitOnce(ctx context.Context, name, previousGroup string) (unitSnapshot, error) {
	u := unitSnapshot{Name: name, PIDs: map[int]string{}}
	out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", name, "--property=ActiveState,SubState,MainPID,ControlGroup,NRestarts")
	if err != nil {
		return u, err
	}
	p := unitProperties(out)
	u.State, u.SubState = p["ActiveState"], p["SubState"]
	u.Active = u.State == "active" && u.SubState == "running"
	u.CGroup, u.Restarts = p["ControlGroup"], p["NRestarts"]
	u.MainPID, err = strconv.Atoi(p["MainPID"])
	if err != nil || u.MainPID < 0 || u.State == "" {
		return u, errors.New("Unable to inspect service state")
	}
	groups := []string{u.CGroup}
	if previousGroup != "" && previousGroup != u.CGroup {
		groups = append(groups, previousGroup)
	}
	for _, group := range groups {
		pids, e := cgroupProcesses(group)
		if e != nil {
			return u, e
		}
		for pid, key := range pids {
			u.PIDs[pid] = key
		}
	}
	if u.Active && (u.CGroup == "" || u.MainPID == 0 || u.PIDs[u.MainPID] == "") {
		return u, errIncoherentActive
	}
	return u, nil
}

func unitState(ctx context.Context, name string) (unitSnapshot, error) {
	u, err := observeUnit(ctx, name, "")
	if err != nil {
		return u, err
	}
	if u.State == "activating" || u.State == "deactivating" || u.State == "reloading" {
		return u, fmt.Errorf("Service %s is changing state; retry later", name)
	}
	if !u.Active && (u.MainPID != 0 || len(u.PIDs) != 0) {
		return u, fmt.Errorf("Service %s still has processes; wait for them to finish", name)
	}
	return u, nil
}

func cgroupProcesses(group string) (map[int]string, error) {
	result := map[int]string{}
	if group == "" {
		return result, nil
	}
	if !strings.HasPrefix(group, "/") || group == "/" || filepath.Clean(group) != group || strings.Contains(group, "..") {
		return nil, errors.New("Unrecognized service process group")
	}
	base := filepath.Join("/sys/fs/cgroup", group)
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil // A cgroup may disappear as its last worker exits.
		}
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != "cgroup.procs" {
			return nil
		}
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, value := range strings.Fields(string(data)) {
			pid, e := strconv.Atoi(value)
			if e != nil || pid <= 0 {
				return errors.New("Invalid service process identifier")
			}
			key, e := processKey(pid)
			if errors.Is(e, os.ErrNotExist) {
				continue
			}
			if e != nil {
				return e
			}
			result[pid] = key
			if len(result) > 4096 {
				return errors.New("Service process group is too large for managed maintenance")
			}
		}
		return nil
	})
	return result, err
}

func processKey(pid int) (string, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	i := strings.LastIndex(string(b), ")")
	if i < 0 {
		return "", errors.New("invalid process stat")
	}
	f := strings.Fields(string(b)[i+1:])
	if len(f) < 20 {
		return "", errors.New("invalid process stat")
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(boot)) + ":" + f[19], nil
}

func fullyDrained(old, current unitSnapshot, key func(int) (string, error)) (bool, error) {
	if current.Active || current.MainPID != 0 || len(current.PIDs) != 0 || (current.State != "inactive" && current.State != "failed") {
		return false, nil
	}
	for pid, identity := range old.PIDs {
		now, err := key(pid)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		if now == identity {
			return false, nil
		}
	}
	return true, nil
}
