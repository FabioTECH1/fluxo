package scheduler

// The standard vendor identity service is a bounded OS trust boundary. It is
// not permission to load an administrator's service, worker selector or module.
// Qualification assumes no administrative PAM/NSS/module/userdb configuration
// changes during a provider proof. Point-in-time checks cannot defeat a root
// administrator who changes inputs transiently between observations.

import (
	"context"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/syscmd"
)

const pamUserdbDaemon = "/usr/lib/systemd/systemd-userdbd"
const pamUserdbWorker = "/usr/lib/systemd/systemd-userwork"

// Fixed system-manager search paths, including transient and generator units.
// canonical records the nearest existing protected parent of every absence;
// the final closure revalidation detects later path creation or replacement.
var pamUserdbUnitDirectories = []string{"/etc/systemd/system.control", "/run/systemd/system.control", "/run/systemd/transient", "/run/systemd/generator.early", "/etc/systemd/system", "/run/systemd/system", "/run/systemd/generator", "/usr/local/lib/systemd/system", "/usr/lib/systemd/system", "/run/systemd/generator.late", "/lib/systemd/system"}

var pamUserdbProperties = []string{"LoadState", "ActiveState", "FragmentPath", "DropInPaths", "NeedDaemonReload", "Transient", "MainPID", "ExecStart", "ExecCondition", "ExecStartPre", "ExecStartPost", "ExecStop", "ExecStopPost", "ExecReload", "Environment", "EnvironmentFiles", "PassEnvironment", "UnsetEnvironment", "RootDirectory", "RootImage", "ExecSearchPath", "BindPaths", "BindReadOnlyPaths", "TemporaryFileSystem", "MountImages", "ExtensionImages", "User", "Group"}

func pamUserdbForbiddenEnvironment(raw string) error {
	for _, entry := range strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == '\x00' }) {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			return errors.New("invalid identity-service environment metadata")
		}
		if strings.HasPrefix(key, "LD_") || strings.HasPrefix(key, "SYSTEMD_NSS_") || strings.HasPrefix(key, "SYSTEMD_USERDB_") || strings.HasPrefix(key, "NSS_") || key == "SYSTEMD_USERWORK_PATH" || key == "SYSTEMD_BYPASS_USERDB" || key == "SYSTEMD_ONLY_USERDB" || key == "GLIBC_TUNABLES" || key == "GCONV_PATH" || key == "LOCPATH" || key == "NLSPATH" {
			// Never include an environment value in errors or public evidence.
			return errors.New("identity service has an unreviewed loader/NSS/worker selector")
		}
	}
	return nil
}

func pamUserdbUnitMetadata(raw, unit string) (map[string]string, error) {
	m := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(raw, "\n"), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" {
			return nil, errors.New("invalid identity-service unit metadata")
		}
		if _, duplicate := m[key]; duplicate {
			return nil, errors.New("duplicate identity-service unit metadata")
		}
		m[key] = value
	}
	required := []string{"LoadState", "ActiveState", "FragmentPath", "DropInPaths", "NeedDaemonReload", "Transient"}
	if unit == "systemd-userdbd.service" && m["LoadState"] == "loaded" {
		required = pamUserdbProperties
	}
	for _, key := range required {
		if _, ok := m[key]; !ok {
			return nil, errors.New("incomplete identity-service unit metadata")
		}
	}
	if m["DropInPaths"] != "" || m["NeedDaemonReload"] != "no" || m["Transient"] != "no" {
		return nil, errors.New("custom or stale identity-service unit configuration")
	}
	for _, key := range []string{"ExecCondition", "ExecStartPre", "ExecStartPost", "ExecStop", "ExecStopPost", "ExecReload", "Environment", "EnvironmentFiles", "PassEnvironment", "UnsetEnvironment", "RootDirectory", "RootImage", "ExecSearchPath", "BindPaths", "BindReadOnlyPaths", "TemporaryFileSystem", "MountImages", "ExtensionImages"} {
		if m[key] != "" {
			return nil, errors.New("identity service has custom hooks, environment or filesystem authority")
		}
	}
	if (m["User"] != "" && m["User"] != "root") || (m["Group"] != "" && m["Group"] != "root") {
		return nil, errors.New("identity service uses an unreviewed credential identity")
	}
	if m["LoadState"] == "not-found" {
		if m["FragmentPath"] != "" || m["ExecStart"] != "" || (m["MainPID"] != "" && m["MainPID"] != "0") || m["ActiveState"] != "inactive" {
			return nil, errors.New("absent identity service retains execution authority")
		}
		return m, nil
	}
	return nil, errors.New("optional userdb manager/worker profile requires separate native qualification")
}

func pamUserdbReadUnit(ctx context.Context, unit string) (string, map[string]string, error) {
	args := []string{"show", unit, "--no-pager"}
	for _, key := range pamUserdbProperties {
		args = append(args, "--property="+key)
	}
	raw, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", args...)
	// systemctl may return nonzero for not-found units but still supplies all
	// requested metadata. Only the strictly parsed absence case may accept it.
	m, parseErr := pamUserdbUnitMetadata(raw, unit)
	if parseErr != nil {
		return "", nil, parseErr
	}
	if err != nil && m["LoadState"] != "not-found" {
		return "", nil, errors.New("cannot inspect standard identity service")
	}
	return raw, m, nil
}

func pamUserdbListeningInodes(raw, path string) ([]string, error) {
	var inodes []string
	seen := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 8 || fields[7] != path {
			continue
		}
		if fields[3] != "00010000" || fields[4] != "0001" || fields[5] != "01" {
			return nil, errors.New("non-listening DynamicUser endpoint")
		}
		if n, e := strconv.ParseUint(fields[6], 10, 64); e != nil || n == 0 || strconv.FormatUint(n, 10) != fields[6] || seen[fields[6]] {
			return nil, errors.New("invalid or duplicate DynamicUser listener identity")
		}
		seen[fields[6]] = true
		inodes = append(inodes, fields[6])
		if len(inodes) > 32 {
			return nil, errors.New("DynamicUser listener set exceeds inspection bound")
		}
	}
	if len(inodes) == 0 {
		return nil, errors.New("DynamicUser listener is not observable")
	}
	slices.Sort(inodes)
	return inodes, nil
}

func pamUserdbAllListenersOwned(inodes []string, targets map[string]bool) error {
	for _, inode := range inodes {
		if !targets["socket:["+inode+"]"] {
			return errors.New("DynamicUser listener is not held by PID1")
		}
	}
	return nil
}

func pamUserdbPID1OwnsListeners(inodes []string) error {
	files, err := os.ReadDir("/proc/1/fd")
	if err != nil {
		return err
	}
	targets := map[string]bool{}
	for _, file := range files {
		if target, e := os.Readlink("/proc/1/fd/" + file.Name()); e == nil {
			targets[target] = true
		}
	}
	return pamUserdbAllListenersOwned(inodes, targets)
}

func (p *pamCandidateInspection) userdbPID1Endpoint() error {
	const endpoint = "/run/systemd/userdb/io.systemd.DynamicUser"
	if _, err := os.Lstat(endpoint); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	raw, err := os.ReadFile("/proc/net/unix")
	if err != nil {
		return err
	}
	inodes, err := pamUserdbListeningInodes(string(raw), endpoint)
	if err != nil {
		return err
	}
	if err = pamUserdbPID1OwnsListeners(inodes); err != nil {
		return err
	}
	// Authentic PID1 is the explicit vendor-OS identity RPC boundary; this
	// does not expand into auditing every service or module on the host.
	path, err := p.canonical("/usr/lib/systemd/systemd")
	if err != nil {
		return err
	}
	image, err := p.read(path, 128<<20)
	if err != nil {
		return err
	}
	if err = p.authenticate(path, image); err != nil {
		return err
	}
	actual, err := fileDigest("/proc/1/exe")
	if err != nil || actual != hashBytes(image) {
		return errors.New("PID1 is not the authentic reviewed vendor manager image")
	}
	after, err := os.ReadFile("/proc/net/unix")
	if err != nil {
		return err
	}
	if now, e := pamUserdbListeningInodes(string(after), endpoint); e != nil || !slices.Equal(now, inodes) {
		return errors.New("DynamicUser listener changed during inspection")
	}
	if err = pamUserdbPID1OwnsListeners(inodes); err != nil {
		return err
	}
	// Native vendor PID1 may hold multiple listening FDs with the same
	// pathname. Inspect the complete bounded set; never pick the first/last
	// inode or accept a set containing a listener outside authenticated PID1.
	p.report.Identity["userdb-dynamic-listener"] = "pid1:" + strings.Join(inodes, ",")
	return nil
}

// The two initially qualified native profiles have no userdbd or userwork.
// Accept only PID1's fixed synchronous DynamicUser identity endpoint; optional
// worker-bearing installations remain untouched and require another profile.
// In particular no environment selector or $ORIGIN build-tree fallback can
// reach userwork while both manager and worker are protected absences.
func (p *pamCandidateInspection) userdbServiceConfiguration(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	managerEnv, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show-environment")
	if err != nil {
		return errors.New("cannot inspect identity-service manager environment")
	}
	if err = pamUserdbForbiddenEnvironment(managerEnv); err != nil {
		return err
	}
	p.report.Identity["userdb-manager-selectors"] = "forbidden-selectors-absent"
	before := map[string]string{}
	var service map[string]string
	var socket map[string]string
	for _, unit := range []string{"systemd-userdbd.service", "systemd-userdbd.socket"} {
		raw, m, e := pamUserdbReadUnit(ctx, unit)
		if e != nil {
			return e
		}
		before[unit] = raw
		if unit == "systemd-userdbd.service" {
			service = m
		} else {
			socket = m
		}
		if m["LoadState"] == "not-found" {
			for _, dir := range pamUserdbUnitDirectories {
				for _, suffix := range []string{"", ".d"} {
					path := dir + "/" + unit + suffix
					if _, e = p.canonical(path); !os.IsNotExist(e) {
						return errors.New("absent identity service has a local unit or drop-in path")
					}
					p.report.Files[path] = "absent"
				}
			}
			continue
		}
		return errors.New("optional userdb manager/worker profile requires separate native qualification")
	}
	if service["LoadState"] != socket["LoadState"] {
		return errors.New("identity-service manager and socket installation disagree")
	}
	if service["LoadState"] == "not-found" {
		if entries, e := os.ReadDir("/run/systemd/userdb"); e == nil {
			for _, entry := range entries {
				if entry.Name() != "io.systemd.DynamicUser" {
					return errors.New("absent identity service has a non-PID1 provider endpoint")
				}
			}
		} else if !os.IsNotExist(e) {
			return e
		}
		if err = p.userdbPID1Endpoint(); err != nil {
			return err
		}
		for _, path := range []string{pamUserdbDaemon, pamUserdbWorker} {
			if _, e := p.canonical(path); !os.IsNotExist(e) {
				return errors.New("absent identity service has an unreviewed executable")
			}
			p.report.Files[path] = "absent"
		}
	} else {
		return errors.New("optional userdb manager/worker profile requires separate native qualification")
	}

	for unit, raw := range before {
		after, _, e := pamUserdbReadUnit(ctx, unit)
		if e != nil || after != raw {
			return errors.New("identity-service configuration changed during inspection")
		}
		p.report.Files["effective:"+unit] = hashBytes([]byte(raw))
	}
	afterEnv, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show-environment")
	if err != nil {
		return errors.New("cannot recheck identity-service manager environment")
	}
	return pamUserdbForbiddenEnvironment(afterEnv)
}
