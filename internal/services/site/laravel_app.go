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

	"fluxo/internal/services/nginx"
	"fluxo/internal/services/php"
	"fluxo/internal/syscmd"
)

type LaravelApp struct{}

func (l *LaravelApp) DefaultWebRoot() string {
	return "/public"
}

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

func (l *LaravelApp) LogSources(domain, phpVersion string) []LogSource {
	return []LogSource{
		{ID: "laravel-log", Label: "Laravel Log (" + domain + ")", Path: filepath.Join("/home/fluxo", domain, "storage/logs/laravel.log")},
		{ID: "site-nginx-access", Label: "Nginx Access (" + domain + ")", Path: fmt.Sprintf("/var/log/nginx/%s.access.log", domain)},
		{ID: "site-nginx-error", Label: "Nginx Error (" + domain + ")", Path: fmt.Sprintf("/var/log/nginx/%s.error.log", domain)},
	}
}

func (l *LaravelApp) Provision(ctx context.Context, req ProvisionRequest) error {
	// 1. Check PHP FPM
	if err := php.EnsureFPMExists(ctx, req.PHPVersion); err != nil {
		return fmt.Errorf("PHP Version %s not installed or not running: %w", req.PHPVersion, err)
	}

	siteDir := filepath.Join("/home/fluxo", req.Domain)
	cleanWebRoot := filepath.Clean(req.WebRoot)
	fullWebRoot := filepath.Join(siteDir, cleanWebRoot)

	// 2. Clone Repository or create Web Directory
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
		indexPath := filepath.Join(fullWebRoot, "index.php")
		if _, err := os.Stat(indexPath); os.IsNotExist(err) {
			os.WriteFile(indexPath, []byte("<?php\necho 'Fluxo Laravel App: ' . htmlspecialchars($_SERVER['HTTP_HOST']);\n"), 0644)
		}
	}

	// 3. Setup .env
	envPath := filepath.Join(siteDir, ".env")
	envExample := filepath.Join(siteDir, ".env.example")

	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		if _, errEx := os.Stat(envExample); errEx == nil {
			if data, errRead := os.ReadFile(envExample); errRead == nil {
				os.WriteFile(envPath, data, 0644)
			}
		} else {
			os.WriteFile(envPath, []byte(l.DefaultEnv(req)), 0644)
		}
	}

	if data, err := os.ReadFile(envPath); err == nil {
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
		os.WriteFile(envPath, []byte(envContent), 0644)
	}

	// 4. Install Composer dependencies
	composerJsonPath := filepath.Join(siteDir, "composer.json")
	if _, err := os.Stat(composerJsonPath); err == nil {
		composerCmd := exec.CommandContext(ctx, "php"+req.PHPVersion, "/usr/local/bin/composer", "install", "--no-dev", "--no-interaction", "--prefer-dist", "--optimize-autoloader")
		composerCmd.Dir = siteDir
		composerCmd.Run()
	}

	// 5. Generate APP_KEY
	artisanPath := filepath.Join(siteDir, "artisan")
	if _, err := os.Stat(artisanPath); err == nil {
		cmd := exec.CommandContext(ctx, "php"+req.PHPVersion, artisanPath, "key:generate", "--force")
		cmd.Dir = siteDir
		cmd.Run()
	}

	// Ensure recursive ownership is fluxo:www-data
	if _, err := syscmd.Run(ctx, 5*time.Second, "chown", "-R", "fluxo:www-data", siteDir); err != nil {
		log.Printf("Warning: failed to chown site directory: %v", err)
	}

	// 6. Setup Nginx
	if err := nginx.GenerateConfig(req.Domain, fullWebRoot, req.PHPVersion, req.AppType, req.AppPort, "none"); err != nil {
		return fmt.Errorf("failed to setup nginx config: %w", err)
	}

	// 7. Setup PHP-FPM Pool
	if err := php.GeneratePoolConfig(ctx, req.Domain, req.PHPVersion); err != nil {
		return fmt.Errorf("failed to setup PHP FPM pool: %w", err)
	}

	return nil
}
