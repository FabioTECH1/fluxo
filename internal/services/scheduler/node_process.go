package scheduler

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"fluxo/internal/services/packages"
	"fluxo/internal/syscmd"
)

// AuthenticateNodeProcess retains the existing Laravel dispatcher contract and
// authenticates its current guard/lease and child credentials on every read.
func AuthenticateNodeProcess(ctx context.Context, pid int, key, group string) (bool, error) {
	unit := strings.TrimPrefix(group, "/system.slice/")
	if group != "/system.slice/"+unit || !strings.HasPrefix(unit, "fluxo-scheduler-") || !strings.HasSuffix(unit, ".service") {
		return false, nil
	}
	if absent() {
		return true, errors.New("Scheduler invocation evidence is missing")
	}
	unlock, err := lock(ctx)
	if err != nil {
		return true, err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return true, err
	}
	v, ok := j.Invocations[unit]
	if !ok || !v.Guarded || !v.Started || v.Unit != unit {
		return true, errors.New("Node process has no admitted Laravel Scheduler invocation")
	}
	if err = NodeMaintenanceReady(); err != nil {
		return true, err
	}
	p, err := inspectInvocationUnit(ctx, unit, v)
	if err != nil {
		return true, err
	}
	guard, err := strconv.Atoi(p["MainPID"])
	if err != nil || guard <= 1 || guard == pid {
		return true, errors.New("Scheduler has no current root execution guard")
	}
	helper := filepath.Join(stateDir, "dispatcher")
	if err = packages.VerifySharedGuardProcess(guard, helper, group, v.Boot); err != nil {
		return true, err
	}
	credential, err := syscmd.ResolveCredential("fluxo")
	if err != nil {
		return true, err
	}
	if err = packages.VerifyLocalChildIdentity(pid, key, group, credential.Uid, credential.Gid); err != nil {
		return true, err
	}
	fresh, err := inspectInvocationUnit(ctx, unit, v)
	if err != nil {
		return true, err
	}
	if fresh["MainPID"] != p["MainPID"] {
		return true, errors.New("Scheduler guard changed during inventory")
	}
	if err = packages.VerifySharedGuardProcess(guard, helper, group, v.Boot); err != nil {
		return true, err
	}
	return true, packages.VerifyLocalChildIdentity(pid, key, group, credential.Uid, credential.Gid)
}
