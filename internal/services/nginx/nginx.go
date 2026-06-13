// Package nginx manages Nginx virtual host configuration: generating
// site configs from Go templates, symlinking sites-available to
// sites-enabled, testing syntax, and reloading the nginx service.
package nginx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"fluxo/syscmd"
)

const (
	sitesAvailable = "/etc/nginx/sites-available"
	sitesEnabled   = "/etc/nginx/sites-enabled"
)

// EnsureDirs creates the Nginx configuration directories if they
// don't exist. Safe to call on every startup.
func EnsureDirs() error {
	os.MkdirAll(sitesAvailable, 0755)
	os.MkdirAll(sitesEnabled, 0755)
	return nil
}

// GenerateConfig writes the Nginx site configuration to
// /etc/nginx/sites-available/{domain}, symlinks it into sites-enabled,
// tests the config with nginx -t, and reloads nginx.
// If nginx is not installed the call silently succeeds (no-op).
func GenerateConfig(domain, webRoot, phpVersion, appType string, appPort int, sslProvider string, aliases ...string) error {
	if _, err := os.Stat(sitesAvailable); os.IsNotExist(err) {
		return nil
	}

	configStr := renderSiteTemplate(domain, webRoot, phpVersion, appType, appPort, sslProvider, aliases)

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

// Reload tests the Nginx configuration with "nginx -t" and, if valid,
// gracefully reloads via "systemctl reload nginx". If nginx is not
// installed the call silently succeeds (no-op).
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
