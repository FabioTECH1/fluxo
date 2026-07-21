package site

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"fluxo/internal/services/nginx"
	"fluxo/internal/syscmd"
)

// ensureSiteOwnedByFluxo makes a site tree writable by commands that are
// intentionally executed as the unprivileged fluxo user. Provisioning itself
// runs as root, so directories created with os.MkdirAll otherwise remain
// root-owned and block git, npm, and other site-level commands.
func ensureSiteOwnedByFluxo(ctx context.Context, siteDir string) error {
	if _, err := syscmd.Run(ctx, 10*time.Second, "chown", "-R", "fluxo:www-data", siteDir); err != nil {
		return fmt.Errorf("failed to set site directory ownership: %w", err)
	}
	return nil
}

// prepareLaravelSharedStorage moves a freshly cloned release's Laravel
// storage entries into the site-level shared directory and links the complete
// storage tree back into the release. Existing shared entries always win so a
// retried provision cannot overwrite persistent application state.
func prepareLaravelSharedStorage(siteDir, workingDir string) error {
	sharedStorage := filepath.Join(siteDir, "storage")
	releaseStorage := filepath.Join(workingDir, "storage")
	if err := os.MkdirAll(sharedStorage, 0755); err != nil {
		return err
	}

	if info, err := os.Lstat(releaseStorage); err == nil && info.Mode()&os.ModeSymlink != 0 {
		resolvedRelease, releaseErr := filepath.EvalSymlinks(releaseStorage)
		resolvedShared, sharedErr := filepath.EvalSymlinks(sharedStorage)
		if releaseErr == nil && sharedErr == nil && resolvedRelease == resolvedShared {
			return ensureLaravelStorageDirectories(sharedStorage)
		}
		if err := os.Remove(releaseStorage); err != nil {
			return err
		}
	}

	if err := moveMissingStorageEntries(releaseStorage, sharedStorage); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.RemoveAll(releaseStorage); err != nil {
		return err
	}
	if err := ensureLaravelStorageDirectories(sharedStorage); err != nil {
		return err
	}
	if err := os.Symlink(sharedStorage, releaseStorage); err != nil {
		return err
	}
	return nil
}

func moveMissingStorageEntries(sourceDir, targetDir string) error {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		source := filepath.Join(sourceDir, entry.Name())
		target := filepath.Join(targetDir, entry.Name())
		targetInfo, targetErr := os.Lstat(target)
		if os.IsNotExist(targetErr) {
			if err := os.Rename(source, target); err != nil {
				return err
			}
			continue
		}
		if targetErr != nil {
			return targetErr
		}
		sourceInfo, err := os.Lstat(source)
		if err != nil {
			return err
		}
		if sourceInfo.IsDir() && sourceInfo.Mode()&os.ModeSymlink == 0 && targetInfo.IsDir() && targetInfo.Mode()&os.ModeSymlink == 0 {
			if err := moveMissingStorageEntries(source, target); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureLaravelStorageDirectories(storageDir string) error {
	for _, relative := range []string{"app/public", "framework/cache/data", "framework/sessions", "framework/views", "logs"} {
		if err := os.MkdirAll(filepath.Join(storageDir, relative), 0755); err != nil {
			return err
		}
	}
	return nil
}

// PrepareZDDDirectory creates the releases directory, clones the repository into a new timestamped release,
// and returns the release directory path and the path to the current symlink.
func PrepareZDDDirectory(ctx context.Context, req ProvisionRequest) (string, string, error) {
	siteDir := filepath.Join("/home/fluxo", req.Domain)
	if err := os.MkdirAll(siteDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create site directory: %w", err)
	}

	releasesDir := filepath.Join(siteDir, "releases")
	if err := os.MkdirAll(releasesDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create releases directory: %w", err)
	}
	if err := ensureSiteOwnedByFluxo(ctx, siteDir); err != nil {
		return "", "", err
	}

	timestamp := time.Now().Format("20060102150405")
	releaseDir := filepath.Join(releasesDir, timestamp)

	if req.Repository != "" {
		out, err := syscmd.RunEnvAsUser(ctx, 120*time.Second, "fluxo",
			[]string{"GIT_SSH_COMMAND=ssh -o StrictHostKeyChecking=no -i " + req.SSHKeyPath},
			"git", "clone", "-b", req.Branch, "git@github.com:"+req.Repository+".git", releaseDir)
		if err != nil {
			return "", "", fmt.Errorf("failed to clone repository: %s %w", out, err)
		}
	} else {
		// Fallback for ZDD without repo (creates empty directory)
		if err := os.MkdirAll(releaseDir, 0755); err != nil {
			return "", "", fmt.Errorf("failed to create release directory: %w", err)
		}
	}

	currentSymlink := filepath.Join(siteDir, "current")
	return releaseDir, currentSymlink, nil
}

// Provision orchestrates site setup: directory structure, Nginx, PHP pool, .env, and ownership.
func Provision(ctx context.Context, req ProvisionRequest) error {
	nginx.EnsureDirs()

	p := Resolve(req.AppType)
	return p.Provision(ctx, req)
}
