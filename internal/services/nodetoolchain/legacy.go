package nodetoolchain

// Legacy migration never makes an APT executable an authenticated official
// release. This receipt is an administrator-approved local rollback baseline.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"fluxo/internal/syscmd"
)

var ErrNotManaged = errors.New("Node.js uses an older package installation; review migration to enable Fluxo updates")

type LegacyFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	Mode   uint32 `json:"mode"`
}
type LegacyLink struct {
	Present bool   `json:"present"`
	Target  string `json:"target,omitempty"`
}
type LegacySource struct {
	Schema             int                   `json:"schema"`
	Kind               string                `json:"kind"`
	Version            string                `json:"version"`
	NPM                string                `json:"npm"`
	PackageVersion     string                `json:"package_version"`
	Architecture       string                `json:"architecture"`
	Node               LegacyFile            `json:"node"`
	Native             []LegacyFile          `json:"native"`
	NPMTreeSHA256      string                `json:"npm_tree_sha256"`
	PackageFilesSHA256 string                `json:"package_files_sha256"`
	Links              map[string]LegacyLink `json:"links"`
	State              []byte                `json:"state"`
	StateMode          uint32                `json:"state_mode"`
	ManagerTreeSHA256  string                `json:"manager_tree_sha256"`
	Managers           map[string]string     `json:"managers"`
	ManagerLinks       map[string]LegacyLink `json:"manager_links"`
}

var nodeSourceRevision = regexp.MustCompile(`^([0-9]+\.[0-9]+\.[0-9]+)-1nodesource1$`)

func decodeLegacyState(data []byte) (managedState, error) {
	var state managedState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return state, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return state, errors.New("Unexpected migration toolchain state data")
	}
	return state, nil
}

func parseLegacyPackage(metadata, architecture string) (string, string, error) {
	parts := strings.Split(strings.TrimSpace(metadata), "\t")
	if len(parts) != 4 || parts[0] != "nodejs" || parts[1] != "installed" || parts[3] != architecture {
		return "", "", errors.New("Unsupported NodeSource package state or architecture")
	}
	match := nodeSourceRevision.FindStringSubmatch(parts[2])
	if match == nil {
		return "", "", errors.New("Only the standard NodeSource APT package layout can be migrated automatically")
	}
	return match[1], parts[2], nil
}

func validLegacySource(source LegacySource) error {
	if source.Schema != 1 || source.Kind != "nodesource-apt-local-baseline" || source.Node.Path != "/usr/bin/node" || !strictReleaseVersion(source.Version) || !strictReleaseVersion(source.NPM) || source.Architecture != runtime.GOARCH {
		return errors.New("Invalid retained legacy Node.js source")
	}
	match := nodeSourceRevision.FindStringSubmatch(source.PackageVersion)
	if match == nil || match[1] != source.Version {
		return errors.New("Invalid legacy package identity")
	}
	for _, hash := range []string{source.Node.SHA256, source.NPMTreeSHA256, source.PackageFilesSHA256, source.ManagerTreeSHA256} {
		if _, err := validatedPinnedChecksum(hash, "legacy runtime"); err != nil {
			return err
		}
	}
	if source.Node.Inode == 0 || os.FileMode(source.Node.Mode).Perm()&0111 == 0 || len(source.Native) == 0 || len(source.Links) != 3 || len(source.Managers) != 4 || len(source.ManagerLinks) != 4 {
		return errors.New("Incomplete legacy runtime receipt")
	}
	for _, name := range []string{"node", "npm", "npx"} {
		link, ok := source.Links[name]
		if !ok || !link.Present && link.Target != "" || link.Present && link.Target != "/usr/bin/"+name {
			return errors.New("Invalid original legacy command link")
		}
	}
	for _, name := range []string{"corepack", "pnpm", "yarn", "bun"} {
		link, ok := source.ManagerLinks[name]
		if !ok || !link.Present || !strings.HasPrefix(link.Target, managedRoot+"/") || filepath.Clean(link.Target) != link.Target {
			return errors.New("Invalid retained package manager command link")
		}
		if !strictReleaseVersion(source.Managers[name]) {
			return errors.New("Invalid retained package manager identity")
		}
	}
	state, err := decodeLegacyState(source.State)
	if err != nil || state.OfficialNode || state.ApprovedNode != nil || !state.Corepack || !state.Bun || source.StateMode&^0777 != 0 || source.StateMode&0133 != 0 {
		return errors.New("Invalid original migration toolchain state")
	}
	return nil
}

func ValidateMigrationVersion(before, after string) error {
	if err := SupportedMajor(before, time.Now()); err != nil {
		return err
	}
	a, ok := parseVersion(before)
	b, valid := parseVersion(after)
	if !ok || !valid || a[0] != b[0] || !versionAtLeast(after, before) {
		return errors.New("Migration cannot downgrade Node.js or change its installed major")
	}
	return nil
}

func legacySafePath(path string, symlink bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("Invalid legacy runtime path")
	}
	for p := path; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || st.Gid != 0 || info.Mode()&os.ModeSymlink != 0 && !(p == path && symlink) || info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("Unsafe legacy runtime ownership or permissions: %s", p)
		}
		if p == "/" {
			return nil
		}
	}
}

func legacyFile(path string) (LegacyFile, error) {
	var r LegacyFile
	if err := legacySafePath(path, false); err != nil {
		return r, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return r, err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return r, err
	}
	st, ok := before.Sys().(*syscall.Stat_t)
	if !ok || !before.Mode().IsRegular() || st.Nlink != 1 {
		return r, errors.New("Unsupported legacy runtime file")
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return r, err
	}
	after, err := f.Stat()
	if err != nil {
		return r, err
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return r, errors.New("Legacy runtime changed during inspection")
	}
	r = LegacyFile{Path: path, SHA256: hex.EncodeToString(h.Sum(nil)), Device: uint64(st.Dev), Inode: st.Ino, Mode: uint32(before.Mode())}
	return r, nil
}

func captureLegacyLink(path string) (LegacyLink, error) {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return LegacyLink{}, legacySafePath(filepath.Dir(path), false)
	} else if err != nil {
		return LegacyLink{}, err
	}
	if err := legacySafePath(path, true); err != nil {
		return LegacyLink{}, err
	}
	target, err := os.Readlink(path)
	if err != nil {
		return LegacyLink{}, fmt.Errorf("Custom command %s requires manual migration", path)
	}
	return LegacyLink{Present: true, Target: target}, nil
}

func legacyJSON(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// InspectLegacyNode accepts only the bounded NodeSource layout used by older
// installers. It does not adopt arbitrary binaries, wrappers, or nvm installs.
func InspectLegacyNode(ctx context.Context) (LegacySource, error) {
	return inspectLegacyNode(ctx, true)
}

func inspectLegacyNode(ctx context.Context, initial bool) (LegacySource, error) {
	s := LegacySource{Schema: 1, Kind: "nodesource-apt-local-baseline", Links: map[string]LegacyLink{}, Managers: map[string]string{}, ManagerLinks: map[string]LegacyLink{}}
	fail := func(err error) (LegacySource, error) {
		return s, fmt.Errorf("Legacy Node.js migration is unavailable: %w", err)
	}
	for _, p := range []string{"/usr/bin/dpkg-query", "/usr/bin/dpkg-divert", "/usr/local/bin", managedStatePath} {
		if err := legacySafePath(p, false); err != nil {
			return fail(err)
		}
	}
	state, err := os.ReadFile(managedStatePath)
	if err != nil {
		return fail(err)
	}
	managed, err := decodeLegacyState(state)
	if err != nil || !managed.Corepack || !managed.Bun {
		return fail(errors.New("Repair the package managers separately before migration"))
	}
	if initial && (managed.ApprovedNode != nil || managed.OfficialNode) {
		return fail(errors.New("Managed Node.js state already exists; review runtime recovery"))
	}
	s.State = state
	stateInfo, err := os.Stat(managedStatePath)
	if err != nil {
		return fail(err)
	}
	s.StateMode = uint32(stateInfo.Mode().Perm())
	if initial {
		if _, e := os.Lstat(filepath.Join(managedNodeRoot, "current")); !os.IsNotExist(e) {
			return fail(errors.New("Managed Node.js selection already exists"))
		}
	}
	metadata, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/dpkg-query", "--admindir=/var/lib/dpkg", "-W", "-f=${Package}\t${db:Status-Status}\t${Version}\t${Architecture}\n", "nodejs")
	if err != nil {
		return fail(errors.New("A fully installed NodeSource nodejs package is required"))
	}
	s.Version, s.PackageVersion, err = parseLegacyPackage(metadata, runtime.GOARCH)
	if err != nil {
		return fail(err)
	}
	s.Architecture = runtime.GOARCH
	if err = SupportedMajor(s.Version, time.Now()); err != nil {
		return fail(err)
	}
	list, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/dpkg-query", "--admindir=/var/lib/dpkg", "-L", "nodejs")
	if err != nil {
		return fail(err)
	}
	files := map[string]bool{}
	for _, p := range strings.Split(strings.TrimSpace(list), "\n") {
		files[p] = true
	}
	for _, p := range []string{"/usr/bin/node", "/usr/bin/npm", "/usr/bin/npx", "/usr/lib/node_modules/npm/package.json", "/usr/lib/node_modules/npm/bin/npm-cli.js", "/usr/lib/node_modules/npm/bin/npx-cli.js"} {
		if !files[p] {
			return fail(fmt.Errorf("The NodeSource package does not own %s", p))
		}
		divert, e := syscmd.Run(ctx, 5*time.Second, "/usr/bin/dpkg-divert", "--admindir=/var/lib/dpkg", "--list", p)
		if e != nil || strings.TrimSpace(divert) != "" {
			return fail(fmt.Errorf("A package diversion prevents migration of %s", p))
		}
	}
	s.PackageFilesSHA256 = legacyJSON(strings.Split(strings.TrimSpace(list), "\n"))
	s.Node, err = legacyFile("/usr/bin/node")
	if err != nil {
		return fail(err)
	}
	if os.FileMode(s.Node.Mode).Perm()&0111 == 0 {
		return fail(errors.New("Legacy Node.js is not executable"))
	}
	for name, relative := range map[string]string{"npm": "../lib/node_modules/npm/bin/npm-cli.js", "npx": "../lib/node_modules/npm/bin/npx-cli.js"} {
		link, e := captureLegacyLink("/usr/bin/" + name)
		if e != nil || !link.Present || link.Target != relative {
			return fail(fmt.Errorf("Unsupported NodeSource %s command link", name))
		}
	}
	const npmRoot = "/usr/lib/node_modules/npm"
	if err = legacySafePath(filepath.Dir(npmRoot), false); err != nil {
		return fail(err)
	}
	if err = validateOwnedTree(npmRoot, 0, 0, true); err != nil {
		return fail(err)
	}
	s.NPMTreeSHA256, err = TreeDigest(npmRoot)
	if err != nil {
		return fail(err)
	}
	var npm struct {
		Version string `json:"version"`
	}
	data, err := os.ReadFile(npmRoot + "/package.json")
	if err != nil || json.Unmarshal(data, &npm) != nil || !strictReleaseVersion(npm.Version) {
		return fail(errors.New("Cannot identify the NodeSource bundled npm"))
	}
	s.NPM = npm.Version
	for _, name := range []string{"npm-cli.js", "npx-cli.js"} {
		b, e := os.ReadFile(npmRoot + "/bin/" + name)
		if e != nil || !strings.HasPrefix(string(b), "#!/usr/bin/env node\n") {
			return fail(errors.New("Unsupported npm executable wrapper"))
		}
	}
	s.Native, err = legacyNativeFiles("/usr/bin/node")
	if err != nil {
		return fail(err)
	}
	if err = validateLegacyLoader(ctx, s.Native); err != nil {
		return fail(err)
	}
	if err = validateOwnedRoots(filepath.Dir(managedRoot), []string{managedRoot}, 0, 0, true); err != nil {
		return fail(err)
	}
	s.ManagerTreeSHA256, err = TreeDigest(managedRoot)
	if err != nil {
		return fail(err)
	}
	for _, name := range []string{"corepack", "pnpm", "yarn", "bun"} {
		p, e := exec.LookPath(name)
		if e != nil || p != "/usr/local/bin/"+name {
			return fail(fmt.Errorf("%s must use Fluxo's installed command", name))
		}
		if e = legacySafePath(p, true); e != nil {
			return fail(e)
		}
		link, e := captureLegacyLink(p)
		if e != nil {
			return fail(e)
		}
		s.ManagerLinks[name] = link
		resolved, e := filepath.EvalSymlinks(p)
		if e != nil || !strings.HasPrefix(resolved, managedRoot+"/") {
			return fail(fmt.Errorf("%s is outside the Fluxo toolchain", name))
		}
		s.Managers[name], e = legacyManagerVersion(ctx, name, resolved, "/usr/bin/node")
		if e != nil || s.Managers[name] == "" {
			return fail(fmt.Errorf("%s cannot run; repair package managers separately", name))
		}
	}
	for _, name := range []string{"node", "npm", "npx"} {
		p := "/usr/local/bin/" + name
		link, e := captureLegacyLink(p)
		if e != nil {
			return fail(e)
		}
		if initial && link.Present && link.Target != "/usr/bin/"+name {
			return fail(fmt.Errorf("Custom %s command shadows the APT installation", name))
		}
		s.Links[name] = link
		if initial {
			selected, e := exec.LookPath(name)
			expected := "/usr/bin/" + name
			if link.Present {
				expected = p
			}
			if e != nil || selected != expected {
				return fail(fmt.Errorf("%s is shadowed in Fluxo's executable search path", name))
			}
		}
	}
	if initial {
		node, e := legacyProbe(ctx, "/usr/bin/node", "--version")
		if e != nil || strings.TrimPrefix(node, "v") != s.Version {
			return fail(errors.New("Legacy Node.js identity does not match the package"))
		}
		npm, e := legacyProbe(ctx, "/usr/bin/node", "/usr/lib/node_modules/npm/bin/npm-cli.js", "--version")
		if e != nil || npm != s.NPM {
			return fail(errors.New("Legacy npm identity does not match the package"))
		}
	}
	if initial {
		if err = validLegacySource(s); err != nil {
			return fail(err)
		}
	}
	return s, nil
}

// ValidateLegacySource ignores only Fluxo's migration-controlled links/state;
// all APT bytes, dependencies and package manager contracts stay immutable.
func ValidateLegacySource(ctx context.Context, expected LegacySource) error {
	if err := validLegacySource(expected); err != nil {
		return err
	}
	current, err := inspectLegacyNode(ctx, false)
	if err != nil {
		return err
	}
	current.State, current.StateMode, current.Links = expected.State, expected.StateMode, expected.Links
	if legacyJSON(current) != legacyJSON(expected) {
		return errors.New("The retained APT Node.js source or package managers changed; exact rollback requires operator review")
	}
	return nil
}

func legacyNativeFiles(binary string) ([]LegacyFile, error) {
	libDir := "/usr/lib/x86_64-linux-gnu"
	machine := elf.EM_X86_64
	if runtime.GOARCH == "arm64" {
		libDir = "/usr/lib/aarch64-linux-gnu"
		machine = elf.EM_AARCH64
	} else if runtime.GOARCH != "amd64" {
		return nil, errors.New("Unsupported legacy Node.js architecture")
	}
	seen := map[string]bool{}
	result := []LegacyFile{}
	cache, err := legacyFile("/etc/ld.so.cache")
	if err != nil {
		return nil, err
	}
	result = append(result, cache)
	if data, err := os.ReadFile("/etc/ld.so.preload"); err == nil {
		if len(bytes.TrimSpace(data)) != 0 {
			return nil, errors.New("Custom native library preloads require manual migration")
		}
		receipt, e := legacyFile("/etc/ld.so.preload")
		if e != nil {
			return nil, e
		}
		result = append(result, receipt)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	queue := []string{binary}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if seen[p] {
			continue
		}
		seen[p] = true
		f, err := elf.Open(p)
		if err != nil {
			return nil, err
		}
		if f.Machine != machine || f.Type != elf.ET_DYN && f.Type != elf.ET_EXEC {
			f.Close()
			return nil, errors.New("Legacy runtime has an unexpected ELF architecture")
		}
		libs, err := f.ImportedLibraries()
		if err != nil {
			f.Close()
			return nil, err
		}
		for _, tag := range []elf.DynTag{elf.DT_RPATH, elf.DT_RUNPATH} {
			paths, e := f.DynString(tag)
			if e != nil || len(paths) > 0 {
				f.Close()
				return nil, errors.New("Custom native library search paths require manual migration")
			}
		}
		for _, prog := range f.Progs {
			if prog.Type == elf.PT_INTERP {
				b, e := io.ReadAll(io.LimitReader(prog.Open(), 4096))
				if e != nil {
					f.Close()
					return nil, e
				}
				loader, e := filepath.EvalSymlinks(strings.TrimSuffix(string(b), "\x00"))
				if e != nil || !strings.HasPrefix(loader, libDir+"/") {
					f.Close()
					return nil, errors.New("Unsupported Node.js ELF interpreter")
				}
				queue = append(queue, loader)
			}
		}
		f.Close()
		for _, lib := range libs {
			if filepath.Base(lib) != lib || strings.Contains(lib, "/") {
				return nil, errors.New("Unsupported Node.js native dependency")
			}
			resolved, e := filepath.EvalSymlinks(filepath.Join(libDir, lib))
			if e != nil || !strings.HasPrefix(resolved, libDir+"/") {
				return nil, errors.New("Unsupported Node.js native library path")
			}
			queue = append(queue, resolved)
		}
		if p != binary {
			r, e := legacyFile(p)
			if e != nil {
				return nil, e
			}
			result = append(result, r)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

func validateLegacyLoader(ctx context.Context, files []LegacyFile) error {
	if err := legacySafePath("/usr/bin/ldd", false); err != nil {
		return err
	}
	output, err := legacyProbe(ctx, "/usr/bin/ldd", "/usr/bin/node")
	if err != nil {
		return errors.New("Cannot identify the legacy runtime's actual native libraries")
	}
	expected := map[string]bool{}
	seen := map[string]bool{}
	for _, file := range files {
		if strings.HasPrefix(file.Path, "/usr/lib/") {
			expected[file.Path] = true
		}
	}
	for _, line := range strings.Split(output, "\n") {
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		if parts[0] == "linux-vdso.so.1" {
			continue
		}
		path := parts[0]
		if len(parts) >= 3 && parts[1] == "=>" {
			path = parts[2]
		}
		if !filepath.IsAbs(path) {
			return errors.New("Unresolved or unfamiliar native Node.js library")
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil || !expected[path] {
			return errors.New("Legacy Node.js uses native libraries outside the supported system layout")
		}
		seen[path] = true
	}
	if len(seen) != len(expected) {
		return errors.New("Legacy Node.js native library inventory is incomplete")
	}
	return nil
}

func migrationLinkPaths() []string {
	return []string{filepath.Join(managedNodeRoot, "current"), "/usr/local/bin/node", "/usr/local/bin/npm", "/usr/local/bin/npx"}
}

// ValidateMigrationLinks permits exact original/target mixtures only. The
// caller journals activation intent before each individual durable mutation.
func ValidateMigrationLinks(source LegacySource, target Release) error {
	if err := validLegacySource(source); err != nil {
		return err
	}
	if err := ValidateMigrationVersion(source.Version, target.Version); err != nil {
		return err
	}
	if err := ValidateReleaseActivation(target); err != nil {
		return err
	}
	for _, p := range migrationLinkPaths() {
		actual, err := captureLegacyLink(p)
		if err != nil {
			return err
		}
		original := source.Links[filepath.Base(p)]
		want := LegacyLink{Present: true, Target: filepath.Join(managedNodeRoot, "current", "bin", filepath.Base(p))}
		if filepath.Base(p) == "current" {
			original = LegacyLink{}
			want.Target = filepath.Join(managedNodeRoot, "v"+target.Version)
		}
		if actual != original && actual != want {
			return fmt.Errorf("Migration command path %s changed outside the reviewed operation", p)
		}
	}
	return validateMigrationState(source, target)
}

func validateMigrationState(source LegacySource, target Release) error {
	if err := legacySafePath(managedStatePath, false); err != nil {
		return err
	}
	current, err := os.ReadFile(managedStatePath)
	if err != nil {
		return err
	}
	old, oldErr := decodeLegacyState(source.State)
	now, nowErr := decodeLegacyState(current)
	if oldErr != nil || nowErr != nil || old.ApprovedNode != nil {
		return errors.New("Invalid migration toolchain state")
	}
	// A reviewed keyring rotation may refresh authentication without changing
	// the approved archive/tree. Do not strand a completed migration's recovery.
	approved := now.ApprovedNode
	approvedPayload := approved != nil && approved.Version == target.Version && approved.NPM == target.NPM && approved.Architecture == target.Architecture && approved.Filename == target.Filename && approved.SHA256 == target.SHA256 && approved.TreeSHA256 == target.TreeSHA256
	base := now
	base.OfficialNode = old.OfficialNode
	base.ApprovedNode = old.ApprovedNode
	if legacyJSON(now) != legacyJSON(old) && (!now.OfficialNode || !approvedPayload || legacyJSON(base) != legacyJSON(old)) {
		return errors.New("Toolchain approval changed outside the reviewed migration")
	}
	return nil
}

func SelectMigration(source LegacySource, target Release, before bool, journal func(string) error) error {
	if err := ValidateMigrationLinks(source, target); err != nil {
		return err
	}
	paths := migrationLinkPaths()
	if before {
		paths = append(paths[1:], paths[0])
	}
	for _, p := range paths {
		if err := journal(p); err != nil {
			return err
		}
		if err := ValidateMigrationLinks(source, target); err != nil {
			return err
		}
		want := LegacyLink{Present: true, Target: filepath.Join(managedNodeRoot, "current", "bin", filepath.Base(p))}
		if filepath.Base(p) == "current" {
			want.Target = filepath.Join(managedNodeRoot, "v"+target.Version)
		}
		if before {
			want = source.Links[filepath.Base(p)]
			if filepath.Base(p) == "current" {
				want = LegacyLink{}
			}
		}
		actual, err := captureLegacyLink(p)
		if err != nil {
			return err
		}
		if actual == want {
			continue
		}
		if want.Present {
			if err = replaceManagedSymlink(p, want.Target); err != nil {
				return err
			}
		} else if err = os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err = syncNodeDirectory(filepath.Dir(p)); err != nil {
			return err
		}
	}
	if err := journal(managedStatePath); err != nil {
		return err
	}
	var state managedState
	if err := json.Unmarshal(source.State, &state); err != nil {
		return err
	}
	if !before {
		state.OfficialNode = true
		state.ApprovedNode = &target
		return saveManagedState(state)
	}
	// Restore the exact previous receipt bytes/mode, not a reconstructed state.
	f, err := os.CreateTemp(filepath.Dir(managedStatePath), ".migration-state-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(os.FileMode(source.StateMode)); err == nil {
		_, err = f.Write(source.State)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), managedStatePath); err != nil {
		return err
	}
	return syncNodeDirectory(filepath.Dir(managedStatePath))
}

func ValidateMigrationSelection(ctx context.Context, source LegacySource, target Release, before bool) error {
	if err := ValidateLegacySource(ctx, source); err != nil {
		return err
	}
	if err := ValidateMigrationLinks(source, target); err != nil {
		return err
	}
	for _, p := range migrationLinkPaths() {
		actual, err := captureLegacyLink(p)
		if err != nil {
			return err
		}
		want := LegacyLink{Present: true, Target: filepath.Join(managedNodeRoot, "current", "bin", filepath.Base(p))}
		if filepath.Base(p) == "current" {
			want.Target = filepath.Join(managedNodeRoot, "v"+target.Version)
		}
		if before {
			want = source.Links[filepath.Base(p)]
			if filepath.Base(p) == "current" {
				want = LegacyLink{}
			}
		}
		if actual != want {
			return errors.New("Migration runtime selection is incomplete")
		}
	}
	version, npm := target.Version, target.NPM
	nodePath := filepath.Join(managedNodeRoot, "v"+target.Version, "bin/node")
	npmPath := filepath.Join(managedNodeRoot, "v"+target.Version, "lib/node_modules/npm/bin/npm-cli.js")
	if before {
		version, npm = source.Version, source.NPM
		nodePath, npmPath = "/usr/bin/node", "/usr/lib/node_modules/npm/bin/npm-cli.js"
	}
	actual, e := legacyProbe(ctx, nodePath, "--version")
	if e != nil || strings.TrimPrefix(actual, "v") != version {
		return errors.New("Selected Node.js has not adopted the reviewed migration target")
	}
	actual, e = legacyProbe(ctx, nodePath, npmPath, "--version")
	if e != nil || actual != npm {
		return errors.New("Selected Node.js/npm has not adopted the reviewed migration target")
	}
	for _, name := range []string{"corepack", "pnpm", "yarn", "bun"} {
		resolved, e := filepath.EvalSymlinks("/usr/local/bin/" + name)
		if e != nil {
			return e
		}
		actual, e := legacyManagerVersion(ctx, name, resolved, nodePath)
		if e != nil || actual != source.Managers[name] {
			return fmt.Errorf("%s changed or cannot run with the selected Node.js release", name)
		}
	}
	for _, name := range []string{"node", "npm", "npx"} {
		expected := "/usr/local/bin/" + name
		if before && !source.Links[name].Present {
			expected = "/usr/bin/" + name
		}
		selected, e := exec.LookPath(name)
		if e != nil || selected != expected {
			return fmt.Errorf("%s is shadowed after the migration switch", name)
		}
	}
	return nil
}

func legacyProbe(ctx context.Context, name string, args ...string) (string, error) {
	out, err := syscmd.RunApplicationProbeEnv(ctx, 10*time.Second, "fluxo", fluxoHome, []string{"PATH=/usr/bin:/bin", "COREPACK_HOME=" + managedCorepackHome, "COREPACK_ENABLE_NETWORK=0", "COREPACK_DEFAULT_TO_LATEST=0", "COREPACK_ENABLE_DOWNLOAD_PROMPT=0", "COREPACK_ENABLE_PROJECT_SPEC=0", "COREPACK_ENV_FILE=0"}, name, args...)
	return strings.TrimSpace(out), err
}
func legacyManagerVersion(ctx context.Context, name, resolved, nodePath string) (string, error) {
	if name == "bun" {
		return legacyProbe(ctx, resolved, "--version")
	}
	// Execute the authenticated root-owned JS launcher with the explicit source
	// interpreter. Foreign/partially switched PATH links are never probe inputs.
	return legacyProbe(ctx, nodePath, resolved, "--version")
}
