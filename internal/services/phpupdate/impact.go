package phpupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/octaneruntime"
	"fluxo/internal/services/phpmyadmin"
	"fluxo/internal/syscmd"
)

// rootOwned checks the complete resolved path, not just the final inode.
func rootOwned(path string) error {
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	for {
		info, err := os.Stat(p)
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("unsafe ownership or permissions: %s", p)
		}
		if p == "/" {
			break
		}
		p = filepath.Dir(p)
	}
	return nil
}
func packageOwns(ctx context.Context, path, branch string) error {
	if err := rootOwned(path); err != nil {
		return err
	}
	out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/dpkg-query", "-S", path)
	if err != nil {
		return fmt.Errorf("unpackaged PHP runtime file: %s", path)
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		owner, _, ok := strings.Cut(line, ": ")
		if !ok || !(owner == "php"+branch || strings.HasPrefix(owner, "php"+branch+"-")) {
			return fmt.Errorf("PHP runtime file is outside the branch package family: %s", path)
		}
	}
	return nil
}

func inspectImpact(ctx context.Context, branch string) (Impact, error) {
	impact := Impact{Sites: []Site{}, Workers: []Consumer{}, BlockedReasons: []string{}}
	rows, err := database.DB.Query(`SELECT id,domain,path,COALESCE(deployment_strategy,''),COALESCE(app_type,''),COALESCE(app_port,0) FROM sites WHERE php_version=? AND app_type IN ('php','laravel','wordpress') ORDER BY id`, branch)
	if err != nil {
		return impact, err
	}
	for rows.Next() {
		var s Site
		if err = rows.Scan(&s.ID, &s.Domain, &s.Path, &s.Strategy, &s.AppType, &s.Port); err != nil {
			rows.Close()
			return impact, err
		}
		impact.Sites = append(impact.Sites, s)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return impact, err
	}
	rows, err = database.DB.Query(`SELECT d.id,d.site_id,COALESCE(d.name,''),COALESCE(d.managed_kind,''),d.command,d.directory,COALESCE(d.instances,1),COALESCE(d.stop_seconds,15),d.user,COALESCE(s.php_version,'') FROM daemons d LEFT JOIN sites s ON s.id=d.site_id ORDER BY d.id`)
	if err != nil {
		return impact, err
	}
	for rows.Next() {
		var c Consumer
		var user, sitePHP string
		if err = rows.Scan(&c.ID, &c.SiteID, &c.Name, &c.Kind, &c.Command, &c.Directory, &c.Instances, &c.StopSeconds, &user, &sitePHP); err != nil {
			rows.Close()
			return impact, err
		}
		fields := strings.Fields(c.Command)
		binary := ""
		if len(fields) > 0 {
			binary = filepath.Base(fields[0])
		}
		affected := binary == "php"+branch || strings.Contains(c.Command, "php"+branch) || sitePHP == branch && c.Kind != "node_app" && c.Kind != "python_app"
		if binary == "php" {
			target, _ := filepath.EvalSymlinks("/usr/bin/php")
			affected = affected || filepath.Base(target) == "php"+branch
		}
		if !affected {
			continue
		}
		known := c.Kind == "laravel_queue" || c.Kind == "laravel_horizon" || c.Kind == "laravel_octane" || c.Kind == "laravel_nightwatch"
		if !known || user != "fluxo" || binary != "php"+branch || c.Instances < 1 || c.Instances > 64 {
			impact.BlockedReasons = append(impact.BlockedReasons, fmt.Sprintf("Installation blocked by background process %q (daemon #%d, site #%d). Its command or service configuration is not supported for automatic PHP maintenance. Disable its managed feature, or remove the custom background-process entry in Fluxo, before reviewing installation again. Stopping alone does not clear this blocker. Wait for its processes to finish and keep it disabled until maintenance completes", c.Name, c.ID, c.SiteID))
			continue
		}
		if c.Kind == "laravel_nightwatch" {
			port, valid := managedNightwatchPort(branch, c.Command)
			if !valid || c.Instances != 1 {
				impact.BlockedReasons = append(impact.BlockedReasons, fmt.Sprintf("Installation blocked by Nightwatch (daemon #%d, site #%d): use Fluxo's generated Nightwatch command and a single instance, or disable the feature before reviewing installation again", c.ID, c.SiteID))
				continue
			}
			c.Port = port
			impact.Notes = append(impact.Notes, fmt.Sprintf("Nightwatch on site #%d (daemon #%d) will stop and restart automatically if it is running. Monitoring pauses during maintenance and buffered telemetry may be lost when the agent stops. Remote authentication and telemetry delivery are not verified by the runtime health check.", c.SiteID, c.ID))
		}
		if c.Kind == "laravel_octane" {
			for _, site := range impact.Sites {
				if site.ID == c.SiteID {
					c.Port = site.Port
				}
			}
			if c.Instances != 1 || c.Port < 2000 || c.Port > 65535 || c.Command != octaneruntime.Command(branch, c.Port) {
				rows.Close()
				return impact, errors.New(octaneruntime.MigrationMessage)
			}
			probe, e := probeOctane(ctx, branch, c, false)
			if e != nil {
				rows.Close()
				return impact, e
			}
			c.Octane = &probe.Descriptor
			if c.Octane.Backend == "frankenphp" {
				impact.Notes = append(impact.Notes, c.Name+": FrankenPHP's embedded serving PHP is outside this APT operation and remains unchanged. An active FrankenPHP controller requires separate maintenance.")
			}
			if c.Octane.Backend == "swoole" || c.Octane.Backend == "roadrunner" {
				impact.Notes = append(impact.Notes, c.Name+": native graceful shutdown respects its configured runtime deadlines ("+c.Octane.Grace+"). Requests or tasks exceeding those deadlines may be ended by the runtime. HTTP checks verify transport and process ownership, not application correctness.")
			}
		}
		impact.Workers = append(impact.Workers, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return impact, err
	}
	schedulerBlockers, err := inspectSchedulerBlockers(branch)
	if err != nil {
		return impact, err
	}
	impact.BlockedReasons = append(impact.BlockedReasons, schedulerBlockers...)
	pma := phpmyadmin.GetStatus()
	impact.PHPMyAdmin = pma.Installed && pma.PHPVersion == branch
	cli := "/usr/bin/php" + branch
	if err := packageOwns(ctx, cli, branch); err != nil {
		return impact, err
	}
	if err := packageOwns(ctx, "/usr/sbin/php-fpm"+branch, branch); err != nil {
		return impact, err
	}
	extDir, err := syscmd.RunAsUser(ctx, 10*time.Second, "fluxo", cli, "-n", "-r", `echo ini_get('extension_dir');`)
	if err != nil {
		return impact, errors.New("Unable to inspect PHP as the fluxo application user")
	}
	extDir = strings.TrimSpace(extDir)
	if !filepath.IsAbs(extDir) {
		return impact, errors.New("PHP extension directory is not absolute")
	}
	var files []string
	configDir := "/etc/php/" + branch
	err = filepath.WalkDir(configDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return rootOwned(path)
		}
		if d.Type()&os.ModeSymlink != 0 {
			info, e := os.Stat(path)
			if e != nil {
				return e
			}
			if info.IsDir() {
				return errors.New("Symlinked PHP configuration directories require manual maintenance")
			}
		}
		if !strings.HasSuffix(path, ".ini") && !strings.HasSuffix(path, ".conf") {
			return nil
		}
		if err := rootOwned(path); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if len(data) > 1024*1024 {
			return errors.New("PHP configuration file is too large")
		}
		files = append(files, path+"\x00"+string(data))
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			key = strings.ToLower(strings.Trim(strings.TrimSpace(key), `"'`))
			for _, prefix := range []string{"php_admin_value[", "php_value["} {
				if strings.HasPrefix(key, prefix) && strings.HasSuffix(key, "]") {
					key = strings.TrimSuffix(strings.TrimPrefix(key, prefix), "]")
				}
			}
			value = strings.Trim(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]), `"'`)
			if key == "opcache.preload" && value != "" {
				return errors.New("PHP preloading requires a manual maintenance plan")
			}
			if key == "extension_dir" && value != "" && value != extDir {
				return errors.New("Custom PHP extension directories require manual maintenance")
			}
			if key == "include" && value != configDir+"/fpm/pool.d/*.conf" {
				return errors.New("Custom FPM includes require manual maintenance")
			}
			if key != "extension" && key != "zend_extension" {
				continue
			}
			module := value
			if !filepath.IsAbs(module) {
				if !strings.HasSuffix(module, ".so") {
					module += ".so"
				}
				module = filepath.Join(extDir, module)
			}
			if err := packageOwns(ctx, module, branch); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return impact, err
	}
	for _, c := range impact.Workers {
		for i := 1; i <= c.Instances; i++ {
			name := unitName(c.ID, i)
			path := "/etc/systemd/system/" + name
			if err := rootOwned(path); err != nil {
				return impact, err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return impact, err
			}
			files = append(files, path+"\x00"+string(data))

		}
	}
	// Hash effective systemd configuration, including drop-ins. Recovery may
	// accept a repaired PHP ini, but must never signal a redefined service.
	names := []string{"php" + branch + "-fpm.service"}
	for _, c := range impact.Workers {
		for i := 1; i <= c.Instances; i++ {
			names = append(names, unitName(c.ID, i))
		}
	}
	var units []string
	for _, name := range names {
		out, e := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", name, "--property="+inspectedUnitProperties)
		if e != nil {
			return impact, e
		}
		var consumer *Consumer
		for _, c := range impact.Workers {
			for i := 1; i <= c.Instances; i++ {
				if unitName(c.ID, i) == name {
					copy := c
					consumer = &copy
				}
			}
		}
		properties, e := normalizeBarrierDefinition(name, unitProperties(out))
		if e != nil {
			return impact, e
		}
		if e := validateUnitDefinition(branch, consumer, properties); e != nil {
			return impact, e
		}
		u, e := unitState(ctx, name)
		if e != nil {
			if barrier, bErr := getBarrier(name); bErr == nil {
				if heldErr := verifyEffectiveBarrier(ctx, barrier); heldErr != nil {
					return impact, heldErr
				}
				u, e = observeUnit(ctx, name, "")
			}
			if e != nil {
				return impact, e
			}
		}
		draining := false
		if b, bErr := getBarrier(name); bErr == nil {
			draining = b.State == "stopping" || b.State == "drained"
		}
		if consumer == nil || consumer.Kind != "laravel_octane" || !draining {
			if e := validateUnitProcesses(ctx, branch, consumer, u); e != nil {
				return impact, e
			}
		}
		for key, value := range properties {
			if key == "FragmentPath" || key == "DropInPaths" {
				for _, path := range strings.Fields(value) {
					if e := rootOwned(path); e != nil {
						return impact, e
					}
				}
			}
		}
		units = append(units, name+"\x00"+canonicalDefinition(properties))
	}
	impact.UnitHash = digest(units)
	sort.Strings(files)
	impact.ConfigHash = digest(files)
	return impact, nil
}
func unitName(id, instance int) string {
	if instance <= 1 {
		return fmt.Sprintf("fluxo-daemon-%d.service", id)
	}
	return fmt.Sprintf("fluxo-daemon-%d-%d.service", id, instance)
}

// Scheduler commands retain their interpreter even if a site later switches
// branches. Inspect the actual command as well as metadata; ambiguous launchers
// fail closed. No cron launch is stopped, altered or authorized by discovery.
func inspectSchedulerBlockers(branch string) ([]string, error) {
	rows, err := database.DB.Query(`SELECT c.id,c.site_id,COALESCE(s.domain,''),COALESCE(s.php_version,''),c.command
      FROM crons c LEFT JOIN sites s ON s.id=c.site_id
      WHERE c.name='Laravel Scheduler' OR c.command LIKE '%artisan schedule:run%' ORDER BY c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reasons := []string{}
	for rows.Next() {
		var id, siteID int
		var domain, siteBranch, command string
		if err := rows.Scan(&id, &siteID, &domain, &siteBranch, &command); err != nil {
			return nil, err
		}
		actual := "unknown"
		fields := strings.Fields(command)
		if len(fields) > 0 {
			binary := filepath.Base(fields[0])
			if binary == "php" {
				if target, e := filepath.EvalSymlinks("/usr/bin/php"); e == nil {
					binary = filepath.Base(target)
				}
			}
			if strings.HasPrefix(binary, "php") && ValidBranch(strings.TrimPrefix(binary, "php")) {
				actual = strings.TrimPrefix(binary, "php")
			}
		}
		if actual != branch && siteBranch != branch && actual != "unknown" {
			continue
		}
		reasons = append(reasons, fmt.Sprintf("Installation blocked by Laravel Scheduler on %s (site #%d, cron #%d, command PHP branch %s). Turn off Scheduler in the site's overview before reviewing installation again, wait for running scheduled tasks to finish, and leave it off until PHP maintenance completes. Fluxo does not yet hold cron-launched tasks automatically.", domain, siteID, id, actual))
	}
	return reasons, rows.Err()
}
