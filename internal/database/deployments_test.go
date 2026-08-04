package database

import (
	"path/filepath"
	"testing"
)

func TestLegacyRepositorySyncDeploymentsAreBackfilled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fluxo.db")
	if err := InitDB(path); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { _ = DB.Close() })

	siteResult, err := DB.Exec("INSERT INTO sites (domain, path) VALUES ('sync.example.com', '/home/fluxo/sync.example.com')")
	if err != nil {
		t.Fatal(err)
	}
	siteID, err := siteResult.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	legacyResult, err := DB.Exec(`INSERT INTO deployments
		(site_id, status, commit_message, trigger_source)
		VALUES (?, 'failed', 'Failed to sync repository: connection refused', 'manual')`, siteID)
	if err != nil {
		t.Fatal(err)
	}
	legacyID, err := legacyResult.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	deploymentResult, err := DB.Exec(`INSERT INTO deployments
		(site_id, status, commit_hash, commit_message, trigger_source)
		VALUES (?, 'success', 'abc123', 'Failed to sync is a real commit message', 'manual')`, siteID)
	if err != nil {
		t.Fatal(err)
	}
	deploymentID, err := deploymentResult.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if err := DB.Close(); err != nil {
		t.Fatal(err)
	}

	if err := InitDB(path); err != nil {
		t.Fatalf("reopen database: %v", err)
	}

	var legacySource, deploymentSource string
	if err := DB.QueryRow("SELECT trigger_source FROM deployments WHERE id = ?", legacyID).Scan(&legacySource); err != nil {
		t.Fatal(err)
	}
	if err := DB.QueryRow("SELECT trigger_source FROM deployments WHERE id = ?", deploymentID).Scan(&deploymentSource); err != nil {
		t.Fatal(err)
	}
	if legacySource != "repo_sync" {
		t.Fatalf("legacy sync source = %q, want repo_sync", legacySource)
	}
	if deploymentSource != "manual" {
		t.Fatalf("real deployment source = %q, want manual", deploymentSource)
	}
}
