// Package nginx manages Nginx virtual host configs, symlinks, syntax checks, and reloads.
package nginx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	sslservice "fluxo/internal/services/ssl"
	"fluxo/internal/syscmd"
)

const (
	sitesAvailable     = "/etc/nginx/sites-available"
	sitesEnabled       = "/etc/nginx/sites-enabled"
	defaultSite        = "-fluxo-default"
	defaultEnabledSite = "zzzzzzzzzzzz-fluxo-default"
	managedHeader      = "# Managed by Fluxo."
)

var (
	defaultServerMu     sync.Mutex
	defaultServerLoaded bool
	defaultServerError  string
	nginxBinaryPath     = "/usr/sbin/nginx"
)

type defaultServerEnvironment struct {
	sitesAvailable string
	sitesEnabled   string
	ensureCert     func() (string, string, error)
	validate       func(context.Context) error
	reload         func(context.Context) error
	reloadRequired bool
}

type siteConfigEnvironment struct {
	sitesAvailable string
	sitesEnabled   string
	validate       func(context.Context) error
	reload         func(context.Context) error
}

type nginxConfigChange struct {
	path     string
	previous []byte
	mode     os.FileMode
}

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
	return ensureDirs(sitesAvailable, sitesEnabled)
}

func ensureDirs(available, enabled string) error {
	if err := os.MkdirAll(available, 0755); err != nil {
		return fmt.Errorf("failed to create Nginx sites-available directory: %w", err)
	}
	if err := os.MkdirAll(enabled, 0755); err != nil {
		return fmt.Errorf("failed to create Nginx sites-enabled directory: %w", err)
	}
	return nil
}

// EnsureDefaultServer installs an explicit catch-all for every Fluxo-managed site.
// Nginx otherwise serves the first virtual host when no server_name matches.
func EnsureDefaultServer(ctx context.Context) error {
	if _, err := os.Stat(nginxBinaryPath); err != nil {
		if os.IsNotExist(err) {
			err = fmt.Errorf("nginx is not installed")
		} else {
			err = fmt.Errorf("failed to inspect Nginx installation: %w", err)
		}
		defaultServerMu.Lock()
		defaultServerLoaded = false
		defaultServerError = err.Error()
		defaultServerMu.Unlock()
		return err
	}

	defaultServerMu.Lock()
	defer defaultServerMu.Unlock()

	err := ensureDefaultServer(ctx, defaultServerEnvironment{
		sitesAvailable: sitesAvailable,
		sitesEnabled:   sitesEnabled,
		ensureCert:     sslservice.EnsureNginxFallbackCertificate,
		validate:       validateConfig,
		reload:         reloadService,
		reloadRequired: !defaultServerLoaded,
	})
	if err == nil {
		defaultServerLoaded = true
		defaultServerError = ""
	} else {
		defaultServerError = err.Error()
	}
	return err
}

// DefaultServerStatus reports whether this process successfully loaded the
// unknown-host guard and the latest installation error, if any.
func DefaultServerStatus() (bool, string) {
	defaultServerMu.Lock()
	defer defaultServerMu.Unlock()
	return defaultServerLoaded, defaultServerError
}

func ensureDefaultServer(ctx context.Context, env defaultServerEnvironment) error {
	if err := ensureDirs(env.sitesAvailable, env.sitesEnabled); err != nil {
		return err
	}
	certPath, keyPath, err := env.ensureCert()
	if err != nil {
		return fmt.Errorf("failed to prepare default HTTPS certificate: %w", err)
	}

	explicitConfig := []byte(renderDefaultServerConfig(certPath, keyPath, true))
	compatibilityConfig := []byte(renderDefaultServerConfig(certPath, keyPath, false))
	availablePath := filepath.Join(env.sitesAvailable, defaultSite)
	enabledPath := filepath.Join(env.sitesEnabled, defaultEnabledSite)
	legacyEnabledPath := filepath.Join(env.sitesEnabled, defaultSite)

	previousConfig, readErr := os.ReadFile(availablePath)
	hadPreviousConfig := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return fmt.Errorf("failed to read existing default Nginx config: %w", readErr)
	}
	if hadPreviousConfig && !bytes.Equal(previousConfig, explicitConfig) && !bytes.Equal(previousConfig, compatibilityConfig) && !bytes.Contains(previousConfig, []byte(managedHeader)) {
		return fmt.Errorf("refusing to overwrite unmanaged default Nginx config: %s", availablePath)
	}
	config := explicitConfig
	if bytes.Equal(previousConfig, compatibilityConfig) && !env.reloadRequired {
		config = compatibilityConfig
	}

	hadEnabledLink := false
	if _, err := os.Lstat(enabledPath); err == nil {
		target, err := os.Readlink(enabledPath)
		if err != nil {
			return fmt.Errorf("default Nginx config is not a symlink: %w", err)
		}
		if !symlinkPointsTo(enabledPath, target, availablePath) {
			return fmt.Errorf("default Nginx config points to an unmanaged target: %s", target)
		}
		hadEnabledLink = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to inspect default Nginx config link: %w", err)
	}
	hadLegacyEnabledLink := false
	legacyEnabledTarget := ""
	if legacyEnabledPath != enabledPath {
		if _, err := os.Lstat(legacyEnabledPath); err == nil {
			legacyEnabledTarget, err = os.Readlink(legacyEnabledPath)
			if err != nil {
				return fmt.Errorf("legacy default Nginx config is not a symlink: %w", err)
			}
			if !symlinkPointsTo(legacyEnabledPath, legacyEnabledTarget, availablePath) {
				return fmt.Errorf("legacy default Nginx config points to an unmanaged target: %s", legacyEnabledTarget)
			}
			hadLegacyEnabledLink = true
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("failed to inspect legacy default Nginx config link: %w", err)
		}
	}

	configModified := !bytes.Equal(previousConfig, config)
	createdEnabledLink := false
	migratedEnabledLink := false
	removedLegacyEnabledLink := false
	var referenceChanges []nginxConfigChange

	rollback := func() error {
		var rollbackErr error
		for i := len(referenceChanges) - 1; i >= 0; i-- {
			change := referenceChanges[i]
			if err := writeNginxFile(change.path, change.previous, change.mode); err != nil {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		}
		if migratedEnabledLink {
			if err := os.Rename(enabledPath, legacyEnabledPath); err != nil {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		} else if createdEnabledLink {
			if err := os.Remove(enabledPath); err != nil && !os.IsNotExist(err) {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		}
		if removedLegacyEnabledLink {
			if err := os.Symlink(legacyEnabledTarget, legacyEnabledPath); err != nil {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		}
		if configModified && hadPreviousConfig {
			if err := os.WriteFile(availablePath, previousConfig, 0644); err != nil {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		} else if configModified {
			if err := os.Remove(availablePath); err != nil && !os.IsNotExist(err) {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		}
		return rollbackErr
	}

	rollbackError := func(operation string, operationErr error) error {
		if rollbackErr := rollback(); rollbackErr != nil {
			return fmt.Errorf("%s: %v (rollback failed: %v)", operation, operationErr, rollbackErr)
		}
		return fmt.Errorf("%s: %w", operation, operationErr)
	}

	writeConfig := func(contents []byte) error {
		temporaryPath := availablePath + ".tmp"
		defer os.Remove(temporaryPath)
		if err := os.WriteFile(temporaryPath, contents, 0644); err != nil {
			return err
		}
		return os.Rename(temporaryPath, availablePath)
	}
	if configModified {
		if err := writeConfig(config); err != nil {
			return fmt.Errorf("failed to install default Nginx config: %w", err)
		}
	}
	if !hadEnabledLink {
		if hadLegacyEnabledLink {
			if err := os.Rename(legacyEnabledPath, enabledPath); err != nil {
				return rollbackError("failed to move default Nginx guard after existing virtual hosts", err)
			}
			migratedEnabledLink = true
		} else {
			if err := os.Symlink(availablePath, enabledPath); err != nil {
				return rollbackError("failed to enable default Nginx config", err)
			}
			createdEnabledLink = true
		}
	} else if hadLegacyEnabledLink {
		if err := os.Remove(legacyEnabledPath); err != nil {
			return rollbackError("failed to remove duplicate legacy Nginx guard link", err)
		}
		removedLegacyEnabledLink = true
	}
	if fallbackRoot, ok := fallbackCertificateRoot(certPath, keyPath); ok {
		referenceChanges, err = migrateFallbackCertificateReferences(env.sitesAvailable, fallbackRoot, certPath, keyPath)
		if err != nil {
			return rollbackError("failed to migrate Nginx fallback certificate references", err)
		}
	}

	changed := configModified || createdEnabledLink || migratedEnabledLink || removedLegacyEnabledLink || len(referenceChanges) > 0
	if !changed && !env.reloadRequired {
		return nil
	}
	validationErr := env.validate(ctx)
	if validationErr != nil && bytes.Equal(config, explicitConfig) {
		if err := writeConfig(compatibilityConfig); err != nil {
			return rollbackError("failed to install compatibility Nginx guard", err)
		}
		config = compatibilityConfig
		configModified = !bytes.Equal(previousConfig, compatibilityConfig)
		validationErr = env.validate(ctx)
	}
	if validationErr != nil {
		return rollbackError("default Nginx config validation failed", validationErr)
	}
	if err := env.reload(ctx); err != nil {
		// Keep a valid guard installed. A later Nginx start or retry will load it.
		return fmt.Errorf("default Nginx config is valid and installed, but reload failed: %w", err)
	}
	return nil
}

func symlinkPointsTo(linkPath, target, expected string) bool {
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(linkPath), target)
	}
	return filepath.Clean(target) == filepath.Clean(expected)
}

func fallbackCertificateRoot(certPath, keyPath string) (string, bool) {
	certCurrent := filepath.Dir(certPath)
	keyCurrent := filepath.Dir(keyPath)
	if filepath.Base(certCurrent) != "current" || certCurrent != keyCurrent {
		return "", false
	}
	return filepath.Dir(certCurrent), true
}

func migrateFallbackCertificateReferences(availableDir, fallbackRoot, certPath, keyPath string) ([]nginxConfigChange, error) {
	entries, err := os.ReadDir(availableDir)
	if err != nil {
		return nil, err
	}
	certPattern := regexp.MustCompile(`(?m)^([ \t]*ssl_certificate[ \t]+)` + regexp.QuoteMeta(filepath.Clean(fallbackRoot)) + `/[^; \t\r\n]+([ \t]*;[^\r\n]*)$`)
	keyPattern := regexp.MustCompile(`(?m)^([ \t]*ssl_certificate_key[ \t]+)` + regexp.QuoteMeta(filepath.Clean(fallbackRoot)) + `/[^; \t\r\n]+([ \t]*;[^\r\n]*)$`)
	changes := make([]nginxConfigChange, 0)
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		path := filepath.Join(availableDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return changes, err
		}
		previous, err := os.ReadFile(path)
		if err != nil {
			return changes, err
		}
		updated := certPattern.ReplaceAll(previous, []byte(`${1}`+certPath+`${2}`))
		updated = keyPattern.ReplaceAll(updated, []byte(`${1}`+keyPath+`${2}`))
		if bytes.Equal(previous, updated) {
			continue
		}
		mode := info.Mode().Perm()
		if err := writeNginxFile(path, updated, mode); err != nil {
			return changes, err
		}
		changes = append(changes, nginxConfigChange{path: path, previous: previous, mode: mode})
	}
	return changes, nil
}

func writeNginxFile(path string, contents []byte, mode os.FileMode) error {
	temporaryPath := path + ".tmp"
	defer os.Remove(temporaryPath)
	if err := os.WriteFile(temporaryPath, contents, mode); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func renderDefaultServerConfig(certPath, keyPath string, explicitDefault bool) string {
	return renderDefaultServerConfigForPorts(certPath, keyPath, explicitDefault, 80, 443)
}

func renderDefaultServerConfigForPorts(certPath, keyPath string, explicitDefault bool, httpPort, httpsPort int) string {
	defaultParameter := ""
	mode := "compatibility catch-all"
	serverName := `"" ~^.*$`
	if explicitDefault {
		defaultParameter = " default_server"
		mode = "explicit default"
		serverName = `""`
	}
	return fmt.Sprintf(`%s This file provides Fluxo's unknown-host guard.
# Mode: %s
# unmatched hostnames never fall through to a hosted website.
server {
    listen %d%s;
    listen [::]:%d%s;
    server_name %s;
    server_tokens off;
    access_log off;
    return 444;
}

server {
    listen %d ssl http2%s;
    listen [::]:%d ssl http2%s;
    server_name %s;
    server_tokens off;
    access_log off;

    ssl_certificate %s;
    ssl_certificate_key %s;
    ssl_protocols TLSv1.2 TLSv1.3;

    return 444;
}
`, managedHeader, mode, httpPort, defaultParameter, httpPort, defaultParameter, serverName, httpsPort, defaultParameter, httpsPort, defaultParameter, serverName, certPath, keyPath)
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
	if err := EnsureDefaultServer(context.Background()); err != nil {
		return fmt.Errorf("failed to install Nginx unknown-host guard: %w", err)
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

	return installSiteConfig(context.Background(), siteConfigEnvironment{
		sitesAvailable: sitesAvailable,
		sitesEnabled:   sitesEnabled,
		validate:       validateConfig,
		reload:         reloadService,
	}, domain, []byte(config))
}

func installSiteConfig(ctx context.Context, env siteConfigEnvironment, domain string, config []byte) error {
	if !validConfigName(domain) {
		return fmt.Errorf("invalid Nginx site name")
	}
	if err := ensureDirs(env.sitesAvailable, env.sitesEnabled); err != nil {
		return err
	}

	availablePath := filepath.Join(env.sitesAvailable, domain)
	enabledPath := filepath.Join(env.sitesEnabled, domain)
	previousConfig, readErr := os.ReadFile(availablePath)
	hadPreviousConfig := readErr == nil
	previousMode := os.FileMode(0644)
	if readErr != nil && !os.IsNotExist(readErr) {
		return fmt.Errorf("failed to read existing Nginx site config: %w", readErr)
	}
	if hadPreviousConfig {
		info, err := os.Lstat(availablePath)
		if err != nil {
			return fmt.Errorf("failed to inspect existing Nginx site config: %w", err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("existing Nginx site config is not a regular file")
		}
		previousMode = info.Mode().Perm()
	}

	previousTarget := ""
	hadEnabledLink := false
	if _, err := os.Lstat(enabledPath); err == nil {
		previousTarget, err = os.Readlink(enabledPath)
		if err != nil {
			return fmt.Errorf("enabled Nginx site is not a symlink: %w", err)
		}
		hadEnabledLink = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to inspect enabled Nginx site: %w", err)
	}

	configModified := !bytes.Equal(previousConfig, config)
	linkModified := !hadEnabledLink || !symlinkPointsTo(enabledPath, previousTarget, availablePath)
	rollback := func() error {
		var rollbackErr error
		if linkModified {
			if hadEnabledLink {
				if err := replaceNginxSymlink(previousTarget, enabledPath); err != nil {
					rollbackErr = errors.Join(rollbackErr, err)
				}
			} else if err := os.Remove(enabledPath); err != nil && !os.IsNotExist(err) {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		}
		if configModified {
			if hadPreviousConfig {
				if err := writeNginxFile(availablePath, previousConfig, previousMode); err != nil {
					rollbackErr = errors.Join(rollbackErr, err)
				}
			} else if err := os.Remove(availablePath); err != nil && !os.IsNotExist(err) {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		}
		return rollbackErr
	}
	rollbackError := func(operation string, operationErr error) error {
		if rollbackErr := rollback(); rollbackErr != nil {
			return fmt.Errorf("%s: %v (rollback failed: %v)", operation, operationErr, rollbackErr)
		}
		return fmt.Errorf("%s: %w", operation, operationErr)
	}

	if configModified {
		if err := writeNginxFile(availablePath, config, 0644); err != nil {
			return fmt.Errorf("failed to install Nginx site config: %w", err)
		}
	}
	if linkModified {
		if err := replaceNginxSymlink(availablePath, enabledPath); err != nil {
			return rollbackError("failed to enable Nginx site config", err)
		}
	}
	if err := env.validate(ctx); err != nil {
		return rollbackError("Nginx site config validation failed", err)
	}
	if err := env.reload(ctx); err != nil {
		// The new config is valid and remains ready for the next reload or start.
		return fmt.Errorf("Nginx site config is valid and installed, but reload failed: %w", err)
	}
	return nil
}

func replaceNginxSymlink(target, path string) error {
	temporaryPath := path + ".tmp"
	defer os.Remove(temporaryPath)
	if err := os.Remove(temporaryPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Symlink(target, temporaryPath); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
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

func validateConfig(ctx context.Context) error {
	if _, err := os.Stat(nginxBinaryPath); os.IsNotExist(err) {
		return nil
	}
	_, err := syscmd.Run(ctx, 10*time.Second, "nginx", "-t")
	if err != nil {
		return fmt.Errorf("nginx config test failed: %w", err)
	}
	return nil
}

func reloadService(ctx context.Context) error {
	_, err := syscmd.Run(ctx, 10*time.Second, "systemctl", "reload", "nginx")
	if err != nil {
		return fmt.Errorf("nginx reload failed: %w", err)
	}
	return nil
}

// Reload tests Nginx config and gracefully reloads if valid.
func Reload(ctx context.Context) error {
	if err := EnsureDefaultServer(ctx); err != nil {
		return fmt.Errorf("failed to install Nginx unknown-host guard: %w", err)
	}
	if err := validateConfig(ctx); err != nil {
		return err
	}
	return reloadService(ctx)
}
