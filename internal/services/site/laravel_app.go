package site

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"fluxo/internal/services/nginx"
	"fluxo/internal/services/php"
	"fluxo/internal/syscmd"
)

type LaravelApp struct{}

// DefaultWebRoot returns /public for Laravel sites.
func (l *LaravelApp) DefaultWebRoot() string {
	return "/public"
}

// DefaultDeployScript returns the git pull + composer + artisan deploy script.
func (l *LaravelApp) DefaultDeployScript(domain, branch, phpVersion string) string {
	return `set -e

cd $FLUXO_SITE_PATH

git pull origin $FLUXO_BRANCH

( [ -f package.json ] && (npm ci || npm install) && npm run build ) || true

$FLUXO_COMPOSER install --no-dev --no-interaction --prefer-dist --optimize-autoloader

if [ -f artisan ]; then
  $FLUXO_PHP artisan optimize
  $FLUXO_PHP artisan storage:link
  $FLUXO_PHP artisan migrate --force
fi`
}

// DefaultEnv builds a default .env file for a Laravel site with database settings.
func (l *LaravelApp) DefaultEnv(req ProvisionRequest) string {
	dbConn := "mysql"
	dbPort := "3306"
	if strings.ToLower(req.DatabaseEngine) == "postgres" || strings.ToLower(req.DatabaseEngine) == "pgsql" {
		dbConn = "pgsql"
		dbPort = "5432"
	}
	dbName := req.DatabaseName
	dbUser := req.DatabaseUser
	if dbUser == "" {
		dbUser = "fluxo"
	}
	dbPass := req.DatabasePassword
	if dbPass == "" {
		dbPass = "secret"
	}

	return `APP_NAME=Fluxo
APP_ENV=production
APP_KEY=
APP_DEBUG=false
APP_URL=http://` + req.Domain + `
LOG_CHANNEL=stack

DB_CONNECTION=` + dbConn + `
DB_HOST=127.0.0.1
DB_PORT=` + dbPort + `
DB_DATABASE=` + dbName + `
DB_USERNAME=` + dbUser + `
DB_PASSWORD=` + dbPass + `

BROADCAST_DRIVER=log
CACHE_DRIVER=file
FILESYSTEM_DISK=local
QUEUE_CONNECTION=sync
SESSION_DRIVER=file
SESSION_LIFETIME=120
`
}

// LogSources returns Laravel, nginx access, and nginx error log paths.
func (l *LaravelApp) LogSources(domain, phpVersion string) []LogSource {
	return []LogSource{
		{ID: "laravel-log", Label: "Laravel Log (" + domain + ")", Path: filepath.Join("/home/fluxo", domain, "storage/logs/laravel.log")},
		{ID: "site-nginx-access", Label: "Nginx Access (" + domain + ")", Path: fmt.Sprintf("/var/log/nginx/%s.access.log", domain)},
		{ID: "site-nginx-error", Label: "Nginx Error (" + domain + ")", Path: fmt.Sprintf("/var/log/nginx/%s.error.log", domain)},
	}
}
// Provision sets up a Laravel site: PHP-FPM, repo, .env, Composer, artisan, Nginx.
func (l *LaravelApp) Provision(ctx context.Context, req ProvisionRequest) error {
	// 1. Check PHP FPM
	if err := php.EnsureFPMExists(ctx, req.PHPVersion); err != nil {
		return fmt.Errorf("PHP Version %s not installed or not running: %w", req.PHPVersion, err)
	}

	siteDir := filepath.Join("/home/fluxo", req.Domain)
	cleanWebRoot := filepath.Clean(req.WebRoot)

	var workingDir string
	var currentSymlink string
	var fullWebRoot string

	if req.DeploymentStrategy == "zero-downtime" {
		var err error
		workingDir, currentSymlink, err = PrepareZDDDirectory(ctx, req)
		if err != nil {
			return err
		}
		fullWebRoot = filepath.Join(currentSymlink, cleanWebRoot)
	} else {
		workingDir = siteDir
		fullWebRoot = filepath.Join(siteDir, cleanWebRoot)
	}

	actLog := func(typ, summary string) {
		if req.ActivityLog != nil {
			req.ActivityLog(req.SiteID, typ, summary)
		}
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
				os.WriteFile(indexPath, []byte("<?php\necho 'Fluxo Laravel App: ' . htmlspecialchars($_SERVER['HTTP_HOST']);\n"), 0644)
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
				os.WriteFile(indexPath, []byte("<?php\necho 'Fluxo Laravel App: ' . htmlspecialchars($_SERVER['HTTP_HOST']);\n"), 0644)
			}
		}
	}

	// 3. Setup .env
	actLog("provision", "Creating environment file")
	var persistentEnvPath string
	if req.DeploymentStrategy == "zero-downtime" {
		persistentEnvPath = filepath.Join(siteDir, ".env")
	} else {
		persistentEnvPath = filepath.Join(workingDir, ".env")
	}
	envExample := filepath.Join(workingDir, ".env.example")

	if _, err := os.Stat(persistentEnvPath); os.IsNotExist(err) {
		if _, errEx := os.Stat(envExample); errEx == nil {
			if data, errRead := os.ReadFile(envExample); errRead == nil {
				os.WriteFile(persistentEnvPath, data, 0644)
			}
		} else {
			os.WriteFile(persistentEnvPath, []byte(l.DefaultEnv(req)), 0644)
		}
	}

	if data, err := os.ReadFile(persistentEnvPath); err == nil {
		envContent := string(data)
		replacements := map[string]string{
			"APP_NAME":  "Fluxo",
			"APP_ENV":   "production",
			"APP_DEBUG": "false",
			"APP_URL":   "http://" + req.Domain,
		}
		if req.DatabaseName != "" {
			replacements["DB_DATABASE"] = req.DatabaseName
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
		}

		hasAppKey := false
		for _, line := range strings.Split(envContent, "\n") {
			if strings.HasPrefix(line, "APP_KEY=") && len(line) > 8 && strings.TrimSpace(line[8:]) != "" {
				hasAppKey = true
				break
			}
		}
		if !hasAppKey {
			keyBytes := make([]byte, 32)
			if _, err := rand.Read(keyBytes); err == nil {
				replacements["APP_KEY"] = "base64:" + base64.StdEncoding.EncodeToString(keyBytes)
			}
		}

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
	}
	if req.DeploymentStrategy == "zero-downtime" {
		os.Remove(filepath.Join(workingDir, ".env"))
		os.Symlink(persistentEnvPath, filepath.Join(workingDir, ".env"))

		// Create shared storage directory and symlink it
		persistentStorageDir := filepath.Join(siteDir, "storage/app")
		os.MkdirAll(persistentStorageDir, 0755)
		os.MkdirAll(filepath.Join(workingDir, "storage"), 0755)
		os.RemoveAll(filepath.Join(workingDir, "storage/app"))
		os.Symlink(persistentStorageDir, filepath.Join(workingDir, "storage/app"))
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

	// 5. Install npm dependencies and build frontend
	packageJsonPath := filepath.Join(workingDir, "package.json")
	if _, err := os.Stat(packageJsonPath); err == nil {
		actLog("provision", "Building frontend assets with npm")
		npmInstallCmd := exec.CommandContext(ctx, "npm", "install")
		npmInstallCmd.Dir = workingDir
		npmInstallCmd.Run()
		npmBuildCmd := exec.CommandContext(ctx, "npm", "run", "--if-present", "build")
		npmBuildCmd.Dir = workingDir
		npmBuildCmd.Run()
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

	// 6. Setup Nginx
	actLog("provision", "Configuring Nginx")
	if err := nginx.GenerateConfig(req.Domain, fullWebRoot, req.PHPVersion, req.AppType, req.AppPort, "", ""); err != nil {
		return fmt.Errorf("failed to setup nginx config: %w", err)
	}

	// 7. Setup PHP-FPM Pool
	actLog("provision", "Configuring PHP-FPM pool")
	if err := php.GeneratePoolConfig(ctx, req.Domain, req.PHPVersion); err != nil {
		return fmt.Errorf("failed to setup PHP FPM pool: %w", err)
	}

	return nil
}
