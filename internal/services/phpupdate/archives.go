package phpupdate

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
)

type archiveManifest struct {
	Schema            int                    `json:"schema"`
	Version           string                 `json:"version"`
	Before            []PackageArchive       `json:"before"`
	After             []PackageArchive       `json:"after"`
	RollbackAvailable bool                   `json:"rollback_available"`
	RollbackReason    string                 `json:"rollback_reason"`
	PolicyHash        string                 `json:"policy_hash"`
	Packages          []Package              `json:"packages"`
	NativeBindings    []nativeArchiveBinding `json:"native_bindings,omitempty"`
}
type nativeArchiveBinding struct {
	Package      string `json:"package"`
	Architecture string `json:"architecture"`
	Member       string `json:"member"`
	SHA256       string `json:"sha256"`
	Size         int64  `json:"size"`
}

func nativeBindings(p Plan) ([]nativeArchiveBinding, error) {
	bindings := map[string]nativeArchiveBinding{}
	for _, consumer := range p.Impact.Workers {
		if consumer.Octane == nil || consumer.Octane.Backend != "swoole" {
			continue
		}
		extension := consumer.Octane.Extension
		if extension == nil {
			return nil, errors.New("Approved native Swoole identity is missing")
		}
		name := "php" + p.Version + "-swoole"
		architecture := ""
		for _, pkg := range p.Inventory.Packages {
			if pkg.Name == name {
				architecture = pkg.Architecture
			}
		}
		if architecture == "" {
			return nil, errors.New("Native Swoole archive is outside the approved package family")
		}
		info, err := os.Stat(extension.File)
		if err != nil {
			return nil, err
		}
		binding := nativeArchiveBinding{Package: name, Architecture: architecture, Member: strings.TrimPrefix(extension.File, "/"), SHA256: extension.SHA256, Size: info.Size()}
		if previous, ok := bindings[binding.Member]; ok && previous != binding {
			return nil, errors.New("Native Swoole consumers disagree on the approved library identity")
		}
		bindings[binding.Member] = binding
	}
	result := []nativeArchiveBinding{}
	for _, binding := range bindings {
		result = append(result, binding)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Member < result[j].Member })
	return result, nil
}

type applyOptions struct {
	AcknowledgeNoRollback bool `json:"acknowledge_no_rollback"`
}

// Retained files are useful after a crash only if every directory containing
// them is durably linked too, including directories left visible by a retry.
func ensureDurableRecoveryDirectory(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(absolute, current), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		next := filepath.Join(current, part)
		if err = os.Mkdir(next, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, e := os.Lstat(next)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Unsafe PHP recovery directory")
		}
		if err = rootOwned(next); err != nil {
			return err
		}
		if err = syncDirectory(current); err != nil {
			return err
		}
		current = next
	}
	return syncDirectory(current)
}

func rollbackCapability(inv Inventory) RollbackCapability {
	if len(inv.Packages) == 0 {
		return RollbackCapability{Reason: "The installed package set has not been inspected."}
	}
	var missing []string
	for _, p := range inv.Packages {
		if p.InstalledArchive == nil || p.InstalledArchive.Name != p.Name || p.InstalledArchive.Version != p.Installed || p.InstalledArchive.Architecture != p.Architecture || !validArchiveMetadata(*p.InstalledArchive) {
			missing = append(missing, p.Name)
		}
	}
	if len(missing) != 0 {
		return RollbackCapability{Reason: "Verified previous packages are unavailable: " + strings.Join(missing, ", ")}
	}
	return RollbackCapability{Available: true}
}

func (m *Manager) archiveDirectory(id string) (string, error) {
	if _, err := uuid.Parse(id); err != nil {
		return "", errors.New("Invalid recovery generation ID")
	}
	dir, err := filepath.Abs(m.DataDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "php-updates", id, "archives"), nil
}

func (m *Manager) prepareArchives(ctx context.Context, o Operation, p Plan, acknowledge bool) (archiveManifest, error) {
	var result struct {
		Archives archiveManifest `json:"archives"`
	}
	dir, err := m.archiveDirectory(o.ID)
	if err != nil {
		return result.Archives, err
	}
	if err = ensureDurableRecoveryDirectory(dir); err != nil {
		return result.Archives, err
	}
	bindings, err := nativeBindings(p)
	if err != nil {
		return result.Archives, err
	}
	raw, err := bridgeRaw(ctx, o.ID, map[string]any{"action": "prepare", "inventory": p.Inventory, "directory": dir, "acknowledge_no_rollback": acknowledge, "native_bindings": bindings})
	if err != nil {
		return result.Archives, err
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return result.Archives, err
	}
	manifest := result.Archives
	if len(manifest.NativeBindings) != len(bindings) || len(bindings) > 0 && digest(manifest.NativeBindings) != digest(bindings) {
		return manifest, errors.New("Retained native package bindings differ from the approved runtime")
	}
	if manifest.Schema != 1 || manifest.Version != p.Version || len(manifest.After) != len(p.Inventory.Packages) || len(manifest.After) == 0 || digest(manifest.Packages) != digest(p.Inventory.Packages) {
		return manifest, errors.New("Archive preparation did not return a complete approved package set")
	}
	if manifest.RollbackAvailable && len(manifest.Before) != len(manifest.After) {
		return manifest, errors.New("Rollback archive is incomplete")
	}
	if !manifest.RollbackAvailable && !acknowledge {
		return manifest, fmt.Errorf("%w: %s", ErrRollbackAcknowledgement, manifest.RollbackReason)
	}
	return manifest, nil
}

func validArchiveMetadata(a PackageArchive) bool {
	hash, err := hex.DecodeString(a.SHA256)
	if err != nil || len(hash) != 32 || a.Size <= 0 || a.Name == "" || a.Architecture == "" || a.Version == "" || len(a.Origins) == 0 {
		return false
	}
	for _, o := range a.Origins {
		if !o.Trusted || o.Origin != "Ubuntu" && o.Origin != "UbuntuESM" && o.Origin != "UbuntuESMApps" && o.Origin != "LP-PPA-ondrej-php" && o.Origin != "deb.sury.org" {
			return false
		}
		if o.Origin == "deb.sury.org" && (o.Site != "packages.sury.org" || o.Component != "main" || o.Archive != "jammy" && o.Archive != "noble" && o.Archive != "resolute") {
			return false
		}
	}
	return a.Control["Package"] == a.Name && a.Control["Version"] == a.Version && a.Control["Architecture"] == a.Architecture
}

func (m *Manager) verifyRetainedArchives(ctx context.Context, id string, g recoveryGeneration, goal string) error {
	directory, err := m.archiveDirectory(g.ID)
	if err != nil {
		return err
	}
	raw, err := bridgeRaw(ctx, id, map[string]any{"action": "verify_archives", "directory": directory, "manifest": g.Archives, "goal": goal})
	if err != nil {
		return err
	}
	var result struct {
		Archives archiveManifest `json:"archives"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return err
	}
	if digest(result.Archives) != digest(g.Archives) {
		return errors.New("Retained archive verification returned a different generation")
	}
	return nil
}
