package database

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDeploymentTimingStartsOnlyOnSuccessfulClaim(t *testing.T) {
	baseDB(t)
	if _, err := DB.Exec(`INSERT INTO sites(id,domain,path) VALUES(1,'timing.test','/unused');
		INSERT INTO deployments(id,site_id,status,created_at,updated_at) VALUES
		(1,1,'pending','2001-01-01 00:00:00','2001-01-01 00:00:00'),
		(2,1,'success','2001-01-01 00:00:00','2001-01-01 00:00:00');
		INSERT INTO daemons(site_id,managed_kind,status,command,directory)
		VALUES(1,'laravel_octane','enable_pending','unused','/unused')`); err != nil {
		t.Fatal(err)
	}
	assertUnstarted := func(id int64, siteID int) {
		t.Helper()
		if claimed, err := ClaimPendingDeployment(id, siteID); err != nil || claimed {
			t.Fatal("unexpected deployment admission", claimed, err)
		}
		var started, finished sql.NullTime
		if err := DB.QueryRow("SELECT started_at,finished_at FROM deployments WHERE id=?", id).Scan(&started, &finished); err != nil || started.Valid || finished.Valid {
			t.Fatal("rejected claim invented execution timing", started, finished, err)
		}
	}
	assertUnstarted(1, 1) // Pending Octane admission.
	assertUnstarted(2, 1) // Terminal history cannot be readmitted.
	if _, err := DB.Exec("DELETE FROM daemons; UPDATE sites SET deletion_status='pending' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	assertUnstarted(1, 1)
	if _, err := DB.Exec("UPDATE sites SET deletion_status='' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	assertUnstarted(1, 2) // Wrong site owner.
	before := time.Now().UTC().Truncate(time.Millisecond)
	if claimed, err := ClaimPendingDeployment(1, 1); err != nil || !claimed {
		t.Fatal("claim failed", claimed, err)
	}
	var started time.Time
	var finished sql.NullTime
	var status string
	if err := DB.QueryRow("SELECT status,started_at,finished_at FROM deployments WHERE id=1").Scan(&status, &started, &finished); err != nil {
		t.Fatal(err)
	}
	if status != "running" || started.Before(before) || started.After(time.Now().UTC()) || started.Location() != time.UTC || finished.Valid {
		t.Fatal("invalid admitted timing", status, started, finished)
	}
	if claimed, err := ClaimPendingDeployment(1, 1); err != nil || claimed {
		t.Fatal("duplicate claim admitted", claimed, err)
	}
	var repeated time.Time
	if err := DB.QueryRow("SELECT started_at FROM deployments WHERE id=1").Scan(&repeated); err != nil || !repeated.Equal(started) {
		t.Fatal("duplicate claim changed start", repeated, err)
	}
}

func TestDeploymentTimingAdditiveBaseInitializationPreservesHistoricalRows(t *testing.T) {
	for _, missing := range [][]string{{"started_at", "finished_at"}, {"finished_at"}, {"started_at"}} {
		t.Run(strings.Join(missing, ","), func(t *testing.T) {
			path := baseDB(t)
			if _, err := DB.Exec(`INSERT INTO sites(id,domain,path,deploy_script,deploy_script_mode) VALUES(1,'old.test','/unused','user-owned','legacy');
				INSERT INTO deployments(id,site_id,status,trigger_source,output,failure_dismissed_at,created_at,updated_at) VALUES
				(1,1,'success','manual','old output',NULL,'2001-01-01','2002-01-01'),
				(2,1,'failed','manual','old failure','2003-01-01','2001-01-01','2002-01-01'),
				(3,1,'success','repo_sync','sync output',NULL,'2001-01-01','2002-01-01'),
				(4,1,'pending','manual','',NULL,'2001-01-01','2002-01-01')`); err != nil {
				t.Fatal(err)
			}
			id, err := InstallationID()
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range missing {
				if _, err := DB.Exec("ALTER TABLE deployments DROP COLUMN " + name); err != nil {
					t.Fatal(err)
				}
			}
			// The same read-only validation used by base/bridge handoff must
			// accept the predecessor shape before writable initialization.
			if err := ValidateCurrentSchema(context.Background(), DB); err != nil {
				t.Fatal(err)
			}
			for _, name := range missing {
				var count int
				if err := DB.QueryRow("SELECT count(*) FROM pragma_table_info('deployments') WHERE name=?", name).Scan(&count); err != nil || count != 0 {
					t.Fatal("read-only validation changed schema", name, count, err)
				}
			}
			DB.Close()
			for range 2 {
				if err := InitBaseDB(path); err != nil {
					t.Fatal(err)
				}
				gotID, err := InstallationID()
				if err != nil || gotID != id {
					t.Fatal("identity changed", gotID, err)
				}
				var count int
				if err := DB.QueryRow(`SELECT count(*) FROM deployments WHERE started_at IS NULL AND finished_at IS NULL
					AND created_at='2001-01-01' AND updated_at='2002-01-01'`).Scan(&count); err != nil || count != 4 {
					t.Fatal("historical timing inferred or timestamps rewritten", count, err)
				}
				var output, dismissed, script, mode string
				if err := DB.QueryRow("SELECT output,CAST(failure_dismissed_at AS TEXT) FROM deployments WHERE id=2").Scan(&output, &dismissed); err != nil || output != "old failure" || dismissed != "2003-01-01" {
					t.Fatal("failure history changed", output, dismissed, err)
				}
				if err := DB.QueryRow("SELECT deploy_script,deploy_script_mode FROM sites WHERE id=1").Scan(&script, &mode); err != nil || script != "user-owned" || mode != "legacy" {
					t.Fatal("base replayed historical conversions", script, mode, err)
				}
				DB.Close()
			}
		})
	}
}

func TestDeploymentTimingBaseRefusesMalformedStorageWithoutRepair(t *testing.T) {
	for _, change := range []string{
		"ALTER TABLE deployments DROP COLUMN started_at; ALTER TABLE deployments ADD COLUMN started_at TEXT",
		"ALTER TABLE deployments DROP COLUMN finished_at; ALTER TABLE deployments ADD COLUMN finished_at DATETIME DEFAULT CURRENT_TIMESTAMP",
		"ALTER TABLE deployments DROP COLUMN started_at; ALTER TABLE deployments ADD COLUMN started_at DATETIME GENERATED ALWAYS AS (created_at)",
		"ALTER TABLE deployments DROP COLUMN finished_at; DROP TABLE commands",
	} {
		t.Run(change, func(t *testing.T) {
			path := baseDB(t)
			if _, err := DB.Exec(change); err != nil {
				t.Fatal(err)
			}
			DB.Close()
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := InitBaseDB(path); err == nil {
				t.Fatal("unsupported storage admitted")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(before) != string(after) {
				t.Fatal("refusal repaired original database", err)
			}
		})
	}
}

func TestDeploymentTimingBridgeInitializationAddsNullableStorage(t *testing.T) {
	path := baseDB(t)
	if _, err := DB.Exec(`INSERT INTO sites(id,domain,path) VALUES(1,'bridge.test','/unused');
		INSERT INTO deployments(site_id,status) VALUES(1,'success');
		ALTER TABLE deployments DROP COLUMN started_at;
		ALTER TABLE deployments DROP COLUMN finished_at;`); err != nil {
		t.Fatal(err)
	}
	DB.Close()
	if err := InitDB(path); err != nil {
		t.Fatal(err)
	}
	var started, finished sql.NullTime
	if err := DB.QueryRow("SELECT started_at,finished_at FROM deployments").Scan(&started, &finished); err != nil || started.Valid || finished.Valid {
		t.Fatal("bridge inferred timing", started, finished, err)
	}
}

func TestDeploymentTimingInterruptedExecutionHasUnknownFinish(t *testing.T) {
	baseDB(t)
	if _, err := DB.Exec(`INSERT INTO sites(id,domain,path) VALUES(1,'restart.test','/unused');
		INSERT INTO deployments(id,site_id,status) VALUES(1,1,'pending'),(2,1,'running'),(3,1,'pending'),(4,1,'success')`); err != nil {
		t.Fatal(err)
	}
	if claimed, err := ClaimPendingDeployment(1, 1); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	var started time.Time
	if err := DB.QueryRow("SELECT started_at FROM deployments WHERE id=1").Scan(&started); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := InterruptRunningDeployments(); err != nil {
			t.Fatal(err)
		}
	}
	var kept time.Time
	var finished sql.NullTime
	var status, reason string
	if err := DB.QueryRow("SELECT status,started_at,finished_at,failure_reason FROM deployments WHERE id=1").Scan(&status, &kept, &finished, &reason); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || !kept.Equal(started) || finished.Valid || !strings.Contains(reason, "interrupted") {
		t.Fatal("restart invented terminal timing", status, kept, finished, reason)
	}
	var count int
	if err := DB.QueryRow(`SELECT count(*) FROM deployments WHERE
		(id=2 AND status='failed' AND started_at IS NULL AND finished_at IS NULL) OR
		(id=3 AND status='pending' AND started_at IS NULL AND finished_at IS NULL) OR
		(id=4 AND status='success' AND started_at IS NULL AND finished_at IS NULL)`).Scan(&count); err != nil || count != 3 {
		t.Fatal("restart changed pending/terminal timing or inferred historical start", count, err)
	}
}
