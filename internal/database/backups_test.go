package database

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackupPlanEncryptionSecretIsStoredButNeverSerialized(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { _ = DB.Close() })
	if _, err := DB.Exec("INSERT INTO sites (id, domain, path) VALUES (1, 'example.com', '/home/fluxo/example.com')"); err != nil {
		t.Fatal(err)
	}
	if _, err := DB.Exec(`INSERT INTO backup_destinations
		(id, name, provider, bucket, prefix, server_id) VALUES (1, 'R2', 'r2', 'bucket', 'backups', 'server')`); err != nil {
		t.Fatal(err)
	}
	plan := BackupPlan{
		Name: "Encrypted", SiteID: 1, DestinationID: 1, IncludeFiles: true,
		Schedule: "manual", BackupHour: 2, RetentionProfile: "recommended",
		EncryptionEnabled: true, EncryptionPassword: "enc:secret-ciphertext",
	}
	if err := CreateBackupPlan(&plan); err != nil {
		t.Fatal(err)
	}
	stored, err := GetBackupPlan(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.EncryptionEnabled || stored.EncryptionPassword != plan.EncryptionPassword {
		t.Fatalf("stored encryption state = %+v", stored)
	}
	payload, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "secret-ciphertext") || strings.Contains(string(payload), "encryption_password") {
		t.Fatalf("backup password leaked through JSON: %s", payload)
	}
}

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
