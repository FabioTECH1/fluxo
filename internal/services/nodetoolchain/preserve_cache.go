package nodetoolchain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"fluxo/internal/syscmd"
)

func activeCachePaths() ([]string, error) {
	paths := []string{"lastKnownGood.json"}
	for _, manager := range []struct{ name, pin string }{{"pnpm", PinnedPNPMVersion}, {"yarn", PinnedYarnVersion}} {
		version, err := payloadPinnedVersion(manager.pin, manager.name)
		if err != nil {
			return nil, err
		}
		paths = append(paths, filepath.Join("v1", manager.name, version))
	}
	return paths, nil
}

// Corepack's live cache belongs to the application account. Probe a small,
// immutable root-owned copy, authenticated before any manager is executed.
// Other project cache versions are irrelevant to the selected global tools.
func verificationCache(ctx context.Context, source string) (string, func(), error) {
	paths, err := activeCachePaths()
	if err != nil {
		return "", nil, err
	}
	root, err := os.MkdirTemp("", "fluxo-node-verify-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() {
		_ = os.Chmod(root, 0700)
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err == nil && entry.IsDir() {
				_ = os.Chmod(path, 0700)
			}
			return err
		})
		_ = os.RemoveAll(root)
	}
	fail := func(err error) (string, func(), error) { cleanup(); return "", nil, err }
	for _, relative := range paths {
		path := filepath.Join(source, relative)
		if err := filepath.WalkDir(path, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() && !info.Mode().IsRegular() {
				return fmt.Errorf("unsupported active Corepack cache entry %s", path)
			}
			if info.Mode().Perm()&0400 == 0 || info.IsDir() && info.Mode().Perm()&0100 == 0 {
				return fmt.Errorf("active Corepack cache entry is not readable by its owner: %s", path)
			}
			return nil
		}); err != nil {
			return fail(err)
		}
		destination := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return fail(err)
		}
		// The staging parent remains private until modes/ownership are checked.
		if _, err := syscmd.Run(ctx, 30*time.Second, "cp", "-R", "--no-dereference", "--preserve=mode", "--no-preserve=ownership", "--", path, destination); err != nil {
			return fail(err)
		}
	}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return errors.New("active Corepack cache changed while copying")
		}
		mode := os.FileMode(0444) | info.Mode().Perm()&0111
		if info.IsDir() {
			mode = 0555
		}
		return os.Chmod(path, mode)
	}); err != nil {
		return fail(err)
	}
	if err := validateOwnedTree(root, os.Geteuid(), os.Getegid(), false); err != nil {
		return fail(err)
	}
	return root, cleanup, nil
}
