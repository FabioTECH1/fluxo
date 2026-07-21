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
