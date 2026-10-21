package deploy

import (
	"path/filepath"
	"testing"

	"fluxo/internal/database"
)

func TestQueuedDeploymentWaitsForDurablePHPMaintenance(t *testing.T) {
	// No host package lock or deployment command is needed: all cases must
	// return before changing the queued deployment's status.
	t.Setenv("FLUXO_ENV", "test")
	for _, tc := range []struct {
		name, kind, status, generation string
		barrier, unreadable            bool
	}{
		{name: "queued repair", kind: "repair", status: "queued"},
		{name: "running repair", kind: "repair", status: "running"},
		{name: "queued rollback", kind: "rollback", status: "queued"},
		{name: "running rollback", kind: "rollback", status: "running"},
		{name: "failed operation pending generation", kind: "apply", status: "failed", generation: "recovery_pending"},
		{name: "interrupted drain", kind: "apply", status: "interrupted", generation: "draining"},
		{name: "partial release", kind: "repair", status: "interrupted", generation: "releasing"},
		{name: "barrier after terminal generation", kind: "apply", status: "succeeded", generation: "succeeded", barrier: true},
		{name: "unreadable maintenance journal", kind: "apply", status: "failed", unreadable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.DB.Close() })
			if _, err := database.DB.Exec("INSERT INTO sites(id,domain,path) VALUES (1,'maintenance.test','/home/fluxo/maintenance.test')"); err != nil {
				t.Fatal(err)
			}
			if _, err := database.DB.Exec("INSERT INTO deployments(id,site_id,status) VALUES (1,1,'pending')"); err != nil {
				t.Fatal(err)
			}
			if _, err := database.DB.Exec("INSERT INTO php_update_operations(id,kind,status,created_at,updated_at) VALUES ('maintenance',?,?,1,1)", tc.kind, tc.status); err != nil {
				t.Fatal(err)
			}
			if tc.generation != "" {
				if _, err := database.DB.Exec("INSERT INTO php_update_generations(id,version,head_operation_id,state,payload,created_at) VALUES ('maintenance','8.4','maintenance',?,'{}',1)", tc.generation); err != nil {
					t.Fatal(err)
				}
			}
			if tc.barrier {
				if _, err := database.DB.Exec("INSERT INTO php_update_barriers(unit_name,generation_id,payload) VALUES ('fluxo-daemon-1.service','maintenance','{}')"); err != nil {
					t.Fatal(err)
				}
			}
			if tc.unreadable {
				if _, err := database.DB.Exec("DROP TABLE php_update_barriers"); err != nil {
					t.Fatal(err)
				}
			}
			processDeployment(1, 1)
			var status string
			if err := database.DB.QueryRow("SELECT status FROM deployments WHERE id=1").Scan(&status); err != nil {
				t.Fatal(err)
			}
			if status != "pending" {
				t.Fatalf("maintenance admitted queued deployment: status=%s", status)
			}
		})
	}
}

func TestQueuedDeploymentWaitsOnlyForItsSitesPendingOctane(t *testing.T) {
	t.Setenv("FLUXO_ENV", "test")
	for _, tc := range []struct {
		name, status, kind string
		daemonSite         int
		blocked            bool
	}{
		{"enable in progress", "enable_pending", "laravel_octane", 1, true},
		{"enable publication pending", "enable_publish_pending", "laravel_octane", 1, true},
		{"enable fallback pending", "enable_fallback_pending", "laravel_octane", 1, true},
		{"failed enable cleanup", "enable_cleanup_pending", "laravel_octane", 1, true},
		{"other site enabling", "enable_pending", "laravel_octane", 2, false},
		{"other site publishing", "enable_publish_pending", "laravel_octane", 2, false},
		{"other site fallback", "enable_fallback_pending", "laravel_octane", 2, false},
		{"other site cleanup", "enable_cleanup_pending", "laravel_octane", 2, false},
		{"enabled Octane", "active", "laravel_octane", 1, false},
		{"ordinary failed Octane", "failed", "laravel_octane", 1, false},
		{"unrelated daemon", "enable_pending", "laravel_queue", 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.DB.Close() })
			// An invalid repository is a deterministic stop immediately after
			// admission. Admitted cases exercise the real entry without commands.
			if _, err := database.DB.Exec("INSERT INTO sites(id,domain,path,repository,branch,php_version) VALUES (1,'queued.test','/home/fluxo/queued.test','invalid repository','main','8.4'),(2,'other.test','/home/fluxo/other.test','','main','8.4')"); err != nil {
				t.Fatal(err)
			}
			if _, err := database.DB.Exec("INSERT INTO deployments(id,site_id,status) VALUES (1,1,'pending')"); err != nil {
				t.Fatal(err)
			}
			if _, err := database.DB.Exec("INSERT INTO daemons(site_id,name,managed_kind,status,command,directory) VALUES (?,'runtime fixture',?,?,'unused','/unused')", tc.daemonSite, tc.kind, tc.status); err != nil {
				t.Fatal(err)
			}
			processDeployment(1, 1)
			var status, reason string
			if err := database.DB.QueryRow("SELECT status,COALESCE(failure_reason,'') FROM deployments WHERE id=1").Scan(&status, &reason); err != nil {
				t.Fatal(err)
			}
			if tc.blocked {
				if status != "pending" || reason != "" {
					t.Fatalf("pending Octane cleanup admitted deployment: %s %s", status, reason)
				}
			} else if status != "failed" || reason != "Invalid repository configuration." {
				t.Fatalf("unrelated/settled runtime blocked admission: %s %s", status, reason)
			}
		})
	}
}
