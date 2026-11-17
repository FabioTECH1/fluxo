package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/packages"
	"fluxo/internal/services/processlog"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

// Package-local selector permits guarded native test executables to exercise
// the real dispatch path. Production selection remains the shared policy.
var invocationAdapterSelector = executiondomain.SelectAdapter

// Dispatch submits one minute's work. The root process never executes Artisan;
// systemd starts it as fluxo in an invocation-specific, non-delegated cgroup.
func Dispatch(ctx context.Context, id int) (string, error) {
	unlock, err := lock(ctx)
	if err != nil {
		return "", err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return "", err
	}
	if historicalRetirementPending(j) {
		return "", errors.New("Scheduler historical retirement must finish before execution")
	}
	if j.Upgrade != nil {
		return "", errors.New("Scheduler dispatch is held for installer upgrade")
	}
	if j.Schema != executiondomain.CurrentSchedulerSchema {
		return "", errors.New("activate current execution reader contracts before creating new Scheduler authority")
	}
	adapter, err := invocationAdapterSelector(ctx)
	if err != nil {
		return "", err
	}
	if err = executiondomain.GuardAdapterWrite(filepath.Dir(databasePath), "scheduler", adapter); err != nil {
		return "", err
	}
	e, ok := j.Entries[id]
	if !ok || !e.Enabled {
		return "", errors.New("Scheduler is not enabled")
	}
	if err = requireEntryLoaderAcknowledgement(ctx, j, e); err != nil {
		return "", err
	}
	if err = committed(ctx, e, true); errors.Is(err, ErrMaintenance) {
		return "Scheduler paused for PHP maintenance; this tick was skipped.", nil
	} else if err != nil {
		return "", fmt.Errorf("Scheduler registration is unavailable or changed: %w", err)
	}
	if j.Holds[e.Version] != "" {
		return "Scheduler paused for PHP maintenance; this tick was skipped.", nil
	}
	if _, ok := Qualify(e.ID, e.SiteID, e.Directory, "* * * * *", e.Command, "fluxo"); !ok {
		return "", errors.New("Invalid Scheduler manifest")
	}
	// Sweep once per branch per minute rather than inspecting every prior
	// invocation for every site tick. Uncertain submissions remain journaled and
	// block maintenance, while other scheduler entries continue normal operation.
	if time.Now().Unix()-j.LastSweep[e.Version] >= 60 {
		if _, err = observe(ctx, &j, e.Version, true); err != nil {
			return "", err
		}
		j.LastSweep[e.Version] = time.Now().Unix()
	}
	if len(j.Invocations) >= 4096 {
		return "", errors.New("Scheduler invocation journal is full; finish or reconcile outstanding tasks")
	}

	credential, err := syscmd.ResolveCredential("fluxo")
	if err != nil {
		return "", err
	}
	if credential.Uid == 0 {
		return "", errors.New("Scheduler application identity cannot be root")
	}
	logPath := fmt.Sprintf("/var/log/fluxo/cron-%d.log", id)
	if err = processlog.Prepare(logPath, "fluxo"); err != nil {
		return "", err
	}
	boot, err := bootID()
	if err != nil {
		return "", err
	}
	unit := "fluxo-scheduler-" + uuid.NewString() + ".service"
	inv := invocation{Unit: unit, Entry: e, Boot: boot, Guarded: true}
	inv.Adapter = adapter
	if j.Schema == executiondomain.SchedulerLoaderSchema {
		inv.ExecutionContract = executiondomain.SchedulerBootstrapContract
		inv.Helper, inv.HelperSHA256, err = installBootstrapHelper()
		if err != nil {
			return "", err
		}
	}
	need, err := executiondomain.InvocationHelperRequirements("scheduler", adapter)
	if err != nil {
		return "", err
	}
	helper, helperSHA := filepath.Join(stateDir, "dispatcher"), ""
	if inv.ExecutionContract != "" {
		helper, helperSHA = inv.Helper, inv.HelperSHA256
		need.RoleContracts["scheduler-execute"] = append(need.RoleContracts["scheduler-execute"], executiondomain.SchedulerLoaderExecutionContract, executiondomain.SchedulerBootstrapContract)
		need.RoleContracts["scheduler-loader-reader"] = []string{executiondomain.SchedulerLoaderReaderContract}
	}
	if err = executiondomain.PersistInvocationWithHelper(ctx, helper, helperSHA, need, func() error { j.Invocations[unit] = inv; return save(j) }); err != nil {
		return "", err
	}
	out, err := submitRecordedInvocation(ctx, inv, logPath)
	if err != nil {
		return "", fmt.Errorf("Scheduler submission needs reconciliation; no maintenance can proceed until its outcome is known: %w %s", err, out)
	}
	inv.Submitted = true
	j.Invocations[unit] = inv
	if err = save(j); err != nil {
		return "", err
	}
	return "Scheduler started. Output is available in its cron log.", nil
}

// Legacy callers/tests explicitly retain the original service contract.
func submitInvocation(ctx context.Context, unit string, e Entry, logPath string) (string, error) {
	return submitInvocationAdapter(ctx, executiondomain.CgroupExitV1, unit, e, logPath)
}
func submitInvocationAdapter(ctx context.Context, adapter executiondomain.AdapterID, unit string, e Entry, logPath string) (string, error) {
	return submitRecordedInvocation(ctx, invocation{Adapter: adapter, Unit: unit, Entry: e, Guarded: true}, logPath)
}
func submitRecordedInvocation(ctx context.Context, inv invocation, logPath string) (string, error) {
	adapter, unit := inv.Adapter, inv.Unit
	helper, command := filepath.Join(stateDir, "dispatcher"), "scheduler-execute"
	caps := "CAP_SETUID CAP_SETGID CAP_DAC_READ_SEARCH"
	if inv.ExecutionContract == executiondomain.SchedulerBootstrapContract {
		helper, command = inv.Helper, "scheduler-bootstrap"
		caps += " CAP_SYS_PTRACE CAP_SETPCAP"
	}
	args, err := executiondomain.SubmissionArguments(adapter, unit)
	if err != nil {
		return "", err
	}
	args = append(args,
		"--property=User=root", "--property=Group=root",
		"--property=CapabilityBoundingSet="+caps,
		"--property=WorkingDirectory=/", "--property=StandardOutput=append:"+logPath,
		"--property=StandardError=inherit", "--setenv=HOME=/home/fluxo", "--setenv=USER=fluxo",
		"--setenv=LOGNAME=fluxo", "--setenv=PATH=/usr/local/bin:/usr/bin:/bin",
		"--", helper, command, unit)
	return syscmd.Run(ctx, 20*time.Second, "/usr/bin/systemd-run", args...)
}

// Execute is the fixed root service entry point. Even a delayed systemd request
// must reacquire admission before application code can run. Retiring an absent
// uncertain submission therefore prevents its later execution without reboot.
func Execute(ctx context.Context, unit string) error {
	leaseCtx, cancelLease := context.WithTimeout(ctx, 5*time.Second)
	lease, leaseErr := packages.Lifecycle(leaseCtx, true)
	cancelLease()
	if leaseErr != nil {
		return leaseErr
	}
	defer lease()
	unlock, err := lock(ctx)
	if err != nil {
		return err
	}
	j, err := read()
	if err != nil {
		unlock()
		return err
	}
	inv, ok := j.Invocations[unit]
	if j.Schema >= executiondomain.SchedulerLoaderSchema {
		unlock()
		return errors.New("schema5 Scheduler launch requires its fixed bootstrap/worker entrypoints")
	}
	if ok && inv.ExecutionContract != "" {
		unlock()
		return errors.New("versioned Scheduler authority requires its fixed bootstrap/worker entrypoints")
	}
	if !ok || !inv.Guarded || inv.Started {
		unlock()
		return errors.New("Scheduler submission is retired or unsupported")
	}
	if err = executiondomain.RequireAdapterHost(ctx, inv.Adapter); err != nil {
		unlock()
		return err
	}
	current, ok := j.Entries[inv.Entry.ID]
	if !ok || !current.Enabled || current.Identity != inv.Entry.Identity || identity(current) != identity(inv.Entry) {
		unlock()
		return errors.New("Scheduler registration was disabled or replaced before execution")
	}
	if err = requireEntryLoaderAcknowledgement(ctx, j, inv.Entry); err != nil {
		unlock()
		return err
	}
	raw, err := os.ReadFile("/proc/self/cgroup")
	if err != nil || strings.TrimSpace(string(raw)) != "0::/system.slice/"+unit {
		unlock()
		return errors.New("Scheduler execution requires its recorded service cgroup")
	}
	boot, err := bootID()
	if err != nil || boot != inv.Boot {
		unlock()
		return errors.New("Scheduler submission belongs to another boot")
	}
	unitState, unitErr := inspectInvocationUnit(ctx, unit, inv)
	if unitErr != nil || unitState["LoadState"] != "loaded" {
		unlock()
		if unitErr != nil {
			return unitErr
		}
		return errors.New("Scheduler service is not loaded")
	}
	if j.Holds[inv.Entry.Version] != "" {
		unlock()
		return nil
	}
	if err = committed(ctx, inv.Entry, true); err != nil {
		unlock()
		if errors.Is(err, ErrMaintenance) {
			return nil
		}
		return err
	}
	if inv.Adapter == executiondomain.PersistentV1 {
		inv.ServiceIdentity, err = executiondomain.CaptureServiceIdentity(unit, unitState)
		if err != nil {
			unlock()
			return err
		}
	}
	inv.Submitted = true
	inv.Started = true
	j.Invocations[unit] = inv
	err = save(j)
	unlock()
	if err != nil {
		return err
	}
	// The root helper stays in the service until the direct application exits;
	// ExitType=cgroup continues tracking detached children afterward.
	runErr := syscmd.RunTrackedApplication("fluxo", inv.Entry.Directory, "/usr/bin/php"+inv.Entry.Version, filepath.Join(inv.Entry.Directory, "artisan"), "schedule:run")
	// Keep the shared lifecycle lease until detached application descendants
	// leave this invocation too. The helper itself is the only excluded PID.
	for {
		pids, inspectErr := groupPIDs("/system.slice/" + unit)
		if inspectErr != nil {
			return inspectErr
		}
		delete(pids, os.Getpid())
		if len(pids) == 0 {
			return runErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func properties(out string) map[string]string {
	p := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			p[k] = v
		}
	}
	return p
}

// observe covers all descendants, including non-PHP children and detached jobs.
// ExitType=cgroup prevents systemd from discarding these when Artisan exits.
func observe(ctx context.Context, j *journal, branch string, bestEffort ...bool) (map[int]bool, error) {
	known := map[int]bool{}
	boot, err := bootID()
	if err != nil {
		return nil, err
	}
	for name, inv := range j.Invocations {
		if name != inv.Unit || !strings.HasPrefix(name, "fluxo-scheduler-") || !strings.HasSuffix(name, ".service") {
			return nil, errors.New("Invalid Scheduler invocation journal")
		}
		if _, err := uuid.Parse(strings.TrimSuffix(strings.TrimPrefix(name, "fluxo-scheduler-"), ".service")); err != nil {
			return nil, err
		}
		if inv.Entry.Version != branch {
			continue
		}
		if inv.Boot != boot {
			delete(j.Invocations, name)
			continue
		}
		if !inv.Submitted && !inv.Guarded {
			if len(bestEffort) > 0 && bestEffort[0] {
				continue
			}
			return nil, fmt.Errorf("Scheduler submission %s needs manual reconciliation before PHP maintenance", name)
		}
		p, err := inspectInvocationUnit(ctx, name, inv)
		if err != nil {
			return nil, err
		}
		group := p["ControlGroup"]
		if group != "" && group != "/system.slice/"+name {
			return nil, errors.New("Scheduler cgroup identity changed")
		}
		pids, err := groupPIDs("/system.slice/" + name)
		if err != nil {
			return nil, err
		}
		for pid := range pids {
			known[pid] = true
		}

		if len(pids) == 0 && executiondomain.UnitQuiescent(inv.Adapter, p) {
			if err = schedulerHierarchyEmpty(name); err != nil {
				return nil, err
			}
			adapter, _ := executiondomain.InvocationAdapter(inv.Adapter)
			if adapter == executiondomain.PersistentV1 && p["LoadState"] != "not-found" {

				if !inv.Started {
					// Revoke and fsync before administrative stop. A delayed
					// StartUnit request must find no application authority.
					delete(j.Invocations, name)
					if err = save(*j); err != nil {
						return nil, err
					}
					if err = executiondomain.StopNaturallyEmpty(ctx, inv.Adapter, name, func() error { return schedulerHierarchyEmpty(name) }); err != nil {
						return nil, err
					}
					continue
				}
				if err = retireEmptySchedulerUnit(ctx, inv, p); err != nil {
					return nil, err
				}
			}
			delete(j.Invocations, name)
		}
	}
	return known, nil
}

func inspectInvocationUnit(ctx context.Context, name string, inv invocation) (map[string]string, error) {
	out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", name, "--property=LoadState,ActiveState,SubState,InvocationID,ControlGroup,MainPID,ControlPID,Type,User,Group,ExitType,Restart,Delegate,NoNewPrivileges,ProtectControlGroups,ExecStart,WorkingDirectory,Transient,CapabilityBoundingSet,AmbientCapabilities,ExecStartPre,ExecStartPost,ExecStop,ExecStopPost,RuntimeMaxUSec,WatchdogUSec,Environment,StandardOutput,StandardError,RemainAfterExit,KillMode,CollectMode,TimeoutStopUSec,SendSIGKILL,DropInPaths")
	if err != nil {
		return nil, err
	}
	p := properties(out)
	if p["LoadState"] == "not-found" {
		return p, nil
	}
	if inv.Adapter == executiondomain.PersistentV1 && inv.Started || inv.ExecutionContract == executiondomain.SchedulerBootstrapContract && inv.Bootstrap != nil {
		if err = executiondomain.ValidateServiceIdentity(inv.ServiceIdentity, name, p); err != nil {
			return nil, err
		}
	}
	prefix := "{ path=/usr/bin/php" + inv.Entry.Version + " ; argv[]=/usr/bin/php" + inv.Entry.Version + " " + filepath.Join(inv.Entry.Directory, "artisan") + " schedule:run ;"
	user, dir := "fluxo", inv.Entry.Directory
	if inv.Guarded {
		user, dir = "root", "/"
		helper, command := filepath.Join(stateDir, "dispatcher"), "scheduler-execute"
		if inv.ExecutionContract == executiondomain.SchedulerBootstrapContract {
			helper, command = inv.Helper, "scheduler-bootstrap"
		}
		prefix = "{ path=" + helper + " ; argv[]=" + helper + " " + command + " " + name + " ;"
	}
	want, err := executiondomain.InvocationProperties(inv.Adapter)
	if err != nil {
		return nil, err
	}
	if err = executiondomain.ValidateLifetime(inv.Adapter, p); err != nil {
		return nil, err
	}
	want["User"], want["Group"], want["WorkingDirectory"] = user, user, dir
	if inv.Guarded {
		for _, key := range []string{"ExecStartPre", "ExecStartPost", "ExecStop", "ExecStopPost", "AmbientCapabilities"} {
			want[key] = ""
		}
		want["RuntimeMaxUSec"], want["WatchdogUSec"] = "infinity", "0"
		caps := strings.Fields(p["CapabilityBoundingSet"])
		sort.Strings(caps)
		wantCaps := "cap_dac_read_search cap_setgid cap_setuid"
		if inv.ExecutionContract == executiondomain.SchedulerBootstrapContract {
			wantCaps = "cap_dac_read_search cap_setgid cap_setpcap cap_setuid cap_sys_ptrace"
		}
		if strings.Join(caps, " ") != wantCaps {
			return nil, errors.New("Scheduler helper capabilities changed")
		}
		env := strings.Fields(p["Environment"])
		sort.Strings(env)
		if strings.Join(env, " ") != "HOME=/home/fluxo LOGNAME=fluxo PATH=/usr/local/bin:/usr/bin:/bin USER=fluxo" {
			return nil, errors.New("Scheduler helper environment changed")
		}
		fragment := filepath.Join("/run/systemd/transient", name)
		if err = trustedFile(fragment); err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(fragment)
		if err != nil {
			return nil, err
		}
		if !strings.Contains(string(raw), "\nStandardOutput=append:/var/log/fluxo/cron-"+strconv.Itoa(inv.Entry.ID)+".log\n") {
			return nil, errors.New("Scheduler helper output changed")
		}
	}
	for key, value := range want {
		if p[key] != value {
			return nil, fmt.Errorf("Scheduler invocation %s changed", key)
		}
	}
	if !strings.HasPrefix(p["ExecStart"], prefix) || strings.Count(p["ExecStart"], "{ path=") != 1 || !strings.Contains(p["ExecStart"], " ; ignore_errors=no ;") {
		return nil, errors.New("Scheduler invocation command changed")
	}
	return p, nil
}

func groupPIDs(group string) (map[int]bool, error) {
	pids := map[int]bool{}
	if group == "" {
		return pids, nil
	}
	base := filepath.Join("/sys/fs/cgroup", group)
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != "cgroup.procs" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, v := range strings.Fields(string(raw)) {
			pid, err := strconv.Atoi(v)
			if err != nil || pid <= 0 {
				return errors.New("Invalid Scheduler cgroup process")
			}
			pids[pid] = true
			if len(pids) > 4096 {
				return errors.New("Scheduler process limit exceeded")
			}
		}
		return nil
	})
	return pids, err
}

// KnownPIDs is used only to recognize journaled invocations during baseline
// capture. The final package gate requires every invocation to be gone.
func KnownPIDs(ctx context.Context, branch string) (map[int]bool, error) {
	if absent() {
		return map[int]bool{}, nil
	}
	unlock, err := lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return nil, err
	}
	pids, err := observe(ctx, &j, branch)
	if err != nil {
		return nil, err
	}
	return pids, save(j)
}

// RuntimeLease prevents new submissions while the caller scans /proc. Children
// created by an admitted invocation during that scan are recognized by cgroup,
// rather than by a stale PID list. Application users cannot delegate this group.
func RuntimeLease(ctx context.Context, branch string) (func(int) bool, func(), error) {
	if absent() {
		return func(int) bool { return false }, func() {}, nil
	}
	unlock, err := lock(ctx)
	if err != nil {
		return nil, nil, err
	}
	j, err := read()
	if err == nil {
		_, err = observe(ctx, &j, branch)
	}
	if err == nil {
		err = save(j)
	}
	if err != nil {
		unlock()
		return nil, nil, err
	}
	groups := map[string]bool{}
	for name, inv := range j.Invocations {
		if inv.Entry.Version == branch {
			groups["/system.slice/"+name] = true
		}
	}
	contains := func(pid int) bool {
		raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
		if err != nil {
			return false
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "0::") && groups[strings.TrimPrefix(line, "0::")] {
				return true
			}
		}
		return false
	}
	var once sync.Once
	release := func() { once.Do(unlock) }
	return contains, release, nil
}

func Hold(ctx context.Context, branch, generation string, entries []Entry) error {
	if err := initialize(ctx); err != nil {
		return err
	}

	unlock, err := lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	if j.Upgrade != nil {
		return errors.New("Fluxo installer is active; PHP maintenance was not started")
	}
	if owner := j.Holds[branch]; owner != "" && owner != generation {
		return errors.New("Scheduler hold belongs to another maintenance generation")
	}
	err = reconcileLegacy(ctx, &j, branch)
	if saveErr := save(j); saveErr != nil {
		return saveErr
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		cur, ok := j.Entries[e.ID]
		if !ok || cur != e || !cur.Ready {
			return errors.New("Scheduler configuration changed after review")
		}
		if err = verifyRegistration(ctx, e); err != nil {
			return err
		}
	}
	delete(j.Released, branch+":"+generation)
	j.Holds[branch] = generation
	return save(j)
}

// Reclose uses the trusted generation identity before checks which may fail.
// Registration drift must never prevent closing a partly released branch.
func Reclose(ctx context.Context, branch, generation string) error {
	if !branchRE.MatchString(branch) {
		return errors.New("Invalid Scheduler branch")
	}
	if _, err := uuid.Parse(generation); err != nil {
		return err
	}
	if err := initialize(ctx); err != nil {
		return err
	}
	unlock, err := lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	if owner := j.Holds[branch]; owner != "" && owner != generation {
		return errors.New("Scheduler hold belongs to another generation")
	}
	j.Holds[branch] = generation
	delete(j.Released, branch+":"+generation)
	return save(j)
}

func AssertDrained(ctx context.Context, branch, generation string, entries []Entry) error {

	unlock, err := lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	if j.Holds[branch] != generation {
		return errors.New("Scheduler admission hold is missing")
	}
	for _, e := range entries {
		if cur, ok := j.Entries[e.ID]; !ok || cur != e {
			return errors.New("Scheduler identity changed during maintenance")
		}
		if err = verifyRegistration(ctx, e); err != nil {
			return err
		}
	}
	_, err = observe(ctx, &j, branch)
	if err != nil {
		return err
	}
	for _, inv := range j.Invocations {
		if inv.Entry.Version == branch {
			return errors.New("Waiting for scheduled tasks and their background children to finish")
		}
	}
	return save(j)
}

func Drain(ctx context.Context, branch, generation string, entries []Entry) error {
	if err := Hold(ctx, branch, generation, entries); err != nil {
		return err
	}
	deadline := time.NewTimer(5 * time.Minute)
	defer deadline.Stop()
	for {
		err := AssertDrained(ctx, branch, generation, entries)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("Scheduler remains paused; tasks did not drain safely: %w", err)
		case <-time.After(time.Second):
		}
	}
}

func Release(ctx context.Context, branch, generation string, entries []Entry) error {

	unlock, err := lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	if j.Holds[branch] == "" {
		if j.Released[branch+":"+generation] {
			return nil
		}
		return errors.New("Scheduler release evidence is missing")
	}
	if j.Holds[branch] != generation {
		return errors.New("Scheduler hold ownership changed")
	}
	for _, e := range entries {
		if cur, ok := j.Entries[e.ID]; !ok || cur != e {
			return errors.New("Scheduler configuration changed before restore")
		}
		if err = verifyRegistration(ctx, e); err != nil {
			return err
		}
	}
	j.Released[branch+":"+generation] = true
	delete(j.Holds, branch)
	return save(j)
}
