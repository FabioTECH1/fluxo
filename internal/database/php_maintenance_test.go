package database

import (
	"path/filepath"
	"testing"
)

func TestPHPMaintenanceGateReleasesOnlyAfterRecoveryAndBarrierCleanup(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = DB.Close() })
	assertPending := func(want bool) {
		t.Helper()
		got, err := PHPMaintenancePending()
		if err != nil || got != want {
			t.Fatalf("pending=%v, want %v: %v", got, want, err)
		}
	}
	exec := func(query string) {
		t.Helper()
		if _, err := DB.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	assertPending(false)
	exec("INSERT INTO php_update_operations(id,kind,status,created_at,updated_at) VALUES ('maintenance','check','running',1,1)")
	assertPending(false) // Read-only availability checks do not block applications.
	exec("UPDATE php_update_operations SET kind='apply'")
	assertPending(true)
	exec("UPDATE php_update_operations SET status='interrupted'")
	exec("INSERT INTO php_update_generations(id,version,head_operation_id,state,payload,created_at) VALUES ('maintenance','8.4','maintenance','recovery_pending','{}',1)")
	assertPending(true)
	exec("INSERT INTO php_update_barriers(unit_name,generation_id,payload) VALUES ('fluxo-daemon-1.service','maintenance','{}')")
	exec("UPDATE php_update_generations SET state='rolled_back'")
	assertPending(true)
	exec("DELETE FROM php_update_barriers")
	assertPending(false)
	exec("UPDATE php_update_operations SET kind='verify',status='queued'")
	assertPending(true)
	exec("UPDATE php_update_operations SET status='succeeded'")
	assertPending(false)
	if err := DB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := PHPMaintenancePending(); err == nil {
		t.Fatal("unreadable maintenance state was silently accepted")
	}
}
