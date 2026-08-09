package database

import (
	"path/filepath"
	"testing"
)

func TestPanelDomainPersistsAndReservesHostname(t *testing.T) {
	previousDB := DB
	if err := InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() {
		_ = DB.Close()
		DB = previousDB
	})

	configured := PanelDomainConfig{
		Domain: "panel.example.com", SSLProvider: "custom",
		CertPath: "/etc/nginx/ssl/panel.example.com/server.crt",
		KeyPath:  "/etc/nginx/ssl/panel.example.com/server.key",
	}
	if err := SetPanelDomainConfig(configured); err != nil {
		t.Fatalf("save panel domain: %v", err)
	}
	loaded, err := GetPanelDomainConfig()
	if err != nil {
		t.Fatalf("load panel domain: %v", err)
	}
	if loaded.Domain != configured.Domain || loaded.SSLProvider != configured.SSLProvider {
		t.Fatalf("loaded panel domain = %+v", loaded)
	}

	if _, err := DB.Exec(`INSERT INTO sites (domain, path) VALUES ('PANEL.EXAMPLE.COM', '/home/fluxo/collision')`); err == nil {
		t.Fatal("site was allowed to claim the panel hostname")
	}
	if _, err := DB.Exec(`INSERT INTO sites (domain, path) VALUES ('site.example.com', '/home/fluxo/site.example.com')`); err != nil {
		t.Fatalf("create non-conflicting site: %v", err)
	}
	if _, err := DB.Exec(`INSERT INTO domain_aliases (site_id, domain) VALUES (1, 'panel.example.com')`); err == nil {
		t.Fatal("alias was allowed to claim the panel hostname")
	}
}

func TestPanelDomainCannotClaimSiteHostname(t *testing.T) {
	previousDB := DB
	if err := InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() {
		_ = DB.Close()
		DB = previousDB
	})

	if _, err := DB.Exec(`INSERT INTO sites (domain, path) VALUES ('site.example.com', '/home/fluxo/site.example.com')`); err != nil {
		t.Fatalf("create site: %v", err)
	}
	if err := SetPanelDomainConfig(PanelDomainConfig{Domain: "SITE.EXAMPLE.COM"}); err == nil {
		t.Fatal("panel was allowed to claim a site hostname")
	}
}
