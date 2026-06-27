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
