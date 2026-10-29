package maintenancetask

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/services/packages"
	"fluxo/internal/services/processlog"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

func processIdentity(pid int) (string, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	i := strings.LastIndexByte(string(b), ')')
	if i < 0 {
		return "", errors.New("Invalid task process identity")
	}
	fields := strings.Fields(string(b)[i+1:])
	if len(fields) < 20 {
		return "", errors.New("Incomplete task process identity")
	}
	boot, err := bootID()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d:%s", boot, pid, fields[19]), nil
}
func processExecutable(pid int) (string, error) { return os.Readlink(fmt.Sprintf("/proc/%d/exe", pid)) }

func inspectInvocation(ctx context.Context, v invocation) (map[string]string, error) {
	p, err := unitProperties(ctx, v.Unit, "Id,LoadState,ActiveState,SubState,MainPID,ControlPID,ControlGroup,Type,User,Group,ExitType,Restart,Delegate,NoNewPrivileges,ProtectControlGroups,Transient,WorkingDirectory,InaccessiblePaths,ExecStart,ExecStartPre,ExecStartPost,ExecStop,ExecStopPost,Environment,DropInPaths,RuntimeMaxUSec,WatchdogUSec")
	if err != nil {
		return nil, err
	}
	if p["LoadState"] == "not-found" {
		return p, nil
	}
	want := map[string]string{"Id": v.Unit, "LoadState": "loaded", "Type": "exec", "User": "root", "Group": "root", "ExitType": "cgroup", "Restart": "no", "Delegate": "no", "NoNewPrivileges": "yes", "ProtectControlGroups": "yes", "Transient": "yes", "WorkingDirectory": "/root", "ExecStartPre": "", "ExecStartPost": "", "ExecStop": "", "ExecStopPost": "", "DropInPaths": "", "RuntimeMaxUSec": "infinity", "WatchdogUSec": "0", "Environment": ""}
	want["InaccessiblePaths"] = ""
	for k, val := range want {
		if p[k] != val {
			return nil, fmt.Errorf("System-task invocation %s changed", k)
		}
	}
	prefix := "{ path=" + v.Registration.Helper + " ; argv[]=" + v.Registration.Helper + " system-task-execute " + v.Unit + " ;"
	if !strings.HasPrefix(p["ExecStart"], prefix) || strings.Count(p["ExecStart"], "{ path=") != 1 {
		return nil, errors.New("System-task invocation executable changed")
	}
	if p["ControlGroup"] != "" && p["ControlGroup"] != "/system.slice/"+v.Unit {
		return nil, errors.New("System-task invocation moved execution domains")
	}
	return p, nil
}

func domainCount(unit string) (int, error) {
	if !validUnit(unit) {
		return 0, errors.New("Invalid system-task execution domain")
	}
	b, err := os.ReadFile(filepath.Join("/sys/fs/cgroup/system.slice", unit, "pids.current"))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || n < 0 {
		return 0, errors.New("Invalid system-task PID-controller observation")
	}
	return n, nil
}

func settled(ctx context.Context, j *journal) error {
	for _, v := range j.Invocations {
		p, err := inspectInvocation(ctx, v)
		if err != nil {
			return err
		}
		n, err := domainCount(v.Unit)
		if err != nil {
			return err
		}
		switch v.Status {
		case "failed":
			return fmt.Errorf("System task %s has unconfirmed effects; reconcile it before runtime maintenance", v.Unit)
		case "submitted":
			// Launch authority is still unconsumed. The fixed helper cannot fork work
			// until it owns the shared lease and rechecks durable runtime admission.
			if n > 0 {
				if err = liveGuard(v, p); err != nil {
					return err
				}
			}
		case "started":
			// Review can approve a running job: enqueueing maintenance closes new
			// admission, then exclusivity waits for its verified shared-lease owner.
			if err = liveGuard(v, p); err != nil {
				return fmt.Errorf("System task %s lost its execution guard: %w", v.Unit, err)
			}
		case "completed", "blocked", "interrupted":
			if n > 0 {
				if err = liveGuard(v, p); err != nil {
					return err
				}
				continue
			}
			if p["LoadState"] != "not-found" && (p["MainPID"] != "0" || p["ControlPID"] != "0" || p["ActiveState"] != "inactive" && p["ActiveState"] != "failed") {
				return errors.New("System-task completion still has an active execution domain")
			}
		}
	}
	return nil
}

func Dispatch(ctx context.Context, id int, identity string) (string, error) {
	unlock, err := gate(ctx, false)
	if err != nil {
		return "", err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return "", err
	}
	if err = retireAbsentSubmissions(ctx, &j); err != nil {
		return "", err
	}
	if err = compact(ctx, &j); err != nil {
		return "", err
	}
	r, ok := j.Entries[id]
	if !ok || !r.Enabled || r.Identity != identity {
		return "", errors.New("System-task registration was retired or replaced")
	}
	if err = verifyRegistration(ctx, r); err != nil {
		return "", err
	}
	if err = withDatabase(ctx, r, true); errors.Is(err, ErrMaintenance) {
		return ErrMaintenance.Error() + "; this tick was skipped.", nil
	} else if err != nil {
		return "", err
	}
	// A dead guard may leave natural descendants and an unknown outcome.
	// It must block other jobs as well as runtime maintenance; only a freshly
	// authenticated live shared-lease owner permits concurrent dispatch.
	if err = settled(ctx, &j); err != nil {
		return "", fmt.Errorf("An earlier scheduled job needs reconciliation before another launch: %w", err)
	}

	if len(j.Invocations) >= 4096 {
		return "", errors.New("System-task invocation journal is full")
	}
	p, err := qualifyContractProfile(ctx, r.Contract)
	if err != nil {
		return "", err
	}
	boot, err := bootID()
	if err != nil {
		return "", err
	}
	unit := "fluxo-system-task-" + uuid.NewString() + ".service"
	v := invocation{Unit: unit, Registration: r, Boot: boot, Status: "submitted", Profile: p}
	j.Invocations[unit] = v
	if err = save(j); err != nil {
		return "", err
	}
	logPath := fmt.Sprintf("/var/log/fluxo/cron-%d.log", id)
	if err = processlog.Prepare(logPath, "root"); err != nil {
		return "", err
	}
	args := []string{"--quiet", "--collect", "--unit=" + unit,
		"--property=Type=exec", "--property=ExitType=cgroup", "--property=Restart=no", "--property=User=root", "--property=Group=root",
		"--property=NoNewPrivileges=yes", "--property=ProtectControlGroups=yes", "--property=Delegate=no", "--property=WorkingDirectory=/root",
		"--property=TimeoutStopSec=infinity", "--property=SendSIGKILL=no", "--property=StandardOutput=append:" + logPath, "--property=StandardError=inherit",
	}

	args = append(args, "--", r.Helper, "system-task-execute", unit)
	_, err = syscmd.Run(ctx, 20*time.Second, "/usr/bin/systemd-run", args...)
	if err != nil {
		return "", fmt.Errorf("System-task submission is uncertain; its durable authority was retained: %w", err)
	}
	return "Scheduled system task started. Output is available in its cron log.", nil
}

func markBlocked(ctx context.Context, unit string, cause error) error {
	unlock, err := gate(ctx, false)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	v, ok := j.Invocations[unit]
	if !ok || v.Status != "submitted" {
		return errors.New("System-task launch authority changed")
	}
	v.Status = "blocked"
	v.Error = cause.Error()
	j.Invocations[unit] = v
	return save(j)
}

// Execute owns the shared lifecycle lease through full descendant drainage and
// declared external-effect verification. The authority is consumed before fork;
// an interrupted invocation is never replayed by a delayed helper or reboot.
func Execute(ctx context.Context, unit string) error {
	if !validUnit(unit) {
		return errors.New("Invalid system-task unit")
	}
	consumed := false
	defer func() {
		if !consumed {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = markBlocked(cleanup, unit, errors.New("Execution ended before consuming launch authority"))
		}
	}()
	admit, cancel := context.WithTimeout(ctx, 5*time.Second)
	release, err := packages.Lifecycle(admit, true)
	cancel()
	if err != nil {
		if e := markBlocked(ctx, unit, err); e != nil {
			return e
		}
		return ErrMaintenance
	}
	defer release()
	unlock, err := gate(ctx, false)
	if err != nil {
		return err
	}
	j, err := read()
	if err != nil {
		unlock()
		return err
	}
	v, ok := j.Invocations[unit]
	if !ok || v.Status != "submitted" {
		unlock()
		return errors.New("System-task launch authority is absent or already consumed")
	}
	r, current := j.Entries[v.Registration.Contract.ID]
	if !current || !r.Enabled || r.Identity != v.Registration.Identity {
		unlock()
		return errors.New("System-task registration was replaced before launch")
	}
	b, err := os.ReadFile("/proc/self/cgroup")
	if err != nil || strings.TrimSpace(string(b)) != "0::/system.slice/"+unit {
		unlock()
		return errors.New("System-task helper is outside its recorded execution domain")
	}
	boot, err := bootID()
	if err != nil || boot != v.Boot {
		unlock()
		return errors.New("System-task launch authority belongs to another boot")
	}
	if _, err = inspectInvocation(ctx, v); err != nil {
		unlock()
		return err
	}
	if err = verifyRegistration(ctx, r); err == nil {
		err = withDatabase(ctx, r, true)
	}
	if err != nil {
		v.Status = "blocked"
		v.Error = err.Error()
		j.Invocations[unit] = v
		saveErr := save(j)
		unlock()
		if saveErr != nil {
			return saveErr
		}
		return err
	}
	// No journal gate is held while waiting for package/native locks.
	unlock()
	if r.Contract.Kind != ApplicationJob {
		packageLease, e := packages.Acquire(ctx)
		if e != nil {
			return e
		}
		defer packageLease()
	}
	if r.Contract.Kind == ComposerUpdate {
		composerUnlock, e := composerLease()
		if e != nil {
			return e
		}
		defer composerUnlock()
	}
	native := func() {}
	if r.Contract.Kind != ApplicationJob {
		var e error
		native, e = packages.NativeDPKG(ctx)
		if e != nil {
			return e
		}
	}
	p, err := qualifyContractProfile(ctx, r.Contract)
	if err != nil {
		native()
		return err
	}
	old, _ := json.Marshal(v.Profile)
	fresh, _ := json.Marshal(p)
	if string(old) != string(fresh) {
		native()
		return errors.New("System-task execution profile changed after submission")
	}
	unlock, err = gate(ctx, false)
	if err != nil {
		native()
		return err
	}
	j, err = read()
	if err != nil {
		unlock()
		native()
		return err
	}
	latest, ok := j.Invocations[unit]
	if !ok || latest.Status != "submitted" {
		unlock()
		native()
		return errors.New("System-task launch authority changed while waiting")
	}
	if err = withDatabase(ctx, r, true); err != nil {
		unlock()
		native()
		return err
	}
	v.Status = "started"
	j.Invocations[unit] = v
	err = save(j)
	unlock()
	if err != nil {
		native()
		return err
	}
	consumed = true
	if r.Contract.Kind == PackageCleanup {
		native()
	} else {
		defer native()
	}
	runErr := runFixed(ctx, v)
	if err = waitDomain(ctx, unit); err != nil {
		return err
	}

	after, postErr := qualifyContractProfile(ctx, r.Contract)
	if r.Contract.Kind == ComposerUpdate && runErr == nil {
		// The authenticated baseline executed the upstream signed self-update;
		// the new PHAR is data until a later compatibility approval. Do not
		// execute it to discover its version or silently approve its future code.
		b, e := readRegular("/usr/local/bin/composer", 128<<20)
		if e != nil {
			postErr = e
		} else {
			after = newProfile(ComposerUpdate)
			postErr = qualifyComposer(ctx, &after, digest(b))
			p.Files["/usr/local/bin/composer"] = digest(b)
		}
	}
	if postErr == nil && !compatibleProfiles(p, after, r.Contract.Kind == ComposerUpdate && runErr == nil, false) {
		postErr = errors.New("System-task code, configuration or external domain changed during execution")
	}
	if err = waitDomain(ctx, unit); err != nil {
		return err
	}
	unlock, err = gate(ctx, false)
	if err != nil {
		return err
	}
	defer unlock()
	j, err = read()
	if err != nil {
		return err
	}
	latest, ok = j.Invocations[unit]
	if !ok || latest.Status != "started" {
		return errors.New("System-task completion authority changed")
	}
	v.ApplicationStarted = latest.ApplicationStarted
	if postErr != nil {
		v.Status = "failed"
		v.Error = postErr.Error()
	} else {
		v.Status = "completed"
		v.ResultProfile = &after
		if runErr != nil {
			v.Error = runErr.Error()
		}
	}
	j.Invocations[unit] = v
	if err = save(j); err != nil {
		return err
	}
	if postErr != nil {
		return postErr
	}
	return runErr
}

func runFixed(ctx context.Context, v invocation) error {
	// Long-running maintenance is not automatically killed by a timeout. The
	// caller remains journaled and retains the execution lease until natural exit.
	switch v.Registration.Contract.Kind {
	case ApplicationJob:
		return syscmd.RunRootApplicationHelper(v.Registration.Helper, v.Unit)
	case CertificateRenewal:
		return syscmd.RunMaintenance(ctx, "", os.Stdout, nil, "/usr/bin/certbot", "renew", "--quiet")
	case ComposerUpdate:
		return runComposer(ctx, v.Profile.External["php-interpreter"])
	case PackageCleanup:
		var output bytes.Buffer
		probe, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := syscmd.RunMaintenance(probe, "", &output, nil, "/usr/bin/apt-get", "--simulate", "--no-download", "autoremove", "-y")
		cancel()
		out := output.String()
		if err != nil {
			return err
		}
		if err = validateNoPackageChanges(out); err != nil {
			return err
		}
		// A resolver race cannot authorize package scripts: any real dpkg
		// invocation is rejected at its first Pre-Invoke hook.
		guard := v.Registration.Helper + " system-task-apt-refuse"
		if err = syscmd.RunMaintenance(ctx, "", os.Stdout, nil, "/usr/bin/apt-get", "-o", "DPkg::Pre-Invoke::="+guard, "autoremove", "-y"); err != nil {
			return err
		}
		return syscmd.RunMaintenance(ctx, "", os.Stdout, nil, "/usr/bin/apt-get", "autoclean")
	}
	return errors.New("Unknown fixed system-task execution")
}
