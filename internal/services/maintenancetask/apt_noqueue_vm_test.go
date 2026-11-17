package maintenancetask

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"fluxo/internal/syscmd"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const noQueuePackage = "fluxo-noqueue-fixture"
const noQueueRootPrefix = "/var/lib/fluxo-apt-noqueue-candidate-"

// These roles exist only in the test image. APT's custom acquisition method is
// intentionally a failing ELF sentinel, not a shell/network implementation.
func init() {
	role := ""
	if filepath.Base(os.Args[0]) == "fluxoprobe" {
		role = "method"
	}
	if len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "apt-noqueue-") {
		role = strings.TrimPrefix(os.Args[1], "apt-noqueue-")
	}
	if role == "" {
		return
	}
	if err := runNoQueueTestRole(role); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(97)
	}
	os.Exit(1) // Every sentinel refuses; none can authorize dpkg or a transfer.
}

func noQueueVMHost() bool {
	host, _ := os.Hostname()
	if os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		return false
	}
	switch host {
	case "fluxo-php-patch-test", "fluxo-bridge-prod-20261101", "fluxo-bridge-noble-final-20261102", "fluxo-bridge-jammy-20261101", "fluxo-bridge-resolute-20261101":
		return true
	}
	return false
}

func runNoQueueTestRole(role string) error {
	if !noQueueVMHost() {
		return errors.New("guarded NoQueue test role only")
	}
	root := os.Getenv("FLUXO_APT_NOQUEUE_ROOT")
	id := strings.TrimPrefix(root, noQueueRootPrefix)
	parsed, err := uuid.Parse(id)
	if err != nil || root != noQueueRootPrefix+parsed.String() {
		return errors.New("invalid private NoQueue fixture identity")
	}
	for _, path := range []string{"/var", "/var/lib", root} {
		i, err := os.Lstat(path)
		if err != nil {
			return err
		}
		st, ok := i.Sys().(*syscall.Stat_t)
		if !ok || !i.IsDir() || st.Uid != 0 || i.Mode().Perm()&0022 != 0 {
			return errors.New("unsafe NoQueue fixture ancestry")
		}
	}
	credential, err := syscmd.ResolveCredential("_apt")
	if err != nil || credential.Uid == 0 {
		return errors.New("missing APT sandbox identity")
	}
	path, expectedUID := filepath.Join(root, "control", role+"-counter"), uint32(0)
	if role == "method" {
		if os.Geteuid() != 0 && uint32(os.Geteuid()) != credential.Uid {
			return errors.New("unexpected method sentinel identity")
		}
		path, expectedUID = filepath.Join(root, "exchange", "method-counter"), credential.Uid
	} else if os.Geteuid() != 0 || role != "refuse" && role != "preinstall" {
		return errors.New("unexpected NoQueue refusal role")
	}
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_APPEND|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Uid != expectedUID || st.Nlink != 1 || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0777 != 0600 {
		return errors.New("unsafe NoQueue sentinel counter")
	}
	_, err = fmt.Fprintln(f, role)
	return err
}

type noQueueFixture struct {
	root, config, guard, archive, index, expectedArchive string
	configDigest, release, aptVersion                    string
	env                                                  []string
	strace                                               bool
	results                                              []map[string]any
}

func newNoQueueFixture(t *testing.T, solver string) *noQueueFixture {
	t.Helper()
	f := &noQueueFixture{root: noQueueRootPrefix + uuid.NewString()}
	if err := os.Mkdir(f.root, 0755); err != nil {
		t.Fatal(err)
	}
	// Registered before any subprocess, including fixture-policy setup. Failed
	// setup must still report every traced write outside the private fixture.
	t.Cleanup(func() { f.checkTraceContainment(t) })
	f.config = filepath.Join(f.root, "etc", "apt.conf")
	f.guard = filepath.Join(f.root, "helpers", "guard")
	f.archive = filepath.Join(f.root, "cache", "archives", noQueuePackage+"_1.0_"+runtime.GOARCH+".deb")
	f.expectedArchive = filepath.Base(f.archive)
	for _, dir := range []string{"etc", "etc/parts", "etc/sources.list.d", "etc/preferences.d", "etc/trusted.gpg.d", "etc/auth.conf.d", "helpers", "methods", "control", "exchange", "tmp", "state", "state/apt", "state/dpkg", "state/dpkg/info", "state/dpkg/updates", "state/dpkg/triggers", "lists", "lists/partial", "cache", "cache/archives", "cache/archives/partial", "logs"} {
		if err := os.Mkdir(filepath.Join(f.root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(f.root, "control"), 0700); err != nil {
		t.Fatal(err)
	}
	credential, err := syscmd.ResolveCredential("_apt")
	if err != nil || credential.Uid == 0 {
		t.Fatal("missing sandbox identity", err)
	}
	for _, dir := range []string{"exchange", "tmp", "lists/partial", "cache/archives/partial"} {
		path := filepath.Join(f.root, dir)
		if err = os.Chown(path, int(credential.Uid), 0); err != nil {
			t.Fatal(err)
		}
		if err = os.Chmod(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	code, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{f.guard, filepath.Join(f.root, "methods", "fluxoprobe")} {
		if err = os.WriteFile(path, code, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"refuse-counter", "preinstall-counter", "script-counter", "trigger-counter"} {
		if err = os.WriteFile(filepath.Join(f.root, "control", name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	methodCounter := filepath.Join(f.root, "exchange", "method-counter")
	if err = os.WriteFile(methodCounter, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(methodCounter, int(credential.Uid), 0); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"main.conf", "preferences", "trusted.gpg", "auth.conf"} {
		if err = os.WriteFile(filepath.Join(f.root, "etc", name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(filepath.Join(f.root, "etc", "sources.list"), []byte("deb [trusted=yes] fluxoprobe://example.invalid/repo stable main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f.env = []string{"APT_CONFIG=" + f.config, "TMPDIR=" + filepath.Join(f.root, "tmp"), "FLUXO_PHP_UPDATE_VM_TEST=1", "FLUXO_APT_NOQUEUE_ROOT=" + f.root}
	config := fmt.Sprintf(`Dir "/";
APT::Architecture "%[2]s";
APT::Architectures { "%[2]s"; };
APT::Sandbox::User "_apt";
Dir::Etc "%[1]s/etc";
Dir::Etc::parts "%[1]s/etc/parts";
Dir::Etc::main "%[1]s/etc/main.conf";
Dir::Etc::sourcelist "%[1]s/etc/sources.list";
Dir::Etc::sourceparts "%[1]s/etc/sources.list.d";
Dir::Etc::preferences "%[1]s/etc/preferences";
Dir::Etc::preferencesparts "%[1]s/etc/preferences.d";
Dir::Etc::trusted "%[1]s/etc/trusted.gpg";
Dir::Etc::trustedparts "%[1]s/etc/trusted.gpg.d";
Dir::Etc::netrc "%[1]s/etc/auth.conf";
Dir::Etc::netrcparts "%[1]s/etc/auth.conf.d";
Dir::State "%[1]s/state/apt";
Dir::State::status "%[1]s/state/dpkg/status";
Dir::State::extended_states "%[1]s/state/apt/extended_states";
Dir::State::lists "%[1]s/lists";
Dir::Cache "%[1]s/cache";
Dir::Cache::archives "%[1]s/cache/archives";
Dir::Cache::pkgcache "%[1]s/cache/pkgcache.bin";
Dir::Cache::srcpkgcache "%[1]s/cache/srcpkgcache.bin";
Dir::Log "%[1]s/logs";
Dir::Log::Terminal "term.log";
Dir::Log::History "history.log";
Dir::Log::Planner "eipp.log.xz";
Dir::Log::Solver "solver.log.xz";
Dir::Bin::dpkg "/usr/bin/dpkg";
Dir::Bin::methods "%[1]s/methods";
Dir::Bin::solvers { "/usr/lib/apt/solvers"; };
Dir::Bin::planners { "/usr/lib/apt/planners"; };
APT::Solver "%[3]s";
APT::Solver::Strict-Pinning "true";
APT::Planner "internal";
DPkg::Chroot-Directory "/";
DPkg::Options { "--admindir=%[1]s/state/dpkg"; "--log=%[1]s/logs/dpkg.log"; };
DPkg::Pre-Invoke { "%[1]s/helpers/guard apt-noqueue-refuse"; };
DPkg::Pre-Install-Pkgs { "%[1]s/helpers/guard apt-noqueue-preinstall"; };
Acquire::Languages "none";
Acquire::GzipIndexes "false";
Acquire::IndexTargets::deb::Packages::KeepCompressed "false";
`, f.root, runtime.GOARCH, solver)
	if err = os.WriteFile(f.config, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	f.configDigest = digest([]byte(config))
	f.index = filepath.Join(f.root, "lists", "example.invalid_repo_dists_stable_main_binary-"+runtime.GOARCH+"_Packages")
	packages := fmt.Sprintf("Package: %s\nVersion: 1.0\nArchitecture: %s\nMaintainer: Fluxo acceptance <nobody@example.invalid>\nInstalled-Size: 1\nPriority: optional\nSection: misc\nFilename: pool/%s\nSize: 128\nSHA256: %s\nDescription: private NoQueue acquisition fixture\n\n", noQueuePackage, runtime.GOARCH, f.expectedArchive, digest(bytes.Repeat([]byte("A"), 128)))
	if err = os.WriteFile(f.index, []byte(packages), 0644); err != nil {
		t.Fatal(err)
	}
	release := fmt.Sprintf("Origin: Fluxo NoQueue Fixture\nLabel: Fluxo NoQueue Fixture\nSuite: stable\nCodename: stable\nDate: %s\nArchitectures: %s\nComponents: main\nSHA256:\n %s %d main/binary-%s/Packages\n", time.Now().UTC().Format(time.RFC1123Z), runtime.GOARCH, digest([]byte(packages)), len(packages), runtime.GOARCH)
	if err = os.WriteFile(filepath.Join(f.root, "lists", "example.invalid_repo_dists_stable_Release"), []byte(release), 0644); err != nil {
		t.Fatal(err)
	}
	f.strace = verifiedNoQueueStrace()
	t.Logf("retained private NoQueue evidence: %s; verified strace available=%v", f.root, f.strace)
	return f
}

func verifiedNoQueueStrace() bool {
	manifest, err := readRegular("/var/lib/dpkg/info/strace.md5sums", 8<<20)
	if err != nil {
		return false
	}
	entries, err := parseProtectedMD5Manifest(manifest)
	if err != nil {
		return false
	}
	code, err := readRegular("/usr/bin/strace", 32<<20)
	if err != nil {
		return false
	}
	sum := md5.Sum(code)
	return entries["usr/bin/strace"] == hex.EncodeToString(sum[:])
}

func (f *noQueueFixture) reset(t *testing.T, status, archive string) {
	t.Helper()
	for _, name := range []string{"refuse-counter", "preinstall-counter", "script-counter", "trigger-counter"} {
		if err := os.WriteFile(filepath.Join(f.root, "control", name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Truncate(filepath.Join(f.root, "exchange", "method-counter"), 0); err != nil {
		t.Fatal(err)
	}
	state := fmt.Sprintf("Package: %s\nStatus: install ok %s\nPriority: optional\nSection: misc\nInstalled-Size: 1\nMaintainer: Fluxo acceptance <nobody@example.invalid>\nArchitecture: %s\nVersion: 1.0\nDescription: private NoQueue fixture\n\n", noQueuePackage, status, runtime.GOARCH)
	for path, raw := range map[string][]byte{
		"state/dpkg/status": []byte(state), "state/dpkg/available": nil, "state/apt/extended_states": nil, "state/dpkg/triggers/Unincorp": nil,
		"state/dpkg/info/" + noQueuePackage + ".list":     []byte(filepath.Join(f.root, "payload") + "\n"),
		"state/dpkg/info/" + noQueuePackage + ".triggers": []byte("interest-noawait fluxo-noqueue-private-trigger\n"),
	} {
		if err := os.WriteFile(filepath.Join(f.root, path), raw, 0644); err != nil {
			t.Fatal(err)
		}
	}
	postinst := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in triggered) echo triggered >> '%s/control/trigger-counter';; *) echo script >> '%s/control/script-counter';; esac\nexit 1\n", f.root, f.root)
	if err := os.WriteFile(filepath.Join(f.root, "state/dpkg/info", noQueuePackage+".postinst"), []byte(postinst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "payload"), []byte("private payload\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(f.archive)
	_ = os.Remove(filepath.Join(f.root, "cache/archives/partial", f.expectedArchive))
	switch archive {
	case "missing":
	case "correct-size-corrupt":
		if err := os.WriteFile(f.archive, bytes.Repeat([]byte("X"), 128), 0644); err != nil {
			t.Fatal(err)
		}
	case "wrong-size-final":
		if err := os.WriteFile(f.archive, []byte("wrong"), 0644); err != nil {
			t.Fatal(err)
		}
	case "oversize-partial":
		if err := os.WriteFile(filepath.Join(f.root, "cache/archives/partial", f.expectedArchive), bytes.Repeat([]byte("X"), 256), 0644); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("unknown archive fixture")
	}
}

func (f *noQueueFixture) packageState(t *testing.T) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, path := range []string{"state/dpkg/status", "state/apt/extended_states", "state/dpkg/info/" + noQueuePackage + ".list", "state/dpkg/info/" + noQueuePackage + ".postinst", "state/dpkg/info/" + noQueuePackage + ".triggers", "payload"} {
		raw, err := os.ReadFile(filepath.Join(f.root, path))
		if err != nil {
			t.Fatal(err)
		}
		result[path] = digest(raw)
	}
	return result
}

func (f *noQueueFixture) counts(t *testing.T) map[string]int {
	t.Helper()
	result := map[string]int{}
	for _, role := range []string{"method", "refuse", "preinstall", "script", "trigger"} {
		path := filepath.Join(f.root, "control", role+"-counter")
		if role == "method" {
			path = filepath.Join(f.root, "exchange", "method-counter")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		result[role] = len(strings.Fields(string(raw)))
	}
	return result
}

func (f *noQueueFixture) run(t *testing.T, label string, noQueue bool, program string, args ...string) (string, error) {
	t.Helper()
	if !noQueue && label != "positive-control" {
		t.Fatal("only the explicit reachability control may omit NoQueue")
	}
	if program != "/usr/bin/apt-get" && program != "/usr/bin/apt-cache" {
		t.Fatal("unreviewed fixture executable")
	}
	config, readErr := readRegular(f.config, 1<<20)
	if readErr != nil || digest(config) != f.configDigest {
		t.Fatal("private fixture configuration changed before execution", readErr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if program == "/usr/bin/apt-get" {
		flag := "true"
		if !noQueue {
			flag = "false"
		}
		args = append([]string{"-o", "Debug::pkgAcqArchive::NoQueue=" + flag, "--no-download"}, args...)
	}
	var output bytes.Buffer
	exe := program
	tracePath := filepath.Join(f.root, "control", "exec-"+label+".log")
	if f.strace {
		args = append([]string{"-f", "-qq", "-yy", "-s", "2048", "-e", "trace=execve,openat,creat,rename,renameat,renameat2,unlink,unlinkat,mkdir,mkdirat,rmdir,link,linkat,symlink,symlinkat", "-o", tracePath, "--", program}, args...)
		exe = "/usr/bin/strace"
	}
	err := syscmd.RunMaintenance(ctx, "", &output, f.env, exe, args...)
	if writeErr := os.WriteFile(filepath.Join(f.root, "control", label+".log"), output.Bytes(), 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	event := map[string]any{"label": label, "method_fence_enabled": noQueue, "program": program, "counters": f.counts(t)}
	if err != nil {
		event["error"] = err.Error()
	}
	encoded, _ := json.MarshalIndent(event, "", "  ")
	if writeErr := os.WriteFile(filepath.Join(f.root, "control", label+"-event.json"), append(encoded, '\n'), 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if f.strace {
		trace, readErr := os.ReadFile(tracePath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.Contains(string(trace), "apt-noqueue-preinstall") || strings.Contains(string(trace), noQueuePackage+".postinst") {
			t.Fatalf("unexpected preinstall/script execution; retained %s", tracePath)
		}
	}
	return output.String(), err
}

var noQueueMutationCall = regexp.MustCompile(`\b(openat|creat|rename|renameat|renameat2|unlink|unlinkat|mkdir|mkdirat|rmdir|link|linkat|symlink|symlinkat)\(`)
var noQueueQuotedPath = regexp.MustCompile(`"([^"\\]*(?:\\.[^"\\]*)*)"`)

func (f *noQueueFixture) checkTraceContainment(t *testing.T) {
	t.Helper()
	if !f.strace {
		return
	}
	entries, err := os.ReadDir(filepath.Join(f.root, "control"))
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Error(err)
		return
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "exec-") || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(f.root, "control", entry.Name()))
		if err != nil {
			t.Error(err)
			continue
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.Contains(line, `execve("/usr/bin/dpkg"`) {
				for _, action := range []string{`"--unpack"`, `"--configure"`, `"--remove"`, `"--purge"`, `"--install"`, `"--triggers-only"`, `"-i"`, `"-r"`, `"-P"`} {
					if strings.Contains(line, action) {
						t.Errorf("unexpected mutating dpkg child in %s: %s", entry.Name(), line)
					}
				}
			}
			match := noQueueMutationCall.FindStringSubmatchIndex(line)
			if match == nil {
				continue
			}
			call := line[match[2]:match[3]]
			if call == "openat" && !strings.Contains(line, "O_WRONLY") && !strings.Contains(line, "O_RDWR") && !strings.Contains(line, "O_CREAT") && !strings.Contains(line, "O_TRUNC") && !strings.Contains(line, "O_APPEND") {
				continue
			}
			for _, quoted := range noQueueQuotedPath.FindAllStringSubmatch(line[match[0]:], -1) {
				path := quoted[1]
				if path == "/dev/null" || path == f.root || strings.HasPrefix(path, f.root+"/") {
					continue
				}
				// -yy annotates private directory descriptors for relative
				// unlink/open operations. Unknown cwd-relative writes fail closed.
				if !filepath.IsAbs(path) && strings.Contains(line, "<"+f.root+"/") {
					continue
				}
				t.Errorf("filesystem write escaped the private fixture in %s: %s", entry.Name(), line)
			}
		}
	}
}

func noQueueRootEntries(t *testing.T) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]bool{}
	for _, entry := range entries {
		result[entry.Name()] = true
	}
	return result
}

func noQueueHostCacheMetadata() (map[string]string, error) {
	result := map[string]string{}
	for _, dir := range []string{"/var/cache/apt/archives", "/var/cache/apt/archives/partial"} {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			result[dir] = "absent"
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			info, err := os.Lstat(path)
			if err != nil {
				return nil, err
			}
			st, ok := info.Sys().(*syscall.Stat_t)
			if !ok {
				return nil, errors.New("missing cache metadata")
			}
			result[path] = fmt.Sprintf("%d:%d:%o:%d:%d:%d", st.Uid, st.Gid, info.Mode(), st.Ino, info.Size(), info.ModTime().UnixNano())
		}
	}
	return result, nil
}

func noQueueEqualMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestVMAPTNoQueueArchiveFence never changes production APT configuration,
// package databases or caches. A failing first dpkg hook is mandatory, and even
// a broken guard could affect only this fake package's private admin/files list.
// This proves the archive constructor fence; solver-3 is a separate contract.
func TestVMAPTNoQueueArchiveFence(t *testing.T) {
	testVMAPTNoQueueArchiveFence(t, "internal")
}

func TestVMAPTNoQueueStockSolver3(t *testing.T) {
	testVMAPTNoQueueArchiveFence(t, "3.0")
}

func testVMAPTNoQueueArchiveFence(t *testing.T, solver string) {
	if !aptProfileVMGuard() {
		t.Skip("explicit root opt-in on dedicated acceptance VM only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	rootEntries := noQueueRootEntries(t)
	hostCache, cacheErr := noQueueHostCacheMetadata()
	if cacheErr != nil {
		t.Fatal(cacheErr)
	}
	hostState := map[string]string{}
	for _, path := range []string{"/var/lib/dpkg/status", "/var/lib/apt/extended_states"} {
		raw, e := readRegular(path, 128<<20)
		if e != nil && !os.IsNotExist(e) {
			t.Fatal(e)
		}
		if os.IsNotExist(e) {
			hostState[path] = "absent"
		} else {
			hostState[path] = digest(raw)
		}
	}
	var f *noQueueFixture
	var hostInputs aptInputManifest
	// This finally path runs even when policy/index setup fails. It never
	// deletes unexpected host files; exact artifacts remain for investigation.
	t.Cleanup(func() {
		currentCache, cacheErr := noQueueHostCacheMetadata()
		if cacheErr != nil {
			t.Error(cacheErr)
		} else if !noQueueEqualMaps(hostCache, currentCache) {
			t.Error("host archive-cache metadata changed during private probe")
		}
		if hostInputs.spec.inputRevision != "" {
			if err := hostInputs.verifyAndRun(diskAPTInputSource(), func() error { return nil }); err != nil {
				t.Error("host configuration changed during private probe", err)
			}
		}
		for path, want := range hostState {
			raw, e := readRegular(path, 128<<20)
			got := digest(raw)
			if os.IsNotExist(e) {
				got = "absent"
			} else if e != nil {
				t.Error(e)
				continue
			}
			if got != want {
				t.Error("host package metadata changed during private probe", path)
			}
		}
		entries, err := os.ReadDir("/")
		if err != nil {
			t.Error(err)
		} else {
			for _, entry := range entries {
				if !rootEntries[entry.Name()] {
					t.Errorf("unexpected host root entry appeared during private probe: /%s", entry.Name())
				}
			}
		}
		if f != nil {
			status := "PASS"
			if t.Failed() {
				status = "FAIL"
			}
			result := map[string]any{"status": status, "platform": f.release, "apt_version": f.aptVersion, "production_enabled": false, "verified_strace": f.strace, "solver_selector": solver, "solver3_qualification": "private fixture only; no production authority", "cases": f.results, "unconditional_host_checks_completed": true}
			raw, _ := json.MarshalIndent(result, "", "  ")
			if err = os.WriteFile(filepath.Join(f.root, "control", "evidence.json"), append(raw, '\n'), 0600); err != nil {
				t.Error(err)
			}
		}
	})
	p := newProfile(PackageCleanup)
	platform, err := selectAPTPlatform(&p)
	if err != nil {
		t.Fatal(err)
	}
	if currentSolver, e := cleanupAPTSolver(platform.aptVersion); solver == "3.0" && (e != nil || currentSolver != "3.0") {
		t.Skip("stock 3.0 selector requires its exact reviewed APT tuple")
	}
	hostInputs, err = inspectAPTPlatformInputs(diskAPTInputSource(), platform)
	if err != nil {
		t.Fatal(err)
	}
	if err = packageInputs(ctx, &p, "apt"); err != nil {
		t.Fatal(err)
	}
	if err = inspectAPTNativeCodecs(&p); err != nil {
		t.Fatal(err)
	}
	if p.External["package:apt"] != platform.aptVersion {
		t.Fatal("exact reviewed APT build required")
	}
	f = newNoQueueFixture(t, solver)
	f.release, f.aptVersion = platform.release, platform.aptVersion
	f.reset(t, "installed", "missing")
	policy, err := f.run(t, "fixture-policy", true, "/usr/bin/apt-cache", "policy", noQueuePackage)
	if err != nil || !strings.Contains(policy, "Candidate: 1.0") {
		t.Fatalf("private index did not establish the candidate: %v\n%s", err, policy)
	}
	before := f.packageState(t)
	_, err = f.run(t, "positive-control", false, "/usr/bin/apt-get", "--reinstall", "install", noQueuePackage, "-y")
	counts := f.counts(t)
	if err == nil || counts["method"] == 0 || counts["refuse"] != 0 || counts["preinstall"] != 0 || !noQueueEqualMaps(before, f.packageState(t)) {
		t.Fatalf("method reachability control failed: %v counts=%v; retained %s", err, counts, f.root)
	}
	f.results = append(f.results, map[string]any{"case": "positive-control", "method_reached": true, "guard_reached": false})
	for _, archive := range []string{"missing", "correct-size-corrupt", "wrong-size-final", "oversize-partial"} {
		t.Run("reinstall/"+archive, func(t *testing.T) {
			f.reset(t, "installed", archive)
			before := f.packageState(t)
			sim, err := f.run(t, "reinstall-"+archive+"-simulate", true, "/usr/bin/apt-get", "--simulate", "--reinstall", "install", noQueuePackage, "-y")
			if err != nil || !strings.Contains(sim, "Inst "+noQueuePackage) {
				t.Fatalf("private reinstall simulation failed: %v\n%s", err, sim)
			}
			if c := f.counts(t); c["method"] != 0 || c["refuse"] != 0 || c["preinstall"] != 0 {
				t.Fatal("simulation crossed archive/guard boundary", c)
			}
			_, err = f.run(t, "reinstall-"+archive+"-real", true, "/usr/bin/apt-get", "--reinstall", "install", noQueuePackage, "-y")
			c := f.counts(t)
			if err == nil || c["method"] != 0 || c["refuse"] == 0 || c["preinstall"] != 0 || c["script"] != 0 || c["trigger"] != 0 || !noQueueEqualMaps(before, f.packageState(t)) {
				t.Fatalf("NoQueue/first-guard contract failed: %v counts=%v", err, c)
			}
			if archive == "correct-size-corrupt" {
				if data, e := os.ReadFile(f.archive); e != nil || len(data) != 128 {
					t.Fatal("guarded corrupt same-size archive changed", e)
				}
			}
			if archive == "wrong-size-final" {
				if _, e := os.Stat(f.archive); !os.IsNotExist(e) {
					t.Fatal("wrong-size final archive was not confinedly discarded", e)
				}
			}
			if archive == "oversize-partial" {
				if _, e := os.Stat(filepath.Join(f.root, "cache/archives/partial", f.expectedArchive)); !os.IsNotExist(e) {
					t.Fatal("oversize private partial archive was not discarded", e)
				}
			}
			f.results = append(f.results, map[string]any{"case": "reinstall/" + archive, "counters": c, "private_package_state_unchanged": true})
		})
	}
	for _, operation := range []string{"empty-autoremove", "remove", "configure"} {
		t.Run(operation, func(t *testing.T) {
			status := "installed"
			if operation == "configure" {
				status = "unpacked"
			}
			f.reset(t, status, "missing")
			before := f.packageState(t)
			args := []string{"autoremove", "-y"}
			if operation == "remove" {
				args = []string{"remove", noQueuePackage, "-y"}
			}
			if operation == "configure" {
				args = []string{"install", noQueuePackage, "-y"}
			}
			out, err := f.run(t, operation, true, "/usr/bin/apt-get", args...)
			c := f.counts(t)
			if operation == "empty-autoremove" {
				if err != nil || validateNoPackageChanges(out) != nil || c["refuse"] != 0 {
					t.Fatalf("empty autoremove did not remain empty: %v counts=%v\n%s", err, c, out)
				}
			} else if err == nil || c["refuse"] == 0 {
				t.Fatalf("changed plan did not hit first refusal: %v counts=%v\n%s", err, c, out)
			}
			if c["method"] != 0 || c["preinstall"] != 0 || c["script"] != 0 || c["trigger"] != 0 || !noQueueEqualMaps(before, f.packageState(t)) {
				t.Fatalf("private package/action boundary changed: counts=%v", c)
			}
			f.results = append(f.results, map[string]any{"case": operation, "counters": c, "private_package_state_unchanged": true})
		})
	}
	f.reset(t, "installed", "missing")
	f.checkNativeCodecs(t)
}
