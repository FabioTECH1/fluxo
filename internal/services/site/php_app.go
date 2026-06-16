package site

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

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

if [ -f .env ]; then
  # Env update helper function
  update_env_var() {
    local key=$1
    local value=$2
    if grep -q "^${key}=" .env; then
      sed -i "s|^${key}=.*|${key}=${value}|" .env
    else
      echo "${key}=${value}" >> .env
    fi
  }

  if [ ! -z "$FLUXO_DB_NAME" ]; then
    update_env_var DB_CONNECTION "$FLUXO_DB_CONN"
    update_env_var DB_HOST "127.0.0.1"
    update_env_var DB_PORT "$FLUXO_DB_PORT"
    update_env_var DB_DATABASE "$FLUXO_DB_NAME"
    update_env_var DB_USERNAME "$FLUXO_DB_USER"
    update_env_var DB_PASSWORD "$FLUXO_DB_PASS"
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
	if dbUser == "" {
		dbUser = "fluxo"
	}
	dbPass := req.DatabasePassword
	if dbPass == "" {
		dbPass = "secret"
	}

	return `DB_CONNECTION=` + dbConn + `
DB_HOST=127.0.0.1
DB_PORT=` + dbPort + `
DB_DATABASE=` + req.DatabaseName + `
DB_USERNAME=` + dbUser + `
DB_PASSWORD=` + dbPass + `
`
}

// LogSources returns the nginx log paths for PHP sites.
func (p *PHPApp) LogSources(domain, phpVersion string) []LogSource {
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
			os.WriteFile(indexPath, []byte("<?php\necho 'Fluxo PHP App: ' . htmlspecialchars($_SERVER['HTTP_HOST']);\n"), 0644)
		}
	}

	// 3. Setup .env if database connected
	if req.DatabaseName != "" {
		envPath := filepath.Join(siteDir, ".env")
		envExample := filepath.Join(siteDir, ".env.example")

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

		os.WriteFile(envPath, []byte(envContent), 0644)
	}

	// Ensure recursive ownership is fluxo:www-data
	if _, err := syscmd.Run(ctx, 5*time.Second, "chown", "-R", "fluxo:www-data", siteDir); err != nil {
		log.Printf("Warning: failed to chown site directory: %v", err)
	}

	// 4. Setup Nginx
	if err := nginx.GenerateConfig(req.Domain, fullWebRoot, req.PHPVersion, req.AppType, req.AppPort, "none"); err != nil {
		return fmt.Errorf("failed to setup nginx config: %w", err)
	}

	// 5. Setup PHP-FPM Pool
	if err := php.GeneratePoolConfig(ctx, req.Domain, req.PHPVersion); err != nil {
		return fmt.Errorf("failed to setup PHP FPM pool: %w", err)
	}

	return nil
}
