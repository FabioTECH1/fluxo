package site

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"fluxo/internal/safeinput"
	"fluxo/internal/services/nginx"
	"fluxo/internal/syscmd"
)

type NodeApp struct{}

func (n *NodeApp) DefaultWebRoot() string {
	return "/"
}

func (n *NodeApp) DefaultDeployScript(domain, branch, phpVersion string) string {
	return `set -e

cd $FLUXO_SITE_PATH

if [ ! -d .git ]; then
  echo "Initializing Git repository..."
  git init
  git remote add origin $FLUXO_REPO
  git fetch origin
  git checkout -f $FLUXO_BRANCH
else
  git pull origin $FLUXO_BRANCH
fi

if [ -f package.json ]; then
  if [ -n "$FLUXO_NODE_INSTALL_COMMAND" ]; then
    bash -lc "$FLUXO_NODE_INSTALL_COMMAND"
  fi

  if [ -n "$FLUXO_NODE_BUILD_COMMAND" ]; then
    bash -lc "$FLUXO_NODE_BUILD_COMMAND"
  fi
fi`
}

func (n *NodeApp) DefaultEnv(req ProvisionRequest) string {
	lines := []string{
		"NODE_ENV=production",
		fmt.Sprintf("PORT=%d", req.AppPort),
		"HOST=127.0.0.1",
	}
	if req.DatabaseName != "" {
		dbConn := "mysql"
		dbPort := "3306"
		if strings.ToLower(req.DatabaseEngine) == "postgres" || strings.ToLower(req.DatabaseEngine) == "pgsql" {
			dbConn = "pgsql"
			dbPort = "5432"
		}
		dbUser := req.DatabaseUser
		dbPass := req.DatabasePassword
		lines = append(lines,
			"DB_CONNECTION="+dbConn,
			"DB_HOST=127.0.0.1",
			"DB_PORT="+dbPort,
			"DB_DATABASE="+req.DatabaseName,
			"DB_USERNAME="+dbUser,
			"DB_PASSWORD="+quoteDotEnvValue(dbPass),
		)
	}
	return strings.Join(lines, "\n") + "\n"
}

func (n *NodeApp) LogSources(domain, sitePath, phpVersion string) []LogSource {
	return []LogSource{
		{ID: "site-nginx-error", Label: "Nginx Error (" + domain + ")", Path: fmt.Sprintf("/var/log/nginx/%s.error.log", domain)},
		{ID: "site-nginx-access", Label: "Nginx Access (" + domain + ")", Path: fmt.Sprintf("/var/log/nginx/%s.access.log", domain)},
	}
}

func (n *NodeApp) Provision(ctx context.Context, req ProvisionRequest) error {
	if _, err := exec.LookPath("node"); err != nil {
		return fmt.Errorf("Node.js is not installed. Install it from Runtime > Node.js before creating a Node.js site")
	}

	pm := NormalizePackageManager(req.PackageManager)
	if pm != "none" {
		if _, err := exec.LookPath(pm); err != nil {
			return fmt.Errorf("%s is not installed on this server", pm)
		}
	}

	actLog := func(typ, summary string) {
		if req.ActivityLog != nil {
			req.ActivityLog(req.SiteID, typ, summary)
		}
	}

	siteDir := filepath.Join("/home/fluxo", req.Domain)
	if err := os.MkdirAll(siteDir, 0755); err != nil {
		return fmt.Errorf("failed to create site directory: %w", err)
	}
	if req.DeploymentStrategy != "zero-downtime" {
		if err := ensureSiteOwnedByFluxo(ctx, siteDir); err != nil {
			return err
		}
	}

	nodeMode := NormalizeNodeMode(req.NodeMode)
	staticOutputDir := NormalizeStaticOutputDir(req.NodePreset, req.StaticOutputDir)
	resolvedStaticRoot, err := safeinput.NormalizeWebRoot(siteDir, staticOutputDir)
	if err != nil {
		return fmt.Errorf("invalid static output directory: %w", err)
	}
	staticRel, err := filepath.Rel(siteDir, resolvedStaticRoot)
	if err != nil {
		return fmt.Errorf("invalid static output directory: %w", err)
	}

	var workingDir string
	var currentSymlink string
	if req.DeploymentStrategy == "zero-downtime" {
		workingDir, currentSymlink, err = PrepareZDDDirectory(ctx, req)
		if err != nil {
			return err
		}
	} else {
		workingDir = siteDir
		if req.Repository != "" {
			gitEnv := []string{"GIT_SSH_COMMAND=ssh -o StrictHostKeyChecking=no -i " + req.SSHKeyPath}
			repoURL := "git@github.com:" + req.Repository + ".git"
			if _, statErr := os.Stat(filepath.Join(workingDir, ".git")); statErr == nil {
				// A failed dependency install leaves the clone in place after the
				// site record is rolled back. Reuse it so creating the site again is
				// safe and does not require manual server cleanup.
				actLog("provision", "Refreshing existing Git repository")
				existingRemote, remoteErr := syscmd.RunAsUserInDir(ctx, 10*time.Second, "fluxo", workingDir,
					"git", "remote", "get-url", "origin")
				if remoteErr != nil {
					return fmt.Errorf("failed to inspect existing repository remote: %s %w", existingRemote, remoteErr)
				}
				if strings.TrimSpace(existingRemote) != repoURL {
					return fmt.Errorf("site directory already contains a different Git repository")
				}
				commands := [][]string{
					{"-C", workingDir, "fetch", "origin", req.Branch},
					{"-C", workingDir, "checkout", "-f", "-B", req.Branch, "origin/" + req.Branch},
				}
				for _, args := range commands {
					if out, runErr := syscmd.RunEnvAsUser(ctx, 120*time.Second, "fluxo", gitEnv, "git", args...); runErr != nil {
						return fmt.Errorf("failed to refresh repository: %s %w", out, runErr)
					}
				}
			} else if os.IsNotExist(statErr) {
				actLog("provision", "Cloning Git repository")
				out, cloneErr := syscmd.RunEnvAsUser(ctx, 120*time.Second, "fluxo", gitEnv,
					"git", "clone", "-b", req.Branch, repoURL, workingDir)
				if cloneErr != nil {
					return fmt.Errorf("failed to clone repository: %s %w", out, cloneErr)
				}
			} else {
				return fmt.Errorf("failed to inspect existing repository: %w", statErr)
			}
		}
	}

	if req.Repository == "" {
		actLog("provision", "Creating starter files")
		if nodeMode == "static" {
			fullStaticRoot := filepath.Join(workingDir, staticRel)
			if err := os.MkdirAll(fullStaticRoot, 0755); err != nil {
				return fmt.Errorf("failed to create static output directory: %w", err)
			}
			indexPath := filepath.Join(fullStaticRoot, "index.html")
			if _, err := os.Stat(indexPath); os.IsNotExist(err) {
				os.WriteFile(indexPath, []byte(nodeStaticPlaceholder(req.Domain)), 0644)
			}
		} else {
			if err := writeNodeStarter(workingDir, req.Domain); err != nil {
				return err
			}
		}
	}

	envPath := filepath.Join(workingDir, ".env")
	if req.DeploymentStrategy == "zero-downtime" {
		envPath = filepath.Join(siteDir, ".env")
	}
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		envExample := filepath.Join(workingDir, ".env.example")
		if data, readErr := os.ReadFile(envExample); readErr == nil {
			os.WriteFile(envPath, data, 0644)
		} else {
			os.WriteFile(envPath, []byte(n.DefaultEnv(req)), 0644)
		}
	}
	if req.DeploymentStrategy == "zero-downtime" {
		os.Remove(filepath.Join(workingDir, ".env"))
		os.Symlink(envPath, filepath.Join(workingDir, ".env"))
	}

	installCommand := PackageInstallCommand(pm)
	if installCommand != "" {
		if _, err := os.Stat(filepath.Join(workingDir, "package.json")); err == nil {
			actLog("provision", "Installing Node.js dependencies")
			if out, err := syscmd.RunAsUserInDir(ctx, 5*time.Minute, "fluxo", workingDir, "bash", "-lc", installCommand); err != nil {
				return fmt.Errorf("failed to install Node.js dependencies: %s %w", out, err)
			}
		}
	}

	buildCommand := strings.TrimSpace(req.BuildCommand)
	if buildCommand != "" {
		if _, err := os.Stat(filepath.Join(workingDir, "package.json")); err == nil {
			actLog("provision", "Building Node.js application")
			if out, err := syscmd.RunAsUserInDir(ctx, 10*time.Minute, "fluxo", workingDir, "bash", "-lc", buildCommand); err != nil {
				return fmt.Errorf("failed to build Node.js application: %s %w", out, err)
			}
		}
	}

	if req.DeploymentStrategy == "zero-downtime" {
		os.Remove(currentSymlink)
		if err := os.Symlink(workingDir, currentSymlink); err != nil {
			return fmt.Errorf("failed to create current symlink: %w", err)
		}
	}

	if _, err := syscmd.Run(ctx, 5*time.Second, "chown", "-R", "fluxo:www-data", siteDir); err != nil {
		log.Printf("Warning: failed to chown site directory: %v", err)
	}

	actLog("provision", "Configuring Nginx")
	nginxAppType := "node"
	fullWebRoot := siteDir
	if req.DeploymentStrategy == "zero-downtime" {
		fullWebRoot = filepath.Join(siteDir, "current")
	}
	if nodeMode == "static" {
		nginxAppType = "html"
		if req.DeploymentStrategy == "zero-downtime" {
			fullWebRoot = filepath.Join(siteDir, "current", staticRel)
		} else {
			fullWebRoot = filepath.Join(siteDir, staticRel)
		}
		if err := os.MkdirAll(fullWebRoot, 0755); err != nil {
			return fmt.Errorf("failed to create static output directory: %w", err)
		}
	}
	if err := nginx.GenerateConfig(req.Domain, fullWebRoot, req.PHPVersion, nginxAppType, req.AppPort, "", ""); err != nil {
		return fmt.Errorf("failed to setup nginx config: %w", err)
	}

	return nil
}

func writeNodeStarter(dir, domain string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create app directory: %w", err)
	}
	packagePath := filepath.Join(dir, "package.json")
	if _, err := os.Stat(packagePath); os.IsNotExist(err) {
		content := `{"scripts":{"start":"node server.js","build":"node -e \"console.log('No build step configured')\""},"dependencies":{}}`
		if err := os.WriteFile(packagePath, []byte(content+"\n"), 0644); err != nil {
			return fmt.Errorf("failed to create package.json: %w", err)
		}
	}
	serverPath := filepath.Join(dir, "server.js")
	if _, err := os.Stat(serverPath); os.IsNotExist(err) {
		content := `const http = require('http');
const port = Number(process.env.PORT || 3000);
const host = process.env.HOST || '127.0.0.1';

http.createServer((req, res) => {
  res.writeHead(200, { 'content-type': 'text/plain; charset=utf-8' });
  res.end('Fluxo Node.js App: ` + domain + `\n');
}).listen(port, host, () => {
  console.log(` + "`Listening on ${host}:${port}`" + `);
});
`
		if err := os.WriteFile(serverPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to create server.js: %w", err)
		}
	}
	return nil
}

func nodeStaticPlaceholder(domain string) string {
	return `<!DOCTYPE html>
<html>
<head>
    <title>Fluxo Node.js Static Site</title>
    <style>
        body { font-family: system-ui; text-align: center; padding: 50px; background: #f9fafb; color: #111827; }
        h1 { color: #2563eb; }
    </style>
</head>
<body>
    <h1>Fluxo Node.js Static Site</h1>
    <p>Your static site for <code>` + domain + `</code> has been successfully provisioned.</p>
</body>
</html>`
}
