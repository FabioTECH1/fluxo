package nodetoolchain

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"fluxo/internal/syscmd"
)

const (
	MinimumNodeVersion  = "22.13.0"
	managedRoot         = "/opt/fluxo/node-toolchain"
	managedNodeRoot     = "/opt/fluxo/node"
	managedStatePath    = "/var/lib/fluxo/node-toolchain.json"
	toolchainLockPath   = "/var/lib/fluxo/.node-toolchain.lock"
	fluxoHome           = "/home/fluxo"
	managedCorepackHome = "/home/fluxo/.cache/node/corepack"
)

var (
	installMu      sync.Mutex
	versionPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

	// Release builds set these with -ldflags so every installation of a Fluxo
	// release resolves the same published toolchain. Development builds deliberately
	// leave them empty and retain the latest-release fallback.
	PinnedNodeVersion            string
	PinnedNodeAMD64SHA256        string
	PinnedNodeARM64SHA256        string
	PinnedCorepackVersion        string
	PinnedCorepackIntegrity      string
	PinnedPNPMVersion            string
	PinnedPNPMIntegrity          string
	PinnedYarnVersion            string
	PinnedYarnIntegrity          string
	PinnedBunVersion             string
	PinnedBunAMD64SHA256         string
	PinnedBunAMD64BaselineSHA256 string
	PinnedBunARM64SHA256         string
)

type Status struct {
	Installed          bool     `json:"installed"`
	Managed            bool     `json:"managed"`
	ToolchainReady     bool     `json:"toolchain_ready"`
	NodeCompatible     bool     `json:"node_compatible"`
	MinimumNodeVersion string   `json:"minimum_node_version"`
	Binary             string   `json:"binary"`
	Version            string   `json:"version"`
	NPM                string   `json:"npm"`
	PNPM               string   `json:"pnpm"`
	Yarn               string   `json:"yarn"`
	Corepack           string   `json:"corepack"`
	Bun                string   `json:"bun"`
	Missing            []string `json:"missing"`
}

type managedState struct {
	OfficialNode bool `json:"official_node"`
	Corepack     bool `json:"corepack"`
	Bun          bool `json:"bun"`
}

type commandLinkSnapshot struct {
	Existed bool   `json:"existed"`
	Managed bool   `json:"managed"`
	Target  string `json:"target,omitempty"`
}

type installSnapshot struct {
	root  string
	links map[string]commandLinkSnapshot
}

type nodeRelease struct {
	Version string          `json:"version"`
	LTS     json.RawMessage `json:"lts"`
	Files   []string        `json:"files"`
}

func (s managedState) any() bool {
	return s.OfficialNode || s.Corepack || s.Bun
}

func Inspect(ctx context.Context) Status {
	status := Status{MinimumNodeVersion: MinimumNodeVersion}
	if state, err := loadManagedState(); err == nil {
		status.Managed = state.any()
	}
	if !status.Managed {
		for _, path := range []string{managedStatePath, managedNodeRoot, managedRoot} {
			if _, err := os.Stat(path); err == nil {
				status.Managed = true
				break
			}
		}
	}
	status.Binary, _ = exec.LookPath("node")
	status.Version = commandVersion(ctx, "node")
	status.Installed = status.Binary != "" && status.Version != ""
	status.NodeCompatible = status.Installed && versionAtLeast(status.Version, MinimumNodeVersion)
	status.NPM = commandVersion(ctx, "npm")
	status.Corepack = commandVersion(ctx, "corepack")
	status.Bun = commandVersion(ctx, "bun")
	status.PNPM = packageManagerVersion(ctx, "pnpm")
	status.Yarn = packageManagerVersion(ctx, "yarn")

	if !status.Installed {
		status.Missing = append(status.Missing, "Node.js")
	} else if !status.NodeCompatible {
		status.Missing = append(status.Missing, "Node.js "+MinimumNodeVersion+" or newer")
	}
	if status.NPM == "" {
		status.Missing = append(status.Missing, "npm")
	}
	if status.Corepack == "" {
		status.Missing = append(status.Missing, "Corepack")
	}
	if status.PNPM == "" {
		status.Missing = append(status.Missing, "pnpm")
	}
	if status.Yarn == "" {
		status.Missing = append(status.Missing, "Yarn")
	}
	if status.Bun == "" {
		status.Missing = append(status.Missing, "Bun")
	}
	status.ToolchainReady = len(status.Missing) == 0
	return status
}

func Install(ctx context.Context) (Status, error) {
	installMu.Lock()
	defer installMu.Unlock()
	releaseLock, err := acquireToolchainLock(ctx)
	if err != nil {
		return Inspect(ctx), err
	}
	defer releaseLock()

	if _, err := user.Lookup("fluxo"); err != nil {
		return Inspect(ctx), fmt.Errorf("the fluxo system user must exist before installing the Node.js toolchain: %w", err)
	}
	if err := ensurePrerequisites(ctx); err != nil {
		return Inspect(ctx), err
	}
	if err := recoverAbandonedInstallSnapshots(ctx); err != nil {
		return Inspect(ctx), err
	}
	state, err := loadManagedState()
	if err != nil {
		return Status{}, err
	}
	snapshot, err := createInstallSnapshot(ctx)
	if err != nil {
		return Inspect(ctx), err
	}
	committed := false
	defer func() {
		if committed {
			_ = os.RemoveAll(snapshot.root)
		}
	}()
	rollback := func(installErr error) (Status, error) {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if rollbackErr := snapshot.restore(rollbackCtx); rollbackErr != nil {
			return Inspect(context.Background()), fmt.Errorf("%w; the previous Node.js toolchain could not be restored completely: %v (snapshot retained at %s)", installErr, rollbackErr, snapshot.root)
		}
		_ = os.RemoveAll(snapshot.root)
		return Inspect(context.Background()), installErr
	}
	if err := ensureNode(ctx, &state); err != nil {
		return rollback(err)
	}
	if err := ensureCorepackPackageManagers(ctx, &state); err != nil {
		return rollback(err)
	}
	if err := ensureBun(ctx, &state); err != nil {
		return rollback(err)
	}

	status := Inspect(ctx)
	if !status.ToolchainReady {
		return rollback(fmt.Errorf("Node.js toolchain installation is incomplete: missing %s", strings.Join(status.Missing, ", ")))
	}
	committed = true
	return status, nil
}

// RecoverInterruptedInstall restores the durable snapshot left by a process or
// machine crash before the dashboard begins serving runtime operations.
func RecoverInterruptedInstall(ctx context.Context) error {
	installMu.Lock()
	defer installMu.Unlock()
	releaseLock, err := acquireToolchainLock(ctx)
	if err != nil {
		return err
	}
	defer releaseLock()
	return recoverAbandonedInstallSnapshots(ctx)
}

func createInstallSnapshot(ctx context.Context) (*installSnapshot, error) {
	rootParent := filepath.Join(filepath.Dir(managedStatePath), "node-transactions")
	if err := os.MkdirAll(rootParent, 0700); err != nil {
		return nil, fmt.Errorf("create Node.js transaction directory: %w", err)
	}
	root, err := os.MkdirTemp(rootParent, "install-")
	if err != nil {
		return nil, fmt.Errorf("create Node.js transaction snapshot: %w", err)
	}
	if err := os.Chmod(root, 0700); err != nil {
		os.RemoveAll(root)
		return nil, fmt.Errorf("secure Node.js transaction snapshot: %w", err)
	}
	snapshot := &installSnapshot{root: root, links: make(map[string]commandLinkSnapshot)}
	for _, item := range []struct {
		path  string
		label string
	}{
		{managedNodeRoot, "node"},
		{managedRoot, "toolchain"},
		{managedStatePath, "state"},
		{managedCorepackHome, "corepack-home"},
	} {
		info, statErr := os.Lstat(item.path)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			os.RemoveAll(root)
			return nil, fmt.Errorf("inspect %s before Node.js installation: %w", item.path, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			os.RemoveAll(root)
			return nil, fmt.Errorf("refusing to snapshot symlinked managed Node.js path %s", item.path)
		}
		if _, err := syscmd.Run(ctx, 2*time.Minute, "cp", "-a", "--reflink=auto", "--", item.path, filepath.Join(root, item.label)); err != nil {
			os.RemoveAll(root)
			return nil, fmt.Errorf("snapshot %s before Node.js installation: %w", item.path, err)
		}
	}
	for _, name := range managedCommandNames() {
		path := filepath.Join("/usr/local/bin", name)
		info, statErr := os.Lstat(path)
		if os.IsNotExist(statErr) {
			snapshot.links[name] = commandLinkSnapshot{}
			continue
		}
		if statErr != nil {
			os.RemoveAll(root)
			return nil, fmt.Errorf("inspect %s before Node.js installation: %w", path, statErr)
		}
		link := commandLinkSnapshot{Existed: true}
		if info.Mode()&os.ModeSymlink != 0 {
			link.Target, err = os.Readlink(path)
			if err != nil {
				os.RemoveAll(root)
				return nil, fmt.Errorf("read %s before Node.js installation: %w", path, err)
			}
			link.Managed = isManagedPath(link.Target)
		}
		snapshot.links[name] = link
	}
	manifest, err := json.Marshal(snapshot.links)
	if err != nil {
		os.RemoveAll(root)
		return nil, fmt.Errorf("encode Node.js transaction snapshot: %w", err)
	}
	if _, err := syscmd.Run(ctx, 30*time.Second, "sync", "-f", root); err != nil {
		os.RemoveAll(root)
		return nil, fmt.Errorf("flush Node.js transaction snapshot: %w", err)
	}
	if err := writeInstallSnapshotManifest(root, manifest); err != nil {
		os.RemoveAll(root)
		return nil, fmt.Errorf("commit Node.js transaction snapshot: %w", err)
	}
	return snapshot, nil
}

func writeInstallSnapshotManifest(root string, manifest []byte) error {
	path := filepath.Join(root, "links.json")
	tempPath := path + ".tmp"
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(manifest); err != nil {
		file.Close()
		os.Remove(tempPath)
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		os.Remove(tempPath)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
		return err
	}
	dir, err := os.Open(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func recoverAbandonedInstallSnapshots(ctx context.Context) error {
	rootParent := filepath.Join(filepath.Dir(managedStatePath), "node-transactions")
	entries, err := os.ReadDir(rootParent)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect abandoned Node.js transactions: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "install-") {
			continue
		}
		root := filepath.Join(rootParent, entry.Name())
		manifestPath := filepath.Join(root, "links.json")
		manifest, err := os.ReadFile(manifestPath)
		if os.IsNotExist(err) {
			if err := os.RemoveAll(root); err != nil {
				return fmt.Errorf("remove incomplete Node.js transaction snapshot: %w", err)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("read abandoned Node.js transaction snapshot: %w", err)
		}
		links := make(map[string]commandLinkSnapshot)
		if err := json.Unmarshal(manifest, &links); err != nil {
			return fmt.Errorf("decode abandoned Node.js transaction snapshot %s: %w", root, err)
		}
		allowed := make(map[string]bool)
		for _, name := range managedCommandNames() {
			allowed[name] = true
		}
		if len(links) != len(allowed) {
			return fmt.Errorf("abandoned Node.js transaction snapshot %s has incomplete command metadata", root)
		}
		for name, link := range links {
			if !allowed[name] || (link.Managed && !isManagedPath(link.Target)) {
				return fmt.Errorf("abandoned Node.js transaction snapshot %s contains invalid command metadata", root)
			}
		}
		snapshot := &installSnapshot{root: root, links: links}
		if err := snapshot.restore(ctx); err != nil {
			return fmt.Errorf("recover abandoned Node.js transaction %s: %w", root, err)
		}
		if err := os.RemoveAll(root); err != nil {
			return fmt.Errorf("remove recovered Node.js transaction snapshot: %w", err)
		}
	}
	return nil
}

func (snapshot *installSnapshot) restore(ctx context.Context) error {
	var restoreErrors []string
	for _, item := range []struct {
		path  string
		label string
	}{
		{managedNodeRoot, "node"},
		{managedRoot, "toolchain"},
		{managedStatePath, "state"},
		{managedCorepackHome, "corepack-home"},
	} {
		if err := os.RemoveAll(item.path); err != nil {
			restoreErrors = append(restoreErrors, fmt.Sprintf("remove candidate %s: %v", item.path, err))
			continue
		}
		backup := filepath.Join(snapshot.root, item.label)
		if _, err := os.Lstat(backup); os.IsNotExist(err) {
			continue
		} else if err != nil {
			restoreErrors = append(restoreErrors, fmt.Sprintf("inspect snapshot %s: %v", backup, err))
			continue
		}
		if err := os.MkdirAll(filepath.Dir(item.path), 0755); err != nil {
			restoreErrors = append(restoreErrors, fmt.Sprintf("prepare restore path %s: %v", item.path, err))
			continue
		}
		if _, err := syscmd.Run(ctx, 2*time.Minute, "cp", "-a", "--reflink=auto", "--", backup, item.path); err != nil {
			restoreErrors = append(restoreErrors, fmt.Sprintf("restore %s: %v", item.path, err))
		}
	}
	for name, original := range snapshot.links {
		path := filepath.Join("/usr/local/bin", name)
		currentManaged := false
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			if target, readErr := os.Readlink(path); readErr == nil {
				currentManaged = isManagedPath(target)
			}
		}
		if original.Managed {
			if !currentManaged {
				if _, err := os.Lstat(path); err == nil {
					restoreErrors = append(restoreErrors, fmt.Sprintf("refusing to overwrite externally replaced %s", path))
					continue
				}
			}
			_ = os.Remove(path)
			if err := os.Symlink(original.Target, path); err != nil {
				restoreErrors = append(restoreErrors, fmt.Sprintf("restore %s: %v", path, err))
			}
		} else if !original.Existed && currentManaged {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				restoreErrors = append(restoreErrors, fmt.Sprintf("remove candidate %s: %v", path, err))
			}
		}
	}
	if len(restoreErrors) > 0 {
		return errors.New(strings.Join(restoreErrors, "; "))
	}
	for _, path := range []string{filepath.Dir(managedNodeRoot), filepath.Dir(managedStatePath), fluxoHome} {
		if _, err := syscmd.Run(ctx, 30*time.Second, "sync", "-f", path); err != nil {
			return fmt.Errorf("flush restored Node.js toolchain at %s: %w", path, err)
		}
	}
	return nil
}

func managedCommandNames() []string {
	return []string{"node", "npm", "npx", "corepack", "pnpm", "pnpx", "yarn", "yarnpkg", "bun", "bunx"}
}

func Remove(ctx context.Context) error {
	installMu.Lock()
	defer installMu.Unlock()
	releaseLock, err := acquireToolchainLock(ctx)
	if err != nil {
		return err
	}
	defer releaseLock()

	state, err := loadManagedState()
	if err != nil {
		return err
	}
	if !state.any() {
		managedFilesExist := false
		for _, path := range []string{managedStatePath, managedNodeRoot, managedRoot} {
			if _, statErr := os.Stat(path); statErr == nil {
				managedFilesExist = true
				break
			}
		}
		if !managedFilesExist {
			return fmt.Errorf("no Fluxo-managed Node.js toolchain is installed")
		}
	}

	for _, name := range managedCommandNames() {
		if err := removeManagedLink(filepath.Join("/usr/local/bin", name)); err != nil {
			return err
		}
	}

	if err := os.RemoveAll(managedNodeRoot); err != nil {
		return fmt.Errorf("remove managed Node.js files: %w", err)
	}
	if err := os.RemoveAll(managedRoot); err != nil {
		return fmt.Errorf("remove managed Node.js toolchain files: %w", err)
	}

	// Dependency and package-manager caches belong to the fluxo account, not to
	// the managed runtime. They may be shared with externally installed tools or
	// surviving application files, so uninstalling Fluxo's binaries preserves them.
	if err := os.Remove(managedStatePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove Node.js toolchain state: %w", err)
	}
	return nil
}

func acquireToolchainLock(ctx context.Context) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(toolchainLockPath), 0700); err != nil {
		return nil, fmt.Errorf("create Node.js toolchain lock directory: %w", err)
	}
	file, err := os.OpenFile(toolchainLockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open Node.js toolchain lock: %w", err)
	}
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
				_ = file.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = file.Close()
			return nil, fmt.Errorf("lock Node.js toolchain: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, fmt.Errorf("wait for Node.js toolchain lock: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func ensurePrerequisites(ctx context.Context) error {
	if _, err := syscmd.Run(ctx, 5*time.Minute, "apt-get", "update"); err != nil {
		return fmt.Errorf("update package lists: %w", err)
	}
	if _, err := syscmd.Run(ctx, 5*time.Minute, "apt-get", "install", "-y", "ca-certificates", "xz-utils", "unzip"); err != nil {
		return fmt.Errorf("install Node.js prerequisites: %w", err)
	}
	return nil
}

func ensureNode(ctx context.Context, state *managedState) error {
	pinnedVersion, err := validatedPinnedVersion(PinnedNodeVersion, "Node.js")
	if err != nil {
		return err
	}
	currentVersion := commandVersion(ctx, "node")
	if versionAtLeast(currentVersion, MinimumNodeVersion) && commandVersion(ctx, "npm") != "" &&
		(!state.OfficialNode || pinnedVersion == "" || versionsEqual(currentVersion, pinnedVersion)) {
		return nil
	}
	if err := installOfficialNodeLTS(ctx); err != nil {
		return err
	}
	state.OfficialNode = true
	if err := saveManagedState(*state); err != nil {
		return err
	}
	installedVersion := commandVersion(ctx, "node")
	if !versionAtLeast(installedVersion, MinimumNodeVersion) {
		path, _ := exec.LookPath("node")
		return fmt.Errorf("Node.js %s at %s takes precedence over Fluxo's managed LTS release; upgrade or remove that external installation", installedVersion, path)
	}
	if pinnedVersion != "" && !versionsEqual(installedVersion, pinnedVersion) {
		path, _ := exec.LookPath("node")
		return fmt.Errorf("Node.js %s at %s takes precedence over Fluxo's pinned managed release %s; remove or relocate that external installation", installedVersion, path, pinnedVersion)
	}
	if commandVersion(ctx, "npm") == "" {
		return fmt.Errorf("npm is unavailable after installing the Fluxo-managed Node.js LTS release")
	}
	return nil
}

func installOfficialNodeLTS(ctx context.Context) error {
	arch, err := nodeArchitecture()
	if err != nil {
		return err
	}
	version, err := validatedPinnedVersion(PinnedNodeVersion, "Node.js")
	if err != nil {
		return err
	}
	if version == "" {
		version, err = latestNodeLTS(ctx, arch)
		if err != nil {
			return err
		}
	}
	baseURL := "https://nodejs.org/dist/v" + version
	filename := "node-v" + version + "-linux-" + arch + ".tar.xz"
	expectedChecksum := ""
	if PinnedNodeVersion != "" {
		expectedChecksum, err = pinnedNodeChecksum(arch)
		if err != nil {
			return err
		}
	} else {
		sums, downloadErr := downloadText(ctx, baseURL+"/SHASUMS256.txt", 1<<20)
		if downloadErr != nil {
			return fmt.Errorf("download Node.js checksums: %w", downloadErr)
		}
		expectedChecksum, err = selectChecksum(sums, filename)
		if err != nil {
			return fmt.Errorf("select Node.js release: %w", err)
		}
	}

	tempDir, err := os.MkdirTemp("", "fluxo-node-install-")
	if err != nil {
		return fmt.Errorf("create Node.js installation directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	archivePath := filepath.Join(tempDir, filename)
	if err := downloadFile(ctx, baseURL+"/"+filename, archivePath, 150<<20); err != nil {
		return fmt.Errorf("download Node.js: %w", err)
	}
	if err := verifyFileChecksum(archivePath, expectedChecksum); err != nil {
		return fmt.Errorf("verify Node.js download: %w", err)
	}
	if err := os.MkdirAll(managedNodeRoot, 0755); err != nil {
		return fmt.Errorf("create managed Node.js directory: %w", err)
	}
	staleStagingRoots, err := filepath.Glob(filepath.Join(managedNodeRoot, ".node-release-*"))
	if err != nil {
		return fmt.Errorf("find stale Node.js release staging directories: %w", err)
	}
	for _, staleStagingRoot := range staleStagingRoots {
		if err := os.RemoveAll(staleStagingRoot); err != nil {
			return fmt.Errorf("remove stale Node.js release staging directory: %w", err)
		}
	}
	stagingRoot, err := os.MkdirTemp(managedNodeRoot, ".node-release-")
	if err != nil {
		return fmt.Errorf("create Node.js release staging directory: %w", err)
	}
	defer os.RemoveAll(stagingRoot)
	if _, err := syscmd.Run(ctx, 2*time.Minute, "tar", "--extract", "--xz", "--file", archivePath, "--directory", stagingRoot, "--no-same-owner"); err != nil {
		return fmt.Errorf("extract Node.js: %w", err)
	}

	rootName := strings.TrimSuffix(filename, ".tar.xz")
	extractedRoot := filepath.Join(stagingRoot, rootName)
	if info, err := os.Stat(filepath.Join(extractedRoot, "bin", "node")); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("Node.js archive did not contain the expected executable")
	}
	if !versionPattern.MatchString(version) {
		return fmt.Errorf("invalid Node.js release version %q", version)
	}

	versionRoot := filepath.Join(managedNodeRoot, "v"+version)
	if _, err := os.Stat(versionRoot); os.IsNotExist(err) {
		if err := os.Rename(extractedRoot, versionRoot); err != nil {
			return fmt.Errorf("install Node.js release: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("inspect managed Node.js release: %w", err)
	} else if !nodeReleaseUsable(ctx, versionRoot, version) {
		if err := os.RemoveAll(versionRoot); err != nil {
			return fmt.Errorf("remove incomplete Node.js release: %w", err)
		}
		if err := os.Rename(extractedRoot, versionRoot); err != nil {
			return fmt.Errorf("replace incomplete Node.js release: %w", err)
		}
	}
	if err := replaceManagedSymlink(filepath.Join(managedNodeRoot, "current"), versionRoot); err != nil {
		return err
	}
	for _, name := range []string{"node", "npm", "npx"} {
		if err := ensureCommandLink(name, filepath.Join(managedNodeRoot, "current", "bin", name)); err != nil {
			return err
		}
	}
	return nil
}

func nodeReleaseUsable(ctx context.Context, root, minimumVersion string) bool {
	nodePath := filepath.Join(root, "bin", "node")
	if !versionAtLeast(commandVersionAtPath(ctx, nodePath), minimumVersion) {
		return false
	}
	for _, script := range []string{"npm-cli.js", "npx-cli.js"} {
		scriptPath := filepath.Join(root, "lib", "node_modules", "npm", "bin", script)
		info, err := os.Stat(scriptPath)
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
		out, err := syscmd.Run(ctx, 5*time.Second, nodePath, scriptPath, "--version")
		if err != nil || strings.TrimSpace(out) == "" {
			return false
		}
	}
	return true
}

func latestNodeLTS(ctx context.Context, arch string) (string, error) {
	contents, err := downloadText(ctx, "https://nodejs.org/dist/index.json", 5<<20)
	if err != nil {
		return "", fmt.Errorf("download Node.js release index: %w", err)
	}
	return selectLatestNodeLTS(contents, arch)
}

func selectLatestNodeLTS(contents, arch string) (string, error) {
	var releases []nodeRelease
	if err := json.Unmarshal([]byte(contents), &releases); err != nil {
		return "", fmt.Errorf("parse Node.js release index: %w", err)
	}

	var selected string
	for _, release := range releases {
		var ltsName string
		if err := json.Unmarshal(release.LTS, &ltsName); err != nil || ltsName == "" {
			continue
		}
		if !containsString(release.Files, "linux-"+arch) {
			continue
		}
		version := strings.TrimPrefix(release.Version, "v")
		if !versionPattern.MatchString(version) {
			continue
		}
		if selected == "" || versionAtLeast(version, selected) {
			selected = version
		}
	}
	if selected == "" {
		return "", fmt.Errorf("no compatible Node.js LTS release was found for linux-%s", arch)
	}
	return selected, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func ensureCorepackPackageManagers(ctx context.Context, state *managedState) error {
	if err := os.MkdirAll(managedRoot, 0755); err != nil {
		return fmt.Errorf("create Node.js toolchain directory: %w", err)
	}
	npmPath, err := exec.LookPath("npm")
	if err != nil {
		return fmt.Errorf("npm is unavailable after Node.js installation")
	}
	corepackVersion, err := validatedPinnedVersion(PinnedCorepackVersion, "Corepack")
	if err != nil {
		return err
	}
	pnpmVersion, err := validatedPinnedVersion(PinnedPNPMVersion, "pnpm")
	if err != nil {
		return err
	}
	yarnVersion, err := validatedPinnedVersion(PinnedYarnVersion, "Yarn")
	if err != nil {
		return err
	}
	for _, check := range []struct {
		name, spec, integrity string
	}{
		{"Corepack", pinnedPackageSpec("corepack", corepackVersion, "latest"), PinnedCorepackIntegrity},
		{"pnpm", pinnedPackageSpec("pnpm", pnpmVersion, "latest"), PinnedPNPMIntegrity},
		{"Yarn", pinnedPackageSpec("@yarnpkg/cli-dist", yarnVersion, "latest"), PinnedYarnIntegrity},
	} {
		if err := verifyNPMIntegrity(ctx, npmPath, check.name, check.spec, check.integrity); err != nil {
			return err
		}
	}
	corepackSpec := pinnedPackageSpec("corepack", corepackVersion, "latest")
	if _, err := syscmd.Run(ctx, 5*time.Minute, npmPath, "install", "--global", "--prefix", managedRoot, corepackSpec); err != nil {
		return fmt.Errorf("install Corepack: %w", err)
	}
	state.Corepack = true
	if err := saveManagedState(*state); err != nil {
		return err
	}
	for _, name := range []string{"corepack", "pnpm", "pnpx", "yarn", "yarnpkg"} {
		if err := ensureCommandLink(name, filepath.Join(managedRoot, "bin", name)); err != nil {
			return err
		}
	}

	managedCorepack := filepath.Join(managedRoot, "bin", "corepack")
	if currentVersion := commandVersion(ctx, "corepack"); corepackVersion != "" && !versionsEqual(currentVersion, corepackVersion) {
		path, _ := exec.LookPath("corepack")
		return fmt.Errorf("Corepack %s at %s takes precedence over Fluxo's pinned managed release %s; remove or relocate that external installation", currentVersion, path, corepackVersion)
	}
	pnpmSpec := pinnedPackageSpec("pnpm", pnpmVersion, "latest")
	if _, err := syscmd.RunAsUserInDir(ctx, 5*time.Minute, "fluxo", fluxoHome, managedCorepack, "install", "--global", pnpmSpec); err != nil {
		return fmt.Errorf("prepare pnpm: %w", err)
	}
	yarnSpec := pinnedPackageSpec("yarn", yarnVersion, "stable")
	if _, err := syscmd.RunAsUserInDir(ctx, 5*time.Minute, "fluxo", fluxoHome, managedCorepack, "install", "--global", yarnSpec); err != nil {
		return fmt.Errorf("prepare Yarn: %w", err)
	}
	installedPNPM := packageManagerVersion(ctx, "pnpm")
	installedYarn := packageManagerVersion(ctx, "yarn")
	if installedPNPM == "" || installedYarn == "" {
		return fmt.Errorf("Corepack installed but pnpm or Yarn could not be executed")
	}
	if pnpmVersion != "" && !versionsEqual(installedPNPM, pnpmVersion) {
		return fmt.Errorf("pnpm %s takes precedence over Fluxo's pinned managed release %s", installedPNPM, pnpmVersion)
	}
	if yarnVersion != "" && !versionsEqual(installedYarn, yarnVersion) {
		return fmt.Errorf("Yarn %s takes precedence over Fluxo's pinned managed release %s", installedYarn, yarnVersion)
	}
	return nil
}

func ensureBun(ctx context.Context, state *managedState) error {
	pinnedVersion, err := validatedPinnedVersion(PinnedBunVersion, "Bun")
	if err != nil {
		return err
	}
	currentVersion := commandVersion(ctx, "bun")
	if currentVersion != "" && (!state.Bun || pinnedVersion == "" || versionsEqual(currentVersion, pinnedVersion)) {
		return nil
	}
	assetName, err := bunAssetName()
	if err != nil {
		return err
	}
	baseURL := "https://github.com/oven-sh/bun/releases/latest/download"
	if pinnedVersion != "" {
		baseURL = "https://github.com/oven-sh/bun/releases/download/bun-v" + pinnedVersion
	}
	expectedChecksum := ""
	if PinnedBunVersion != "" {
		expectedChecksum, err = pinnedBunChecksum(assetName)
		if err != nil {
			return err
		}
	} else {
		sums, downloadErr := downloadText(ctx, baseURL+"/SHASUMS256.txt", 1<<20)
		if downloadErr != nil {
			return fmt.Errorf("download Bun checksums: %w", downloadErr)
		}
		expectedChecksum, err = selectChecksum(sums, assetName)
		if err != nil {
			return fmt.Errorf("select Bun release: %w", err)
		}
	}

	tempDir, err := os.MkdirTemp("", "fluxo-bun-install-")
	if err != nil {
		return fmt.Errorf("create Bun installation directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	archivePath := filepath.Join(tempDir, assetName)
	if err := downloadFile(ctx, baseURL+"/"+assetName, archivePath, 100<<20); err != nil {
		return fmt.Errorf("download Bun: %w", err)
	}
	if err := verifyFileChecksum(archivePath, expectedChecksum); err != nil {
		return fmt.Errorf("verify Bun download: %w", err)
	}
	if _, err := syscmd.Run(ctx, 2*time.Minute, "unzip", "-q", archivePath, "-d", tempDir); err != nil {
		return fmt.Errorf("extract Bun: %w", err)
	}
	bunPath := filepath.Join(tempDir, strings.TrimSuffix(assetName, ".zip"), "bun")
	info, err := os.Stat(bunPath)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("Bun archive did not contain the expected executable")
	}
	if err := os.MkdirAll(filepath.Join(managedRoot, "bin"), 0755); err != nil {
		return fmt.Errorf("create Bun installation directory: %w", err)
	}
	managedBun := filepath.Join(managedRoot, "bin", "bun")
	if err := copyExecutable(bunPath, managedBun); err != nil {
		return err
	}
	for _, name := range []string{"bun", "bunx"} {
		if err := ensureCommandLink(name, managedBun); err != nil {
			return err
		}
	}
	if installedVersion := commandVersion(ctx, "bun"); pinnedVersion != "" && !versionsEqual(installedVersion, pinnedVersion) {
		path, _ := exec.LookPath("bun")
		return fmt.Errorf("Bun %s at %s takes precedence over Fluxo's pinned managed release %s; remove or relocate that external installation", installedVersion, path, pinnedVersion)
	}
	state.Bun = true
	return saveManagedState(*state)
}

func validatedPinnedVersion(value, tool string) (string, error) {
	version := strings.TrimPrefix(strings.TrimSpace(value), "v")
	if version != "" && !versionPattern.MatchString(version) {
		return "", fmt.Errorf("invalid pinned %s version %q", tool, value)
	}
	return version, nil
}

func pinnedNodeChecksum(arch string) (string, error) {
	checksum := PinnedNodeAMD64SHA256
	if arch == "arm64" {
		checksum = PinnedNodeARM64SHA256
	}
	return validatedPinnedChecksum(checksum, "Node.js "+arch)
}

func pinnedBunChecksum(assetName string) (string, error) {
	checksum := ""
	switch assetName {
	case "bun-linux-x64.zip":
		checksum = PinnedBunAMD64SHA256
	case "bun-linux-x64-baseline.zip":
		checksum = PinnedBunAMD64BaselineSHA256
	case "bun-linux-aarch64.zip":
		checksum = PinnedBunARM64SHA256
	default:
		return "", fmt.Errorf("no pinned checksum is defined for %s", assetName)
	}
	return validatedPinnedChecksum(checksum, assetName)
}

func validatedPinnedChecksum(value, tool string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return "", fmt.Errorf("release metadata has no valid SHA-256 for %s", tool)
	}
	return value, nil
}

func verifyNPMIntegrity(ctx context.Context, npmPath, tool, spec, expected string) error {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		if strings.Contains(spec, "@latest") {
			return nil
		}
		return fmt.Errorf("release metadata has no pinned npm integrity for %s", tool)
	}
	const prefix = "sha512-"
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(expected, prefix))
	if !strings.HasPrefix(expected, prefix) || err != nil || len(decoded) != sha512.Size {
		return fmt.Errorf("release metadata has an invalid npm integrity for %s", tool)
	}
	out, err := syscmd.Run(ctx, 30*time.Second, npmPath, "view", spec, "dist.integrity")
	if err != nil {
		return fmt.Errorf("verify %s package integrity: %w", tool, err)
	}
	if strings.TrimSpace(out) != expected {
		return fmt.Errorf("%s registry integrity does not match the Fluxo release metadata", tool)
	}
	return nil
}

func versionsEqual(left, right string) bool {
	leftParts, leftOK := parseVersion(left)
	rightParts, rightOK := parseVersion(right)
	return leftOK && rightOK && leftParts == rightParts
}

func pinnedPackageSpec(name, pinned, fallbackTag string) string {
	pinned = strings.TrimPrefix(strings.TrimSpace(pinned), "v")
	if pinned == "" {
		return name + "@" + fallbackTag
	}
	return name + "@" + pinned
}

func commandVersion(ctx context.Context, name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	if _, err := user.Lookup("fluxo"); err == nil {
		out, runErr := syscmd.RunAsUserInDir(ctx, 5*time.Second, "fluxo", fluxoHome, path, "--version")
		if runErr != nil {
			return ""
		}
		return strings.TrimSpace(out)
	}
	return commandVersionAtPath(ctx, path)
}

func commandVersionAtPath(ctx context.Context, path string) string {
	out, err := syscmd.Run(ctx, 5*time.Second, path, "--version")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func packageManagerVersion(ctx context.Context, name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	offlineEnv := []string{
		"COREPACK_HOME=" + managedCorepackHome,
		"COREPACK_ENABLE_NETWORK=0",
		"COREPACK_DEFAULT_TO_LATEST=0",
		"COREPACK_ENABLE_DOWNLOAD_PROMPT=0",
		"COREPACK_ENABLE_PROJECT_SPEC=0",
		"COREPACK_ENV_FILE=0",
	}
	if _, err := user.Lookup("fluxo"); err != nil {
		out, runErr := syscmd.RunEnv(ctx, 10*time.Second, offlineEnv, path, "--version")
		if runErr != nil {
			return ""
		}
		return strings.TrimSpace(out)
	}
	out, err := syscmd.RunEnvAsUserInDir(ctx, 10*time.Second, "fluxo", fluxoHome, offlineEnv, path, "--version")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func versionAtLeast(current, minimum string) bool {
	currentParts, ok := parseVersion(current)
	if !ok {
		return false
	}
	minimumParts, ok := parseVersion(minimum)
	if !ok {
		return false
	}
	for i := range currentParts {
		if currentParts[i] != minimumParts[i] {
			return currentParts[i] > minimumParts[i]
		}
	}
	return true
}

func parseVersion(value string) ([3]int, bool) {
	match := versionPattern.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 4 {
		return [3]int{}, false
	}
	var parts [3]int
	for i := 1; i < len(match); i++ {
		part, err := strconv.Atoi(match[i])
		if err != nil {
			return [3]int{}, false
		}
		parts[i-1] = part
	}
	return parts, true
}

func nodeArchitecture() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return "x64", nil
	case "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported Node.js architecture: %s", runtime.GOARCH)
	}
}

func bunAssetName() (string, error) {
	switch runtime.GOARCH {
	case "arm64":
		return "bun-linux-aarch64.zip", nil
	case "amd64":
		data, err := os.ReadFile("/proc/cpuinfo")
		if err != nil || !strings.Contains(strings.ToLower(string(data)), "avx2") {
			return "bun-linux-x64-baseline.zip", nil
		}
		return "bun-linux-x64.zip", nil
	default:
		return "", fmt.Errorf("unsupported Bun architecture: %s", runtime.GOARCH)
	}
}

func selectChecksum(contents, expectedFilename string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(contents))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		filename := strings.TrimPrefix(fields[len(fields)-1], "*")
		if filename == expectedFilename {
			checksum := strings.ToLower(fields[0])
			if len(checksum) != sha256.Size*2 {
				return "", fmt.Errorf("invalid checksum for %s", filename)
			}
			if _, err := hex.DecodeString(checksum); err != nil {
				return "", fmt.Errorf("invalid checksum for %s", filename)
			}
			return checksum, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("checksum for %s was not found", expectedFilename)
}

func downloadText(ctx context.Context, url string, maxBytes int64) (string, error) {
	tempFile, err := os.CreateTemp("", "fluxo-download-")
	if err != nil {
		return "", err
	}
	path := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	defer os.Remove(path)
	if err := downloadFile(ctx, url, path, maxBytes); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func downloadFile(ctx context.Context, url, destination string, maxBytes int64) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "Fluxo-Node-Toolchain")
	client := &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" {
				return errors.New("refusing non-HTTPS download redirect")
			}
			return nil
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxBytes {
		return fmt.Errorf("download exceeds the %d-byte size limit", maxBytes)
	}

	file, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(response.Body, maxBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > maxBytes {
		return fmt.Errorf("download exceeds the %d-byte size limit", maxBytes)
	}
	return nil
}

func verifyFileChecksum(path, expected string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != strings.ToLower(expected) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}

func ensureCommandLink(name, target string) error {
	linkPath := filepath.Join("/usr/local/bin", name)
	if info, err := os.Lstat(linkPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			existingTarget, readErr := os.Readlink(linkPath)
			if readErr != nil {
				return fmt.Errorf("inspect %s: %w", linkPath, readErr)
			}
			if isManagedPath(existingTarget) {
				return replaceManagedSymlink(linkPath, target)
			}
		}
		if resolved, lookErr := exec.LookPath(name); lookErr == nil && resolved == linkPath {
			return nil
		}
		return fmt.Errorf("cannot install %s because %s is not managed by Fluxo", name, linkPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect %s: %w", linkPath, err)
	}
	return replaceManagedSymlink(linkPath, target)
}

func replaceManagedSymlink(linkPath, target string) error {
	if err := os.MkdirAll(filepath.Dir(linkPath), 0755); err != nil {
		return err
	}
	tempLink := linkPath + ".fluxo-new"
	_ = os.Remove(tempLink)
	if err := os.Symlink(target, tempLink); err != nil {
		return fmt.Errorf("create %s symlink: %w", filepath.Base(linkPath), err)
	}
	if err := os.Rename(tempLink, linkPath); err != nil {
		_ = os.Remove(tempLink)
		return fmt.Errorf("activate %s symlink: %w", filepath.Base(linkPath), err)
	}
	return nil
}

func removeManagedLink(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	target, err := os.Readlink(path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if !isManagedPath(target) {
		return nil
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

func isManagedPath(path string) bool {
	cleaned := filepath.Clean(path)
	return cleaned == managedRoot || strings.HasPrefix(cleaned, managedRoot+string(os.PathSeparator)) ||
		cleaned == managedNodeRoot || strings.HasPrefix(cleaned, managedNodeRoot+string(os.PathSeparator))
}

func copyExecutable(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open executable: %w", err)
	}
	defer input.Close()
	tempFile, err := os.CreateTemp(filepath.Dir(destination), ".fluxo-executable-")
	if err != nil {
		return fmt.Errorf("create executable: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	if _, err := io.Copy(tempFile, input); err != nil {
		tempFile.Close()
		return fmt.Errorf("copy executable: %w", err)
	}
	if err := tempFile.Chmod(0755); err != nil {
		tempFile.Close()
		return fmt.Errorf("set executable permissions: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close executable: %w", err)
	}
	if err := os.Rename(tempPath, destination); err != nil {
		return fmt.Errorf("install executable: %w", err)
	}
	return nil
}

func loadManagedState() (managedState, error) {
	data, err := os.ReadFile(managedStatePath)
	if os.IsNotExist(err) {
		return managedState{}, nil
	}
	if err != nil {
		return managedState{}, fmt.Errorf("read Node.js toolchain state: %w", err)
	}
	state, valid := parseManagedState(data)
	if !valid {
		// The reserved installation directories are root-owned and remain a safe
		// source of truth if an interrupted disk write or manual edit corrupts the
		// advisory state marker.
		return inferManagedState(), nil
	}
	return state, nil
}

func parseManagedState(data []byte) (managedState, bool) {
	var state managedState
	if err := json.Unmarshal(data, &state); err != nil {
		return managedState{}, false
	}
	return state, true
}

func inferManagedState() managedState {
	return managedState{
		OfficialNode: pathExists(managedNodeRoot),
		Corepack:     pathExists(filepath.Join(managedRoot, "lib", "node_modules", "corepack")),
		Bun:          pathExists(filepath.Join(managedRoot, "bin", "bun")),
	}
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func saveManagedState(state managedState) error {
	if err := os.MkdirAll(filepath.Dir(managedStatePath), 0700); err != nil {
		return fmt.Errorf("create Node.js toolchain state directory: %w", err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode Node.js toolchain state: %w", err)
	}
	tempFile, err := os.CreateTemp(filepath.Dir(managedStatePath), ".node-toolchain-")
	if err != nil {
		return fmt.Errorf("create Node.js toolchain state: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	if err := tempFile.Chmod(0600); err != nil {
		tempFile.Close()
		return err
	}
	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, managedStatePath); err != nil {
		return fmt.Errorf("save Node.js toolchain state: %w", err)
	}
	return nil
}
