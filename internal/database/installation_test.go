package database

import (
	"path/filepath"
	"testing"
)

func TestInstallationIDPersistsAcrossDatabaseReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fluxo.db")
	if err := InitDB(path); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	first, err := InstallationID()
	if err != nil {
		t.Fatalf("load installation ID: %v", err)
	}
	if len(first) != 32 {
		t.Fatalf("installation ID length = %d, want 32", len(first))
	}
	destination := BackupDestination{
		Name: "Legacy", Provider: "r2", Bucket: "legacy-bucket",
		AccountID: "0123456789abcdef0123456789abcdef", Jurisdiction: "default",
		Prefix: "fluxo", ServerID: "legacy-destination-id",
	}
	if err := CreateBackupDestination(&destination); err != nil {
		t.Fatalf("create legacy destination: %v", err)
	}
	legacyManifest := "fluxo/servers/legacy-destination-id/sites/8-c.com/2026/07/run-old/manifest.json"
	legacyArtifact := "fluxo/servers/legacy-destination-id/sites/8-c.com/2026/07/run-old/site-files.tar.gz"
	if _, err := DB.Exec(`INSERT INTO backup_runs
		(id, plan_id, plan_name, destination_id, destination_name, site_id, site_domain, status, manifest_key)
		VALUES ('run-old', 1, 'Legacy plan', ?, 'Legacy', 8, 'c.com', 'completed', ?)`, destination.ID, legacyManifest); err != nil {
		t.Fatalf("create legacy run: %v", err)
	}
	if _, err := DB.Exec(`INSERT INTO backup_artifacts
		(run_id, kind, object_key, filename, sha256)
		VALUES ('run-old', 'files', ?, 'site-files.tar.gz', 'checksum')`, legacyArtifact); err != nil {
		t.Fatalf("create legacy artifact: %v", err)
	}
	if err := DB.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	if err := InitDB(path); err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	t.Cleanup(func() { _ = DB.Close() })
	second, err := InstallationID()
	if err != nil {
		t.Fatalf("reload installation ID: %v", err)
	}
	if second != first {
		t.Fatalf("installation ID changed from %q to %q", first, second)
	}
	migratedDestination, err := GetBackupDestination(destination.ID)
	if err != nil {
		t.Fatalf("load migrated destination: %v", err)
	}
	if migratedDestination.ServerID != first {
		t.Fatalf("destination server ID = %q, want installation ID %q", migratedDestination.ServerID, first)
	}
	legacyRun, err := GetBackupRun("run-old")
	if err != nil {
		t.Fatalf("load legacy run: %v", err)
	}
	if legacyRun.ManifestKey != legacyManifest {
		t.Fatalf("legacy manifest key changed to %q", legacyRun.ManifestKey)
	}
	if len(legacyRun.Artifacts) != 1 || legacyRun.Artifacts[0].ObjectKey != legacyArtifact {
		t.Fatalf("legacy artifact key changed: %+v", legacyRun.Artifacts)
	}
}
