// Package nginx manages Nginx virtual host configs, symlinks, syntax checks, and reloads.
package nginx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// DisableConfig removes a site's enabled symlink and reloads Nginx. The returned
// function restores the symlink if a later deletion step fails.
func DisableConfig(ctx context.Context, domain string) (func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }
	if !validConfigName(domain) {
		return noop, fmt.Errorf("invalid Nginx site name")
	}

	enabledPath := filepath.Join(sitesEnabled, domain)
	target, err := os.Readlink(enabledPath)
	if os.IsNotExist(err) {
		return noop, nil
	}
	if err != nil {
		return noop, fmt.Errorf("enabled Nginx site is not a symlink: %w", err)
	}
	if err := os.Remove(enabledPath); err != nil {
		return noop, fmt.Errorf("failed to disable Nginx site: %w", err)
	}

	restore := func(restoreCtx context.Context) error {
		if _, err := os.Lstat(enabledPath); err == nil {
			return fmt.Errorf("cannot restore Nginx site: enabled path already exists")
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("failed to inspect disabled Nginx site: %w", err)
		}
		if err := os.Symlink(target, enabledPath); err != nil {
			return fmt.Errorf("failed to restore Nginx site symlink: %w", err)
		}
		if err := Reload(restoreCtx); err != nil {
			return fmt.Errorf("restored Nginx site but reload failed: %w", err)
		}
		return nil
	}

	if err := Reload(ctx); err != nil {
		if restoreErr := restore(ctx); restoreErr != nil {
			return noop, fmt.Errorf("failed to disable Nginx site: %v (rollback failed: %v)", err, restoreErr)
		}
		return noop, fmt.Errorf("failed to disable Nginx site: %w", err)
	}
	return restore, nil
}

// RemoveConfigFiles removes a disabled site's Nginx configuration files.
func RemoveConfigFiles(domain string) error {
	if !validConfigName(domain) {
		return fmt.Errorf("invalid Nginx site name")
	}
	var cleanupErr error
	for _, path := range []string{
		filepath.Join(sitesEnabled, domain),
		filepath.Join(sitesAvailable, domain),
	} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	if cleanupErr != nil {
		return fmt.Errorf("failed to remove Nginx site files: %w", cleanupErr)
	}
	return nil
}

func validConfigName(domain string) bool {
	return domain != "" && domain != "." && domain != ".." && filepath.Base(domain) == domain && !strings.ContainsAny(domain, `/\\`)
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
