package phpupdate

import (
	"archive/tar"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/scheduler"
	"fluxo/internal/syscmd"
	"golang.org/x/sys/unix"
)

type unitSnapshot struct {
	Natural  bool             `json:"natural,omitempty"`
	Name     string           `json:"name"`
	Active   bool             `json:"active"`
	PIDs     map[int]string   `json:"pids"`
	CGroup   string           `json:"cgroup"`
	MainPID  int              `json:"main_pid"`
	Restarts string           `json:"restarts"`
	State    string           `json:"state"`
	SubState string           `json:"sub_state"`
	Octane   *octaneProcesses `json:"octane,omitempty"`
}
type runtimeSnapshot struct {
	Units              []unitSnapshot    `json:"units"`
	Sockets            []string          `json:"sockets"`
	DefaultCLI         string            `json:"default_cli"`
	Signalled          map[string]bool   `json:"signalled,omitempty"`
	SignalDelivery     map[string]string `json:"signal_delivery,omitempty"`
	SignalAttempts     map[string]string `json:"signal_attempts,omitempty"`
	ReloadApprovedHead string            `json:"reload_approved_head,omitempty"`
}

func processCurrent(pid int) error {
	path := fmt.Sprintf("/proc/%d/exe", pid)
	target, err := os.Readlink(path)
	if err != nil {
		return err
	}
	if strings.HasSuffix(target, " (deleted)") {
		return errors.New("process retains an old executable")
	}
	a, err := os.Stat(path)
	if err != nil {
		return err
	}
	b, err := os.Stat(target)
	if err != nil {
		return err
	}
	if !os.SameFile(a, b) {
		return errors.New("process executable is stale")
	}
	maps, err := os.ReadFile(fmt.Sprintf("/proc/%d/maps", pid))
	if err != nil {
		return err
	}
	if hasReplacedCodeMapping(string(maps)) {
		return errors.New("process retains replaced libraries")
	}
	return nil
}

func hasReplacedCodeMapping(maps string) bool {
	for _, line := range strings.Split(maps, "\n") {
		f := strings.Fields(line)
		if len(f) < 7 || f[len(f)-1] != "(deleted)" {
			continue
		}
		path := strings.Join(f[5:len(f)-1], " ")
		// OPcache legitimately retains anonymous /dev/zero, memfd and SYSV
		// shared-memory mappings. Only replaced executable/library files count.
		if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "/dev/") || strings.HasPrefix(path, "/memfd:") || strings.HasPrefix(path, "/SYSV") {
			continue
		}
		if strings.Contains(f[1], "x") || strings.HasSuffix(path, ".so") || strings.Contains(filepath.Base(path), ".so.") {
			return true
		}
	}
	return false
}
func captureRuntime(ctx context.Context, p Plan) (runtimeSnapshot, error) {
	r := runtimeSnapshot{Units: []unitSnapshot{}, Sockets: []string{}}
	r.DefaultCLI, _ = filepath.EvalSymlinks("/usr/bin/php")
	names := []string{"php" + p.Version + "-fpm.service"}
	for _, c := range p.Impact.Workers {
		for i := 1; i <= c.Instances; i++ {
			names = append(names, unitName(c.ID, i))
		}
	}

	known := map[int]bool{}
	for _, name := range names {
		u, err := unitState(ctx, name)
		if err != nil {
			return r, fmt.Errorf("%s: %w", runtimeLabel(p, name), err)
		}
		for _, consumer := range p.Impact.Workers {
			for i := 1; i <= consumer.Instances; i++ {
				if unitName(consumer.ID, i) == name && isNaturalConsumer(&consumer) {
					u.Natural = true
				}
				if unitName(consumer.ID, i) == name && consumer.Kind == "laravel_octane" && u.Active {
					u.Octane, err = inspectOctaneProcesses(ctx, p.Version, consumer, u)
					if err != nil {
						return r, err
					}
					if err = octaneHealth(ctx, p.Version, consumer, u); err != nil {
						return r, fmt.Errorf("Octane baseline health for %s: %w", consumerLabel(consumer, p.Impact.Sites), err)
					}
				}
			}
		}
		for _, consumer := range p.Impact.Workers {
			if unitName(consumer.ID, 1) == name && consumer.Kind == "laravel_nightwatch" && u.Active {
				if err = validateUnitProcesses(ctx, p.Version, &consumer, u); err != nil {
					return r, err
				}
				if err = verifyListenerOwnership(ctx, u, consumer.Port); err != nil {
					return r, fmt.Errorf("Nightwatch listener verification for %s: %w", consumerLabel(consumer, p.Impact.Sites), err)
				}
			}
		}
		r.Units = append(r.Units, u)
		for pid := range u.PIDs {
			known[pid] = true
		}
	}
	schedulerPID, unlockScheduler, err := scheduler.RuntimeLease(ctx, p.Version)
	if err != nil {
		return r, err
	}
	defer unlockScheduler()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return r, err
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		exe, err := os.Readlink("/proc/" + entry.Name() + "/exe")
		if err != nil {
			continue
		}
		base := strings.TrimSuffix(filepath.Base(exe), " (deleted)")
		if (base == "php"+p.Version || base == "php-fpm"+p.Version) && !known[pid] && !schedulerPID(pid) {
			return r, fmt.Errorf("PHP %s has an unmanaged running process (%d); finish it before updating", p.Version, pid)
		}
	}
	unlockScheduler()
	if r.Units[0].Active {
		r.Sockets, err = filepath.Glob("/run/php/php" + p.Version + "-fpm*.sock")
		if err != nil {
			return r, err
		}
		if len(r.Sockets) == 0 {
			return r, errors.New("Active FPM has no verifiable pool sockets")
		}
		if err = probeSockets(r.Sockets, p.Version); err != nil {
			return r, fmt.Errorf("FPM baseline health: %w", err)
		}
	}
	return r, nil
}

func (m *Manager) apply(ctx context.Context, o Operation) error {
	if err := admitApply(database.DB, o.Version, o.PlanID); err != nil {
		return err
	}
	p, err := GetPlan(o.PlanID)
	if err != nil {
		return err
	}
	if err := validateNativeChanges(p.Inventory, p.Impact); err != nil {
		return err
	}
	_ = progress(o.ID, "revalidating", "Validating the approved PHP update and affected services")
	var n int
	if err = database.DB.QueryRow(`SELECT COUNT(*) FROM deployments d JOIN sites s ON s.id=d.site_id WHERE s.php_version=? AND d.status IN ('pending','running')`, p.Version).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return errors.New("Wait for affected deployments to finish before updating PHP")
	}
	if err = database.DB.QueryRow(`SELECT COUNT(*) FROM commands c JOIN sites s ON s.id=c.site_id WHERE s.php_version=? AND c.status='running'`, p.Version).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return errors.New("Wait for affected site commands to finish before updating PHP")
	}
	preCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	w := &bridgeWriter{id: o.ID}
	for _, note := range p.Impact.Notes {
		_, _ = w.Write([]byte(note + "\n"))
	}
	if err = syscmd.RunMaintenance(preCtx, "", w, []string{"DEBIAN_FRONTEND=noninteractive"}, "/usr/bin/apt-get", "-o", "APT::Update::Error-Mode=any", "-o", "DPkg::Lock::Timeout=120", "update"); err != nil {
		return errors.New("Repository refresh failed; no PHP packages were changed")
	}
	invs, err := bridge(preCtx, o.ID, map[string]any{"action": "inspect"})
	if err != nil {
		return err
	}
	matched := false
	for _, inv := range invs {
		if inv.Version == p.Version && digest(inv) == digest(p.Inventory) {
			matched = true
		}
	}
	if !matched {
		return ErrPlanChanged
	}
	impact, err := inspectImpact(preCtx, p.Version)
	addPreservedExtensionNotes(&impact, p.Inventory)
	if err != nil {
		return err
	}
	if digest(impact) != digest(p.Impact) {
		return ErrReviewChanged
	}
	audit, err := syscmd.Run(preCtx, 15*time.Second, "/usr/bin/dpkg", "--audit")
	if err != nil || strings.TrimSpace(audit) != "" {
		return errors.New("The package database requires operator repair")
	}
	var st syscall.Statfs_t
	for _, path := range []string{"/", "/var/cache/apt/archives", "/etc/php/" + p.Version} {
		if err = syscall.Statfs(path, &st); err != nil {
			return err
		}
		var needed int64 = 256 * 1024 * 1024
		for _, pkg := range p.Inventory.Changes {
			needed += pkg.Size * 3
		}
		if uint64(st.Bavail)*uint64(st.Bsize) < uint64(needed) || st.Ffree < 1024 {
			return errors.New("Insufficient disk space or inodes for a safe PHP update")
		}
	}
	if _, err = syscmd.Run(preCtx, 30*time.Second, "/usr/sbin/php-fpm"+p.Version, "-t"); err != nil {
		return fmt.Errorf("FPM configuration check failed: %w", err)
	}
	before, err := captureRuntime(preCtx, p)
	if err != nil {
		return err
	}
	if err = m.backupConfiguration(o.ID, p.Version); err != nil {
		return err
	}

	var acknowledge bool
	if err = database.DB.QueryRow("SELECT acknowledge_no_rollback FROM php_update_approvals WHERE operation_id=?", o.ID).Scan(&acknowledge); err != nil {
		return err
	}
	_ = progress(o.ID, "preparing_recovery", "Retaining verified PHP packages before stopping affected consumers")
	archives, err := m.prepareArchives(preCtx, o, p, acknowledge)
	if err != nil {
		return err
	}
	g := recoveryGeneration{SchedulerManaged: true, Schedulers: p.Impact.Schedulers, ID: o.ID, Version: p.Version, PlanID: p.ID, Head: o.ID, State: "prepared", Pending: true, Goal: "after", Archives: archives, Original: before, Attempt: before, AcknowledgeNoRollback: acknowledge}
	if err = validateConsumerProtocols(p, g); err != nil {
		return err
	}
	if err = createGeneration(g); err != nil {
		return err
	}
	if err = scheduler.Drain(ctx, g.Version, g.ID, g.Schedulers); err != nil {
		return err
	}
	if err = m.drainConsumers(ctx, &g); err != nil {
		return err
	}
	if err = m.packageTransaction(ctx, o, &g); err != nil {
		return err
	}
	return m.activateGeneration(ctx, o, p, &g)
}

func (m *Manager) activate(ctx context.Context, o Operation, p Plan, before runtimeSnapshot, generation ...*recoveryGeneration) error {
	var err error
	_ = progress(o.ID, "activating", "Refreshing affected PHP services and workers")
	if _, err = syscmd.Run(ctx, 30*time.Second, "/usr/sbin/php-fpm"+p.Version, "-t"); err != nil {
		_ = finish(o.ID, "health_failed", "Packages updated, but FPM configuration validation failed; repair the configuration before restarting.")
		return err
	}
	if before.Signalled == nil {
		before.Signalled = map[string]bool{}
	}
	if before.SignalDelivery == nil {
		before.SignalDelivery = map[string]string{}
	}
	if before.SignalAttempts == nil {
		before.SignalAttempts = map[string]string{}
	}
	for _, u := range before.Units {
		if u.Natural {
			continue // Unknown Artisan commands never receive an inferred signal.
		}
		state, e := unitState(ctx, u.Name)
		if e != nil {
			return e
		}
		if !u.Active {
			if state.Active {
				return fmt.Errorf("%s did not preserve its stopped state; inspect it manually", runtimeLabel(p, u.Name))
			}
			continue
		}
		fpm := u.Name == "php"+p.Version+"-fpm.service"
		if runtimeAdopted(u, state, fpm, processCurrent, processKey) {
			continue
		}
		// FPM reexec preserves PID/start time. Current executable and mappings
		// prove the master already adopted the packages; observe old children only.
		if fpm && processCurrent(state.MainPID) == nil {
			continue
		}
		retry := fpm && o.Kind == "repair" && retryUnconfirmedFPM(before.ReloadApprovedHead == o.ID, u, state, before.SignalDelivery[u.Name], before.SignalAttempts[u.Name], o.ID, processCurrent)
		if before.Signalled[u.Name] && !retry || !sameOriginalMain(u, state) {
			continue
		}
		signal := unix.SIGTERM
		if fpm {
			signal = unix.SIGUSR2
		}
		before.Signalled[u.Name] = true
		before.SignalDelivery[u.Name] = "intent"
		before.SignalAttempts[u.Name] = o.ID
		if e = m.saveActivation(o, before, generation); e != nil {
			return e
		}
		e = signalRecordedProcess(ctx, state, state.MainPID, signal)
		if e != nil {
			before.SignalDelivery[u.Name] = "failed"
			_ = m.saveActivation(o, before, generation)
			_ = finish(o.ID, "restart_required", "Could not deliver the graceful service signal for "+u.Name+". Review Repair to retry safely: "+e.Error())
			return e
		}
		before.SignalDelivery[u.Name] = "sent"
		if e = m.saveActivation(o, before, generation); e != nil {
			return e
		}
	}
	_ = progress(o.ID, "verifying", "Waiting for old PHP processes to drain and verifying stable replacements")
	deadline := time.Now().Add(60 * time.Second)
	stable := map[string]string{}
	stableSince := map[string]time.Time{}
	for {
		ready := true
		for _, u := range before.Units {
			state, e := unitState(ctx, u.Name)
			if !u.Active {
				if e != nil || state.Active {
					return fmt.Errorf("%s did not preserve its stopped state", runtimeLabel(p, u.Name))
				}
				continue
			}
			if u.Natural {
				if len(generation) != 1 {
					return errors.New("Custom Artisan activation requires its durable maintenance generation")
				}
				ok, completed, err := naturalActivationReady(ctx, generation[0], u)
				if err != nil {
					return err
				}
				if completed {
					continue
				}
				if !ok {
					ready = false
					continue
				}
			}
			if e != nil || !runtimeAdopted(u, state, u.Name == "php"+p.Version+"-fpm.service", processCurrent, processKey) {
				delete(stable, u.Name)
				delete(stableSince, u.Name)
				ready = false
				continue
			}
			identity := fmt.Sprintf("%d:%s:%s", state.MainPID, state.PIDs[state.MainPID], state.Restarts)
			if stable[u.Name] != identity {
				stable[u.Name] = identity
				stableSince[u.Name] = time.Now()
			}
			if time.Since(stableSince[u.Name]) < 5*time.Second {
				ready = false
			}
		}
		if ready {
			break
		}
		if time.Now().After(deadline) {
			_ = finish(o.ID, "restart_required", "PHP packages updated, but some processes have not refreshed or remained healthy. Fluxo did not force-stop them; configured job and native runtime timeouts still apply. Review affected services before retrying verification.")
			return nil
		}
		time.Sleep(time.Second)
	}
	if err = probeSockets(before.Sockets, p.Version); err != nil {
		_ = finish(o.ID, "health_failed", "Packages updated, but FPM request verification failed: "+err.Error())
		return err
	}
	if current, _ := filepath.EvalSymlinks("/usr/bin/php"); current != before.DefaultCLI {
		_ = finish(o.ID, "health_failed", "PHP packages updated, but the system CLI default changed unexpectedly. Restore the intended default and retry service verification.")
		return nil
	}
	if _, err = captureRuntime(ctx, p); err != nil {
		_ = finish(o.ID, "restart_required", "PHP packages updated; runtime verification needs attention: "+err.Error())
		return nil
	}
	for _, c := range p.Impact.Workers {
		if c.Kind != "laravel_octane" || !consumerWasActive(c, before) {
			continue
		}
		state, e := unitState(ctx, unitName(c.ID, 1))
		if e != nil {
			return e
		}
		if e = octaneHealth(ctx, p.Version, c, state); e != nil {
			_ = finish(o.ID, "health_failed", "Octane process/HTTP verification failed for "+c.Name+": "+e.Error())
			return e
		}
	}

	// Clear availability only after package verification; runtime failures above
	// remain durable independently of the next daily package check.
	if len(generation) == 1 && generation[0].Goal == "before" {
		if err = invalidateAvailability(p.Version); err != nil {
			return err
		}
	} else if err = resolveAvailability(p.Version, p.ID); err != nil {
		return err
	}
	if len(generation) == 1 {
		g := generation[0]
		g.State = "releasing"
		g.VerifiedGoal = g.Goal
		if err = saveGeneration(*g); err != nil {
			return err
		}
		if err = m.releaseBarriers(ctx, *g); err != nil {
			return err
		}
		if g.SchedulerManaged {
			if err = scheduler.Release(ctx, g.Version, g.ID, g.Schedulers); err != nil {
				return err
			}
		}
		g.Pending = false
		g.State = "succeeded"
		if g.Goal == "before" {
			g.State = "rolled_back"
		}
		if err = saveGeneration(*g); err != nil {
			return err
		}
	}
	message := "PHP " + p.Version + " packages and affected running services were updated and verified"
	if o.Kind == "repair" {
		message = "PHP " + p.Version + " repair completed; recorded package revisions and affected services verified"
	}
	if o.Kind == "rollback" {
		message = "PHP " + p.Version + " previous package revisions restored and affected services verified. Application files, data and jobs were not rolled back"
	}
	return finish(o.ID, "succeeded", message)
}

// runtimeAdopted is read-only; successful recovery must not restart healthy units.
func runtimeAdopted(old, current unitSnapshot, fpm bool, check func(int) error, key func(int) (string, error)) bool {
	if !current.Active || current.MainPID == 0 || len(current.PIDs) == 0 {
		return false
	}
	for pid, identity := range current.PIDs {
		if check(pid) != nil {
			return false
		}
		if !fpm && old.PIDs[pid] == identity {
			return false
		}
	}
	for pid, identity := range old.PIDs {
		if now, err := key(pid); err == nil && now == identity {
			if !fpm || check(pid) != nil {
				return false
			}
		}
	}
	return true
}
func sameOriginalMain(old, current unitSnapshot) bool {
	return old.MainPID != 0 && old.MainPID == current.MainPID && old.PIDs[old.MainPID] == current.PIDs[current.MainPID]
}
func consumerWasActive(c Consumer, before runtimeSnapshot) bool {
	for _, u := range before.Units {
		for i := 1; i <= c.Instances; i++ {
			if u.Name == unitName(c.ID, i) && u.Active {
				return true
			}
		}
	}
	return false
}
func recoveryCompatible(old, current Impact) error {
	if len(current.BlockedReasons) > 0 {
		return fmt.Errorf("Recovery requires operator attention: %s", strings.Join(current.BlockedReasons, "; "))
	}
	// Allow validated ini repairs, but require the approved consumers and their
	// effective service definitions to retain the same meaning.
	if (len(old.Schedulers) != len(current.Schedulers) || len(old.Schedulers) > 0 && digest(old.Schedulers) != digest(current.Schedulers)) || digest(old.Sites) != digest(current.Sites) || digest(old.Workers) != digest(current.Workers) || old.UnitHash == "" || old.UnitHash != current.UnitHash || old.PHPMyAdmin != current.PHPMyAdmin {
		return errors.New("Affected applications or service definitions changed since this update. Reconcile them manually before verifying; no services were signalled")
	}
	return nil
}

// Verification retries never run APT installation. They first prove that all
// approved revisions are installed, then refresh/verify the recorded services.
func (m *Manager) verify(ctx context.Context, o Operation) error {
	p, err := GetPlan(o.PlanID)
	if err != nil {
		return err
	}
	var before runtimeSnapshot
	if err = json.Unmarshal(o.Result, &before); err != nil || len(before.Units) == 0 {
		return errors.New("This operation has no recoverable runtime snapshot; inspect the server manually")
	}
	audit, err := syscmd.Run(ctx, 15*time.Second, "/usr/bin/dpkg", "--audit")
	if err != nil || strings.TrimSpace(audit) != "" {
		return errors.New("Package configuration requires operator repair before runtime verification; no services were signalled")
	}
	inventories, err := bridge(ctx, o.ID, map[string]any{"action": "inspect"})
	if err != nil {
		return err
	}
	verified := false
	for _, inv := range inventories {
		if inv.Version != p.Version {
			continue
		}
		verified = true
		if err := verifyInstalledInventory(p.Inventory, inv); err != nil {
			return err
		}
	}
	if !verified {
		return errors.New("The updated PHP branch is no longer installed")
	}
	impact, err := inspectImpact(ctx, p.Version)
	if err != nil {
		return err
	}
	if err = recoveryCompatible(p.Impact, impact); err != nil {
		return err
	}
	return m.activate(ctx, o, p, before)
}

func verifyInstalledInventory(expectedInventory, actualInventory Inventory) error {
	for _, expected := range expectedInventory.Packages {
		target := expected.Installed
		for _, change := range expectedInventory.Changes {
			if change.Name == expected.Name && change.Architecture == expected.Architecture {
				target = change.Candidate
			}
		}
		found := false
		for _, actual := range actualInventory.Packages {
			if actual.Name == expected.Name && actual.Architecture == expected.Architecture && actual.Installed == target && actual.Automatic == expected.Automatic && actual.Configured {
				found = true
			}
		}
		if !found {
			return errors.New("Package revisions, automatic marks or configuration still require operator repair; no package installation was retried")
		}
	}
	return nil
}

func preparePolicyTarget() (func(), error) {
	path := "/usr/sbin/policy-rc.d"
	if _, err := os.Lstat(path); err == nil {
		return func() {}, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	f.Close()
	if err != nil {
		return nil, err
	}
	return func() {
		if current, e := os.Lstat(path); e == nil && os.SameFile(info, current) {
			_ = os.Remove(path)
		}
	}, nil
}

// RunTransaction is called only by the supervisor-owned worker in unshare's
// private mount namespace. No application-supplied command is executed as root.
func (m *Manager) RunTransaction(id string) error {
	o, err := GetOperation(id)
	if err != nil {
		return err
	}
	if (o.Kind != "apply" && o.Kind != "repair" && o.Kind != "rollback") || o.Status != "running" || o.Checkpoint != "package_transaction" {
		return errors.New("invalid transaction state")
	}
	g, err := generationForOperation(id)
	if err != nil {
		return err
	}
	if g.Head != id || g.State != "package_transaction" {
		return errors.New("Invalid retained transaction generation")
	}
	if !g.SchedulerManaged {
		return errors.New("Scheduler coordination must be established through Repair before package execution")
	}
	if err = scheduler.AssertDrained(context.Background(), g.Version, g.ID, g.Schedulers); err != nil {
		return err
	}
	var a, b unix.Stat_t
	if err = unix.Stat("/proc/self/ns/mnt", &a); err != nil {
		return err
	}
	if err = unix.Stat("/proc/1/ns/mnt", &b); err != nil {
		return err
	}
	if a.Ino == b.Ino {
		return errors.New("Package execution requires a private mount namespace")
	}
	policyDir := filepath.Join(m.DataDir, "php-updates")
	if err = os.MkdirAll(policyDir, 0700); err != nil {
		return err
	}
	if err = rootOwned(policyDir); err != nil {
		return err
	}
	deny := filepath.Join(policyDir, "deny-service-start")
	if err = os.WriteFile(deny, []byte("#!/bin/sh\nexit 101\n"), 0700); err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err = unix.Mount(deny, "/usr/sbin/policy-rc.d", "", unix.MS_BIND, ""); err != nil {
		return err
	}
	for _, socket := range []string{"/run/systemd/private", "/run/dbus/system_bus_socket"} {
		if _, e := os.Stat(socket); e == nil {
			if e = unix.Mount("/dev/null", socket, "", unix.MS_BIND, ""); e != nil {
				return e
			}
		}
	}
	directory, e := m.archiveDirectory(g.ID)
	if e != nil {
		return e
	}
	ob, e := m.observePackages(context.Background(), id, g.Version)
	if e != nil {
		return e
	}
	if !recoveryPolicyAuthorized(g, ob.PolicyHash) {
		return errors.New("Repository policy changed outside an approved migration")
	}
	_, err = bridgeRaw(context.Background(), id, map[string]any{"action": "recover", "directory": directory, "manifest": g.Archives, "goal": g.Goal, "authorized_policy_hash": ob.PolicyHash})
	return err
}

func (m *Manager) backupConfiguration(id, branch string) error {
	dir := filepath.Join(m.DataDir, "php-updates", id)
	if err := ensureDurableRecoveryDirectory(dir); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "configuration.tar"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(f)
	var total int64
	count := 0
	err = filepath.WalkDir("/etc/php/"+branch, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, e = os.Readlink(path)
			if e != nil {
				return e
			}
		}
		h, e := tar.FileInfoHeader(info, link)
		if e != nil {
			return e
		}
		h.Name = strings.TrimPrefix(path, "/")
		if e = tw.WriteHeader(h); e != nil {
			return e
		}
		if info.Mode().IsRegular() {
			total += info.Size()
			count++
			if total > 16*1024*1024 || count > 1024 {
				return errors.New("PHP configuration snapshot exceeds its safe size bound")
			}
			file, e := os.Open(path)
			if e != nil {
				return e
			}
			_, e = io.Copy(tw, file)
			file.Close()
			return e
		}
		return nil
	})
	closeErr := tw.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr = f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = syncDirectory(dir)
	}
	return err
}

func probeSockets(sockets []string, branch string) error {
	if len(sockets) == 0 {
		return nil
	}
	dir := "/run/fluxo-php-health"
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := rootOwned(dir); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "probe-*.php")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0644); err != nil {
		f.Close()
		return err
	}
	_, err = f.WriteString(`<?php echo 'fluxo-php-health:'.PHP_MAJOR_VERSION.'.'.PHP_MINOR_VERSION;`)
	f.Close()
	if err != nil {
		return err
	}
	for _, socket := range sockets {
		if err = fastCGIProbe(socket, f.Name(), branch); err != nil {
			return err
		}
	}
	return nil
}
func fastCGIProbe(socket, script, branch string) error {
	c, err := net.DialTimeout("unix", socket, 5*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	record := func(kind byte, data []byte) error {
		header := []byte{1, kind, 0, 1, 0, 0, 0, 0}
		binary.BigEndian.PutUint16(header[4:6], uint16(len(data)))
		if _, e := c.Write(append(header, data...)); e != nil {
			return e
		}
		return nil
	}
	if err = record(1, []byte{0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return err
	}
	var params []byte
	for _, kv := range [][2]string{{"SCRIPT_FILENAME", script}, {"SCRIPT_NAME", "/fluxo-php-health.php"}, {"REQUEST_METHOD", "GET"}, {"SERVER_PROTOCOL", "HTTP/1.1"}, {"CONTENT_LENGTH", "0"}} {
		for _, n := range []int{len(kv[0]), len(kv[1])} {
			if n < 128 {
				params = append(params, byte(n))
			} else {
				var b [4]byte
				binary.BigEndian.PutUint32(b[:], uint32(n)|0x80000000)
				params = append(params, b[:]...)
			}
		}
		params = append(params, []byte(kv[0])...)
		params = append(params, []byte(kv[1])...)
	}
	if err = record(4, params); err != nil {
		return err
	}
	if err = record(4, nil); err != nil {
		return err
	}
	if err = record(5, nil); err != nil {
		return err
	}
	var output strings.Builder
	for i := 0; i < 100; i++ {
		var h [8]byte
		if _, err = io.ReadFull(c, h[:]); err != nil {
			return err
		}
		n := int(binary.BigEndian.Uint16(h[4:6]))
		data := make([]byte, n+int(h[6]))
		if _, err = io.ReadFull(c, data); err != nil {
			return err
		}
		if h[1] == 6 {
			output.Write(data[:n])
		}
		if h[1] == 3 {
			if strings.Contains(output.String(), "fluxo-php-health:"+branch) {
				return nil
			}
			return errors.New("FPM returned an unexpected health response")
		}
	}
	return errors.New("FPM health response exceeded its limit")
}
