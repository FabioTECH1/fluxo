package phpupdate

import (
	"context"
	"errors"
	"fluxo/internal/services/octaneruntime"
	"fluxo/internal/syscmd"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func unitProperties(out string) map[string]string {
	p := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			p[k] = v
		}
	}
	return p
}
func serviceCommand(value string) (string, []string, error) {
	if strings.Count(value, "{ path=") != 1 {
		return "", nil, errors.New("unsupported service command")
	}
	path, rest, ok := strings.Cut(strings.TrimPrefix(value, "{ path="), " ; argv[]=")
	if !ok {
		return "", nil, errors.New("unrecognized effective command")
	}
	argv, _, ok := strings.Cut(rest, " ; ignore_errors=")
	if !ok || strings.ContainsAny(argv, "\\\"'\n\r") {
		return "", nil, errors.New("unsupported effective command arguments")
	}
	return path, strings.Fields(argv), nil
}

// An unchanged unit hash is insufficient: only the package-owned runtime and
// the normal generated invocation have a supported activation contract.
func validateUnitDefinition(branch string, c *Consumer, p map[string]string) error {
	return validateUnitDefinitionWithResolver(branch, c, p, resolveLegacyPHPCommand)
}

func validateUnitDefinitionWithResolver(branch string, c *Consumer, p map[string]string, resolve func(string) error) error {
	for _, key := range []string{"DropInPaths", "Environment", "EnvironmentFiles", "ExecStartPre", "ExecStop", "RootDirectory", "RootImage", "ExecSearchPath", "PassEnvironment"} {
		if p[key] != "" {
			return fmt.Errorf("Custom service %s requires manual maintenance (%s)", p["Id"], key)
		}
	}
	path, args, err := serviceCommand(p["ExecStart"])
	if err != nil {
		return err
	}
	if c == nil {
		expected := []string{"/usr/sbin/php-fpm" + branch, "--nodaemonize", "--fpm-config", "/etc/php/" + branch + "/fpm/php-fpm.conf"}
		if path != expected[0] || digest(args) != digest(expected) || p["User"] != "" && p["User"] != "root" || p["Group"] != "" || p["Type"] != "notify" || p["WorkingDirectory"] != "" && p["WorkingDirectory"] != "/" {
			return errors.New("Custom FPM executable or configuration requires manual maintenance")
		}
		for _, item := range []struct{ key, action string }{{"ExecStartPost", "install"}, {"ExecStopPost", "remove"}} {
			if p[item.key] == "" {
				continue
			}
			helper, argv, e := serviceCommand(p[item.key])
			want := []string{"/usr/lib/php/php-fpm-socket-helper", item.action, "/run/php/php-fpm.sock", "/etc/php/" + branch + "/fpm/pool.d/www.conf", strings.ReplaceAll(branch, ".", "")}
			if e != nil || helper != want[0] || digest(argv) != digest(want) {
				return errors.New("Custom FPM lifecycle helper requires manual maintenance")
			}
		}
		return nil
	}
	if p["ExecStartPost"] != "" || p["ExecStopPost"] != "" || p["ExecReload"] != "" {
		return errors.New("Custom worker lifecycle hooks require manual maintenance")
	}
	expected := strings.Fields(c.Command)
	action := map[string]string{"laravel_queue": "queue:work", "laravel_horizon": "horizon", "laravel_octane": "octane:start"}[c.Kind]
	valid := len(expected) >= 3 && action != "" && expected[0] == "php"+branch && expected[1] == "artisan" && expected[2] == action && (c.Kind != "laravel_horizon" || len(expected) == 3)
	if c.Kind == "laravel_octane" {
		valid = c.Command == octaneruntime.Command(branch, c.Port)
	}
	if !valid || strings.ContainsAny(c.Command, "\\\"'\n\r") {
		return errors.New("Unsupported managed worker command")
	}
	if len(args) == 0 || args[0] != "php"+branch && args[0] != "/usr/bin/php"+branch {
		return errors.New("Unsupported managed worker interpreter")
	}
	args[0] = "php" + branch
	if path == "php"+branch {
		if err := resolve(branch); err != nil {
			return err
		}
		path = "/usr/bin/php" + branch
	}
	if path != "/usr/bin/php"+branch || digest(args) != digest(expected) || p["User"] != "fluxo" || p["Group"] != "" || p["Type"] != "simple" || p["WorkingDirectory"] != c.Directory || p["Restart"] != "always" {
		return errors.New("Custom managed worker executable or service definition requires manual maintenance")
	}
	return nil
}

// Legacy generated units used a bare phpX.Y executable. Admit them only when
// systemd's actual default search and the filesystem resolve to our fixed PHP.
func resolveLegacyPHPCommand(branch string) error {
	ctx := context.Background()
	search, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemd-path", "search-binaries-default")
	if err != nil {
		return errors.New("Cannot verify legacy service executable search path")
	}
	search = strings.TrimSpace(search)
	// systemd resolves ExecStart using its compiled search path (or the
	// unit's ExecSearchPath, rejected above), not the manager's PATH. See
	// systemd v255 exec-invoke.c: find_executable_full(..., false, ...).
	expected := "/usr/bin/php" + branch
	target, err := os.Stat(expected)
	if err != nil {
		return err
	}
	if err := rootOwned(expected); err != nil {
		return err
	}
	for _, directory := range strings.Split(search, ":") {
		if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
			return errors.New("Unsupported systemd executable search directory")
		}
		if err := rootOwned(directory); err != nil {
			return err
		}
		candidate := filepath.Join(directory, "php"+branch)
		info, err := os.Stat(candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			continue
		}
		if !os.SameFile(info, target) {
			return errors.New("Legacy managed PHP interpreter is shadowed in systemd's executable search path")
		}
		if err := rootOwned(candidate); err != nil {
			return err
		}
		return nil
	}
	return errors.New("Legacy managed PHP interpreter cannot be resolved safely")
}

func validateUnitProcesses(ctx context.Context, branch string, c *Consumer, u unitSnapshot) error {
	if !u.Active {
		return nil
	}
	if c != nil && c.Kind == "laravel_octane" {
		_, err := inspectOctaneProcesses(ctx, branch, *c, u)
		return err
	}
	expected := "/usr/sbin/php-fpm" + branch
	if c != nil {
		expected = "/usr/bin/php" + branch
	}
	for pid := range u.PIDs {
		exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
		if err != nil {
			return err
		}
		if strings.TrimSuffix(exe, " (deleted)") != expected {
			return fmt.Errorf("Service %s uses an unsupported runtime process (%d)", u.Name, pid)
		}
	}
	environ, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", u.MainPID))
	if err != nil {
		return err
	}
	for _, entry := range strings.Split(string(environ), "\x00") {
		key, value, _ := strings.Cut(entry, "=")
		if value != "" && (key == "PHPRC" || key == "PHP_INI_SCAN_DIR" || key == "LD_PRELOAD" || key == "LD_LIBRARY_PATH") {
			return errors.New("Custom PHP process environment requires manual maintenance")
		}
	}
	if c == nil {
		cmd, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", u.MainPID))
		if err != nil {
			return err
		}
		if strings.TrimRight(string(cmd), "\x00 ") != "php-fpm: master process (/etc/php/"+branch+"/fpm/php-fpm.conf)" {
			return errors.New("Running FPM configuration is not the package-provided configuration")
		}
	}
	if c != nil && (c.Kind == "laravel_queue" || c.Kind == "laravel_horizon") {
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", u.MainPID))
		if err != nil {
			return err
		}
		args := strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")
		if len(args) == 0 {
			return errors.New("Unable to verify consumer process arguments")
		}
		args[0] = "php" + branch
		if digest(args) != digest(strings.Fields(c.Command)) {
			return errors.New("Running consumer invocation differs from its managed command")
		}
		status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", u.MainPID))
		if err != nil {
			return err
		}
		if !queueHasTerminationHandler(string(status)) {
			return errors.New("Consumer has no registered graceful SIGTERM handler; wait for initialization or use manual maintenance")
		}
	}
	return nil
}
func queueHasTerminationHandler(status string) bool {
	for _, line := range strings.Split(status, "\n") {
		if strings.HasPrefix(line, "SigCgt:") {
			mask, err := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "SigCgt:")), 16, 64)
			return err == nil && mask&(1<<14) != 0
		}
	}
	return false
}

func supervisorTerminal(p map[string]string) bool {
	return (p["ActiveState"] == "inactive" || p["ActiveState"] == "failed") && (p["LoadState"] == "loaded" || p["LoadState"] == "not-found") && p["MainPID"] == "0" && p["ControlPID"] == "0"
}
func supervisorProcesses(group string) (bool, error) {
	if group == "" {
		return false, nil
	}
	if !strings.HasPrefix(group, "/") || group == "/" || strings.Contains(group, "..") {
		return true, errors.New("Unrecognized supervisor process group")
	}
	path := filepath.Join("/sys/fs/cgroup", group)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return true, err
	}
	found := false
	err := filepath.WalkDir(path, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "cgroup.procs" {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if len(strings.Fields(string(data))) > 0 {
				found = true
			}
		}
		return nil
	})
	return found, err
}
