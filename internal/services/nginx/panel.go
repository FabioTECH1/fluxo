package nginx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

const (
	panelSiteName      = "fluxo-panel"
	panelManagedHeader = managedHeader + "\n# Fluxo administration panel domain."
)

var panelDomainPattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+$`)

// PanelProxyConfig contains the non-secret values needed to render the
// dedicated administration-panel reverse proxy.
type PanelProxyConfig struct {
	Domain         string
	CertPath       string
	KeyPath        string
	ChallengeRoot  string
	UpstreamScheme string
	UpstreamPort   int
}

type panelConfigEnvironment struct {
	sitesAvailable string
	sitesEnabled   string
	validate       func(context.Context) error
	reload         func(context.Context) error
}

type panelConfigSnapshot struct {
	availableExists bool
	available       []byte
	availableMode   os.FileMode
	enabledExists   bool
	enabledTarget   string
}

// InstallPanelChallenge exposes only the ACME webroot for a new hostname. If
// an existing panel domain is supplied, its HTTPS proxy remains available
// while the replacement certificate is issued.
func InstallPanelChallenge(ctx context.Context, domain, challengeRoot string, existing *PanelProxyConfig) (func(context.Context) error, error) {
	challenge, err := renderPanelChallenge(domain, challengeRoot)
	if err != nil {
		return nil, err
	}
	config := challenge
	if existing != nil && strings.EqualFold(existing.Domain, domain) {
		config, err = renderPanelProxy(*existing)
	} else if existing != nil {
		var current string
		current, err = renderPanelProxy(*existing)
		if err == nil {
			config = current + "\n" + challenge
		}
	}
	if err != nil {
		return nil, err
	}
	return installPanelConfig(ctx, productionPanelEnvironment(), []byte(config))
}

// InstallPanelProxy atomically validates and activates the HTTPS panel vhost.
// The returned function restores the exact previous config if a later database
// or health-verification step fails.
func InstallPanelProxy(ctx context.Context, config PanelProxyConfig) (func(context.Context) error, error) {
	rendered, err := renderPanelProxy(config)
	if err != nil {
		return nil, err
	}
	return installPanelConfig(ctx, productionPanelEnvironment(), []byte(rendered))
}

// RemovePanelConfig atomically removes the dedicated panel vhost. It never
// touches ordinary site configurations or the daemon's direct listener.
func RemovePanelConfig(ctx context.Context) (func(context.Context) error, error) {
	return removePanelConfig(ctx, productionPanelEnvironment())
}

// PanelConfigStatus checks ownership, hostname, and enabled-link state without
// modifying Nginx.
func PanelConfigStatus(domain string) (bool, string) {
	if !validPanelDomain(domain) {
		return false, "invalid panel hostname"
	}
	env := productionPanelEnvironment()
	availablePath := filepath.Join(env.sitesAvailable, panelSiteName)
	enabledPath := filepath.Join(env.sitesEnabled, panelSiteName)
	config, err := os.ReadFile(availablePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, "panel Nginx configuration is missing"
		}
		return false, "panel Nginx configuration cannot be read"
	}
	if !bytes.Contains(config, []byte(panelManagedHeader)) {
		return false, "panel Nginx configuration is not Fluxo-managed"
	}
	if !bytes.Contains(config, []byte("server_name "+domain+";")) {
		return false, "panel Nginx configuration does not contain the active hostname"
	}
	target, err := os.Readlink(enabledPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, "panel Nginx configuration is disabled"
		}
		return false, "panel Nginx enabled entry is invalid"
	}
	if !symlinkPointsTo(enabledPath, target, availablePath) {
		return false, "panel Nginx configuration points to an unexpected file"
	}
	return true, ""
}

// RemoveOrphanedPanelConfig removes an interrupted Fluxo-managed challenge or
// proxy only when no panel domain is persisted. Unmanaged files are refused.
func RemoveOrphanedPanelConfig(ctx context.Context) error {
	env := productionPanelEnvironment()
	present, err := panelConfigPresent(env)
	if err != nil || !present {
		return err
	}
	_, err = removePanelConfig(ctx, env)
	return err
}

func productionPanelEnvironment() panelConfigEnvironment {
	return panelConfigEnvironment{
		sitesAvailable: sitesAvailable,
		sitesEnabled:   sitesEnabled,
		validate:       validateConfig,
		reload:         reloadService,
	}
}

func validPanelDomain(domain string) bool {
	return panelDomainPattern.MatchString(domain) && net.ParseIP(domain) == nil
}

func validatePanelConfig(config PanelProxyConfig) error {
	if !validPanelDomain(config.Domain) {
		return fmt.Errorf("invalid panel hostname")
	}
	if config.UpstreamScheme != "http" && config.UpstreamScheme != "https" {
		return fmt.Errorf("invalid panel upstream scheme")
	}
	if config.UpstreamPort < 1 || config.UpstreamPort > 65535 {
		return fmt.Errorf("invalid panel upstream port")
	}
	for label, path := range map[string]string{
		"certificate": config.CertPath,
		"private key": config.KeyPath,
		"challenge":   config.ChallengeRoot,
	} {
		if !filepath.IsAbs(path) || strings.ContainsAny(path, "\r\n\x00") {
			return fmt.Errorf("invalid panel %s path", label)
		}
	}
	return nil
}

func renderPanelChallenge(domain, challengeRoot string) (string, error) {
	if !validPanelDomain(domain) {
		return "", fmt.Errorf("invalid panel hostname")
	}
	if !filepath.IsAbs(challengeRoot) || strings.ContainsAny(challengeRoot, "\r\n\x00") {
		return "", fmt.Errorf("invalid panel challenge path")
	}
	return fmt.Sprintf(`%s
server {
    listen 80;
    listen [::]:80;
    server_name %s;
    server_tokens off;

    if ($host != %s) {
        return 444;
    }

    location ^~ /.well-known/acme-challenge/ {
        allow all;
        root %s;
        try_files $uri =404;
    }

    location / {
        return 503;
    }

    access_log /var/log/nginx/fluxo-panel.access.log;
    error_log /var/log/nginx/fluxo-panel.error.log error;
}
`, panelManagedHeader, domain, domain, strconv.Quote(challengeRoot)), nil
}

func renderPanelProxy(config PanelProxyConfig) (string, error) {
	if err := validatePanelConfig(config); err != nil {
		return "", err
	}
	proxyTLS := ""
	if config.UpstreamScheme == "https" {
		// Fluxo's direct listener intentionally uses its local self-signed
		// certificate. Public certificate validation terminates at Nginx.
		proxyTLS = "        proxy_ssl_verify off;\n"
	}
	return fmt.Sprintf(`%s
server {
    listen 80;
    listen [::]:80;
    server_name %s;
    server_tokens off;

    if ($host != %s) {
        return 444;
    }

    location ^~ /.well-known/acme-challenge/ {
        allow all;
        root %s;
        try_files $uri =404;
    }

    location / {
        return 301 https://%s$request_uri;
    }
}

server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name %s;
    server_tokens off;

    if ($host != %s) {
        return 444;
    }

    ssl_certificate %s;
    ssl_certificate_key %s;
    add_header Strict-Transport-Security "max-age=31536000" always;
%s
    client_max_body_size 110m;

    location / {
        proxy_pass %s://127.0.0.1:%d;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_cache_bypass $http_upgrade;
        proxy_buffering off;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
%s    }

    access_log /var/log/nginx/fluxo-panel.access.log;
    error_log /var/log/nginx/fluxo-panel.error.log error;
}
`, panelManagedHeader, config.Domain, config.Domain, strconv.Quote(config.ChallengeRoot), config.Domain, config.Domain,
		config.Domain, strconv.Quote(config.CertPath), strconv.Quote(config.KeyPath), tlsCommon,
		config.UpstreamScheme, config.UpstreamPort, proxyTLS), nil
}

func installPanelConfig(ctx context.Context, env panelConfigEnvironment, config []byte) (func(context.Context) error, error) {
	snapshot, err := capturePanelConfig(env)
	if err != nil {
		return nil, err
	}
	if err := applyPanelConfigFiles(env, config); err != nil {
		if rollbackErr := restorePanelSnapshot(ctx, env, snapshot); rollbackErr != nil {
			return nil, fmt.Errorf("%v (rollback failed: %v)", err, rollbackErr)
		}
		return nil, err
	}
	rollback := func(rollbackCtx context.Context) error {
		return restorePanelSnapshot(rollbackCtx, env, snapshot)
	}
	if err := env.validate(ctx); err != nil {
		return nil, panelRollbackError(ctx, rollback, "panel Nginx configuration validation failed", err)
	}
	if err := env.reload(ctx); err != nil {
		return nil, panelRollbackError(ctx, rollback, "panel Nginx reload failed", err)
	}

	var once sync.Once
	var restoreErr error
	return func(restoreCtx context.Context) error {
		once.Do(func() { restoreErr = rollback(restoreCtx) })
		return restoreErr
	}, nil
}

func removePanelConfig(ctx context.Context, env panelConfigEnvironment) (func(context.Context) error, error) {
	snapshot, err := capturePanelConfig(env)
	if err != nil {
		return nil, err
	}
	availablePath := filepath.Join(env.sitesAvailable, panelSiteName)
	enabledPath := filepath.Join(env.sitesEnabled, panelSiteName)
	if err := os.Remove(enabledPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("disable panel Nginx configuration: %w", err)
	}
	if err := os.Remove(availablePath); err != nil && !os.IsNotExist(err) {
		_ = restorePanelSnapshot(ctx, env, snapshot)
		return nil, fmt.Errorf("remove panel Nginx configuration: %w", err)
	}
	rollback := func(rollbackCtx context.Context) error {
		return restorePanelSnapshot(rollbackCtx, env, snapshot)
	}
	if err := env.validate(ctx); err != nil {
		return nil, panelRollbackError(ctx, rollback, "Nginx validation after panel removal failed", err)
	}
	if err := env.reload(ctx); err != nil {
		return nil, panelRollbackError(ctx, rollback, "Nginx reload after panel removal failed", err)
	}

	var once sync.Once
	var restoreErr error
	return func(restoreCtx context.Context) error {
		once.Do(func() { restoreErr = rollback(restoreCtx) })
		return restoreErr
	}, nil
}

func capturePanelConfig(env panelConfigEnvironment) (panelConfigSnapshot, error) {
	if err := ensureDirs(env.sitesAvailable, env.sitesEnabled); err != nil {
		return panelConfigSnapshot{}, err
	}
	availablePath := filepath.Join(env.sitesAvailable, panelSiteName)
	enabledPath := filepath.Join(env.sitesEnabled, panelSiteName)
	snapshot := panelConfigSnapshot{availableMode: 0644}

	if info, err := os.Lstat(availablePath); err == nil {
		if !info.Mode().IsRegular() {
			return snapshot, fmt.Errorf("existing panel Nginx configuration is not a regular file")
		}
		contents, err := os.ReadFile(availablePath)
		if err != nil {
			return snapshot, fmt.Errorf("read existing panel Nginx configuration: %w", err)
		}
		if !bytes.Contains(contents, []byte(panelManagedHeader)) {
			return snapshot, fmt.Errorf("refusing to overwrite unmanaged Nginx configuration: %s", availablePath)
		}
		snapshot.availableExists = true
		snapshot.available = contents
		snapshot.availableMode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return snapshot, fmt.Errorf("inspect existing panel Nginx configuration: %w", err)
	}

	if _, err := os.Lstat(enabledPath); err == nil {
		target, err := os.Readlink(enabledPath)
		if err != nil {
			return snapshot, fmt.Errorf("enabled panel Nginx configuration is not a symlink: %w", err)
		}
		if !symlinkPointsTo(enabledPath, target, availablePath) {
			return snapshot, fmt.Errorf("enabled panel Nginx configuration points to an unmanaged target")
		}
		snapshot.enabledExists = true
		snapshot.enabledTarget = target
	} else if !os.IsNotExist(err) {
		return snapshot, fmt.Errorf("inspect enabled panel Nginx configuration: %w", err)
	}
	return snapshot, nil
}

func panelConfigPresent(env panelConfigEnvironment) (bool, error) {
	availablePath := filepath.Join(env.sitesAvailable, panelSiteName)
	enabledPath := filepath.Join(env.sitesEnabled, panelSiteName)
	contents, err := os.ReadFile(availablePath)
	if err == nil {
		if !bytes.Contains(contents, []byte(panelManagedHeader)) {
			return false, fmt.Errorf("refusing to remove unmanaged Nginx configuration: %s", availablePath)
		}
		return true, nil
	}
	if !os.IsNotExist(err) {
		return false, fmt.Errorf("inspect panel Nginx configuration: %w", err)
	}
	if _, err := os.Lstat(enabledPath); err == nil {
		target, readErr := os.Readlink(enabledPath)
		if readErr != nil || !symlinkPointsTo(enabledPath, target, availablePath) {
			return false, fmt.Errorf("refusing to remove unmanaged panel Nginx enabled entry")
		}
		return true, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("inspect panel Nginx enabled entry: %w", err)
	}
	return false, nil
}

func applyPanelConfigFiles(env panelConfigEnvironment, config []byte) error {
	availablePath := filepath.Join(env.sitesAvailable, panelSiteName)
	enabledPath := filepath.Join(env.sitesEnabled, panelSiteName)
	if err := writeNginxFile(availablePath, config, 0644); err != nil {
		return fmt.Errorf("write panel Nginx configuration: %w", err)
	}
	if err := replaceNginxSymlink(availablePath, enabledPath); err != nil {
		return fmt.Errorf("enable panel Nginx configuration: %w", err)
	}
	return nil
}

func restorePanelSnapshot(ctx context.Context, env panelConfigEnvironment, snapshot panelConfigSnapshot) error {
	availablePath := filepath.Join(env.sitesAvailable, panelSiteName)
	enabledPath := filepath.Join(env.sitesEnabled, panelSiteName)
	var restoreErr error

	if snapshot.availableExists {
		if err := writeNginxFile(availablePath, snapshot.available, snapshot.availableMode); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("restore panel Nginx configuration: %w", err))
		}
	} else if err := os.Remove(availablePath); err != nil && !os.IsNotExist(err) {
		restoreErr = errors.Join(restoreErr, fmt.Errorf("remove staged panel Nginx configuration: %w", err))
	}

	if snapshot.enabledExists {
		if err := replaceNginxSymlink(snapshot.enabledTarget, enabledPath); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("restore panel Nginx symlink: %w", err))
		}
	} else if err := os.Remove(enabledPath); err != nil && !os.IsNotExist(err) {
		restoreErr = errors.Join(restoreErr, fmt.Errorf("remove staged panel Nginx symlink: %w", err))
	}

	if restoreErr != nil {
		return restoreErr
	}
	if err := env.validate(ctx); err != nil {
		return fmt.Errorf("validate restored panel Nginx configuration: %w", err)
	}
	if err := env.reload(ctx); err != nil {
		return fmt.Errorf("reload restored panel Nginx configuration: %w", err)
	}
	return nil
}

func panelRollbackError(ctx context.Context, rollback func(context.Context) error, operation string, operationErr error) error {
	if rollbackErr := rollback(ctx); rollbackErr != nil {
		return fmt.Errorf("%s: %v (rollback failed: %v)", operation, operationErr, rollbackErr)
	}
	return fmt.Errorf("%s: %w", operation, operationErr)
}
