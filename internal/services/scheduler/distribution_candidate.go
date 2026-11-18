package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"fluxo/internal/syscmd"
)

// These identities are immutable. Selection is bounded to the exact reviewed
// host profiles; this unpublished candidate still requires native acceptance.
const jammyCronCandidateID = "ubuntu-jammy-cron-v1"
const resoluteCronCandidateID = "ubuntu-resolute-cron-v1"

// Test executables exercise the exact production qualification path through a
// private test-only fixture. There is no environment or hostname production
// override. Enabling this qualification candidate is not publication approval.
var distributionCronQualificationEnabled = true

// QualifyDistributionCronBackend is strictly read-only. Observing a qualified
// host does not grant permission to persist its identity or publish a trigger.
func QualifyDistributionCronBackend(ctx context.Context, id string) (CronBackendIdentity, error) {
	c, err := distributionCandidate(id)
	if err != nil {
		return legacyHost{}, err
	}
	observation := backendObserver(ctx)
	if observation.Engine != "cron" || observation.State != "active" || observation.Version != c.packageVersion || observation.Architecture != runtime.GOARCH {
		return legacyHost{}, errors.New("distribution cron does not match its sole active reviewed provider")
	}
	before, err := inspectDistributionCronCandidate(ctx, id)
	if err != nil {
		return legacyHost{}, err
	}
	pam, err := inspectPAMCandidate(id, before.PID)
	if err != nil {
		return legacyHost{}, err
	}
	after, err := inspectDistributionCronCandidate(ctx, id)
	if err != nil || before != after {
		return legacyHost{}, errors.New("distribution cron changed while qualifying PAM/NSS inputs")
	}
	// Bind the interpreted inputs and authentic vendor payloads, not the
	// candidate/reporting flags or its human-readable qualification message.
	proof, err := json.Marshal(struct {
		Distribution string              `json:"distribution"`
		Revision     string              `json:"pam_revision"`
		Files        map[string]string   `json:"files"`
		Packages     map[string]string   `json:"packages"`
		Dependencies map[string][]string `json:"elf_dependencies"`
		Identity     map[string]string   `json:"identity"`
	}{before.Configuration, pam.Revision, pam.Files, pam.Packages, pam.Dependencies, pam.Identity})
	if err != nil {
		return legacyHost{}, err
	}
	after.Configuration = hashBytes(proof)
	return after, ValidateBackendIdentity(after)
}

type distributionCronCandidate struct {
	id, release, packageVersion, binary string
	executable                          string
	pamModules                          []string
	files                               map[string]string
	localeTarget                        string
	directories                         map[string][]string
}

func distributionCandidate(id string) (distributionCronCandidate, error) {
	return distributionCandidateForArchitecture(id, runtime.GOARCH)
}

func distributionCandidateForArchitecture(id, architecture string) (distributionCronCandidate, error) {
	if _, err := pamArchitectureFor(architecture); err != nil {
		return distributionCronCandidate{}, err
	}
	c := distributionCronCandidate{id: id, executable: "/usr/sbin/cron", pamModules: []string{"pam_unix.so", "pam_deny.so", "pam_permit.so", "pam_cap.so", "pam_loginuid.so", "pam_env.so", "pam_umask.so", "pam_limits.so", "pam_systemd.so"}, directories: map[string][]string{"/etc/security/limits.d": {}, "/etc/security/pam_env.d": {}}, files: map[string]string{
		"/etc/pam.d/cron":                          "50a7062347b76fe4a429065547f2b53ffe49ac7d896f6b238010a3ee90446a84",
		"/etc/pam.d/common-auth":                   "628197de9e50b6be37421b04a67f07924f515e0b0f4c06aed9fea953d20ed6e6",
		"/etc/pam.d/common-account":                "aa8a63d72e79399b6c51ebe4e9f828c954145a799eb4b8f3224724f51cbb9fac",
		"/etc/pam.d/common-session-noninteractive": "1bd1ef2b6241fc8b3c97bbaf57e5c6038c96c275e30c62ebee6eec15701e2e07",
		"/etc/default/cron":                        "6a8f0b25001017f55a227be357cd418e46f514acd58c069d17a78c3072d9d569",
		"/etc/security/pam_env.conf":               "7038e2676178dcda6a9bdd6b8948af8206af56f5291cb2cda7f563a9904c15bd",
		"/etc/environment":                         "3d88795b8d42b87bbbf91f8d30dae43b6db9ecd4b1eb93a883062352c902ff5d",
	}}
	switch id {
	case cronieLoaderPAMProfileID:
		// This is a separate loader policy, never a new retirement backend.
		// Reuse only the reviewed Noble OS closure; replace every cron-specific
		// input rather than treating Cronie as an alias for /usr/sbin/cron.
		c, err := distributionCandidateForArchitecture(nobleLoaderPAMProfileID, architecture)
		if err != nil {
			return c, err
		}
		c.id, c.packageVersion, c.executable = id, "1.7.1-1", "/usr/sbin/crond"
		c.binary = cronieAdapter.binaries[architecture]
		delete(c.files, "/etc/pam.d/cron")
		delete(c.files, "/etc/default/cron")
		delete(c.files, "/usr/lib/systemd/system/cron.service")
		c.files["/etc/pam.d/cronie"] = "181a408adfc59ee2e769b894ab7ab949b0bd85267abceedc49c515de745d6d6e"
		c.files["/etc/default/cronie"] = "65abfdedbee0d38444aaa896181951108b4a0b2756b859dd7e2566cad232a4dc"
		c.files["/usr/lib/systemd/system/cronie.service"] = "d63def6ac1b4b89c2c7e4535a94a2e9f74a1006c9a19ef7fe9add7e839982627"
		// The pinned pam_access stack has no module arguments. Accept only its
		// exact inert vendor policy and an absent/empty fragment directory.
		c.files["/etc/security/access.conf"] = "f68915c4eb637aacbfa01cf26dc469a73f70acb0495efc4b074ecbc626bcb345"
		c.directories["/etc/security/access.d"] = []string{}
		c.pamModules = append(c.pamModules, "pam_access.so")
		return c, nil
	case nobleLoaderPAMProfileID:
		c.release = "24.04"
		c.packageVersion = "3.0pl1-184ubuntu2"
		c.binary = "6bd8593640af2413bce259fa0affc18dbf149892756ebe805bf316624f8b590f"
		c.localeTarget = "/etc/default/locale"
		c.files["/usr/lib/systemd/system/cron.service"] = "bbcd656457d22e12d8983ed0c253b5bd8cc6b12308fd16d9324400040de68e6f"
		c.files["/etc/security/limits.conf"] = "0adf64deecea56e9ed5c2229db357900218b10b76f7e69830c9d209347896206"
		c.files[c.localeTarget] = "89dd29db91ea608d72b5b4d3d3f5816cc2d3c1dd730741dc41b20ce12f1c2b3b"
	case jammyCronCandidateID:
		c.release = "22.04"
		c.packageVersion = "3.0pl1-137ubuntu3"
		c.binary = "ffc30864da514025c073a29d5afc6705ff8bbe4ecfdbc7917dd674e37b7b1b8a"
		c.localeTarget = "/etc/default/locale"
		c.files["/usr/lib/systemd/system/cron.service"] = "cb16493193f2340eb19705251c90de3b6574b18b77c90a63b0f6326353bbc416"
		c.files["/etc/security/limits.conf"] = "efe8446a3c499818e7c5736c9494f222cb9098e8733aee91508b3631ad053b27"
		c.files[c.localeTarget] = "89dd29db91ea608d72b5b4d3d3f5816cc2d3c1dd730741dc41b20ce12f1c2b3b"
	case resoluteCronCandidateID:
		c.release = "26.04"
		c.packageVersion = "3.0pl1-200ubuntu1"
		c.binary = "c53edb3824d4fd29f2c3178c468422894466baba4585c1244319eb67c3ebf582"
		c.localeTarget = "/etc/locale.conf"
		c.files["/usr/lib/systemd/system/cron.service"] = "bbcd656457d22e12d8983ed0c253b5bd8cc6b12308fd16d9324400040de68e6f"
		c.files["/etc/security/limits.conf"] = "0adf64deecea56e9ed5c2229db357900218b10b76f7e69830c9d209347896206"
		// Stock systemd-owned fragment sets only RLIMIT_CORE. Pin both exact
		// content and directory membership; no other limits fragment is accepted.
		c.files["/etc/security/limits.d/10-coredump-debian.conf"] = "f4ac5b01865241e90f26cfb4e483b04ff80eb231d3b9fe8ad7057b498dd8f58e"
		c.directories["/etc/security/limits.d"] = []string{"10-coredump-debian.conf"}
		c.files[c.localeTarget] = "89dd29db91ea608d72b5b4d3d3f5816cc2d3c1dd730741dc41b20ce12f1c2b3b"
	default:
		return c, errors.New("unknown cron profile")
	}
	if architecture == "arm64" {
		// Native inventory 44318752237; the fixture also downloads the exact
		// version from authenticated Ubuntu APT metadata. These pins do not
		// replace the independent PAM/ELF/runtime closure below.
		c.binary = map[string]string{
			nobleLoaderPAMProfileID: "613eacb4e7b0528b6b643955fdfb877f226d8bd11349dc7c8e84543290dd21de",
			jammyCronCandidateID:    "fd98b9474487c0db9c7b37512f552cbd993cb0df82126fb8aaf486c47837c66a",
			resoluteCronCandidateID: "c2429c30d8eea913470696777be2028da65d2c7074364bd815cf80c339e849dd",
		}[id]
	}
	return c, nil
}

// inspectDistributionCronCandidate binds inspected files and the actual daemon.
// It is NOT full PAM module/dependency qualification or migration certification.
// Its distinct backend identity must not be written using old proof readers.
func inspectDistributionCronCandidate(ctx context.Context, id string) (legacyHost, error) {
	var h legacyHost
	c, err := distributionCandidate(id)
	if err != nil {
		return h, err
	}
	unsupported := func(detail string) (legacyHost, error) {
		return h, fmt.Errorf("%s profile differs at %s; execution is refused", id, detail)
	}
	identities := map[string]string{}
	for path, want := range c.files {
		if err = trustedFile(path); err != nil {
			return unsupported(path)
		}
		got, e := fileDigest(path)
		if e != nil || got != want {
			return unsupported(path)
		}
		identities[path] = got
	}
	if c.localeTarget != "/etc/default/locale" {
		if err = candidateProtectedAlias("/etc/default/locale", c.localeTarget); err != nil {
			return unsupported("locale alias")
		}
		identities["/etc/default/locale"] = "symlink:" + c.localeTarget
	}
	// No unknown limits fragment receives a candidate certificate. PAM module
	// source/dependency and vendor-configuration closure still needs review.
	for dir, expected := range c.directories {
		entries, e := os.ReadDir(dir)
		if os.IsNotExist(e) {
			if len(expected) != 0 {
				return unsupported(dir)
			}
			identities[dir] = "absent"
			continue
		}
		if e != nil || len(entries) != len(expected) || candidateTrustedDirectory(dir) != nil {
			return unsupported(dir)
		}
		for i, entry := range entries {
			if entry.Name() != expected[i] || !entry.Type().IsRegular() {
				return unsupported(dir)
			}
		}
		if len(expected) == 0 {
			identities[dir] = "empty"
		} else {
			names, _ := json.Marshal(expected)
			identities[dir] = string(names)
		}
	}
	if err = trustedFile("/usr/sbin/cron"); err != nil {
		return unsupported("binary")
	}
	if got, e := fileDigest("/usr/sbin/cron"); e != nil || got != c.binary {
		return unsupported("binary")
	}
	version, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/dpkg-query", "-W", "-f=${Version}\t${Architecture}", "cron")
	if err != nil || strings.TrimSpace(version) != c.packageVersion+"\t"+runtime.GOARCH {
		return unsupported("package")
	}
	// Read the trusted vendor release file rather than executing shell content.
	if err = trustedFile("/usr/lib/os-release"); err != nil {
		return unsupported("distribution identity")
	}
	release, err := os.ReadFile("/usr/lib/os-release")
	if err != nil || !strings.Contains("\n"+string(release), "\nID=ubuntu\n") || !strings.Contains("\n"+string(release), "\nVERSION_ID=\""+c.release+"\"\n") {
		return unsupported("distribution identity")
	}
	out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", "cron.service", "--property=Id,MainPID,ControlPID,ControlGroup,Delegate,DropInPaths,ActiveState,SubState,ExecStart,Environment,EnvironmentFiles,User,Group,FragmentPath,KillMode,NeedDaemonReload")
	if err != nil {
		return h, err
	}
	p := properties(out)
	h.PID, err = strconv.Atoi(p["MainPID"])
	if err != nil || h.PID <= 1 || p["ControlPID"] != "0" || p["Id"] != "cron.service" || p["ControlGroup"] != "/system.slice/cron.service" || p["Delegate"] != "no" || p["DropInPaths"] != "" || p["ActiveState"] != "active" || p["SubState"] != "running" || p["Environment"] != "" || p["EnvironmentFiles"] != "/etc/default/cron (ignore_errors=yes)" || p["User"] != "" && p["User"] != "root" || p["Group"] != "" && p["Group"] != "root" || p["KillMode"] != "process" || p["NeedDaemonReload"] != "no" {
		return unsupported("effective unit")
	}
	fragment := p["FragmentPath"]
	if fragment != "/usr/lib/systemd/system/cron.service" {
		if fragment != "/lib/systemd/system/cron.service" || candidateProtectedAlias("/lib", "/usr/lib") != nil {
			return unsupported("unit fragment")
		}
	}
	if !strings.HasPrefix(p["ExecStart"], "{ path=/usr/sbin/cron ; argv[]=/usr/sbin/cron -f -P $EXTRA_OPTS ; ignore_errors=no ;") || strings.Count(p["ExecStart"], "{ path=") != 1 {
		return unsupported("unit command")
	}
	h.Start, err = startKey(h.PID)
	if err != nil {
		return h, err
	}
	h.Boot, err = bootID()
	if err != nil {
		return h, err
	}
	cmd, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", h.PID))
	if err != nil || string(cmd) != "/usr/sbin/cron\x00-f\x00-P\x00" {
		return unsupported("running command")
	}
	if got, e := fileDigest(fmt.Sprintf("/proc/%d/exe", h.PID)); e != nil || got != c.binary {
		return unsupported("running image")
	}
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", h.PID))
	if err != nil || !strings.Contains(string(status), "Threads:\t1\n") || !strings.Contains(string(status), "Uid:\t0\t0\t0\t0\n") || !strings.Contains(string(status), "Gid:\t0\t0\t0\t0\n") {
		return unsupported("running credentials")
	}
	group, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", h.PID))
	if err != nil || strings.TrimSpace(string(group)) != "0::/system.slice/cron.service" {
		return unsupported("running cgroup")
	}
	environment, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", h.PID))
	if err != nil {
		return h, err
	}
	for _, entry := range strings.Split(string(environment), "\x00") {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "LD_") || key == "BASH_ENV" || key == "ENV" || key == "TZ" || key == "CRON_TZ" || key == "EXTRA_OPTS" {
			return unsupported("execution-affecting environment")
		}
	}
	var st syscall.Stat_t
	if err = syscall.Stat("/sys/fs/cgroup/system.slice/cron.service", &st); err != nil {
		return h, err
	}
	h.GroupInode = st.Ino
	h.Backend, h.Group = id, "/system.slice/cron.service"
	identities["binary"], identities["unit"], identities["release"] = c.binary, "cron.service", c.release
	identities["daemon_environment"] = hashBytes(environment)
	encoded, _ := json.Marshal(identities)
	h.Configuration = hashBytes(encoded)
	// Bracket the complete configuration inspection with daemon identity.
	if current, e := startKey(h.PID); e != nil || current != h.Start {
		return unsupported("daemon changed during inspection")
	}
	return h, nil
}

func candidateTrustedDirectory(path string) error {
	for dir := path; ; dir = filepath.Dir(dir) {
		if err := owned(dir, true); err != nil {
			return err
		}
		if dir == "/" {
			return nil
		}
	}
}

func candidateProtectedAlias(path, canonical string) error {
	if err := candidateTrustedDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	var st syscall.Stat_t
	if err := syscall.Lstat(path, &st); err != nil {
		return err
	}
	if st.Uid != 0 || st.Mode&syscall.S_IFMT != syscall.S_IFLNK || st.Nlink != 1 {
		return errors.New("unexpected distribution alias")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != canonical {
		return errors.New("distribution alias points outside its canonical target")
	}
	return nil
}
