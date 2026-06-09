package nginx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"fluxo/syscmd"
)

// Nginx config locations
const (
	sitesAvailable = "/etc/nginx/sites-available"
	sitesEnabled   = "/etc/nginx/sites-enabled"
)

// EnsureDirs ensures the Nginx config directories exist.
func EnsureDirs() error {
	os.MkdirAll(sitesAvailable, 0755)
	os.MkdirAll(sitesEnabled, 0755)
	return nil
}

// GenerateConfig generates and symlinks the Nginx config, then reloads the service.
// If nginx is not installed, it silently skips.
func GenerateConfig(domain, webRoot, phpVersion, appType string, appPort int, sslProvider string) error {
	// Silently skip if nginx isn't installed
	if _, err := os.Stat(sitesAvailable); os.IsNotExist(err) {
		return nil
	}

	configStr := renderSiteTemplate(domain, webRoot, phpVersion, appType, appPort, sslProvider)

	availPath := filepath.Join(sitesAvailable, domain)
	err := os.WriteFile(availPath, []byte(configStr), 0644)
	if err != nil {
		return fmt.Errorf("failed to write nginx config: %w", err)
	}

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

// Reload safely tests and reloads Nginx.
// If nginx is not installed, it silently skips.
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
