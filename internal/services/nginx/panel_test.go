package nginx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderPanelProxyIncludesTLSACMEWebSocketAndLegacyUpstream(t *testing.T) {
	config, err := renderPanelProxy(PanelProxyConfig{
		Domain: "panel.example.com", CertPath: "/etc/letsencrypt/live/panel.example.com/fullchain.pem",
		KeyPath: "/etc/letsencrypt/live/panel.example.com/privkey.pem", ChallengeRoot: "/var/lib/fluxo/acme",
		UpstreamScheme: "https", UpstreamPort: 9595,
	})
	if err != nil {
		t.Fatalf("render panel proxy: %v", err)
	}
	for _, expected := range []string{
		panelManagedHeader,
		"server_name panel.example.com;",
		"if ($host != panel.example.com)",
		`root "/var/lib/fluxo/acme";`,
		"return 301 https://panel.example.com$request_uri;",
		"proxy_pass https://127.0.0.1:9595;",
		"proxy_ssl_verify off;",
		`add_header Strict-Transport-Security "max-age=31536000" always;`,
		"proxy_set_header Upgrade $http_upgrade;",
		"proxy_set_header X-Forwarded-For $remote_addr;",
		"client_max_body_size 110m;",
	} {
		if !strings.Contains(config, expected) {
			t.Fatalf("panel config does not contain %q:\n%s", expected, config)
		}
	}
	if strings.Index(config, "location ^~ /.well-known/acme-challenge/") > strings.Index(config, "return 301 https://panel.example.com$request_uri;") {
		t.Fatal("ACME challenge location must be rendered before the redirect location")
	}
}

func TestRenderPanelProxyRejectsIPAddress(t *testing.T) {
	_, err := renderPanelProxy(PanelProxyConfig{
		Domain: "192.0.2.10", CertPath: "/etc/nginx/ssl/panel/server.crt",
		KeyPath: "/etc/nginx/ssl/panel/server.key", ChallengeRoot: "/var/lib/fluxo-acme",
		UpstreamScheme: "https", UpstreamPort: 9595,
	})
	if err == nil {
		t.Fatal("IP address was accepted as a panel hostname")
	}
}

func TestRenderPanelChallengePinsConfiguredHostname(t *testing.T) {
	config, err := renderPanelChallenge("panel.example.com", "/var/lib/fluxo-acme")
	if err != nil {
		t.Fatalf("render panel challenge: %v", err)
	}
	for _, expected := range []string{
		"server_name panel.example.com;",
		"if ($host != panel.example.com)",
		`root "/var/lib/fluxo-acme";`,
		"return 503;",
	} {
		if !strings.Contains(config, expected) {
			t.Fatalf("panel challenge does not contain %q:\n%s", expected, config)
		}
	}
}

func TestInstallPanelConfigRollsBackValidationFailure(t *testing.T) {
	available := filepath.Join(t.TempDir(), "available")
	enabled := filepath.Join(t.TempDir(), "enabled")
	if err := os.MkdirAll(available, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(enabled, 0755); err != nil {
		t.Fatal(err)
	}
	availablePath := filepath.Join(available, panelSiteName)
	oldConfig := []byte(panelManagedHeader + "\n# old\n")
	if err := os.WriteFile(availablePath, oldConfig, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(availablePath, filepath.Join(enabled, panelSiteName)); err != nil {
		t.Fatal(err)
	}

	validateCalls := 0
	reloadCalls := 0
	env := panelConfigEnvironment{
		sitesAvailable: available,
		sitesEnabled:   enabled,
		validate: func(context.Context) error {
			validateCalls++
			if validateCalls == 1 {
				return errors.New("invalid")
			}
			return nil
		},
		reload: func(context.Context) error { reloadCalls++; return nil },
	}
	if _, err := installPanelConfig(context.Background(), env, []byte(panelManagedHeader+"\n# new\n")); err == nil {
		t.Fatal("invalid panel config was accepted")
	}
	restored, err := os.ReadFile(availablePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(oldConfig) {
		t.Fatalf("panel config was not rolled back: %q", restored)
	}
	if validateCalls != 2 || reloadCalls != 1 {
		t.Fatalf("validation calls = %d, reload calls = %d", validateCalls, reloadCalls)
	}
}

func TestInstallPanelConfigRestoreClosureRemovesFirstConfiguration(t *testing.T) {
	available := filepath.Join(t.TempDir(), "available")
	enabled := filepath.Join(t.TempDir(), "enabled")
	env := panelConfigEnvironment{
		sitesAvailable: available,
		sitesEnabled:   enabled,
		validate:       func(context.Context) error { return nil },
		reload:         func(context.Context) error { return nil },
	}
	restore, err := installPanelConfig(context.Background(), env, []byte(panelManagedHeader+"\n# new\n"))
	if err != nil {
		t.Fatalf("install panel config: %v", err)
	}
	if err := restore(context.Background()); err != nil {
		t.Fatalf("restore panel config: %v", err)
	}
	for _, path := range []string{filepath.Join(available, panelSiteName), filepath.Join(enabled, panelSiteName)} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("restored path still exists: %s (%v)", path, err)
		}
	}
}

func TestPanelConfigPresentRefusesUnmanagedFile(t *testing.T) {
	available := filepath.Join(t.TempDir(), "available")
	enabled := filepath.Join(t.TempDir(), "enabled")
	if err := os.MkdirAll(available, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(enabled, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(available, panelSiteName), []byte("# owned by administrator\n"), 0644); err != nil {
		t.Fatal(err)
	}
	present, err := panelConfigPresent(panelConfigEnvironment{sitesAvailable: available, sitesEnabled: enabled})
	if err == nil || present {
		t.Fatalf("unmanaged config presence = %v, error = %v", present, err)
	}
}
