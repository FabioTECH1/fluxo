package database

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCreateBackupRunPreservesMillisecondTimestamp(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { _ = DB.Close() })

	created := time.Date(2026, time.July, 23, 2, 0, 0, 123456000, time.UTC)
	run := BackupRun{
		ID: "run-with-milliseconds", PlanID: 1, PlanName: "Plan",
		DestinationID: 1, DestinationName: "Destination",
		SiteID: 8, SiteDomain: "c.com", Trigger: "manual", CreatedAt: created,
	}
	if err := CreateBackupRun(run); err != nil {
		t.Fatalf("create backup run: %v", err)
	}
	stored, err := GetBackupRun(run.ID)
	if err != nil {
		t.Fatalf("load backup run: %v", err)
	}
	if stored.CreatedAt.UnixMilli() != created.UnixMilli() {
		t.Fatalf("created timestamp = %s, want %s", stored.CreatedAt, created)
	}
}
