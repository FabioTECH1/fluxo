package database

import (
	"path/filepath"
	"testing"
)

func TestSiteVhostOverrideLifecycleAndCascade(t *testing.T) {
	previousDB := DB
	if err := InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() {
		_ = DB.Close()
		DB = previousDB
	})

	result, err := DB.Exec("INSERT INTO sites (domain, path) VALUES (?, ?)", "example.com", "/home/fluxo/example.com")
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	siteID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("site ID: %v", err)
	}

	override, err := GetSiteVhostOverride(int(siteID))
	if err != nil || override != nil {
		t.Fatalf("new site override = %#v, err = %v", override, err)
	}

	if err := SaveSiteVhostOverride(int(siteID), "server { listen 80; }\n"); err != nil {
		t.Fatalf("save override: %v", err)
	}
	override, err = GetSiteVhostOverride(int(siteID))
	if err != nil || override == nil || override.Config != "server { listen 80; }\n" {
		t.Fatalf("saved override = %#v, err = %v", override, err)
	}

	if err := SaveSiteVhostOverride(int(siteID), "server { listen 443 ssl; }\n"); err != nil {
		t.Fatalf("replace override: %v", err)
	}
	override, err = GetSiteVhostOverride(int(siteID))
	if err != nil || override == nil || override.Config != "server { listen 443 ssl; }\n" {
		t.Fatalf("replaced override = %#v, err = %v", override, err)
	}

	if _, err := DB.Exec("DELETE FROM sites WHERE id = ?", siteID); err != nil {
		t.Fatalf("delete site: %v", err)
	}
	override, err = GetSiteVhostOverride(int(siteID))
	if err != nil || override != nil {
		t.Fatalf("cascaded override = %#v, err = %v", override, err)
	}
}

func TestSaveSiteVhostOverrideRejectsMissingSite(t *testing.T) {
	previousDB := DB
	if err := InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() {
		_ = DB.Close()
		DB = previousDB
	})

	if err := SaveSiteVhostOverride(999, "server {}\n"); err == nil {
		t.Fatal("missing-site override was accepted")
	}
}
