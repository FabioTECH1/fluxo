package nodeupdate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/daemon"
	"fluxo/internal/services/nodetoolchain"
	"fluxo/internal/services/phpupdate"
	"fluxo/internal/services/scheduler"
	"fluxo/internal/services/site"
	"fluxo/internal/syscmd"
)

func properties(raw string) map[string]string {
	p := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			p[k] = v
		}
	}
	return p
}
func bootID() (string, error) {
	b, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return strings.TrimSpace(string(b)), e
}
func processKey(pid int) (string, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	i := strings.LastIndex(string(b), ")")
	if i < 0 {
		return "", errors.New("Invalid process identity")
	}
	f := strings.Fields(string(b)[i+1:])
	if len(f) < 20 {
		return "", errors.New("Incomplete process identity")
	}
	boot, err := bootID()
	return boot + ":" + f[19], err
}
func groupPIDs(group string) (map[int]string, error) {
	result := map[int]string{}
	if group == "" {
		return result, nil
	}
	if filepath.Clean(group) != group || !strings.HasPrefix(group, "/system.slice/") || strings.Contains(group, "..") {
		return nil, errors.New("Unsupported application cgroup")
	}
	err := filepath.WalkDir(filepath.Join("/sys/fs/cgroup", group), func(p string, d os.DirEntry, e error) error {
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		if d.IsDir() || d.Name() != "cgroup.procs" {
			return nil
		}
		b, e := os.ReadFile(p)
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		for _, s := range strings.Fields(string(b)) {
			pid, e := strconv.Atoi(s)
			if e != nil || pid <= 0 {
				return errors.New("Invalid application process")
			}
			key, e := processKey(pid)
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return e
			}
			result[pid] = key
		}
		return nil
	})
	return result, err
}
func inspectUnit(ctx context.Context, u Unit, generationID string) (Unit, error) {
	return observeUnit(ctx, u, generationID, false)
}

// Recovery authenticates the same service contract as a healthy apply, but
// does not require a failed or draining service to repair itself first.
func observeUnit(ctx context.Context, u Unit, generationID string, recovery bool) (Unit, error) {
	service := filepath.Join("/etc/systemd/system", u.Name)
	if err := trustedPath(service); err != nil {
		return u, err
	}
	raw, err := os.ReadFile(service)
	if err != nil {
		return u, err
	}
	expected := daemon.NodeServiceDefinition(u.ID, u.Command, u.Directory)
	if string(raw) != expected {
		return u, fmt.Errorf("Custom service definition for %s requires manual maintenance", u.Domain)
	}
	u.Definition = digest(string(raw))
	out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", u.Name, "--property=LoadState,ActiveState,SubState,MainPID,ControlGroup,UnitFileState,DropInPaths,User,Group,Type,WorkingDirectory,Environment,EnvironmentFiles,ExecSearchPath,ExecStartPre,ExecStartPost,ExecStop,ExecStopPost,RootDirectory,RootImage,KillMode,KillSignal,SendSIGKILL,TimeoutStopUSec,Restart")
	if err != nil {
		return u, err
	}
	p := properties(out)
	if p["LoadState"] != "loaded" || p["User"] != "fluxo" || p["Group"] != "" || p["Type"] != "simple" || p["WorkingDirectory"] != u.Directory {
		return u, errors.New("Unsupported effective Node.js service")
	}
	for _, key := range []string{"Environment", "EnvironmentFiles", "ExecSearchPath", "ExecStartPre", "ExecStartPost", "ExecStop", "ExecStopPost", "RootDirectory", "RootImage"} {
		if p[key] != "" {
			return u, fmt.Errorf("Custom %s on %s requires manual maintenance", key, u.Domain)
		}
	}
	drop := p["DropInPaths"]
	if generationID == "" && drop != "" {
		return u, errors.New("Custom Node.js service drop-ins require manual maintenance")
	}
	if generationID != "" {
		if drop != "" && drop != barrierPath(u) {
			return u, errors.New("Node.js recovery service drop-ins changed")
		}
		var owner string
		err = database.DB.QueryRow("SELECT generation_id FROM node_update_barriers WHERE unit=?", u.Name).Scan(&owner)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return u, err
		}
		if owner != "" && owner != generationID {
			return u, errors.New("Node.js start barrier belongs to another generation")
		}
		_, statErr := os.Lstat(barrierPath(u))
		if statErr == nil || drop != "" {
			if owner != generationID {
				return u, errors.New("Node.js start barrier has no matching journal")
			}
			if err = verifyBarrier(u, generationID); err != nil {
				return u, err
			}
		} else if !os.IsNotExist(statErr) {
			return u, statErr
		}
	}
	group := "/system.slice/" + u.Name
	if p["ControlGroup"] != "" && p["ControlGroup"] != group {
		return u, errors.New("Unexpected Node.js service cgroup")
	}
	pids, err := groupPIDs(group)
	if err != nil {
		return u, err
	}
	return observeUnitState(u, p, pids, recovery)
}

func observeUnitState(u Unit, p map[string]string, pids map[int]string, recovery bool) (Unit, error) {
	u.State, u.SubState = p["ActiveState"], p["SubState"]
	if recovery {
		switch u.State {
		case "active", "inactive", "failed", "activating", "deactivating":
		default:
			return u, errors.New("Unsupported Node.js recovery service state")
		}
	} else if u.State != "active" && u.State != "inactive" {
		return u, fmt.Errorf("%s is failed or changing state; stop or repair it before review", u.Domain)
	}
	if !recovery && u.State == "active" && u.SubState != "running" {
		return u, errors.New("Node.js service is not stably running")
	}
	u.Active = u.State == "active"
	u.Enabled = p["UnitFileState"]
	if u.Enabled != "enabled" && u.Enabled != "disabled" && u.Enabled != "static" {
		return u, errors.New("Masked or unsupported Node.js service state")
	}
	var err error
	u.MainPID, err = strconv.Atoi(p["MainPID"])
	if err != nil {
		return u, err
	}
	u.PIDs = pids
	if u.MainPID < 0 || u.MainPID != 0 && u.PIDs[u.MainPID] == "" || !recovery && u.Active && u.MainPID == 0 {
		return u, errors.New("Node.js main process cannot be identified")
	}
	if !recovery && !u.Active && len(u.PIDs) > 0 {
		return u, errors.New("Node.js service still has draining processes")
	}
	return u, nil
}
func inspectImpact(ctx context.Context, nodeRoot, generationID string) (Impact, error) {
	return inspectConsumers(ctx, []string{nodeRoot}, generationID, nil, observeUnit)
}

func inspectRecoveryImpact(ctx context.Context, g Generation) (Impact, error) {
	// Both retained trees can have live processes after an interrupted switch.
	// Never accept a process merely because it has a plausible executable name.
	roots := []string{}
	for _, r := range []nodetoolchain.Release{g.Plan.Before.Release, g.Plan.After} {
		root := filepath.Join(nodetoolchain.NodeRoot(), "v"+r.Version)
		hash, err := nodetoolchain.TreeDigest(root)
		if err != nil || hash != r.TreeSHA256 {
			return Impact{}, errors.New("Retained Node.js release needs manual repair")
		}
		roots = append(roots, root)
	}
	original := g.executionImpact()
	impact, err := inspectConsumers(ctx, roots, g.ID, &original, observeUnit)
	if g.PHPState != nil {
		impact.PHPResumes = g.PHPState.Resumes
	}
	return impact, err
}

func inspectConsumers(ctx context.Context, roots []string, generationID string, original *Impact, observe func(context.Context, Unit, string, bool) (Unit, error)) (Impact, error) {
	impact := Impact{PHPPolicy: 1, Sites: []Site{}, Units: []Unit{}, BlockedReasons: []string{}}
	rows, err := database.DB.Query(`SELECT id,domain,COALESCE(node_mode,''),COALESCE(app_type,''),COALESCE(deletion_status,'') FROM sites ORDER BY id`)
	if err != nil {
		return impact, err
	}
	for rows.Next() {
		var s Site
		var deleting string
		if err = rows.Scan(&s.ID, &s.Domain, &s.Mode, &s.AppType, &deleting); err != nil {
			rows.Close()
			return impact, err
		}
		if deleting != "" {
			impact.BlockedReasons = append(impact.BlockedReasons, "Wait for site deletion to finish")
		}
		impact.Sites = append(impact.Sites, s)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return impact, err
	}
	for _, s := range impact.Sites {
		if s.AppType != "node" || site.NormalizeNodeMode(s.Mode) != "server" {
			continue
		}
		var u Unit
		var kind, preset, pm, storedStart, strategy, base, user, signal string
		var instances, start, stop int
		err = database.DB.QueryRow(`SELECT d.id,d.command,d.directory,d.user,d.instances,d.start_seconds,d.stop_seconds,d.stop_signal,d.managed_kind,s.path,s.deployment_strategy,s.node_preset,s.package_manager,s.start_command,s.app_port FROM daemons d JOIN sites s ON s.id=d.site_id WHERE s.id=? AND d.name='Node.js'`, s.ID).Scan(&u.ID, &u.Command, &u.Directory, &user, &instances, &start, &stop, &signal, &kind, &base, &strategy, &preset, &pm, &storedStart, &u.Port)
		if err != nil {
			impact.BlockedReasons = append(impact.BlockedReasons, "Managed Node.js application is missing for "+s.Domain)
			continue
		}
		u.SiteID, u.Domain, u.Name = s.ID, s.Domain, fmt.Sprintf("fluxo-daemon-%d.service", u.ID)
		expected := site.RenderNodeStartCommand(site.DefaultNodeStartCommand(preset, pm), u.Port)
		directory := base
		if strategy == "zero-downtime" {
			directory = filepath.Join(base, "current")
		}
		if kind != "node_app" || instances != 1 || start != 1 || stop != 15 || signal != "SIGTERM" || user != "fluxo" || u.Command != expected || u.Directory != directory || storedStart != "" && storedStart != site.DefaultNodeStartCommand(preset, pm) {
			impact.BlockedReasons = append(impact.BlockedReasons, "Custom Node.js command or daemon for "+s.Domain+" requires manual maintenance")
			continue
		}
		u, err = observe(ctx, u, generationID, original != nil)
		if err != nil {
			impact.BlockedReasons = append(impact.BlockedReasons, s.Domain+": "+err.Error())
			continue
		}
		// Even a stopped Bun launcher needs a receipt: it may run after reboot
		// or during recovery. A package-manager label never exempts its cgroup.
		if pm == "bun" {
			u.Bun, err = inspectManagedBun(ctx)
			if err != nil {
				impact.BlockedReasons = append(impact.BlockedReasons, s.Domain+": "+err.Error())
				continue
			}
		}
		u, err = inspectConsumer(ctx, u, roots, original)
		if err != nil {
			impact.BlockedReasons = append(impact.BlockedReasons, s.Domain+": "+err.Error())
			continue
		}
		impact.Units = append(impact.Units, u)
	}
	reasons, err := inspectCronInventory(ctx, verifySchedulerAdmission)
	impact.BlockedReasons = append(impact.BlockedReasons, reasons...)
	if err != nil {
		return impact, err
	}
	impact.PHP, reasons, err = inspectPHPConsumers(ctx, roots, generationID, impact.Units, original)
	impact.BlockedReasons = append(impact.BlockedReasons, reasons...)
	if err != nil {
		return impact, err
	}
	reasons, err = inspectDaemonInventory(ctx, impact.Units, impact.PHP)
	impact.BlockedReasons = append(impact.BlockedReasons, reasons...)
	if err != nil {
		return impact, err
	}
	known := map[int]bool{}
	for _, u := range impact.Units {
		for pid := range u.PIDs {
			known[pid] = true
		}
	}
	for _, c := range impact.PHP {
		for _, u := range c.Units {
			for pid := range u.Runtime.PIDs {
				known[pid] = true
			}
		}
	}
	processes, err := os.ReadDir("/proc")
	if err != nil {
		return impact, err
	}
	for _, p := range processes {
		pid, e := strconv.Atoi(p.Name())
		if e != nil {
			continue
		}
		exe, e := os.Readlink(filepath.Join("/proc", p.Name(), "exe"))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return impact, e
		}
		base := filepath.Base(strings.TrimSuffix(exe, " (deleted)"))
		if (base == "node" || base == "nodejs") && !known[pid] {
			impact.BlockedReasons = append(impact.BlockedReasons, fmt.Sprintf("Node.js process %d is outside a supported managed application; stop it before review", pid))
		}
	}
	return impact, nil
}

// A management label does not establish a runtime or an execution-time lease.
// Include global/orphan/duplicate records too: only a positively inspected
// supported Node unit can be excluded from this fail-closed inventory.
func inspectDaemonInventory(ctx context.Context, supported []Unit, phpSets ...[]phpupdate.MaintenanceConsumer) ([]string, error) {
	phpKnown := map[int]phpupdate.MaintenanceConsumer{}
	for _, set := range phpSets {
		for _, c := range set {
			phpKnown[c.Consumer.ID] = c
		}
	}
	known := map[int]Unit{}
	seen := map[int]bool{}
	for _, u := range supported {
		known[u.ID] = u
	}
	reasons := []string{}
	rows, err := database.DB.QueryContext(ctx, `SELECT id,site_id,COALESCE(name,''),COALESCE(managed_kind,'') FROM daemons ORDER BY id`)
	if err != nil {
		return reasons, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, siteID int
		var name, kind string
		if err = rows.Scan(&id, &siteID, &name, &kind); err != nil {
			return reasons, err
		}
		if u, ok := known[id]; ok && id > 0 && siteID > 0 && u.Name == fmt.Sprintf("fluxo-daemon-%d.service", id) && kind == "node_app" && name == "Node.js" && siteID == u.SiteID {
			seen[id] = true
			continue
		}
		if c, ok := phpKnown[id]; ok && siteID == c.Consumer.SiteID && name == c.Consumer.Name && kind == c.Consumer.Kind {
			seen[id] = true
			continue
		}
		label := kind
		if label == "" {
			label = "custom"
		}
		reasons = append(reasons, fmt.Sprintf("Background process #%d (%q, kind %q) requires manual Node.js maintenance. Only inspected Node.js applications and qualified PHP consumers participate in automatic updates; stopping alone does not clear this blocker", id, name, label))
	}
	if err := rows.Err(); err != nil {
		return reasons, err
	}
	for _, u := range supported {
		if !seen[u.ID] {
			reasons = append(reasons, fmt.Sprintf("Inspected Node.js application #%d changed during consumer inventory; review again", u.ID))
		}
	}
	for id := range phpKnown {
		if !seen[id] {
			reasons = append(reasons, fmt.Sprintf("Inspected PHP daemon #%d changed during inventory", id))
		}
	}
	return reasons, nil
}

func verifySchedulerAdmission(ctx context.Context, id int, entry scheduler.Entry) error {
	actual, err := scheduler.Inspect(ctx, id)
	if err != nil || !actual.Ready || actual.Identity == "" || actual.Directory != entry.Directory {
		return fmt.Errorf("Scheduler #%d needs verified managed admission before Node.js maintenance", id)
	}
	return scheduler.NodeMaintenanceReady()
}

// Inventory every Fluxo-owned scheduled job, including globals and records
// without a site. Only the exact managed Scheduler contract participates in
// execution-time admission; an arbitrary wrapper can launch Node later.
func inspectCronInventory(ctx context.Context, admission func(context.Context, int, scheduler.Entry) error) ([]string, error) {
	reasons := []string{}
	rows, err := database.DB.QueryContext(ctx, `SELECT c.id,c.site_id,COALESCE(c.name,''),c.expression,c.command,c.user,COALESCE(s.path,'') FROM crons c LEFT JOIN sites s ON s.id=c.site_id ORDER BY c.id`)
	if err != nil {
		return reasons, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, siteID int
		var name, expression, command, user, dir string
		if err = rows.Scan(&id, &siteID, &name, &expression, &command, &user, &dir); err != nil {
			return reasons, err
		}
		entry, ok := scheduler.Qualify(id, siteID, dir, expression, command, user)
		if !ok {
			reasons = append(reasons, fmt.Sprintf("Custom cron #%d (%q) requires manual Node.js maintenance", id, name))
			continue
		}
		if err := admission(ctx, id, entry); err != nil {
			reasons = append(reasons, err.Error())
		}
	}
	return reasons, rows.Err()
}
func inspectConsumer(ctx context.Context, u Unit, roots []string, original *Impact) (Unit, error) {
	if original != nil {
		var saved *Unit
		for i := range original.Units {
			if original.Units[i].ID == u.ID && original.Units[i].Name == u.Name {
				saved = &original.Units[i]
				break
			}
		}
		if saved == nil {
			return u, errors.New("Application is not part of this maintenance generation")
		}
		if u.Bun == nil && (saved.Bun != nil || saved.BunOnly || hasBunProcess(u)) {
			var err error
			u.Bun, err = inspectManagedBun(ctx)
			if err != nil {
				return u, err
			}
		}
		if saved.Bun != nil && (u.Bun == nil || *saved.Bun != *u.Bun) {
			return u, errors.New("Bun executable changed since the recorded coordination")
		}
		_, _, err := runtimeProcesses(u, roots...)
		if err != nil {
			return u, err
		}
		// Known Node children are drained with the complete app cgroup.
		// A missing listener is recoverable. An existing foreign listener is not:
		// recovery must never stop or adopt another application's port owner.
		present, err := inspectListenerOwnership(ctx, u)
		if err != nil {
			return u, err
		}
		u.ListenerPresent = &present
		u.Active, u.BunOnly = saved.Active, saved.BunOnly
		return u, nil
	}
	if u.Active {
		if u.Bun == nil && hasBunProcess(u) {
			var err error
			u.Bun, err = inspectManagedBun(ctx)
			if err != nil {
				return u, err
			}
		}
		node, bun, err := runtimeProcesses(u, roots...)
		if err != nil {
			return u, err
		}
		if node == 0 && bun == 0 {
			return u, errors.New("Cannot identify the serving runtime")
		}
		u.BunOnly = node == 0 && bun > 0
		if err = verifyListener(ctx, u, roots[0], !u.BunOnly); err != nil {
			return u, err
		}
	}
	return u, nil
}

func runtimeProcesses(u Unit, roots ...string) (int, int, error) {
	infos := []os.FileInfo{}
	for _, root := range roots {
		info, err := os.Stat(filepath.Join(root, "bin", "node"))
		if err != nil {
			return 0, 0, err
		}
		infos = append(infos, info)
	}
	nodes, buns := 0, 0
	for pid, key := range u.PIDs {
		exe := fmt.Sprintf("/proc/%d/exe", pid)
		target, e := os.Readlink(exe)
		if e != nil {
			return 0, 0, e
		}
		name := filepath.Base(strings.TrimSuffix(target, " (deleted)"))
		if name == "node" || name == "nodejs" {
			actual, e := os.Stat(exe)
			matched := false
			for _, info := range infos {
				if e == nil && os.SameFile(actual, info) {
					matched = true
				}
			}
			if !matched {
				return 0, 0, errors.New("Application uses a different Node.js executable")
			}
			nodes++
		}
		if name == "bun" {
			if u.Bun == nil || u.Bun.Path != managedBunPath {
				return 0, 0, errors.New("Application uses an unreviewed Bun executable")
			}
			actual, e := os.Stat(exe)
			managed, mErr := os.Stat(u.Bun.Path)
			if e != nil || mErr != nil || !os.SameFile(actual, managed) {
				return 0, 0, errors.New("Application uses a different Bun executable")
			}
			buns++
		}
		if now, e := processKey(pid); e != nil || now != key {
			return 0, 0, errors.New("Application process changed during inspection")
		}
	}
	return nodes, buns, nil
}
func verifyListener(ctx context.Context, u Unit, root string, requireNode bool) error {
	n, bun, err := runtimeProcesses(u, root)
	if err != nil {
		return err
	}
	if requireNode && n == 0 {
		return errors.New("Application has not adopted the selected Node.js executable")
	}
	if !requireNode && bun == 0 {
		return errors.New("Application has not adopted its reviewed Bun executable")
	}
	present, err := inspectListenerOwnership(ctx, u)
	if err != nil {
		return err
	}
	if !present {
		return errors.New("Application has no listener on its assigned port")
	}
	c, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(u.Port)))
	if err != nil {
		return err
	}
	return c.Close()
}

func inspectListenerOwnership(ctx context.Context, u Unit) (bool, error) {
	return inspectListenerOwnershipAt(ctx, u, "/proc")
}

func inspectListenerOwnershipAt(ctx context.Context, u Unit, procRoot string) (bool, error) {
	inodes := map[string]bool{}
	for _, file := range []string{filepath.Join(procRoot, "net/tcp"), filepath.Join(procRoot, "net/tcp6")} {
		b, err := os.ReadFile(file)
		if err != nil {
			return false, err
		}
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) < 10 || f[3] != "0A" {
				continue
			}
			_, hex, ok := strings.Cut(f[1], ":")
			port, e := strconv.ParseInt(hex, 16, 32)
			if ok && e == nil && int(port) == u.Port {
				inodes[f[9]] = true
			}
		}
	}
	if len(inodes) == 0 {
		return false, nil
	}
	owned := map[string]bool{}
	processes, err := os.ReadDir(procRoot)
	if err != nil {
		return false, err
	}
	count := 0
	for _, p := range processes {
		if err = ctx.Err(); err != nil {
			return false, err
		}
		pid, e := strconv.Atoi(p.Name())
		if e != nil {
			continue
		}
		fds, e := os.ReadDir(filepath.Join(procRoot, p.Name(), "fd"))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return false, e
		}
		for _, fd := range fds {
			count++
			if count > 131072 {
				return false, errors.New("Listener inspection exceeded its process limit")
			}
			link, e := os.Readlink(filepath.Join(procRoot, p.Name(), "fd", fd.Name()))
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return false, e
			}
			if !strings.HasPrefix(link, "socket:[") {
				continue
			}
			inode := strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")
			if !inodes[inode] {
				continue
			}
			key, e := processKey(pid)
			if e != nil || u.PIDs[pid] != key {
				return false, errors.New("Assigned application port is owned outside its managed cgroup")
			}
			owned[inode] = true
		}
	}
	if len(owned) != len(inodes) {
		return false, errors.New("Cannot prove ownership of the application listener")
	}
	return true, nil
}
func sameConsumers(a, b Impact) bool {
	if a.PHPPolicy != b.PHPPolicy || !phpupdate.SameMaintenanceConsumers(a.PHP, b.PHP) {
		return false
	}
	if len(a.Units) != len(b.Units) || digest(a.Sites) != digest(b.Sites) || len(a.BlockedReasons) > 0 {
		return false
	}
	for i := range a.Units {
		x, y := a.Units[i], b.Units[i]
		x.Active, y.Active = false, false
		x.MainPID, y.MainPID = 0, 0
		x.PIDs, y.PIDs = nil, nil
		x.BunOnly, y.BunOnly = false, false
		x.State, y.State, x.SubState, y.SubState = "", "", "", ""
		x.ListenerPresent, y.ListenerPresent = nil, nil
		if digest(x) != digest(y) {
			return false
		}
	}
	return true
}

// Older immutable generations have no Bun receipt. Fresh recovery can add that
// evidence, but cannot change their service contract or original state intent.
func sameOriginalConsumers(a, b Impact) bool {
	if b.PHPPolicy > 0 {
		if a.PHPPolicy != b.PHPPolicy || !samePHPIntent(a.PHP, b.PHP) {
			return false
		}
	} else {
		a.PHP, b.PHP = nil, nil
		a.PHPPolicy, b.PHPPolicy = 0, 0
	}
	if len(a.Units) != len(b.Units) {
		return false
	}
	for i := range a.Units {
		if a.Units[i].Active != b.Units[i].Active || a.Units[i].BunOnly != b.Units[i].BunOnly {
			return false
		}
	}
	a.Units = append([]Unit(nil), a.Units...)
	b.Units = append([]Unit(nil), b.Units...)
	for i := range a.Units {
		a.Units[i].Bun = nil
	}
	for i := range b.Units {
		b.Units[i].Bun = nil
	}
	return sameConsumers(a, b)
}
