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

// HostCertificate describes the certificate served for one hostname. Empty
// certificate paths keep that hostname on HTTP until SSL is configured.
type HostCertificate struct {
	Domain   string
	CertPath string
	KeyPath  string
}

type hostGroup struct {
	certPath string
	keyPath  string
	domains  []string
}

// EnsureDirs creates Nginx config directories if they don't exist.
func EnsureDirs() error {
	os.MkdirAll(sitesAvailable, 0755)
	os.MkdirAll(sitesEnabled, 0755)
	return nil
}

// GenerateConfig writes the site config, symlinks it, tests syntax, and reloads Nginx.
func GenerateConfig(domain, webRoot, phpVersion, appType string, appPort int, certPath, keyPath string, aliases ...string) error {
	hosts := make([]HostCertificate, 0, len(aliases)+1)
	hosts = append(hosts, HostCertificate{Domain: domain, CertPath: certPath, KeyPath: keyPath})
	for _, alias := range aliases {
		hosts = append(hosts, HostCertificate{Domain: alias, CertPath: certPath, KeyPath: keyPath})
	}
	return GenerateConfigWithHosts(domain, webRoot, phpVersion, appType, appPort, hosts)
}

// GenerateConfigWithHosts writes a site config with independent TLS state per hostname.
func GenerateConfigWithHosts(domain, webRoot, phpVersion, appType string, appPort int, hosts []HostCertificate) error {
	if _, err := os.Stat(sitesAvailable); os.IsNotExist(err) {
		return nil
	}

	groups, needsFallback := groupHostCertificates(hosts)
	if len(groups) == 0 {
		groups = append(groups, hostGroup{domains: []string{domain}})
		needsFallback = true
	}

	fallbackCertPath := ""
	fallbackKeyPath := ""
	if needsFallback {
		var err error
		fallbackCertPath, fallbackKeyPath, err = sslservice.EnsureNginxFallbackCertificate()
		if err != nil {
			return fmt.Errorf("failed to prepare HTTPS guard: %w", err)
		}
	}

	config := renderHostGroups(
		domain, webRoot, phpVersion, appType, appPort,
		fallbackCertPath, fallbackKeyPath, groups,
	)

	availPath := filepath.Join(sitesAvailable, domain)
	err := os.WriteFile(availPath, []byte(config), 0644)
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

func renderHostGroups(domain, webRoot, phpVersion, appType string, appPort int, fallbackCertPath, fallbackKeyPath string, groups []hostGroup) string {
	var config strings.Builder
	for i, group := range groups {
		if i > 0 {
			config.WriteString("\n")
		}
		config.WriteString(renderSiteTemplate(
			domain, webRoot, phpVersion, appType, appPort,
			group.certPath, group.keyPath, fallbackCertPath, fallbackKeyPath, group.domains,
		))
	}
	return config.String()
}

func groupHostCertificates(hosts []HostCertificate) ([]hostGroup, bool) {
	groups := make([]hostGroup, 0)
	groupIndexes := make(map[string]int)
	needsFallback := false
	for _, host := range hosts {
		host.Domain = strings.TrimSpace(host.Domain)
		if host.Domain == "" {
			continue
		}
		if host.CertPath == "" || host.KeyPath == "" {
			host.CertPath = ""
			host.KeyPath = ""
			needsFallback = true
		}
		key := host.CertPath + "\x00" + host.KeyPath
		index, exists := groupIndexes[key]
		if !exists {
			index = len(groups)
			groupIndexes[key] = index
			groups = append(groups, hostGroup{certPath: host.CertPath, keyPath: host.KeyPath})
		}
		groups[index].domains = append(groups[index].domains, host.Domain)
	}
	return groups, needsFallback
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
