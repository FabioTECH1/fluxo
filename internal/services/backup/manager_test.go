package backup

import (
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/database"
)

func TestSiteMutationGuardBlocksConcurrentMutationAndActiveBackup(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { _ = database.DB.Close() })

	result, err := database.DB.Exec(
		"INSERT INTO sites (domain, path) VALUES (?, ?)", "example.com", "/home/fluxo/example.com",
	)
	if err != nil {
		t.Fatal(err)
	}
	siteID64, _ := result.LastInsertId()
	siteID := int(siteID64)
	manager := NewManager(t.TempDir())

	if err := manager.PrepareSiteMutation(siteID); err != nil {
		t.Fatalf("prepare mutation: %v", err)
	}
	if err := manager.PrepareSiteMutation(siteID); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("second mutation should be blocked, got %v", err)
	}
	manager.FinishSiteMutation(siteID)

	if _, err := database.DB.Exec(`
		INSERT INTO backup_runs
			(id, plan_id, plan_name, destination_id, destination_name, site_id, site_domain, trigger, status)
		VALUES (?, 1, 'Plan', 1, 'Destination', ?, 'example.com', 'manual', 'queued')`,
		"run-1", siteID,
	); err != nil {
		t.Fatal(err)
	}
	if err := manager.PrepareSiteMutation(siteID); err == nil || !strings.Contains(err.Error(), "active backup") {
		t.Fatalf("active backup should block mutation, got %v", err)
	}
}
