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

	"fluxo/services/nginx"
	"fluxo/services/php"
	"fluxo/syscmd"
)

// Provision sets up the directory structure, Nginx configuration, PHP pool,
// database credentials in .env, and sets proper ownership.
func Provision(ctx context.Context, domain, phpVersion, webRoot, appType string, appPort int, dbName, dbUser, dbPass string) error {
	// 1. Ensure Nginx dirs exist
	nginx.EnsureDirs()

	// 2. Check PHP FPM
	if err := php.EnsureFPMExists(ctx, phpVersion); err != nil {
		return fmt.Errorf("PHP Version %s not installed or not running: %w", phpVersion, err)
	}

	// 3. Create Web Directory
	cleanWebRoot := filepath.Clean(webRoot)
	fullWebRoot := filepath.Join("/home/fluxo", domain, cleanWebRoot)
	if err := os.MkdirAll(fullWebRoot, 0755); err != nil {
		return fmt.Errorf("failed to create web root: %w", err)
	}

	indexPath := filepath.Join(fullWebRoot, "index.php")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		os.WriteFile(indexPath, []byte("<?php\necho 'Fluxo Site: ' . htmlspecialchars($_SERVER['HTTP_HOST']);\n"), 0644)
	}

	siteDir := filepath.Join("/home/fluxo", domain)

	// 3.5. Provision .env for PHP/Laravel apps with database
	if dbName != "" && (appType == "laravel" || appType == "php") {
		user := dbUser
		if user == "" {
			user = "fluxo"
		}
		pass := dbPass
		if pass == "" {
			pass = "secret"
		}

		envPath := filepath.Join(siteDir, ".env")
		envExample := filepath.Join(siteDir, ".env.example")

		var envContent string
		if data, err := os.ReadFile(envExample); err == nil {
			envContent = string(data)
		} else {
			envContent = `APP_NAME=Fluxo
APP_ENV=production
APP_KEY=
APP_DEBUG=false
APP_URL=http://` + domain + `
LOG_CHANNEL=stack
DB_CONNECTION=mysql
DB_HOST=127.0.0.1
DB_PORT=3306
DB_DATABASE=` + dbName + `
DB_USERNAME=` + user + `
DB_PASSWORD=` + pass + ``
		}

		// Replace values regardless of source
		replacements := map[string]string{
			"APP_NAME":    "Fluxo",
			"APP_ENV":     "production",
			"APP_DEBUG":   "false",
			"APP_URL":     "http://" + domain,
			"DB_DATABASE": dbName,
			"DB_USERNAME": user,
			"DB_PASSWORD": pass,
		}
		for key, val := range replacements {
			envContent = strings.ReplaceAll(envContent, key+"=", key+"="+val+"\n"+key+"=")
		}

		// Simple line-by-line replacement
		lines := strings.Split(envContent, "\n")
		for i, line := range lines {
			for key, val := range replacements {
				if strings.HasPrefix(line, key+"=") {
					lines[i] = key + "=" + val
				}
			}
		}
		envContent = strings.Join(lines, "\n")

		os.WriteFile(envPath, []byte(envContent), 0644)

		// Generate APP_KEY if artisan exists
		artisanPath := filepath.Join(siteDir, "artisan")
		if _, err := os.Stat(artisanPath); err == nil {
			cmd := exec.Command("php", artisanPath, "key:generate", "--force")
			cmd.Dir = siteDir
			cmd.Run()
		}
	}

	// Ensure recursive ownership is fluxo:www-data for the site folder
	if _, err := syscmd.Run(ctx, 5*time.Second, "chown", "-R", "fluxo:www-data", siteDir); err != nil {
		log.Printf("Warning: failed to chown site directory: %v", err)
	}

	// 4. Setup Nginx
	if err := nginx.GenerateConfig(domain, fullWebRoot, phpVersion, appType, appPort, "none"); err != nil {
		return fmt.Errorf("failed to setup nginx config: %w", err)
	}

	// 5. Setup PHP-FPM Pool (Only for PHP/Laravel)
	if appType == "php" || appType == "laravel" {
		if err := php.GeneratePoolConfig(ctx, domain, phpVersion); err != nil {
			return fmt.Errorf("failed to setup PHP FPM pool: %w", err)
		}
	}

	return nil
}
