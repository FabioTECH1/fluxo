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
	"fluxo/internal/services/scheduler"
	sitepkg "fluxo/internal/services/site"
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
	rows, err = database.DB.Query(`SELECT d.id,d.site_id,COALESCE(d.name,''),COALESCE(d.managed_kind,''),d.command,d.directory,COALESCE(d.instances,1),COALESCE(d.stop_seconds,15),d.user,COALESCE(s.php_version,''),COALESCE(s.id,0),COALESCE(s.path,''),COALESCE(s.deployment_strategy,'standard'),COALESCE(s.app_type,''),COALESCE(s.domain,''),COALESCE(s.app_port,0) FROM daemons d LEFT JOIN sites s ON s.id=d.site_id ORDER BY d.id`)
	if err != nil {
		return impact, err
	}
	for rows.Next() {
		var c Consumer
		var user, sitePHP string
		var site Site
		if err = rows.Scan(&c.ID, &c.SiteID, &c.Name, &c.Kind, &c.Command, &c.Directory, &c.Instances, &c.StopSeconds, &user, &sitePHP, &site.ID, &site.Path, &site.Strategy, &site.AppType, &site.Domain, &site.Port); err != nil {
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
		if qualifyCustomQueue(branch, c, site) {
			c.Protocol = "laravel_queue"
		} else if customArtisanSite(c, site) {
			if _, ok := customArtisanArguments(branch, c.Command); ok {
				c.Protocol = "artisan_natural"
			}
		}
		label := sitepkg.MaintenanceLabel(c.Name, site.Domain)
		if issue := workerMetadataIssue(branch, c, user); issue != "" {
			impact.BlockedReasons = append(impact.BlockedReasons, fmt.Sprintf("Installation blocked by background process %s: %s. Review its full configuration in Processes before repairing, migrating or retiring it. Preserve its settings and let in-flight jobs finish. Stopping alone does not clear this blocker", label, issue))
			continue
		}
		if c.Protocol != "" {
			// The recorded interpreter can differ from the site's current FPM
			// branch. Keep that affected application visible in the review.
			found := false
			for _, affectedSite := range impact.Sites {
				found = found || affectedSite.ID == site.ID
			}
			if !found {
				impact.Sites = append(impact.Sites, site)
			}
			if c.Protocol == "laravel_queue" {
				impact.Notes = append(impact.Notes, fmt.Sprintf("Custom queue worker %s will participate independently in PHP maintenance. Its record, command, queues and feature ownership stay unchanged. Running instances drain naturally and restart afterward; stopped instances remain stopped.", label))
			} else {
				impact.Notes = append(impact.Notes, fmt.Sprintf("Custom Artisan process %s: Fluxo holds restarts and waits for every process to exit naturally. It does not send this command a stop signal. A continuously running command needs its application's own graceful shutdown before Repair can continue; keep its entry. Originally running instances are resumed once after package verification; a verified successful completion is not replayed by recovery. Stopped instances stay stopped. Settings and enabled state are preserved; application-specific health is not certified.", label))
			}
		}
		if c.Kind == "laravel_nightwatch" {
			port, valid := managedNightwatchPort(branch, c.Command)
			if !valid || c.Instances != 1 {
				impact.BlockedReasons = append(impact.BlockedReasons, fmt.Sprintf("Installation blocked by Nightwatch %s: use Fluxo's generated Nightwatch command and a single instance, or disable the feature before reviewing installation again", label))
				continue
			}
			c.Port = port
			impact.Notes = append(impact.Notes, fmt.Sprintf("Nightwatch %s will stop and restart automatically if it is running. Monitoring pauses during maintenance and buffered telemetry may be lost when the agent stops. Remote authentication and telemetry delivery are not verified by the runtime health check.", label))
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
				impact.Notes = append(impact.Notes, label+": FrankenPHP's embedded serving PHP is outside this APT operation and remains unchanged. An active FrankenPHP controller requires separate maintenance.")
			}
			if c.Octane.Backend == "swoole" || c.Octane.Backend == "roadrunner" {
				impact.Notes = append(impact.Notes, label+": native graceful shutdown respects its configured runtime deadlines ("+c.Octane.Grace+"). Requests or tasks exceeding those deadlines may be ended by the runtime. HTTP checks verify transport and process ownership, not application correctness.")
			}
		}
		impact.Workers = append(impact.Workers, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return impact, err
	}
	schedulerEntries, schedulerBlockers, err := inspectSchedulers(ctx, branch)
	if err != nil {
		return impact, err
	}
	impact.Schedulers = schedulerEntries
	impact.BlockedReasons = append(impact.BlockedReasons, schedulerBlockers...)
	for _, e := range schedulerEntries {
		found := false
		for _, site := range impact.Sites {
			if site.ID == e.SiteID {
				found = true
			}
		}
		if !found {
			var site Site
			if err = database.DB.QueryRow("SELECT id,domain,path,COALESCE(deployment_strategy,'standard'),app_type,COALESCE(app_port,0) FROM sites WHERE id=?", e.SiteID).Scan(&site.ID, &site.Domain, &site.Path, &site.Strategy, &site.AppType, &site.Port); err != nil {
				return impact, err
			}
			impact.Sites = append(impact.Sites, site)
		}
	}
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

// Labels and a queue-looking command do not establish managed ownership. Keep
// custom/legacy entries separate even when a managed worker exists on the site.
// Live unit and process validation still follows for accepted metadata.
func workerMetadataIssue(branch string, c Consumer, user string) string {
	if c.Kind != "laravel_queue" && c.Kind != "laravel_horizon" && c.Kind != "laravel_octane" && c.Kind != "laravel_nightwatch" && !isQueueConsumer(&c) && !isNaturalConsumer(&c) {
		return "this entry has no verified PHP maintenance protocol; supported direct custom Artisan commands can participate without managed feature ownership"
	}
	if c.Protocol != "" {
		if c.Kind != "" || c.Protocol != "laravel_queue" && c.Protocol != "artisan_natural" {
			return "this entry has an unsupported PHP maintenance protocol"
		}
		parse := customQueueArguments
		if isNaturalConsumer(&c) {
			parse = customArtisanArguments
		}
		if _, ok := parse(branch, c.Command); !ok {
			return "this custom command is outside its supported PHP maintenance protocol"
		}
	}
	if user != "fluxo" {
		return "automatic PHP maintenance requires the fluxo service user"
	}
	fields := strings.Fields(c.Command)
	if len(fields) == 0 || filepath.Base(fields[0]) != "php"+branch {
		return "automatic PHP maintenance requires an explicit php" + branch + " interpreter"
	}
	if c.Instances < 1 || c.Instances > 64 {
		return "automatic PHP maintenance requires between 1 and 64 process instances"
	}
	return ""
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
	_, reasons, err := inspectSchedulers(context.Background(), branch)
	return reasons, err
}
func inspectSchedulers(ctx context.Context, branch string) ([]scheduler.Entry, []string, error) {
	var dbPath string
	if err := database.DB.QueryRowContext(ctx, "SELECT file FROM pragma_database_list WHERE name='main'").Scan(&dbPath); err != nil {
		return nil, nil, err
	}
	rows, err := database.DB.Query(`SELECT c.id,c.site_id,COALESCE(s.domain,''),COALESCE(s.php_version,''),c.command,COALESCE(s.path,''),COALESCE(s.deployment_strategy,''),COALESCE(s.app_type,''),c.expression,c.user
      FROM crons c LEFT JOIN sites s ON s.id=c.site_id
      WHERE c.name='Laravel Scheduler' OR c.command LIKE '%artisan schedule:run%' ORDER BY c.id`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	reasons := []string{}
	branchReadinessErr := scheduler.BranchReady(ctx, branch)
	if branchReadinessErr != nil {
		reasons = append(reasons, branchReadinessErr.Error())
	}
	entries := []scheduler.Entry{}
	for rows.Next() {
		var id, siteID int
		var domain, siteBranch, command, path, strategy, appType, expression, user string
		if err := rows.Scan(&id, &siteID, &domain, &siteBranch, &command, &path, &strategy, &appType, &expression, &user); err != nil {
			return nil, nil, err
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
		if actual != branch && actual != "unknown" {
			continue
		}
		entry, inspectErr := scheduler.Inspect(ctx, id)
		if entry.ID == id && !schedulerDatabaseMatches(entry, dbPath) {
			reasons = append(reasons, fmt.Sprintf("Scheduler registration for %s belongs to a different panel database. Restore its original database configuration before PHP maintenance.", sitepkg.MaintenanceLabel("Laravel Scheduler", domain)))
			continue
		}
		if entry.ID == id && entry.Version == branch && scheduler.MatchesSite(entry, siteID, path, strategy, appType, expression, command, user) {
			entries = append(entries, entry)
			if inspectErr == nil {
				continue
			}
		}
		if branchReadinessErr != nil && inspectErr != nil && branchReadinessErr.Error() == inspectErr.Error() {
			continue
		}
		reason := "Custom Scheduler requires a manual maintenance plan"
		if inspectErr != nil {
			reason = inspectErr.Error()
		}
		reasons = append(reasons, fmt.Sprintf("%s: %s.", sitepkg.MaintenanceLabel("Laravel Scheduler", domain), reason))
	}
	return entries, reasons, rows.Err()
}

// Dispatch admission checks pending generations in the registered database.
// A moved panel must not authorize maintenance against an older database copy.
func schedulerDatabaseMatches(entry scheduler.Entry, dbPath string) bool {
	return filepath.IsAbs(dbPath) && filepath.IsAbs(entry.Database) && filepath.Clean(entry.Database) == filepath.Clean(dbPath)
}
