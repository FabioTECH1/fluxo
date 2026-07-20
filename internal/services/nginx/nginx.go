// Package nginx manages Nginx virtual host configs, symlinks, syntax checks, and reloads.
package nginx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	sslservice "fluxo/internal/services/ssl"
	"fluxo/internal/syscmd"
)

const (
	sitesAvailable = "/etc/nginx/sites-available"
	sitesEnabled   = "/etc/nginx/sites-enabled"
)

// EnsureDirs creates Nginx config directories if they don't exist.
func EnsureDirs() error {
	os.MkdirAll(sitesAvailable, 0755)
	os.MkdirAll(sitesEnabled, 0755)
	return nil
}

// GenerateConfig writes the site config, symlinks it, tests syntax, and reloads Nginx.
func GenerateConfig(domain, webRoot, phpVersion, appType string, appPort int, certPath, keyPath string, aliases ...string) error {
	if _, err := os.Stat(sitesAvailable); os.IsNotExist(err) {
		return nil
	}

	fallbackCertPath := ""
	fallbackKeyPath := ""
	if certPath == "" || keyPath == "" {
		certPath = ""
		keyPath = ""
		var err error
		fallbackCertPath, fallbackKeyPath, err = sslservice.EnsureNginxFallbackCertificate()
		if err != nil {
			return fmt.Errorf("failed to prepare HTTPS guard: %w", err)
		}
	}

	configStr := renderSiteTemplate(domain, webRoot, phpVersion, appType, appPort, certPath, keyPath, fallbackCertPath, fallbackKeyPath, aliases)

	availPath := filepath.Join(sitesAvailable, domain)
	err := os.WriteFile(availPath, []byte(configStr), 0644)
	if err != nil {
		return fmt.Errorf("failed to write nginx config: %w", err)
	}

	// Atomically replace symlink: remove old, create new.
	enabledPath := filepath.Join(sitesEnabled, domain)
	if _, err := os.Lstat(enabledPath); err == nil {
		os.Remove(enabledPath)
	}

	err = os.Symlink(availPath, enabledPath)
	if err != nil {
		return fmt.Errorf("failed to symlink nginx config: %w", err)
	}

	return Reload(context.Background())
}

// Reload tests Nginx config and gracefully reloads if valid.
func Reload(ctx context.Context) error {
	if _, err := os.Stat("/usr/sbin/nginx"); os.IsNotExist(err) {
		return nil
	}
	_, err := syscmd.Run(ctx, 10*time.Second, "nginx", "-t")
	if err != nil {
		return fmt.Errorf("nginx config test failed: %w", err)
	}

	_, err = syscmd.Run(ctx, 10*time.Second, "systemctl", "reload", "nginx")
	if err != nil {
		return fmt.Errorf("nginx reload failed: %w", err)
	}

	return nil
}
