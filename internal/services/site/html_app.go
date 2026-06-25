package site

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"fluxo/internal/services/nginx"
	"fluxo/internal/syscmd"
)

type HTMLApp struct{}

// DefaultWebRoot returns the default web root for static HTML sites.
func (h *HTMLApp) DefaultWebRoot() string {
	return "/"
}

// DefaultDeployScript returns the default git clone/pull script for static sites.
func (h *HTMLApp) DefaultDeployScript(domain, branch, phpVersion string) string {
	return `cd $FLUXO_SITE_PATH

if [ ! -d .git ]; then
  echo "Initializing Git repository..."
  git init
  git remote add origin $FLUXO_REPO
  git fetch origin
  git checkout -f $FLUXO_BRANCH
else
  git pull origin $FLUXO_BRANCH
fi

( [ -f package.json ] && (npm ci || npm install) && npm run build ) || true`
}

// DefaultEnv returns an empty env for static sites (no env file needed).
func (h *HTMLApp) DefaultEnv(req ProvisionRequest) string {
	return ""
}

// LogSources returns the nginx log paths for static sites.
func (h *HTMLApp) LogSources(domain, phpVersion string) []LogSource {
	return []LogSource{
		{ID: "site-nginx-error", Label: "Nginx Error (" + domain + ")", Path: fmt.Sprintf("/var/log/nginx/%s.error.log", domain)},
		{ID: "site-nginx-access", Label: "Nginx Access (" + domain + ")", Path: fmt.Sprintf("/var/log/nginx/%s.access.log", domain)},
	}
}

// Provision sets up a static HTML site directory and Nginx config.
func (h *HTMLApp) Provision(ctx context.Context, req ProvisionRequest) error {
	siteDir := filepath.Join("/home/fluxo", req.Domain)
	cleanWebRoot := filepath.Clean(req.WebRoot)
	fullWebRoot := filepath.Join(siteDir, cleanWebRoot)

	// 1. Clone Repository or create Web Directory
	if req.Repository != "" {
		os.MkdirAll("/home/fluxo", 0755)
		out, err := syscmd.RunEnvAsUser(ctx, 120*time.Second, "fluxo",
			[]string{"GIT_SSH_COMMAND=ssh -o StrictHostKeyChecking=no -i " + req.SSHKeyPath},
			"git", "clone", "-b", req.Branch, "git@github.com:"+req.Repository+".git", siteDir)
		if err != nil {
			return fmt.Errorf("failed to clone repository: %s %w", out, err)
		}
	} else {
		if err := os.MkdirAll(fullWebRoot, 0755); err != nil {
			return fmt.Errorf("failed to create web root: %w", err)
		}

		indexPath := filepath.Join(fullWebRoot, "index.html")
		if _, err := os.Stat(indexPath); os.IsNotExist(err) {
			placeholderHTML := `<!DOCTYPE html>
<html>
<head>
    <title>Fluxo Static Site</title>
    <style>
        body { font-family: system-ui; text-align: center; padding: 50px; background: #f9fafb; color: #111827; }
        h1 { color: #2563eb; }
    </style>
</head>
<body>
    <h1>Fluxo Static Site</h1>
    <p>Your static site for <code>` + req.Domain + `</code> has been successfully provisioned.</p>
</body>
</html>`
			os.WriteFile(indexPath, []byte(placeholderHTML), 0644)
		}
	}

	// Ensure recursive ownership is fluxo:www-data
	if _, err := syscmd.Run(ctx, 5*time.Second, "chown", "-R", "fluxo:www-data", siteDir); err != nil {
		log.Printf("Warning: failed to chown site directory: %v", err)
	}

	// 2. Setup Nginx
	if err := nginx.GenerateConfig(req.Domain, fullWebRoot, req.PHPVersion, req.AppType, req.AppPort, "", ""); err != nil {
		return fmt.Errorf("failed to setup nginx config: %w", err)
	}

	return nil
}
