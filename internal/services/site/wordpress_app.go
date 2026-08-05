package site

import (
	"context"
	"fmt"
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

type WordPressApp struct{}

func (w *WordPressApp) DefaultWebRoot() string { return "/public" }

// WordPress is managed in place by default, so deployments are opt-in.
func (w *WordPressApp) DefaultDeployScript(domain, branch, phpVersion string) string { return "" }

func (w *WordPressApp) DefaultEnv(req ProvisionRequest) string { return "" }

func (w *WordPressApp) LogSources(domain, sitePath, phpVersion string) []LogSource {
	return []LogSource{
		{ID: "site-nginx-error", Label: "Nginx Error (" + domain + ")", Path: fmt.Sprintf("/var/log/nginx/%s.error.log", domain)},
		{ID: "site-nginx-access", Label: "Nginx Access (" + domain + ")", Path: fmt.Sprintf("/var/log/nginx/%s.access.log", domain)},
	}
}

func (w *WordPressApp) Provision(ctx context.Context, req ProvisionRequest) error {
	if req.DeploymentStrategy != "standard" {
		return fmt.Errorf("WordPress sites only support standard in-place hosting")
	}
	if req.DatabaseName == "" || req.DatabaseEngine != "mysql" {
		return fmt.Errorf("WordPress requires a MySQL or MariaDB database")
	}
	if err := php.EnsureFPMExists(ctx, req.PHPVersion); err != nil {
		return fmt.Errorf("PHP Version %s not installed or not running: %w", req.PHPVersion, err)
	}

	actLog := func(typ, summary string) {
		if req.ActivityLog != nil {
			req.ActivityLog(req.SiteID, typ, summary)
		}
	}

	siteDir := filepath.Join("/home/fluxo", req.Domain)
	webRoot, err := safeinput.NormalizeWebRoot(siteDir, req.WebRoot)
	if err != nil {
		return fmt.Errorf("invalid web root: %w", err)
	}
	if err := os.MkdirAll(webRoot, 0755); err != nil {
		return fmt.Errorf("failed to create WordPress web root: %w", err)
	}
	if _, err := syscmd.Run(ctx, 10*time.Second, "chown", "-R", "fluxo:www-data", siteDir); err != nil {
		return fmt.Errorf("failed to set WordPress directory ownership: %w", err)
	}

	actLog("provision", "Checking WP-CLI")
	if err := ensureWPCLI(ctx); err != nil {
		return err
	}

	if _, err := os.Stat(filepath.Join(webRoot, "wp-load.php")); os.IsNotExist(err) {
		actLog("provision", "Downloading WordPress")
		out, downloadErr := syscmd.RunAsUserInDir(ctx, 5*time.Minute, "fluxo", siteDir,
			"wp", "core", "download", "--path="+webRoot, "--force")
		if downloadErr != nil {
			return fmt.Errorf("failed to download WordPress: %s %w", out, downloadErr)
		}
	} else if err != nil {
		return fmt.Errorf("failed to inspect WordPress installation: %w", err)
	}

	configPath := filepath.Join(webRoot, "wp-config.php")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		actLog("provision", "Creating WordPress configuration")
		config, configErr := buildWordPressConfig(req)
		if configErr != nil {
			return configErr
		}
		if err := os.WriteFile(configPath, []byte(config), 0640); err != nil {
			return fmt.Errorf("failed to create wp-config.php: %w", err)
		}
	}

	if _, err := syscmd.Run(ctx, 10*time.Second, "chown", "-R", "fluxo:www-data", siteDir); err != nil {
		return fmt.Errorf("failed to secure WordPress directory ownership: %w", err)
	}

	actLog("provision", "Configuring Nginx")
	if err := nginx.GenerateConfig(req.Domain, webRoot, req.PHPVersion, req.AppType, 0, "", ""); err != nil {
		return fmt.Errorf("failed to setup nginx config: %w", err)
	}
	actLog("provision", "Configuring PHP-FPM pool")
	if err := php.GeneratePoolConfig(ctx, req.Domain, req.PHPVersion); err != nil {
		return fmt.Errorf("failed to setup PHP FPM pool: %w", err)
	}
	return nil
}

func ensureWPCLI(ctx context.Context) error {
	path, err := exec.LookPath("wp")
	if err != nil {
		return fmt.Errorf("WP-CLI is not installed; rerun the Fluxo installer to restore the release-verified WP-CLI tool")
	}
	if out, err := syscmd.Run(ctx, 30*time.Second, path, "--info"); err != nil {
		return fmt.Errorf("WP-CLI is installed but unusable; rerun the Fluxo installer to repair it: %s %w", out, err)
	}
	return nil
}

func buildWordPressConfig(req ProvisionRequest) (string, error) {
	saltNames := []string{
		"AUTH_KEY",
		"SECURE_AUTH_KEY",
		"LOGGED_IN_KEY",
		"NONCE_KEY",
		"AUTH_SALT",
		"SECURE_AUTH_SALT",
		"LOGGED_IN_SALT",
		"NONCE_SALT",
		"WP_CACHE_KEY_SALT",
	}
	var salts strings.Builder
	for _, name := range saltNames {
		value, err := safeinput.GenerateSecretHex(32)
		if err != nil {
			return "", fmt.Errorf("failed to generate WordPress security keys: %w", err)
		}
		fmt.Fprintf(&salts, "define( '%s', '%s' );\n", name, value)
	}

	return fmt.Sprintf(`<?php
/** WordPress configuration managed by Fluxo. */
define( 'DB_NAME', %s );
define( 'DB_USER', %s );
define( 'DB_PASSWORD', %s );
define( 'DB_HOST', '127.0.0.1' );
define( 'DB_CHARSET', 'utf8mb4' );
define( 'DB_COLLATE', '' );

%s
$table_prefix = 'wp_';

if ( isset( $_SERVER['HTTP_X_FORWARDED_PROTO'] ) && strpos( $_SERVER['HTTP_X_FORWARDED_PROTO'], 'https' ) !== false ) {
	$_SERVER['HTTPS'] = 'on';
}

define( 'WP_DEBUG', false );

if ( ! defined( 'ABSPATH' ) ) {
	define( 'ABSPATH', __DIR__ . '/' );
}

require_once ABSPATH . 'wp-settings.php';
`, phpSingleQuoted(req.DatabaseName), phpSingleQuoted(req.DatabaseUser), phpSingleQuoted(req.DatabasePassword), salts.String()), nil
}

func phpSingleQuoted(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `'`, `\'`)
	return "'" + value + "'"
}
