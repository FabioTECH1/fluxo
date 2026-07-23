package server

import (
	"path/filepath"
	"testing"

	"fluxo/internal/database"
)

func TestDomainInUseReservesStableInfrastructureNameForSiteCreation(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { _ = database.DB.Close() })

	if _, err := database.DB.Exec(
		"INSERT INTO sites (domain, path) VALUES (?, ?)", "new.example.com", "/home/fluxo/old.example.com",
	); err != nil {
		t.Fatal(err)
	}

	usedForSite, err := domainInUse("old.example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	if !usedForSite {
		t.Fatal("stable infrastructure name must be reserved for site creation")
	}
	usedForAlias, err := domainInUse("old.example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if usedForAlias {
		t.Fatal("an infrastructure name alone must not reserve a public alias")
	}
}
