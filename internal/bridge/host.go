package bridge

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"fluxo/internal/config"
	"fluxo/internal/safeinput"
	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/mysql"
	"fluxo/internal/services/nginx"
	"fluxo/internal/services/nodetoolchain"
	"fluxo/internal/services/octaneruntime"
	"fluxo/internal/services/phpupdate"
	"fluxo/internal/services/postgres"
	"fluxo/internal/services/processlog"
	"fluxo/internal/services/scheduler"
	sslservice "fluxo/internal/services/ssl"
	"fluxo/internal/syscmd"

	"golang.org/x/sys/unix"
)

func trustedDirectory(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("bridge requires a canonical absolute data directory")
	}
	for p := path; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || st.Uid != 0 || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("unsafe bridge directory %s", p)
		}
		if p == "/" {
			return nil
		}
	}
}

func protectedInfo(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || st.Uid != 0 || st.Nlink != 1 || info.Mode().Perm()&0022 != 0 {
		return nil, fmt.Errorf("unsafe protected file %s", path)
	}
	return info, nil
}

func readProtected(path string, limit int64) ([]byte, error) {
	if err := trustedDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	before, err := protectedInfo(path)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return nil, errors.New("protected file changed before read")
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	current, err := protectedInfo(path)
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit || !os.SameFile(before, current) || before.Size() != after.Size() || before.ModTime() != after.ModTime() {
		return nil, errors.New("protected file changed or exceeded its size bound")
	}
	return b, nil
}

func run(ctx context.Context, exe string, args ...string) (string, error) {
	return syscmd.Run(ctx, 15*time.Second, exe, args...)
}
func panelStopped(ctx context.Context) error {
	out, err := run(ctx, "/usr/bin/systemctl", "show", "fluxo.service", "--property=LoadState,ActiveState,MainPID,ControlPID,ControlGroup")
	if err != nil {
		return errors.New("cannot establish stopped Fluxo panel; certification requires systemd")
	}
	p := properties(out)
	if p["LoadState"] != "loaded" || p["ActiveState"] != "inactive" || p["MainPID"] != "0" || p["ControlPID"] != "0" {
		return errors.New("stop fluxo.service from SSH before certification; the bridge never stops the panel automatically")
	}
	if p["ControlGroup"] != "" {
		b, e := os.ReadFile(filepath.Join("/sys/fs/cgroup", p["ControlGroup"], "cgroup.events"))
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if strings.Contains(string(b), "populated 1") {
			return errors.New("wait for the stopped panel execution domain to empty")
		}
	}
	return nil
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

func checkHost(ctx context.Context, q queryer, dataDir string, key []byte) error {
	if len(key) != 32 {
		return errors.New("recover the existing encryption key before host qualification")
	}
	out, err := run(ctx, "/usr/bin/systemctl", "show", "fluxo.service", "--property=LoadState,NeedDaemonReload,User,DynamicUser,RootDirectory,RootImage,BindPaths,BindReadOnlyPaths,TemporaryFileSystem,MountImages,ExtensionImages,ExtensionDirectories,InaccessiblePaths,ReadOnlyPaths,ReadWritePaths,ProtectHome,ProtectSystem,PrivateUsers,FragmentPath,DropInPaths,ExecStart,Environment,EnvironmentFiles,PassEnvironment,UnsetEnvironment")
	if err != nil {
		return errors.New("cannot read the installed panel service context")
	}
	service := properties(out)
	if err = serviceContext(service, dataDir, os.Getenv("FLUXO_USE_HTTP") == "1"); err != nil {
		return err
	}
	unit, err := readProtected(service["FragmentPath"], 1<<20)
	if err != nil {
		return err
	}
	if err = supportedServiceFile(string(unit)); err != nil {
		return err
	}
	if err = checkSecurity(ctx, service["Environment"]); err != nil {
		return err
	}
	for _, path := range []string{"/etc/systemd/system/fluxo.service", "/etc/sudoers.d/fluxo", "/etc/fail2ban/filter.d/fluxo-auth.conf", "/etc/fail2ban/jail.d/fluxo-auth.conf"} {
		if _, err := readProtected(path, 1<<20); err != nil {
			return err
		}
	}
	if _, err := os.Lstat("/home/fluxo/.fluxo_credentials"); !os.IsNotExist(err) {
		return errors.New("resolve the historical credentials file before certification")
	}
	for _, cmd := range [][]string{{"/usr/sbin/sshd", "-T"}, {"/usr/sbin/visudo", "-c"}, {"/usr/bin/dpkg", "--audit"}} {
		out, err := run(ctx, cmd[0], cmd[1:]...)
		if err != nil {
			return fmt.Errorf("verify %s configuration before certification", filepath.Base(cmd[0]))
		}
		if cmd[0] == "/usr/bin/dpkg" && strings.TrimSpace(out) != "" {
			return errors.New("resolve incomplete dpkg state before certification")
		}
	}
	if err := phpupdate.RepositoryBridgeReady(ctx, dataDir); err != nil {
		return fmt.Errorf("PHP repository: %w", err)
	}
	if err := scheduler.BridgeReady(ctx, filepath.Join(dataDir, "fluxo.db")); err != nil {
		return fmt.Errorf("Scheduler: %w", err)
	}
	if err := maintenancetask.BridgeReady(ctx); err != nil {
		return fmt.Errorf("guarded jobs: %w", err)
	}
	if err := checkSites(ctx, q); err != nil {
		return err
	}
	if err := checkDaemons(ctx, q); err != nil {
		return err
	}
	if err := checkCrons(ctx, q, dataDir); err != nil {
		return err
	}
	if err := checkCertificates(ctx, q, dataDir); err != nil {
		return err
	}
	if err := checkDatabaseAccess(ctx, q, key); err != nil {
		return err
	}
	// The official runtime inspector authenticates the approved payload/tree;
	// it performs no discovery, download, activation or package installation.
	if _, err := os.Lstat("/var/lib/fluxo/node-toolchain.json"); err == nil {
		if err = nodetoolchain.BridgeReady(); err != nil {
			return errors.New("finish reviewed managed Node runtime qualification")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Only direct, unambiguous systemd assignments are admitted. Environment files
// and inherited/unset assignments need a separately reviewed service profile.
func serviceContext(p map[string]string, dataDir string, http bool) error {
	if p["LoadState"] != "loaded" || (p["User"] != "" && p["User"] != "root") {
		return errors.New("bridge requires the installed root panel service")
	}
	if p["DynamicUser"] != "no" || p["RootDirectory"] != "" || p["RootImage"] != "" || !strings.HasPrefix(p["ExecStart"], "{ path=/usr/local/bin/fluxo ; argv[]=/usr/local/bin/fluxo ; ") || strings.Count(p["ExecStart"], "{ path=") != 1 {
		return errors.New("review custom panel execution configuration before qualification")
	}
	if p["NeedDaemonReload"] != "no" {
		return errors.New("reload and review the installed panel unit before qualification")
	}
	if p["FragmentPath"] != "/etc/systemd/system/fluxo.service" || p["DropInPaths"] != "" {
		return errors.New("review panel service fragment/drop-ins before qualification")
	}
	for _, name := range []string{"BindPaths", "BindReadOnlyPaths", "TemporaryFileSystem", "MountImages", "ExtensionImages", "ExtensionDirectories", "InaccessiblePaths", "ReadOnlyPaths", "ReadWritePaths"} {
		if p[name] != "" {
			return fmt.Errorf("review panel filesystem remapping %s before qualification", name)
		}
	}
	for _, name := range []string{"ProtectHome", "ProtectSystem", "PrivateUsers"} {
		if p[name] != "no" {
			return fmt.Errorf("review panel filesystem namespace %s before qualification", name)
		}
	}
	for _, name := range []string{"EnvironmentFiles", "PassEnvironment", "UnsetEnvironment"} {
		if p[name] != "" {
			return fmt.Errorf("review custom panel %s before qualification", name)
		}
	}
	env := map[string]string{}
	assignments, err := serviceAssignments(p["Environment"])
	if err != nil {
		return err
	}
	for _, assignment := range assignments {
		k, v, ok := strings.Cut(assignment, "=")
		if !ok || k == "" {
			return errors.New("invalid installed panel environment")
		}
		switch k {
		case "FLUXO_ENV", "FLUXO_PORT", "FLUXO_DATA_DIR", "FLUXO_USE_HTTP":
		default:
			return errors.New("review custom panel environment before qualification")
		}
		env[k] = v
	}
	installedDir := env["FLUXO_DATA_DIR"]
	if env["FLUXO_ENV"] != "prod" {
		return errors.New("bridge requires the installed service's production runtime mode")
	}
	if port := env["FLUXO_PORT"]; port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 || n == 6060 {
			return errors.New("review the installed panel port before qualification")
		}
	}
	if installedDir == "" {
		installedDir = "/var/lib/fluxo"
	}
	if installedDir != dataDir || (env["FLUXO_USE_HTTP"] == "1") != http {
		return errors.New("invoke the bridge with the installed service's FLUXO_DATA_DIR and FLUXO_USE_HTTP values")
	}
	return nil
}

func serviceAssignments(s string) ([]string, error) {
	var result []string
	for s = strings.TrimSpace(s); s != ""; s = strings.TrimSpace(s) {
		end := 0
		if s[0] == '"' {
			end = 1
			for end < len(s) {
				if s[end] == '\\' {
					end += 2
					continue
				}
				if s[end] == '"' {
					end++
					break
				}
				end++
			}
			if end > len(s) || end < 2 || s[end-1] != '"' || (end < len(s) && s[end] != ' ' && s[end] != '\t') {
				return nil, errors.New("unsupported installed panel environment quoting")
			}
			value, err := strconv.Unquote(s[:end])
			if err != nil {
				return nil, errors.New("unsupported installed panel environment escape")
			}
			result = append(result, value)
		} else {
			end = strings.IndexAny(s, " \t")
			if end < 0 {
				end = len(s)
			}
			if strings.ContainsAny(s[:end], "\\\"'") {
				return nil, errors.New("unsupported installed panel environment quoting")
			}
			result = append(result, s[:end])
		}
		s = s[end:]
	}
	return result, nil
}

func readKey(dataDir string) ([]byte, error) {
	path := filepath.Join(dataDir, "encryption.key")
	info, err := protectedInfo(path)
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("encryption key must have owner-only permissions")
	}
	key, err := readProtected(path, 32)
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("invalid existing encryption-key length")
	}
	return key, nil
}

func checkSites(ctx context.Context, q queryer) error {
	if err := nginx.HTTPDefaultsRecoveryReady(); err != nil {
		return fmt.Errorf("finish pending Nginx HTTP defaults recovery: %w", err)
	}
	u, err := user.Lookup("fluxo")
	if err != nil {
		return errors.New("restore the Fluxo system account")
	}
	g, err := user.LookupGroup("www-data")
	if err != nil {
		return err
	}
	uid, _ := strconv.ParseUint(u.Uid, 10, 32)
	gid, _ := strconv.ParseUint(g.Gid, 10, 32)
	files := map[string][]string{}
	if err = readNginxGraph("/etc/nginx/nginx.conf", nil, files, map[string]string{}, map[string]bool{}); err != nil {
		return fmt.Errorf("verify effective Nginx includes: %w", err)
	}
	rows, err := q.QueryContext(ctx, "SELECT id,domain,path,COALESCE(deploy_script_mode,'legacy') FROM sites ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var domain, path, mode string
		if err = rows.Scan(&id, &domain, &path, &mode); err != nil {
			return err
		}
		if !safeinput.ValidateDomain(domain) {
			return fmt.Errorf("site %d needs domain review", id)
		}
		path, err = safeinput.NormalizeManagedSitePath(path)
		if err != nil {
			return fmt.Errorf("site %d needs path review", id)
		}
		info, e := os.Lstat(path)
		if e != nil || !info.IsDir() {
			return fmt.Errorf("restore site %d managed directory", id)
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || uint64(st.Uid) != uid || uint64(st.Gid) != gid {
			return fmt.Errorf("complete site %d ownership conversion", id)
		}
		if mode != "managed" && mode != "legacy" {
			return fmt.Errorf("site %d has an unknown script mode", id)
		}
		name, expected, e := existingSiteConfig(ctx, q, id)
		if e != nil {
			return fmt.Errorf("site %d vhost contract: %w", id, e)
		}
		configPath := filepath.Join("/etc/nginx/sites-available", name)
		if !nginxHTTPIncluded(files, configPath) {
			return fmt.Errorf("site %d is not included in the effective Nginx http configuration", id)
		}
		b, e := readProtected(configPath, 2<<20)
		if e != nil {
			return fmt.Errorf("verify site %d Nginx configuration", id)
		}
		if e = sameNginxConfig(string(b), expected); e != nil {
			return fmt.Errorf("site %d: %w", id, e)
		}
	}
	return rows.Err()
}

func checkDaemons(ctx context.Context, q queryer) error {
	rows, err := q.QueryContext(ctx, "SELECT id,instances,command,directory,user,COALESCE(managed_kind,'') FROM daemons ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, n int
		var command, dir, username, kind string
		if err = rows.Scan(&id, &n, &command, &dir, &username, &kind); err != nil {
			return err
		}
		if n < 1 || n > 64 {
			return fmt.Errorf("daemon %d has unsupported instance count", id)
		}
		for i := 1; i <= n; i++ {
			name := fmt.Sprintf("fluxo-daemon-%d.service", id)
			if i > 1 {
				name = fmt.Sprintf("fluxo-daemon-%d-%d.service", id, i)
			}
			out, e := run(ctx, "/usr/bin/systemctl", "show", name, "--property=LoadState,User,WorkingDirectory,DropInPaths,FragmentPath,ExecStart")
			if e != nil {
				return fmt.Errorf("verify daemon %d service group", id)
			}
			p := properties(out)
			if p["LoadState"] != "loaded" || p["User"] != username || p["WorkingDirectory"] != dir || p["DropInPaths"] != "" {
				return fmt.Errorf("daemon %d needs an explicit reviewed service configuration", id)
			}
			if _, e = readProtected(p["FragmentPath"], 1<<20); e != nil {
				return e
			}
			if command == "" {
				return fmt.Errorf("daemon %d has no command", id)
			}
			expected := command
			if strings.HasPrefix(command, "php") && len(strings.Fields(command)) > 1 {
				first := strings.Fields(command)[0]
				if safeinput.ValidatePHPVersion(strings.TrimPrefix(first, "php")) {
					expected = "/usr/bin/" + command
				}
			}
			if !strings.Contains(p["ExecStart"], " ; argv[]="+expected+" ; ignore_errors=") {
				return fmt.Errorf("daemon %d executable differs from its recorded command", id)
			}
		}
		if kind == "laravel_octane" {
			if err = octaneruntime.Verify(); err != nil {
				return errors.New(octaneruntime.MigrationMessage)
			}
		}
		if safe, e := processlog.IsSafe(fmt.Sprintf("/var/log/fluxo/fluxo-daemon-%d.log", id)); e != nil || !safe {
			return fmt.Errorf("repair daemon %d log safely before certification", id)
		}
	}
	return rows.Err()
}

func checkCrons(ctx context.Context, q queryer, dataDir string) error {
	schedulerIDs, err := scheduler.BridgeRegisteredIDs()
	if err != nil {
		return err
	}
	taskIDs, err := maintenancetask.BridgeRegisteredIDs()
	if err != nil {
		return err
	}
	registered := map[int]bool{}
	for _, id := range append(schedulerIDs, taskIDs...) {
		registered[id] = true
	}
	rows, err := q.QueryContext(ctx, `SELECT c.id,c.site_id,c.expression,c.command,c.user,COALESCE(s.path,''),COALESCE(s.deployment_strategy,''),COALESCE(s.app_type,''),COALESCE(s.app_directory,'.') FROM crons c LEFT JOIN sites s ON s.id=c.site_id ORDER BY c.id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, site int
		var expr, cmd, user, path, strategy, app, dir string
		if err = rows.Scan(&id, &site, &expr, &cmd, &user, &path, &strategy, &app, &dir); err != nil {
			return err
		}
		delete(registered, id)
		if expected, ok := scheduler.QualifySite(id, site, path, strategy, app, expr, cmd, user); ok {
			if err = scheduler.BridgeRegistration(ctx, expected, filepath.Join(dataDir, "fluxo.db")); err != nil {
				return fmt.Errorf("qualify Scheduler cron %d before certification: %w", id, err)
			}
		} else if c, ok := maintenancetask.QualifySite(id, site, path, strategy, app, dir, expr, cmd, user); ok {
			if err = maintenancetask.BridgeRegistration(ctx, c, filepath.Join(dataDir, "fluxo.db")); err != nil {
				return fmt.Errorf("qualify guarded cron %d before certification: %w", id, err)
			}
		} else {
			return fmt.Errorf("custom cron %d requires an explicit supported bridge profile", id)
		}
		if safe, e := processlog.IsSafe(fmt.Sprintf("/var/log/fluxo/cron-%d.log", id)); e != nil || !safe {
			return fmt.Errorf("repair cron %d log safely", id)
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(registered) > 0 {
		return errors.New("retire orphaned guarded registrations through supported reconciliation before certification")
	}
	return nil
}

func validateNginx(ctx context.Context) error {
	if _, err := run(ctx, "/usr/sbin/nginx", "-t"); err != nil {
		return errors.New("Nginx configuration validation failed")
	}
	return nil
}

func checkCertificates(ctx context.Context, q queryer, dataDir string) error {
	// A missing/partial pair on an established installation must never enter
	// fresh TLS generation during bridge qualification.
	cert, key := filepath.Join(dataDir, "tls", "cert.pem"), filepath.Join(dataDir, "tls", "key.pem")
	if os.Getenv("FLUXO_USE_HTTP") != "1" {
		if _, err := tls.LoadX509KeyPair(cert, key); err != nil {
			return errors.New("restore the existing complete panel TLS pair or use the installed HTTP service context")
		}
	}
	rows, err := q.QueryContext(ctx, "SELECT id,cert_path,key_path FROM certificates WHERE active=1")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var cert, key string
		if err = rows.Scan(&id, &cert, &key); err != nil {
			return err
		}
		if _, err = sslservice.InspectCertificateFiles(cert, key); err != nil {
			return fmt.Errorf("verify active certificate %d and its existing key", id)
		}
	}
	return rows.Err()
}

func checkDatabaseAccess(ctx context.Context, q queryer, key []byte) error {
	rows, err := q.QueryContext(ctx, "SELECT id,site_id,engine,name,username,COALESCE(password,'') FROM databases ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, siteID int
		var engine, name, user, stored string
		if err = rows.Scan(&id, &siteID, &engine, &name, &user, &stored); err != nil {
			return err
		}
		pass, e := config.DecryptSecretWithKey(stored, key)
		if e != nil {
			return fmt.Errorf("database %d requires credential key recovery", id)
		}
		databaseOnly := siteID == 0 && user == "fluxo" && pass == ""
		switch engine {
		case "mysql":
			e = mysql.BridgeDatabaseReady(ctx, name, user, databaseOnly)
			if e == nil && pass != "" {
				e = mysql.VerifyDatabaseAccess(name, user, pass)
			}
		case "postgres":
			e = postgres.BridgeDatabaseReady(ctx, name, user, databaseOnly)
			if e == nil && pass != "" {
				e = postgres.VerifyDatabaseAccess(name, user, pass)
			}
		default:
			e = errors.New("unknown engine")
		}
		if e != nil {
			return fmt.Errorf("database %d requires engine-specific credential and grant verification: %w", id, e)
		}
	}
	return rows.Err()
}
