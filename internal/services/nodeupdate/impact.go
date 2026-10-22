package nodeupdate

import (
	"context"
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
	if generationID != "" && drop != "" {
		expected := barrierPath(u)
		if drop != expected {
			return u, errors.New("Node.js recovery service drop-ins changed")
		}
		if err = verifyBarrier(u, generationID); err != nil {
			return u, err
		}
	}
	if p["ActiveState"] != "active" && p["ActiveState"] != "inactive" {
		return u, fmt.Errorf("%s is failed or changing state; stop or repair it before review", u.Domain)
	}
	if p["ActiveState"] == "active" && p["SubState"] != "running" {
		return u, errors.New("Node.js service is not stably running")
	}
	u.Active = p["ActiveState"] == "active"
	u.Enabled = p["UnitFileState"]
	if u.Enabled != "enabled" && u.Enabled != "disabled" && u.Enabled != "static" {
		return u, errors.New("Masked or unsupported Node.js service state")
	}
	u.MainPID, err = strconv.Atoi(p["MainPID"])
	if err != nil {
		return u, err
	}
	group := "/system.slice/" + u.Name
	if p["ControlGroup"] != "" && p["ControlGroup"] != group {
		return u, errors.New("Unexpected Node.js service cgroup")
	}
	u.PIDs, err = groupPIDs(group)
	if err != nil {
		return u, err
	}
	if u.Active && (u.MainPID == 0 || u.PIDs[u.MainPID] == "") {
		return u, errors.New("Node.js main process cannot be identified")
	}
	if !u.Active && len(u.PIDs) > 0 {
		return u, errors.New("Node.js service still has draining processes")
	}
	return u, nil
}
func inspectImpact(ctx context.Context, nodeRoot, generationID string) (Impact, error) {
	impact := Impact{Sites: []Site{}, Units: []Unit{}, BlockedReasons: []string{}}
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
		u, err = inspectUnit(ctx, u, generationID)
		if err != nil {
			impact.BlockedReasons = append(impact.BlockedReasons, s.Domain+": "+err.Error())
			continue
		}
		if u.Active {
			node, bun, e := runtimeProcesses(u, nodeRoot)
			if e != nil {
				impact.BlockedReasons = append(impact.BlockedReasons, s.Domain+": "+e.Error())
				continue
			}
			if node == 0 && bun == 0 {
				impact.BlockedReasons = append(impact.BlockedReasons, "Cannot identify the serving runtime for "+s.Domain)
				continue
			}
			u.BunOnly = node == 0 && bun > 0
			if err = verifyListener(ctx, u, nodeRoot, !u.BunOnly); err != nil {
				impact.BlockedReasons = append(impact.BlockedReasons, s.Domain+": "+err.Error())
				continue
			}
		}
		impact.Units = append(impact.Units, u)
	}
	// Only qualified managed Scheduler entries have an admission/descendant
	// tracking protocol. Arbitrary cron commands require an operator's plan.
	rows, err = database.DB.Query(`SELECT c.id,c.site_id,c.expression,c.command,c.user,s.path FROM crons c JOIN sites s ON s.id=c.site_id ORDER BY c.id`)
	if err != nil {
		return impact, err
	}
	for rows.Next() {
		var id, siteID int
		var expression, command, user, dir string
		if err = rows.Scan(&id, &siteID, &expression, &command, &user, &dir); err != nil {
			rows.Close()
			return impact, err
		}
		entry, ok := scheduler.Qualify(id, siteID, dir, expression, command, user)
		if !ok {
			impact.BlockedReasons = append(impact.BlockedReasons, fmt.Sprintf("Custom cron #%d requires manual Node.js maintenance", id))
			continue
		}
		actual, e := scheduler.Inspect(ctx, id)
		if e != nil || !actual.Ready || actual.Identity == "" || actual.Directory != entry.Directory {
			impact.BlockedReasons = append(impact.BlockedReasons, fmt.Sprintf("Scheduler #%d needs verified managed admission before Node.js maintenance", id))
		}
		if e := scheduler.NodeMaintenanceReady(); e != nil {
			impact.BlockedReasons = append(impact.BlockedReasons, e.Error())
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return impact, err
	}
	var custom int
	if err = database.DB.QueryRow(`SELECT COUNT(*) FROM daemons WHERE COALESCE(managed_kind,'')=''`).Scan(&custom); err != nil {
		return impact, err
	}
	if custom > 0 {
		impact.BlockedReasons = append(impact.BlockedReasons, "Custom background processes require a manual Node.js maintenance plan")
	}
	known := map[int]bool{}
	for _, u := range impact.Units {
		for pid := range u.PIDs {
			known[pid] = true
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
func runtimeProcesses(u Unit, root string) (int, int, error) {
	info, err := os.Stat(filepath.Join(root, "bin", "node"))
	if err != nil {
		return 0, 0, err
	}
	nodes, buns := 0, 0
	for pid, key := range u.PIDs {
		exe := fmt.Sprintf("/proc/%d/exe", pid)
		target, e := os.Readlink(exe)
		if e != nil {
			return 0, 0, e
		}
		name := filepath.Base(target)
		if name == "node" || name == "nodejs" {
			actual, e := os.Stat(exe)
			if e != nil || !os.SameFile(actual, info) {
				return 0, 0, errors.New("Application uses a different Node.js executable")
			}
			nodes++
		}
		if name == "bun" {
			buns++
		}
		if now, e := processKey(pid); e != nil || now != key {
			return 0, 0, errors.New("Application process changed during inspection")
		}
	}
	return nodes, buns, nil
}
func verifyListener(ctx context.Context, u Unit, root string, requireNode bool) error {
	if requireNode {
		n, _, err := runtimeProcesses(u, root)
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("Application has not adopted the selected Node.js executable")
		}
	}
	inodes := map[string]bool{}
	for _, file := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		b, err := os.ReadFile(file)
		if err != nil {
			return err
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
		return errors.New("Application has no listener on its assigned port")
	}
	owned := map[string]bool{}
	processes, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	count := 0
	for _, p := range processes {
		if err = ctx.Err(); err != nil {
			return err
		}
		pid, e := strconv.Atoi(p.Name())
		if e != nil {
			continue
		}
		fds, e := os.ReadDir(filepath.Join("/proc", p.Name(), "fd"))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		for _, fd := range fds {
			count++
			if count > 131072 {
				return errors.New("Listener inspection exceeded its process limit")
			}
			link, e := os.Readlink(filepath.Join("/proc", p.Name(), "fd", fd.Name()))
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return e
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
				return errors.New("Assigned application port is owned outside its managed cgroup")
			}
			owned[inode] = true
		}
	}
	if len(owned) != len(inodes) {
		return errors.New("Cannot prove ownership of the application listener")
	}
	c, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(u.Port)))
	if err != nil {
		return err
	}
	return c.Close()
}
func sameConsumers(a, b Impact) bool {
	if len(a.Units) != len(b.Units) || digest(a.Sites) != digest(b.Sites) || len(a.BlockedReasons) > 0 {
		return false
	}
	for i := range a.Units {
		x, y := a.Units[i], b.Units[i]
		x.Active, y.Active = false, false
		x.MainPID, y.MainPID = 0, 0
		x.PIDs, y.PIDs = nil, nil
		x.BunOnly, y.BunOnly = false, false
		if digest(x) != digest(y) {
			return false
		}
	}
	return true
}
