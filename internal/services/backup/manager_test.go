package backup

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
)

func TestRunObjectBaseUsesCompactStableLayout(t *testing.T) {
	destination := database.BackupDestination{
		Prefix:   "fluxo-backups",
		ServerID: "e32d2ca6-e9de-4791-8c94-8509352c15c6",
	}
	run := database.BackupRun{
		ID:        "05le23f4-31a2-4c62-b783-10fe0951d473",
		CreatedAt: time.Date(2026, time.July, 23, 2, 0, 0, 123456000, time.UTC),
	}
	site := database.Site{
		ID:     8,
		Domain: "new-primary.example.com",
		Path:   "/home/fluxo/hotel.fottify.com",
	}

	base, err := runObjectBase(destination, run, site)
	if err != nil {
		t.Fatalf("build object base: %v", err)
	}
	want := "fluxo-backups/srv-e32d2ca6e9de/8-hotel.fottify.com/20260723-020000123"
	if base != want {
		t.Fatalf("object base = %q, want %q", base, want)
	}
}

func TestRunObjectBaseUsesDefaultBackupPrefix(t *testing.T) {
	base, err := runObjectBase(
		database.BackupDestination{ServerID: "server-identity"},
		database.BackupRun{CreatedAt: time.Date(2026, time.July, 23, 2, 0, 0, 0, time.UTC)},
		database.Site{ID: 8, Domain: "c.com", Path: "/home/fluxo/c.com"},
	)
	if err != nil {
		t.Fatalf("build object base: %v", err)
	}
	want := "fluxo-backups/srv-serveridenti/8-c.com/20260723-020000000"
	if base != want {
		t.Fatalf("object base = %q, want %q", base, want)
	}
}

func TestAvailableRunObjectBaseAdvancesOnTimestampCollision(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { _ = database.DB.Close() })

	created := time.Date(2026, time.July, 23, 2, 0, 0, 123000000, time.UTC)
	destination := database.BackupDestination{
		ID: 2, Prefix: "fluxo-backups", ServerID: "e32d2ca6e9de",
		Provider: "r2", Bucket: "backup-bucket", AccountID: "0123456789ABCDEF0123456789ABCDEF", Jurisdiction: "default",
	}
	site := database.Site{ID: 8, Domain: "c.com", Path: "/home/fluxo/c.com"}
	collidingBase, err := runObjectBase(destination, database.BackupRun{ID: "existing", CreatedAt: created}, site)
	if err != nil {
		t.Fatalf("build colliding base: %v", err)
	}
	if _, err := database.DB.Exec(`INSERT INTO backup_destinations
		(id, name, provider, bucket, account_id, jurisdiction, prefix, server_id)
		VALUES (1, 'Existing destination', 'r2', 'backup-bucket', ?, 'default', 'fluxo-backups', 'old-id')`, strings.ToLower(destination.AccountID)); err != nil {
		t.Fatalf("insert existing destination: %v", err)
	}
	if _, err := database.DB.Exec(`INSERT INTO backup_runs
		(id, plan_id, plan_name, destination_id, destination_name, site_id, site_domain, status, manifest_key)
		VALUES ('existing', 1, 'Plan', 1, 'Destination', 8, 'c.com', 'completed', ?)`, collidingBase+"/manifest.json"); err != nil {
		t.Fatalf("insert existing run: %v", err)
	}

	base, err := availableRunObjectBase(destination, database.BackupRun{ID: "new", CreatedAt: created}, site)
	if err != nil {
		t.Fatalf("allocate object base: %v", err)
	}
	want := "fluxo-backups/srv-e32d2ca6e9de/8-c.com/20260723-020000124"
	if base != want {
		t.Fatalf("object base = %q, want %q", base, want)
	}

	otherBucket := destination
	otherBucket.Bucket = "independent-bucket"
	base, err = availableRunObjectBase(otherBucket, database.BackupRun{ID: "other", CreatedAt: created}, site)
	if err != nil {
		t.Fatalf("allocate object base in independent bucket: %v", err)
	}
	if base != collidingBase {
		t.Fatalf("independent bucket base = %q, want unmodified %q", base, collidingBase)
	}
}

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
