package maintenancetask

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"fluxo/internal/services/packages"
)

// AuthenticateNodeProcess is a fresh, role-bound proof, never a cgroup-name
// exemption. Node's caller separately authenticates the exact executable before
// and after it. No transient job PID is added to the immutable update plan.
func AuthenticateNodeProcess(ctx context.Context, pid int, key, group string) (bool, error) {
	unit := strings.TrimPrefix(group, "/system.slice/")
	if group != "/system.slice/"+unit || !validUnit(unit) {
		return false, nil
	}
	unlock, err := gate(ctx, false)
	if err != nil {
		return true, err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return true, err
	}
	v, ok := j.Invocations[unit]
	if !ok || v.Status != "started" || !v.ApplicationStarted || v.Registration.Contract.Kind != ApplicationJob {
		return true, errors.New("Node process has no admitted application-job invocation")
	}
	r, ok := j.Entries[v.Registration.Contract.ID]
	if !ok || r.Identity != v.Registration.Identity || r.Contract != v.Registration.Contract || r.Helper != v.Registration.Helper || r.Database != v.Registration.Database {
		return true, errors.New("Scheduled-job invocation provenance changed")
	}
	raw, err := readRegular(r.Helper, 128<<20)
	if err != nil {
		return true, err
	}
	if digest(raw) != filepath.Base(r.Helper) {
		return true, errors.New("Scheduled-job retained helper changed")
	}
	p, err := inspectInvocation(ctx, v)
	if err != nil {
		return true, err
	}
	if err = liveGuard(v, p); err != nil {
		return true, err
	}
	credential := strings.Split(v.Profile.External["credential"], ":")
	if len(credential) != 2 {
		return true, errors.New("Scheduled-job credential evidence is missing")
	}
	uid, e1 := strconv.ParseUint(credential[0], 10, 32)
	gid, e2 := strconv.ParseUint(credential[1], 10, 32)
	if e1 != nil || e2 != nil || uid == 0 || p["MainPID"] == strconv.Itoa(pid) {
		return true, errors.New("Node process is not an admitted non-root job child")
	}
	if err = packages.VerifyLocalChildIdentity(pid, key, group, uint32(uid), uint32(gid)); err != nil {
		return true, err
	}
	fresh, err := inspectInvocation(ctx, v)
	if err != nil {
		return true, err
	}
	if fresh["MainPID"] != p["MainPID"] {
		return true, errors.New("Scheduled-job guard changed during inventory")
	}
	if err = liveGuard(v, fresh); err != nil {
		return true, err
	}
	return true, packages.VerifyLocalChildIdentity(pid, key, group, uint32(uid), uint32(gid))
}
