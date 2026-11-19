package nodetoolchain

// Preservation receipts reduce repeated authentication work. They confer no
// runtime authority: updates, activation and recovery retain their own guards.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"time"
)

type toolchainIntegrity struct {
	Schema    int               `json:"schema"`
	Profile   string            `json:"profile"`
	Selection string            `json:"selection"`
	Trees     map[string]string `json:"trees"`
}

func hashJSON(v any) string {
	raw, _ := json.Marshal(v)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

func integrityProfile() (string, error) {
	values := []string{runtime.GOARCH, TrustRevision(), PinnedNodeVersion,
		PinnedNodeAMD64SHA256, PinnedNodeARM64SHA256, PinnedCorepackVersion,
		PinnedCorepackIntegrity, PinnedPNPMVersion, PinnedPNPMIntegrity,
		PinnedYarnVersion, PinnedYarnIntegrity, PinnedBunVersion,
		PinnedBunAMD64SHA256, PinnedBunAMD64BaselineSHA256, PinnedBunARM64SHA256}
	for _, v := range values {
		if v == "" {
			return "", errors.New("verification requires a release-pinned Fluxo binary")
		}
	}
	return hashJSON(values), nil
}

func integritySelection(state managedState) string {
	state.Integrity = nil
	return hashJSON(state)
}

// Read the advisory state strictly before allowing it to carry integrity proof.
func readProtectedManagedState(path string) (managedState, error) {
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if err != nil {
			return managedState{}, err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || st.Uid != 0 || st.Gid != 0 || info.Mode().Perm()&0022 != 0 {
			return managedState{}, fmt.Errorf("unsafe Node.js state parent %s", parent)
		}
		if parent == filepath.Dir(parent) {
			break
		}
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return managedState{}, fmt.Errorf("read protected Node.js state: %w", err)
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return managedState{}, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || st.Uid != 0 || st.Gid != 0 || st.Nlink != 1 || info.Mode().Perm()&0077 != 0 || info.Size() > 65536 {
		return managedState{}, errors.New("Node.js state must be a private root-owned regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(raw) > 65536 {
		return managedState{}, errors.New("cannot read bounded Node.js state")
	}
	var state managedState
	if err := json.Unmarshal(raw, &state); err != nil {
		return state, errors.New("Node.js state is damaged")
	}
	return state, nil
}

func pendingNodeInstall(parent string) error {
	entries, err := os.ReadDir(parent)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "install-") {
			return errors.New("an interrupted Node.js installation needs recovery")
		}
	}
	return nil
}

func selectedNodeRoot(state managedState) (string, error) {
	current := filepath.Join(managedNodeRoot, "current")
	target, err := os.Readlink(current)
	if err != nil {
		return "", fmt.Errorf("managed Node.js selection is missing: %w", err)
	}
	version := strings.TrimPrefix(filepath.Base(target), "v")
	if !strictReleaseVersion(version) || target != filepath.Join(managedNodeRoot, "v"+version) {
		return "", errors.New("managed Node.js selection is unrecognized")
	}
	if err := SupportedMajor(version, time.Now()); err != nil {
		return "", err
	}
	if state.ApprovedNode != nil && (state.ApprovedNode.Version != version || !validRelease(*state.ApprovedNode)) {
		return "", errors.New("Node.js selection differs from its approved release; use Runtime > Node.js recovery")
	}
	return target, nil
}

func preservationTrees(state managedState) (map[string]string, error) {
	if !state.OfficialNode || !state.Corepack || !state.Bun {
		return nil, errors.New("managed Node.js setup is incomplete")
	}
	if err := validateManagedOwnership(state); err != nil {
		return nil, err
	}
	if err := validateManagerCommandLinks(managedRoot); err != nil {
		return nil, err
	}
	root, err := selectedNodeRoot(state)
	if err != nil {
		return nil, err
	}
	for _, name := range managedCommandNames() {
		expected := filepath.Join(managedRoot, "bin", name)
		if name == "node" || name == "npm" || name == "npx" {
			expected = filepath.Join(managedNodeRoot, "current", "bin", name)
		}
		if name == "bunx" {
			expected = filepath.Join(managedRoot, "bin", "bun")
		}
		link := filepath.Join("/usr/local/bin", name)
		target, err := os.Readlink(link)
		if err != nil || target != expected {
			return nil, fmt.Errorf("%s does not select its Fluxo-managed executable", name)
		}
		resolved, err := exec.LookPath(name)
		if err != nil || resolved != link {
			return nil, fmt.Errorf("%s is shadowed in the executable search path", name)
		}
	}
	trees := map[string]string{}
	for _, path := range []string{root, managedRoot, managedCorepackHome} {
		digest, err := TreeDigest(path)
		if err != nil {
			return nil, err
		}
		trees[path] = digest
	}
	if state.ApprovedNode != nil && state.ApprovedNode.TreeSHA256 != trees[root] {
		return nil, errors.New("Node.js payload differs from its approved release; use Runtime > Node.js recovery")
	}
	return trees, nil
}

// Authenticate the actual loader entrypoints as well as the package directory;
// a root-owned alternative script with a matching --version is insufficient.
func validateManagerCommandLinks(root string) error {
	for _, name := range []string{"corepack", "pnpm", "pnpx", "yarn", "yarnpkg"} {
		link := filepath.Join(root, "bin", name)
		info, err := os.Lstat(link)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("managed %s loader is not a package symlink", name)
		}
		resolved, err := filepath.EvalSymlinks(link)
		if err != nil || resolved != filepath.Join(root, "lib/node_modules/corepack/dist", name+".js") {
			return fmt.Errorf("managed %s loader does not select its authenticated Corepack entrypoint", name)
		}
	}
	return nil
}

func authenticateSelectedNode(ctx context.Context, state managedState, progress ProgressFunc) error {
	if state.ApprovedNode != nil {
		return ValidateReleaseActivation(*state.ApprovedNode)
	}
	root, err := selectedNodeRoot(state)
	if err != nil {
		return err
	}
	version := strings.TrimPrefix(filepath.Base(root), "v")
	reportProgress(progress, "Authenticating the installed Node.js release...")
	r, err := AuthenticateRelease(ctx, version, "")
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "fluxo-node-assess-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	archive := filepath.Join(dir, r.Filename)
	f, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	err = fetchNode(ctx, "https://nodejs.org/dist/v"+r.Version+"/"+r.Filename, 160<<20, f)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := verifyFileChecksum(archive, r.SHA256); err != nil {
		return err
	}
	name := strings.TrimSuffix(r.Filename, ".tar.gz")
	if err := extractNodeArchive(archive, dir, name); err != nil {
		return err
	}
	want, err := TreeDigest(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	got, err := TreeDigest(root)
	if err != nil {
		return err
	}
	if got != want {
		return errors.New("installed Node.js payload differs from its authenticated release")
	}
	return nil
}

type preservationOps struct {
	trees        func() (map[string]string, error)
	authenticate func() error
	executables  func() (Status, error)
	save         func(managedState) error
}

func verifyOrEnroll(state managedState, profile string, ops preservationOps) (Status, error) {
	before, err := ops.trees()
	if err != nil {
		return Status{}, err
	}
	proof := state.Integrity
	selection := integritySelection(state)
	if proof != nil && proof.Schema == 1 && proof.Profile == profile && proof.Selection == selection {
		if !reflect.DeepEqual(proof.Trees, before) {
			return Status{}, errors.New("managed Node.js payload changed since authentication")
		}
	} else {
		if proof != nil && proof.Schema != 1 {
			return Status{}, errors.New("unsupported Node.js integrity evidence; use a compatible Fluxo release")
		}
		if err := ops.authenticate(); err != nil {
			return Status{}, err
		}
	}
	status, err := ops.executables()
	if err != nil {
		return Status{}, err
	}
	after, err := ops.trees()
	if err != nil {
		return Status{}, err
	}
	if !reflect.DeepEqual(before, after) {
		return Status{}, errors.New("Node.js files changed during verification")
	}
	if proof == nil || proof.Profile != profile || proof.Selection != selection {
		state.Integrity = &toolchainIntegrity{Schema: 1, Profile: profile, Selection: selection, Trees: after}
		if err := ops.save(state); err != nil {
			return Status{}, err
		}
	}
	return status, nil
}

// PreserveWithProgress never repairs toolchain payloads or restores snapshots.
// The caller retains the same package lifecycle, installer lease and runtime
// guards as installation. Older installations can enroll through a read-only
// authenticated assessment; only their protected optimization receipt is saved.
func PreserveWithProgress(ctx context.Context, progress ProgressFunc) (Status, error) {
	installMu.Lock()
	defer installMu.Unlock()
	release, err := acquireToolchainLock(ctx)
	if err != nil {
		return Status{}, err
	}
	defer release()
	fail := func(err error) (Status, error) {
		return Status{}, fmt.Errorf("Node.js toolchain preserved: %w. Resolve runtime recovery first if reported; for explicitly chosen toolchain repair run sudo /usr/local/bin/fluxo node-toolchain install, then rerun the Fluxo installer", err)
	}
	if err := pendingNodeInstall(filepath.Join(filepath.Dir(managedStatePath), "node-transactions")); err != nil {
		return fail(err)
	}
	state, err := readProtectedManagedState(managedStatePath)
	if err != nil {
		return fail(err)
	}
	profile, err := integrityProfile()
	if err != nil {
		return fail(err)
	}
	status, err := verifyOrEnroll(state, profile, preservationOps{
		trees: func() (map[string]string, error) { return preservationTrees(state) },
		authenticate: func() error {
			if err := authenticateSelectedNode(ctx, state, progress); err != nil {
				return err
			}
			if err := authenticateManagerPayloads(ctx, progress); err != nil {
				return err
			}
			return authenticateBunPayload(ctx, progress)
		},
		executables: func() (Status, error) {
			status := Inspect(ctx)
			if !status.ToolchainReady {
				return status, fmt.Errorf("unusable tools: %s", strings.Join(status.Missing, ", "))
			}
			for name, pair := range map[string][2]string{"Corepack": {status.Corepack, PinnedCorepackVersion}, "pnpm": {status.PNPM, PinnedPNPMVersion}, "Yarn": {status.Yarn, PinnedYarnVersion}, "Bun": {status.Bun, PinnedBunVersion}} {
				if !versionsEqual(pair[0], pair[1]) {
					return status, fmt.Errorf("%s %s differs from the supported selection %s", name, pair[0], pair[1])
				}
			}
			root, err := selectedNodeRoot(state)
			if err != nil {
				return status, err
			}
			if !versionsEqual(status.Version, strings.TrimPrefix(filepath.Base(root), "v")) {
				return status, errors.New("Node.js executable differs from the selected release")
			}
			return status, ctx.Err()
		},
		save: func(updated managedState) error {
			current, err := readProtectedManagedState(managedStatePath)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(current, state) {
				return errors.New("Node.js selection changed during verification")
			}
			return saveManagedState(updated)
		},
	})
	if err != nil {
		return fail(err)
	}
	reportProgress(progress, "Node.js toolchain already verified; no changes needed.")
	return status, nil
}
