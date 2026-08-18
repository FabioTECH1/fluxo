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
	"fluxo/internal/services/php"
	"fluxo/internal/syscmd"
)

type PHPApp struct{}

// DefaultWebRoot returns / for generic PHP sites.
func (p *PHPApp) DefaultWebRoot() string {
	return "/"
}

// DefaultDeployScript returns the git clone/pull + composer deploy script.
func (p *PHPApp) DefaultDeployScript(domain, branch, phpVersion string) string {
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

if [ ! -f .env ]; then
  if [ -f .env.example ]; then
    cp .env.example .env
  fi
fi

( [ -f composer.json ] && $FLUXO_COMPOSER install --no-dev --no-interaction --prefer-dist --optimize-autoloader ) || true`
}

// DefaultEnv builds a default .env file with database settings for a PHP site.
func (p *PHPApp) DefaultEnv(req ProvisionRequest) string {
	if req.DatabaseName == "" {
		return ""
	}
	dbConn := "mysql"
	dbPort := "3306"
	if strings.ToLower(req.DatabaseEngine) == "postgres" || strings.ToLower(req.DatabaseEngine) == "pgsql" {
		dbConn = "pgsql"
		dbPort = "5432"
	}
	dbUser := req.DatabaseUser
	dbPass := req.DatabasePassword

	return `DB_CONNECTION=` + dbConn + `
DB_HOST=127.0.0.1
DB_PORT=` + dbPort + `
DB_DATABASE=` + req.DatabaseName + `
DB_USERNAME=` + dbUser + `
DB_PASSWORD=` + quoteDotEnvValue(dbPass) + `
`
}

// LogSources returns the nginx log paths for PHP sites.
func (p *PHPApp) LogSources(domain, sitePath, phpVersion string) []LogSource {
	return []LogSource{
		{ID: "site-nginx-error", Label: "Nginx Error (" + domain + ")", Path: fmt.Sprintf("/var/log/nginx/%s.error.log", domain)},
		{ID: "site-nginx-access", Label: "Nginx Access (" + domain + ")", Path: fmt.Sprintf("/var/log/nginx/%s.access.log", domain)},
	}
}

// Provision sets up a PHP site: PHP-FPM, repo, .env, Nginx, and FPM pool.
func (p *PHPApp) Provision(ctx context.Context, req ProvisionRequest) error {
	// 1. Check PHP FPM
	if err := php.EnsureFPMExists(ctx, req.PHPVersion); err != nil {
		return fmt.Errorf("PHP Version %s not installed or not running: %w", req.PHPVersion, err)
	}

	actLog := func(typ, summary string) {
		if req.ActivityLog != nil {
			req.ActivityLog(req.SiteID, typ, summary)
		}
	}

	siteDir := filepath.Join("/home/fluxo", req.Domain)
	resolvedWebRoot, err := safeinput.NormalizeWebRoot(siteDir, req.WebRoot)
	if err != nil {
		return fmt.Errorf("invalid web root: %w", err)
	}
	webRootRel, err := filepath.Rel(siteDir, resolvedWebRoot)
	if err != nil {
		return fmt.Errorf("invalid web root: %w", err)
	}

	var workingDir string
	var currentSymlink string
	var fullWebRoot string

	if req.DeploymentStrategy == "zero-downtime" {
		workingDir, currentSymlink, err = PrepareZDDDirectory(ctx, req)
		if err != nil {
			return err
		}
		fullWebRoot = filepath.Join(currentSymlink, webRootRel)
	} else {
		workingDir = siteDir
		fullWebRoot = resolvedWebRoot
	}

	// 2. Clone Repository or create Web Directory
	if req.DeploymentStrategy != "zero-downtime" {
		if req.Repository != "" {
			actLog("provision", "Cloning Git repository")
			os.MkdirAll("/home/fluxo", 0755)
			out, err := syscmd.RunEnvAsUser(ctx, 120*time.Second, "fluxo",
				[]string{"GIT_SSH_COMMAND=ssh -o StrictHostKeyChecking=no -i " + req.SSHKeyPath},
				"git", "clone", "-b", req.Branch, "git@github.com:"+req.Repository+".git", workingDir)
			if err != nil {
				return fmt.Errorf("failed to clone repository: %s %w", out, err)
			}
		} else {
			actLog("provision", "Creating web directory")
			if err := os.MkdirAll(fullWebRoot, 0755); err != nil {
				return fmt.Errorf("failed to create web root: %w", err)
			}
			indexPath := filepath.Join(fullWebRoot, "index.php")
			if _, err := os.Stat(indexPath); os.IsNotExist(err) {
				os.WriteFile(indexPath, []byte("<?php\necho 'Fluxo PHP App: ' . htmlspecialchars($_SERVER['HTTP_HOST']);\n"), 0644)
			}
		}
	} else {
		if req.Repository == "" {
			actLog("provision", "Creating web directory")
			if err := os.MkdirAll(fullWebRoot, 0755); err != nil {
				return fmt.Errorf("failed to create web root: %w", err)
			}
			indexPath := filepath.Join(fullWebRoot, "index.php")
			if _, err := os.Stat(indexPath); os.IsNotExist(err) {
				os.WriteFile(indexPath, []byte("<?php\necho 'Fluxo PHP App: ' . htmlspecialchars($_SERVER['HTTP_HOST']);\n"), 0644)
			}
		}
	}

	// 3. Setup .env if database connected
	if req.DatabaseName != "" {
		actLog("provision", "Creating environment file")
		var persistentEnvPath string
		if req.DeploymentStrategy == "zero-downtime" {
			persistentEnvPath = filepath.Join(siteDir, ".env")
		} else {
			persistentEnvPath = filepath.Join(workingDir, ".env")
		}
		envExample := filepath.Join(workingDir, ".env.example")

		var envContent string
		if data, err := os.ReadFile(envExample); err == nil {
			envContent = string(data)
		} else {
			envContent = p.DefaultEnv(req)
		}

		// Replace values
		replacements := map[string]string{
			"DB_DATABASE": req.DatabaseName,
		}
		user := req.DatabaseUser
		if user == "" {
			user = "fluxo"
		}
		pass := req.DatabasePassword
		if pass == "" {
			pass = "secret"
		}
		replacements["DB_USERNAME"] = user
		replacements["DB_PASSWORD"] = pass

		dbConn := "mysql"
		dbPort := "3306"
		if strings.ToLower(req.DatabaseEngine) == "postgres" || strings.ToLower(req.DatabaseEngine) == "pgsql" {
			dbConn = "pgsql"
			dbPort = "5432"
		}
		replacements["DB_CONNECTION"] = dbConn
		replacements["DB_PORT"] = dbPort

		lines := strings.Split(envContent, "\n")
		replaced := make(map[string]bool)
		for i, line := range lines {
			for key, val := range replacements {
				if strings.HasPrefix(line, key+"=") {
					lines[i] = key + "=" + val
					replaced[key] = true
				}
			}
		}
		for key, val := range replacements {
			if !replaced[key] {
				lines = append(lines, key+"="+val)
			}
		}
		envContent = strings.Join(lines, "\n")

		os.WriteFile(persistentEnvPath, []byte(envContent), 0644)

		if req.DeploymentStrategy == "zero-downtime" {
			os.Remove(filepath.Join(workingDir, ".env"))
			os.Symlink(persistentEnvPath, filepath.Join(workingDir, ".env"))
		}
	}

	// 4. Install Composer dependencies (if toggle is on)
	if req.InstallComposer {
		composerJsonPath := filepath.Join(workingDir, "composer.json")
		if _, err := os.Stat(composerJsonPath); err == nil {
			actLog("provision", "Installing Composer dependencies")
			composerCmd := exec.CommandContext(ctx, "php"+req.PHPVersion, "/usr/local/bin/composer", "install", "--no-dev", "--no-interaction", "--prefer-dist", "--optimize-autoloader")
			composerCmd.Dir = workingDir
			composerCmd.Run()
		}
	}

	// Create/Update the current symlink if ZDD is enabled
	if req.DeploymentStrategy == "zero-downtime" {
		os.Remove(currentSymlink)
		if err := os.Symlink(workingDir, currentSymlink); err != nil {
			return fmt.Errorf("failed to create current symlink: %w", err)
		}
	}

	// Ensure recursive ownership is fluxo:www-data
	if _, err := syscmd.Run(ctx, 5*time.Second, "chown", "-R", "fluxo:www-data", siteDir); err != nil {
		log.Printf("Warning: failed to chown site directory: %v", err)
	}

	// 5. Setup Nginx
	actLog("provision", "Configuring Nginx")
	if err := nginx.GenerateConfig(req.Domain, fullWebRoot, req.PHPVersion, req.AppType, req.AppPort, "", ""); err != nil {
		return fmt.Errorf("failed to setup nginx config: %w", err)
	}

	// 6. Setup PHP-FPM Pool
	actLog("provision", "Configuring PHP-FPM pool")
	if err := php.GeneratePoolConfig(ctx, req.Domain, req.PHPVersion); err != nil {
		return fmt.Errorf("failed to setup PHP FPM pool: %w", err)
	}

	return nil
}
