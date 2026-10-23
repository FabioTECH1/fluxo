package nodeupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fluxo/internal/services/phpupdate"
)

// A domain starts with an authenticated snapshot, not a cgroup allowlist. New
// PIDs must pass a fresh inspection of that exact service before being retained.
type processDomain struct {
	pids            map[int]string
	nodeApplication bool
	refresh         func(context.Context) (map[int]string, error)
}

var errNodeNotObserved = errors.New("Node.js process changed during fresh consumer inspection")
var errOutsideNodeConsumer = errors.New("is outside a supported managed application; stop it before review")

func (d *processDomain) authenticate(ctx context.Context, pid int, key string) error {
	if key == "" {
		return ErrChanged
	}
	if d.nodeApplication && d.pids[pid] == key {
		return nil
	}
	fresh, err := d.refresh(ctx)
	if err != nil {
		return err
	}
	if fresh[pid] != key {
		return errNodeNotObserved
	}
	// Keep prior execution-domain evidence as well as the new observation. A
	// later drain must still account for previously recorded live descendants.
	for p, identity := range fresh {
		d.pids[p] = identity
	}
	return nil
}

func inspectNodeProcessInventory(ctx context.Context, roots []string, generationID string, recovery bool, impact *Impact) error {
	domains := map[string]*processDomain{}
	for i := range impact.Units {
		u := &impact.Units[i]
		if u.PIDs == nil {
			u.PIDs = map[int]string{}
		}
		domains["/system.slice/"+u.Name] = &processDomain{pids: u.PIDs, nodeApplication: true, refresh: func(ctx context.Context) (map[int]string, error) {
			fresh, err := observeUnit(ctx, *u, generationID, recovery)
			if err != nil {
				return nil, err
			}
			if fresh.Definition != u.Definition || fresh.Enabled != u.Enabled || fresh.MainPID != u.MainPID || fresh.MainPID > 0 && fresh.PIDs[fresh.MainPID] != u.PIDs[u.MainPID] {
				return nil, errors.New("Node.js consumer configuration or controller changed during process inventory")
			}
			_, _, err = runtimeProcesses(fresh, roots...)
			return fresh.PIDs, err
		}}
	}
	for i := range impact.PHP {
		c := &impact.PHP[i]
		for j := range c.Units {
			u := &c.Units[j]
			if u.Runtime.PIDs == nil {
				u.Runtime.PIDs = map[int]string{}
			}
			// exec preserves PID/start identity. A recorded PHP controller or
			// native process becoming Node must not inherit a child's exemption.
			domains["/system.slice/"+u.Runtime.Name] = &processDomain{pids: u.Runtime.PIDs, refresh: func(ctx context.Context) (map[int]string, error) {
				fresh, err := phpupdate.ReinspectMaintenanceUnit(ctx, *c, *u, phpupdate.MaintenanceInspection{
					Recovery: recovery,
					Normalize: func(name string, properties map[string]string) (map[string]string, error) {
						return normalizePHPBarrier(name, generationID, properties)
					},
					AdditionalProcess: phpNodeProcess(roots),
				})
				return fresh.Runtime.PIDs, err
			}}
		}
	}
	processes, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	for _, p := range processes {
		if err := ctx.Err(); err != nil {
			return err
		}
		pid, err := strconv.Atoi(p.Name())
		if err != nil || pid <= 0 {
			continue
		}
		exe, err := os.Readlink(processExePath(pid))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		name := filepath.Base(strings.TrimSuffix(exe, " (deleted)"))
		if name != "node" && name != "nodejs" {
			continue
		}
		key, err := processKey(pid)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		group, err := nodeProcessGroup(pid)
		if err == nil {
			domain := domains[group]
			if domain == nil {
				err = errOutsideNodeConsumer
			} else {
				// Check the current exact executable even for a recorded PID. The
				// same process may exec again without changing its boot/start key.
				_, _, err = runtimeProcesses(Unit{PIDs: map[int]string{pid: key}}, roots...)
				if err == nil {
					err = domain.authenticate(ctx, pid, key)
				}
			}
		}
		// Only disappearance is benign. Reuse, movement, permission errors,
		// controller changes and untrusted runtime failures remain blockers.
		if err == nil {
			_, _, err = runtimeProcesses(Unit{PIDs: map[int]string{pid: key}}, roots...)
		}
		current, currentErr := processKey(pid)
		if os.IsNotExist(currentErr) {
			if err == nil || errors.Is(err, errNodeNotObserved) || missingNodeProcess(err, pid) {
				continue
			}
			impact.BlockedReasons = append(impact.BlockedReasons, nodeProcessBlocker(pid, err))
			continue
		}
		if currentErr != nil {
			return currentErr
		}
		if current != key {
			err = errors.New("Node.js process identity changed during inventory; review again")
		} else if currentGroup, e := nodeProcessGroup(pid); e != nil || currentGroup != group {
			err = errors.New("Node.js process domain changed during inventory; review again")
		}
		if err != nil {
			impact.BlockedReasons = append(impact.BlockedReasons, nodeProcessBlocker(pid, err))
		}
	}
	return nil
}

func nodeProcessBlocker(pid int, err error) string {
	if errors.Is(err, errOutsideNodeConsumer) {
		return fmt.Sprintf("Node.js process %d %v", pid, err)
	}
	return fmt.Sprintf("Node.js process %d: %v", pid, err)
}

func missingNodeProcess(err error, pid int) bool {
	var pathErr *os.PathError
	return errors.As(err, &pathErr) && os.IsNotExist(err) && strings.HasPrefix(pathErr.Path, fmt.Sprintf("/proc/%d/", pid)) && filepath.Clean(pathErr.Path) == pathErr.Path
}

func nodeProcessGroup(pid int) (string, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return "", err
	}
	return unifiedProcessGroup(string(b))
}

func unifiedProcessGroup(raw string) (string, error) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "0::/") {
		return "", errors.New("Unsupported Node.js process cgroup")
	}
	group := strings.TrimPrefix(lines[0], "0::")
	if filepath.Clean(group) != group || strings.Contains(group, "..") {
		return "", errors.New("Invalid Node.js process cgroup")
	}
	return group, nil
}
