package nodeupdate

import (
	"context"
	"encoding/json"
	"fluxo/internal/database"
	"github.com/google/uuid"
	"testing"
)

func TestMaintenanceActivityOmitsChecksAndRepeatedStatus(t *testing.T) {
	m := testManager(t)
	for _, kind := range []string{"migrate", "check"} {
		o, e := m.Enqueue(context.Background(), "check", "", uuid.NewString(), "admin", "192.0.2.1")
		if e != nil {
			t.Fatal(e)
		}
		o.Kind = kind
		b, _ := json.Marshal(o)
		if _, e = database.DB.Exec("UPDATE node_update_operations SET kind=?,payload=? WHERE id=?", kind, string(b), o.ID); e != nil {
			t.Fatal(e)
		}
		for _, status := range []string{"running", "succeeded", "succeeded"} {
			if e = updateOperation(o.ID, status, status, "Verified"); e != nil {
				t.Fatal(e)
			}
		}
	}
	var n int
	if e := database.DB.QueryRow("SELECT COUNT(*) FROM activity WHERE type='node_update' AND username='admin' AND ip_address='192.0.2.1'").Scan(&n); e != nil || n != 1 {
		t.Fatal(n, e)
	}
}

func TestActivityFailureDoesNotChangeMaintenanceOutcome(t *testing.T) {
	m := testManager(t)
	o, e := m.Enqueue(context.Background(), "check", "", uuid.NewString(), "admin", "")
	if e != nil {
		t.Fatal(e)
	}
	o.Kind = "migrate"
	b, _ := json.Marshal(o)
	if _, e = database.DB.Exec("UPDATE node_update_operations SET kind=?,payload=? WHERE id=?", o.Kind, string(b), o.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = database.DB.Exec("CREATE TRIGGER fail_activity BEFORE INSERT ON activity BEGIN SELECT RAISE(ABORT,'test activity failure'); END"); e != nil {
		t.Fatal(e)
	}
	if e = updateOperation(o.ID, "recovery_required", "recovery_required", "Recovery needed"); e != nil {
		t.Fatal(e)
	}
	actual, e := GetOperation(o.ID)
	if e != nil || actual.Status != "recovery_required" {
		t.Fatal(actual, e)
	}
}

func TestMaintenanceActivityKeepsDiagnosticsOnOperation(t *testing.T) {
	m := testManager(t)
	o, err := m.Enqueue(context.Background(), "check", "", uuid.NewString(), "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	o.Kind = "apply"
	b, _ := json.Marshal(o)
	if _, err = database.DB.Exec("UPDATE node_update_operations SET kind=?,payload=? WHERE id=?", o.Kind, string(b), o.ID); err != nil {
		t.Fatal(err)
	}
	const diagnostic = "Update failed: queue worker has no verified Laravel signal initialization; rollback needs recovery: original error"
	if err = updateOperation(o.ID, "recovery_required", "recovery_required", diagnostic); err != nil {
		t.Fatal(err)
	}
	var summary string
	if err = database.DB.QueryRow("SELECT summary FROM activity WHERE type='node_update'").Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if summary != "Node.js update needs recovery." {
		t.Fatal("Activity contains technical diagnostics:", summary)
	}
	actual, err := GetOperation(o.ID)
	if err != nil || actual.Message != diagnostic || actual.Status != "recovery_required" {
		t.Fatal("Detailed operation failure was lost:", actual, err)
	}
}
