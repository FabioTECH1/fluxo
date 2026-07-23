package server

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNeedsFallbackHTTPSMigration(t *testing.T) {
	legacy := []byte(`server {
    default_type text/plain;
    return 421 "HTTPS is not configured for this site.\n";
}`)
	if !needsFallbackHTTPSMigration(legacy) {
		t.Fatal("legacy fallback HTTPS response was not detected")
	}

	current := []byte(`server {
    listen 80;
    listen 443 ssl http2;
    try_files $uri $uri/ =404;
}`)
	if needsFallbackHTTPSMigration(current) {
		t.Fatal("current fallback HTTPS application config requires migration")
	}
}

func TestMigrateLegacyUnconfiguredHTTPSConfigsRegeneratesOnlyLegacySites(t *testing.T) {
	availableDir := t.TempDir()
	legacyConfig := []byte(`server {
    return 421 "HTTPS is not configured for this site.\n";
}`)
	currentConfig := []byte(`server {
    listen 80;
    listen 443 ssl http2;
}`)
	if err := os.WriteFile(filepath.Join(availableDir, "legacy.test"), legacyConfig, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(availableDir, "current.test"), currentConfig, 0644); err != nil {
		t.Fatal(err)
	}

	sites := []fallbackHTTPSMigrationSite{
		{id: 1, domain: "legacy.test"},
		{id: 2, domain: "current.test"},
		{id: 3, domain: "missing.test"},
	}
	var regenerated []int
	attempted, err := migrateLegacyUnconfiguredHTTPSConfigs(sites, availableDir, func(siteID int) error {
		regenerated = append(regenerated, siteID)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempted != 1 {
		t.Fatalf("attempted %d migrations, want 1", attempted)
	}
	if !reflect.DeepEqual(regenerated, []int{1}) {
		t.Fatalf("regenerated site IDs are %v, want [1]", regenerated)
	}
}
