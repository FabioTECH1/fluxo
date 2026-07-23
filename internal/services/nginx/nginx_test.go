package nginx

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGroupHostCertificates(t *testing.T) {
	groups, needsFallback := groupHostCertificates([]HostCertificate{
		{Domain: "example.com", CertPath: "/certs/default.pem", KeyPath: "/certs/default.key"},
		{Domain: "www.example.com", CertPath: "/certs/default.pem", KeyPath: "/certs/default.key"},
		{Domain: "new.example.com"},
		{Domain: "app.example.com", CertPath: "/certs/app.pem", KeyPath: "/certs/app.key"},
	})

	if !needsFallback {
		t.Fatal("expected an uncovered hostname to require the HTTPS guard")
	}
	if len(groups) != 3 {
		t.Fatalf("expected three certificate groups, got %d", len(groups))
	}
	if !reflect.DeepEqual(groups[0].domains, []string{"example.com", "www.example.com"}) {
		t.Fatalf("unexpected default certificate domains: %#v", groups[0].domains)
	}
	if groups[1].certPath != "" || !reflect.DeepEqual(groups[1].domains, []string{"new.example.com"}) {
		t.Fatalf("unexpected fallback TLS group: %#v", groups[1])
	}
	if groups[2].certPath != "/certs/app.pem" || !reflect.DeepEqual(groups[2].domains, []string{"app.example.com"}) {
		t.Fatalf("unexpected alias certificate group: %#v", groups[2])
	}
}

func TestRenderHostGroupsUsesIndependentCertificatesAndPrimaryRuntime(t *testing.T) {
	groups, _ := groupHostCertificates([]HostCertificate{
		{Domain: "example.com", CertPath: "/certs/default.pem", KeyPath: "/certs/default.key"},
		{Domain: "www.example.com", CertPath: "/certs/default.pem", KeyPath: "/certs/default.key"},
		{Domain: "new.example.com"},
		{Domain: "app.example.com", CertPath: "/certs/app.pem", KeyPath: "/certs/app.key"},
	})
	config := renderHostGroups(
		"example.com", "/srv/example.com/public", "8.4", "php", 0,
		"/certs/fallback.pem", "/certs/fallback.key", groups,
	)

	for _, expected := range []string{
		"server_name example.com www.example.com;",
		"ssl_certificate /certs/default.pem;",
		"server_name new.example.com;",
		"ssl_certificate /certs/fallback.pem;",
		"server_name app.example.com;",
		"ssl_certificate /certs/app.pem;",
		"fastcgi_pass unix:/var/run/php/php8.4-fpm-example.com.sock;",
	} {
		if !strings.Contains(config, expected) {
			t.Fatalf("rendered config does not contain %q:\n%s", expected, config)
		}
	}
	if strings.Contains(config, "php8.4-fpm-app.example.com.sock") {
		t.Fatalf("alias block must use the primary site's PHP runtime:\n%s", config)
	}
}

func TestRenderHostGroupsKeepsStablePHPFPMNameAfterDomainPromotion(t *testing.T) {
	groups, _ := groupHostCertificates([]HostCertificate{{Domain: "new.example.com"}})
	config := renderHostGroupsWithPool(
		"new.example.com", "/home/fluxo/old.example.com/public", "8.4", "php", 0,
		"old.example.com", "/certs/fallback.pem", "/certs/fallback.key", groups,
	)

	if !strings.Contains(config, "server_name new.example.com;") {
		t.Fatalf("rendered config does not contain the promoted domain:\n%s", config)
	}
	if !strings.Contains(config, "fastcgi_pass unix:/var/run/php/php8.4-fpm-old.example.com.sock;") {
		t.Fatalf("rendered config does not use the stable PHP-FPM pool:\n%s", config)
	}
	if strings.Contains(config, "php8.4-fpm-new.example.com.sock") {
		t.Fatalf("rendered config must not invent a new PHP-FPM pool:\n%s", config)
	}
}

func TestRenderSiteTemplateServesConfiguredHostOverFallbackHTTPS(t *testing.T) {
	tests := []struct {
		appType  string
		appPort  int
		expected string
	}{
		{appType: "php", expected: "fastcgi_pass unix:/var/run/php/php8.4-fpm-example.com.sock;"},
		{appType: "wordpress", expected: "fastcgi_pass unix:/var/run/php/php8.4-fpm-example.com.sock;"},
		{appType: "node", appPort: 3000, expected: "proxy_pass http://127.0.0.1:3000;"},
		{appType: "html", expected: "try_files $uri $uri/ =404;"},
	}

	for _, test := range tests {
		t.Run(test.appType, func(t *testing.T) {
			config := renderSiteTemplate(
				"example.com", "/srv/example.com/public", "8.4", test.appType, test.appPort,
				"", "", "/certs/fallback.pem", "/certs/fallback.key", []string{"example.com"},
			)

			for _, expected := range []string{
				"listen 80;",
				"listen 443 ssl http2;",
				"server_name example.com;",
				"ssl_certificate /certs/fallback.pem;",
				test.expected,
			} {
				if !strings.Contains(config, expected) {
					t.Fatalf("fallback HTTPS config does not contain %q:\n%s", expected, config)
				}
			}
			if strings.Count(config, "server {") != 1 {
				t.Fatalf("HTTP and fallback HTTPS must share one application server block:\n%s", config)
			}
			if strings.Contains(config, "HTTPS is not configured") || strings.Contains(config, "return 421") {
				t.Fatalf("configured fallback HTTPS must serve the application:\n%s", config)
			}
			if strings.Contains(config, "Strict-Transport-Security") {
				t.Fatalf("fallback HTTPS must not enable HSTS for an untrusted certificate:\n%s", config)
			}
		})
	}
}

func TestRenderSiteTemplateWithCertificateStillRedirectsHTTP(t *testing.T) {
	config := renderSiteTemplate(
		"example.com", "/srv/example.com/public", "8.4", "php", 0,
		"/certs/site.pem", "/certs/site.key", "/certs/fallback.pem", "/certs/fallback.key", []string{"example.com"},
	)

	if strings.Count(config, "server {") != 2 || !strings.Contains(config, "return 301 https://$host$request_uri;") {
		t.Fatalf("trusted HTTPS config must keep separate redirect and application blocks:\n%s", config)
	}
	if !strings.Contains(config, "ssl_certificate /certs/site.pem;") || strings.Contains(config, "/certs/fallback.pem") {
		t.Fatalf("trusted HTTPS config selected the wrong certificate:\n%s", config)
	}
}

func TestInstallSiteConfigRollsBackExistingConfigOnValidationFailure(t *testing.T) {
	available := filepath.Join(t.TempDir(), "sites-available")
	enabled := filepath.Join(filepath.Dir(available), "sites-enabled")
	if err := os.MkdirAll(available, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(enabled, 0755); err != nil {
		t.Fatal(err)
	}
	availablePath := filepath.Join(available, "example.com")
	enabledPath := filepath.Join(enabled, "example.com")
	previousConfig := []byte("previous valid config\n")
	if err := os.WriteFile(availablePath, previousConfig, 0600); err != nil {
		t.Fatal(err)
	}
	previousTarget := "../sites-available/example.com"
	if err := os.Symlink(previousTarget, enabledPath); err != nil {
		t.Fatal(err)
	}
	validationErr := errors.New("invalid replacement")
	env := siteConfigEnvironment{
		sitesAvailable: available,
		sitesEnabled:   enabled,
		validate: func(context.Context) error {
			installed, err := os.ReadFile(availablePath)
			if err != nil || !bytes.Equal(installed, []byte("replacement config\n")) {
				t.Fatalf("replacement was not staged for validation: contents=%q err=%v", installed, err)
			}
			return validationErr
		},
		reload: func(context.Context) error {
			t.Fatal("reload called after failed validation")
			return nil
		},
	}

	if err := installSiteConfig(context.Background(), env, "example.com", []byte("replacement config\n")); !errors.Is(err, validationErr) {
		t.Fatalf("expected validation error, got %v", err)
	}
	restored, err := os.ReadFile(availablePath)
	if err != nil || !bytes.Equal(restored, previousConfig) {
		t.Fatalf("previous config was not restored: contents=%q err=%v", restored, err)
	}
	if info, err := os.Stat(availablePath); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("previous config mode was not restored: info=%v err=%v", info, err)
	}
	if target, err := os.Readlink(enabledPath); err != nil || target != previousTarget {
		t.Fatalf("previous enabled link was not preserved: target=%q err=%v", target, err)
	}
}

func TestInstallSiteConfigRemovesNewFilesOnValidationFailure(t *testing.T) {
	available := filepath.Join(t.TempDir(), "sites-available")
	enabled := filepath.Join(filepath.Dir(available), "sites-enabled")
	validationErr := errors.New("invalid new config")
	env := siteConfigEnvironment{
		sitesAvailable: available,
		sitesEnabled:   enabled,
		validate:       func(context.Context) error { return validationErr },
		reload: func(context.Context) error {
			t.Fatal("reload called after failed validation")
			return nil
		},
	}

	if err := installSiteConfig(context.Background(), env, "example.com", []byte("invalid config\n")); !errors.Is(err, validationErr) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(available, "example.com")); !os.IsNotExist(err) {
		t.Fatalf("new invalid config was not removed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(enabled, "example.com")); !os.IsNotExist(err) {
		t.Fatalf("new invalid enabled link was not removed: %v", err)
	}
}

func TestInstallSiteConfigKeepsValidConfigWhenReloadFails(t *testing.T) {
	available := filepath.Join(t.TempDir(), "sites-available")
	enabled := filepath.Join(filepath.Dir(available), "sites-enabled")
	reloadErr := errors.New("nginx stopped")
	env := siteConfigEnvironment{
		sitesAvailable: available,
		sitesEnabled:   enabled,
		validate:       func(context.Context) error { return nil },
		reload:         func(context.Context) error { return reloadErr },
	}
	config := []byte("valid config\n")
	if err := installSiteConfig(context.Background(), env, "example.com", config); !errors.Is(err, reloadErr) {
		t.Fatalf("expected reload error, got %v", err)
	}
	installed, err := os.ReadFile(filepath.Join(available, "example.com"))
	if err != nil || !bytes.Equal(installed, config) {
		t.Fatalf("valid config was not retained: contents=%q err=%v", installed, err)
	}
	if _, err := os.Lstat(filepath.Join(enabled, "example.com")); err != nil {
		t.Fatalf("valid enabled link was not retained: %v", err)
	}
}

func TestRenderDefaultServerConfigRejectsUnknownHosts(t *testing.T) {
	config := renderDefaultServerConfig("/certs/fallback.pem", "/certs/fallback.key", true)

	for _, expected := range []string{
		"listen 80 default_server;",
		"listen [::]:80 default_server;",
		"listen 443 ssl http2 default_server;",
		"listen [::]:443 ssl http2 default_server;",
		"server_name \"\";",
		"ssl_certificate /certs/fallback.pem;",
		"ssl_certificate_key /certs/fallback.key;",
		"return 444;",
	} {
		if !strings.Contains(config, expected) {
			t.Fatalf("default server config does not contain %q:\n%s", expected, config)
		}
	}
	if strings.Contains(config, "proxy_pass") || strings.Contains(config, "fastcgi_pass") {
		t.Fatalf("default server must not forward unmatched requests:\n%s", config)
	}
	if strings.Contains(config, "~^.*$") {
		t.Fatalf("explicit default must not shadow regex virtual hosts:\n%s", config)
	}
}

func TestEnsureDefaultServerFallsBackForExistingCustomDefault(t *testing.T) {
	available := filepath.Join(t.TempDir(), "sites-available")
	enabled := filepath.Join(filepath.Dir(available), "sites-enabled")
	if err := os.MkdirAll(available, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(enabled, 0755); err != nil {
		t.Fatal(err)
	}
	customDefault := []byte("server { listen 80 default_server; server_name _; return 418; }\n")
	if err := os.WriteFile(filepath.Join(available, "default"), customDefault, 0644); err != nil {
		t.Fatal(err)
	}
	customTarget := "../sites-available/default"
	if err := os.Symlink(customTarget, filepath.Join(enabled, "default")); err != nil {
		t.Fatal(err)
	}
	validations := 0
	reloads := 0
	env := defaultServerEnvironment{
		sitesAvailable: available,
		sitesEnabled:   enabled,
		ensureCert:     func() (string, string, error) { return "/cert.pem", "/key.pem", nil },
		validate: func(context.Context) error {
			validations++
			config, err := os.ReadFile(filepath.Join(available, defaultSite))
			if err != nil {
				return err
			}
			if strings.Contains(string(config), "default_server") {
				return errors.New("duplicate default server")
			}
			return nil
		},
		reload:         func(context.Context) error { reloads++; return nil },
		reloadRequired: true,
	}
	if err := ensureDefaultServer(context.Background(), env); err != nil {
		t.Fatalf("install compatibility guard: %v", err)
	}
	config, err := os.ReadFile(filepath.Join(available, defaultSite))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), "default_server") {
		t.Fatalf("compatibility guard must not claim an existing default:\n%s", config)
	}
	if !strings.Contains(string(config), "server_name \"\" ~^.*$;") {
		t.Fatalf("compatibility guard must match unknown hostnames:\n%s", config)
	}
	if validations != 2 || reloads != 1 {
		t.Fatalf("expected explicit and compatibility validation plus one reload, got %d and %d", validations, reloads)
	}
	preservedDefault, err := os.ReadFile(filepath.Join(available, "default"))
	if err != nil || !bytes.Equal(preservedDefault, customDefault) {
		t.Fatalf("custom default config was modified, contents=%q err=%v", preservedDefault, err)
	}
	if target, err := os.Readlink(filepath.Join(enabled, "default")); err != nil || target != customTarget {
		t.Fatalf("custom default symlink was modified, target=%q err=%v", target, err)
	}

	env.validate = func(context.Context) error { validations++; return nil }
	env.reloadRequired = true
	if err := ensureDefaultServer(context.Background(), env); err != nil {
		t.Fatalf("upgrade compatibility guard after conflict removal: %v", err)
	}
	upgraded, err := os.ReadFile(filepath.Join(available, defaultSite))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(upgraded), "default_server;") != 4 {
		t.Fatalf("daemon restart did not restore explicit default ownership:\n%s", upgraded)
	}
}

func TestMigrateFallbackCertificateReferences(t *testing.T) {
	available := t.TempDir()
	fallbackRoot := filepath.Join(t.TempDir(), "fluxo-fallback")
	certPath := filepath.Join(fallbackRoot, "current", "server.crt")
	keyPath := filepath.Join(fallbackRoot, "current", "server.key")
	legacy := []byte("server {\n    ssl_certificate " + fallbackRoot + "/server-old.crt;\n    ssl_certificate_key " + fallbackRoot + "/server-old.key;\n}\n")
	unrelated := []byte("server {\n    ssl_certificate /etc/letsencrypt/live/example/fullchain.pem;\n}\n")
	legacyPath := filepath.Join(available, "legacy.test")
	unrelatedPath := filepath.Join(available, "unrelated.test")
	if err := os.WriteFile(legacyPath, legacy, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unrelatedPath, unrelated, 0644); err != nil {
		t.Fatal(err)
	}

	changes, err := migrateFallbackCertificateReferences(available, fallbackRoot, certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected one migrated config, got %d", len(changes))
	}
	migrated, _ := os.ReadFile(legacyPath)
	if !bytes.Contains(migrated, []byte("ssl_certificate "+certPath+";")) || !bytes.Contains(migrated, []byte("ssl_certificate_key "+keyPath+";")) {
		t.Fatalf("fallback references were not migrated:\n%s", migrated)
	}
	preserved, _ := os.ReadFile(unrelatedPath)
	if !bytes.Equal(preserved, unrelated) {
		t.Fatalf("unrelated certificate config was modified:\n%s", preserved)
	}
}

func TestEnsureDefaultServerInstallsGuard(t *testing.T) {
	available := filepath.Join(t.TempDir(), "sites-available")
	enabled := filepath.Join(filepath.Dir(available), "sites-enabled")

	validations := 0
	reloads := 0
	env := defaultServerEnvironment{
		sitesAvailable: available,
		sitesEnabled:   enabled,
		ensureCert:     func() (string, string, error) { return "/cert.pem", "/key.pem", nil },
		validate:       func(context.Context) error { validations++; return nil },
		reload:         func(context.Context) error { reloads++; return nil },
		reloadRequired: true,
	}
	if err := ensureDefaultServer(context.Background(), env); err != nil {
		t.Fatalf("install default server: %v", err)
	}

	guardConfig, err := os.ReadFile(filepath.Join(available, defaultSite))
	if err != nil {
		t.Fatalf("read installed guard: %v", err)
	}
	if strings.Count(string(guardConfig), "default_server;") != 4 {
		t.Fatalf("guard must own all four default listeners:\n%s", guardConfig)
	}
	if _, err := os.Lstat(filepath.Join(enabled, defaultEnabledSite)); err != nil {
		t.Fatalf("guard symlink was not installed: %v", err)
	}
	if validations != 1 || reloads != 1 {
		t.Fatalf("expected one validation and reload, got %d and %d", validations, reloads)
	}

	env.reloadRequired = false
	if err := ensureDefaultServer(context.Background(), env); err != nil {
		t.Fatalf("repeat default server ensure: %v", err)
	}
	if validations != 1 || reloads != 1 {
		t.Fatalf("unchanged active guard should not reload, got %d validations and %d reloads", validations, reloads)
	}
}

func TestEnsureDefaultServerRollsBackWhenValidationFails(t *testing.T) {
	available := filepath.Join(t.TempDir(), "sites-available")
	enabled := filepath.Join(filepath.Dir(available), "sites-enabled")
	if err := os.MkdirAll(available, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(enabled, 0755); err != nil {
		t.Fatal(err)
	}
	fallbackRoot := filepath.Join(t.TempDir(), "fluxo-fallback")
	certPath := filepath.Join(fallbackRoot, "current", "server.crt")
	keyPath := filepath.Join(fallbackRoot, "current", "server.key")
	legacyConfig := []byte("server {\n    ssl_certificate " + fallbackRoot + "/server.crt;\n    ssl_certificate_key " + fallbackRoot + "/server.key;\n}\n")
	legacyPath := filepath.Join(available, "legacy.test")
	if err := os.WriteFile(legacyPath, legacyConfig, 0644); err != nil {
		t.Fatal(err)
	}
	env := defaultServerEnvironment{
		sitesAvailable: available,
		sitesEnabled:   enabled,
		ensureCert:     func() (string, string, error) { return certPath, keyPath, nil },
		validate:       func(context.Context) error { return errors.New("invalid config") },
		reload:         func(context.Context) error { t.Fatal("reload called after failed validation"); return nil },
		reloadRequired: true,
	}
	if err := ensureDefaultServer(context.Background(), env); err == nil {
		t.Fatal("expected validation failure")
	}
	if _, err := os.Stat(filepath.Join(available, defaultSite)); !os.IsNotExist(err) {
		t.Fatalf("new guard config was not rolled back: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(enabled, defaultEnabledSite)); !os.IsNotExist(err) {
		t.Fatalf("new guard symlink was not rolled back: %v", err)
	}
	rolledBack, err := os.ReadFile(legacyPath)
	if err != nil || !bytes.Equal(rolledBack, legacyConfig) {
		t.Fatalf("fallback reference migration was not rolled back, contents=%q err=%v", rolledBack, err)
	}
}

func TestEnsureDefaultServerKeepsValidGuardWhenReloadFails(t *testing.T) {
	available := filepath.Join(t.TempDir(), "sites-available")
	enabled := filepath.Join(filepath.Dir(available), "sites-enabled")
	reloadErr := errors.New("nginx is stopped")
	env := defaultServerEnvironment{
		sitesAvailable: available,
		sitesEnabled:   enabled,
		ensureCert:     func() (string, string, error) { return "/cert.pem", "/key.pem", nil },
		validate:       func(context.Context) error { return nil },
		reload:         func(context.Context) error { return reloadErr },
		reloadRequired: true,
	}
	if err := ensureDefaultServer(context.Background(), env); !errors.Is(err, reloadErr) {
		t.Fatalf("expected reload failure, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(available, defaultSite)); err != nil {
		t.Fatalf("valid guard config should remain installed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(enabled, defaultEnabledSite)); err != nil {
		t.Fatalf("valid guard symlink should remain installed: %v", err)
	}

	reloads := 0
	env.reload = func(context.Context) error { reloads++; return nil }
	if err := ensureDefaultServer(context.Background(), env); err != nil {
		t.Fatalf("retry valid guard reload: %v", err)
	}
	if reloads != 1 {
		t.Fatalf("expected retry to reload existing guard once, got %d", reloads)
	}
}

func TestEnsureDefaultServerRefusesUnmanagedConfig(t *testing.T) {
	available := filepath.Join(t.TempDir(), "sites-available")
	enabled := filepath.Join(filepath.Dir(available), "sites-enabled")
	if err := os.MkdirAll(available, 0755); err != nil {
		t.Fatal(err)
	}
	customConfig := []byte("# custom administrator config\n")
	path := filepath.Join(available, defaultSite)
	if err := os.WriteFile(path, customConfig, 0644); err != nil {
		t.Fatal(err)
	}
	env := defaultServerEnvironment{
		sitesAvailable: available,
		sitesEnabled:   enabled,
		ensureCert:     func() (string, string, error) { return "/cert.pem", "/key.pem", nil },
		validate:       func(context.Context) error { return nil },
		reload:         func(context.Context) error { return nil },
		reloadRequired: true,
	}
	if err := ensureDefaultServer(context.Background(), env); err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("expected unmanaged config refusal, got %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(contents, customConfig) {
		t.Fatalf("unmanaged config was modified, contents=%q err=%v", contents, err)
	}
}

func TestEnsureDefaultServerReportsMissingNginx(t *testing.T) {
	defaultServerMu.Lock()
	previousPath := nginxBinaryPath
	previousLoaded := defaultServerLoaded
	previousError := defaultServerError
	nginxBinaryPath = filepath.Join(t.TempDir(), "missing-nginx")
	defaultServerLoaded = true
	defaultServerError = ""
	defaultServerMu.Unlock()
	t.Cleanup(func() {
		defaultServerMu.Lock()
		nginxBinaryPath = previousPath
		defaultServerLoaded = previousLoaded
		defaultServerError = previousError
		defaultServerMu.Unlock()
	})

	if err := EnsureDefaultServer(context.Background()); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("expected missing Nginx error, got %v", err)
	}
	active, statusErr := DefaultServerStatus()
	if active || !strings.Contains(statusErr, "not installed") {
		t.Fatalf("missing Nginx status is active=%v error=%q", active, statusErr)
	}
	if err := Reload(context.Background()); err == nil || !strings.Contains(err.Error(), "unknown-host guard") {
		t.Fatalf("reload proceeded without the unknown-host guard: %v", err)
	}
	if err := GenerateConfigWithHosts("blocked.test", "/tmp", "8.4", "html", 0, nil); err == nil || !strings.Contains(err.Error(), "unknown-host guard") {
		t.Fatalf("site config generation proceeded without the unknown-host guard: %v", err)
	}
}

func TestEnsureDefaultServerMovesLegacyGuardLinkAfterVirtualHosts(t *testing.T) {
	available := filepath.Join(t.TempDir(), "sites-available")
	enabled := filepath.Join(filepath.Dir(available), "sites-enabled")
	if err := os.MkdirAll(available, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(enabled, 0755); err != nil {
		t.Fatal(err)
	}
	availablePath := filepath.Join(available, defaultSite)
	legacyEnabledPath := filepath.Join(enabled, defaultSite)
	if err := os.Symlink(availablePath, legacyEnabledPath); err != nil {
		t.Fatal(err)
	}

	env := defaultServerEnvironment{
		sitesAvailable: available,
		sitesEnabled:   enabled,
		ensureCert:     func() (string, string, error) { return "/cert.pem", "/key.pem", nil },
		validate:       func(context.Context) error { return nil },
		reload:         func(context.Context) error { return nil },
		reloadRequired: true,
	}
	if err := ensureDefaultServer(context.Background(), env); err != nil {
		t.Fatalf("migrate legacy guard link: %v", err)
	}
	if _, err := os.Lstat(legacyEnabledPath); !os.IsNotExist(err) {
		t.Fatalf("legacy early-sorting guard link still exists: %v", err)
	}
	newEnabledPath := filepath.Join(enabled, defaultEnabledSite)
	target, err := os.Readlink(newEnabledPath)
	if err != nil || target != availablePath {
		t.Fatalf("late-sorting guard link target=%q err=%v", target, err)
	}
}
