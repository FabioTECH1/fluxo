package phpupdate

import (
	"context"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/scheduler"
	"github.com/google/uuid"
)

func TestVMSchedulerPackageEntryRequiresCoordination(t *testing.T) {
	m := vmWorkerStore(t)
	id := uuid.NewString()
	now := time.Now().Unix()
	if _, err := database.DB.Exec(`INSERT INTO php_update_operations(id,kind,version,plan_id,status,phase,message,created_at,updated_at,checkpoint) VALUES(?,'apply','8.4','','running','','',?,?,'package_transaction')`, id, now, now); err != nil {
		t.Fatal(err)
	}
	o, err := GetOperation(id)
	if err != nil {
		t.Fatal(err)
	}
	g := recoveryGeneration{ID: id, Head: id, Version: "8.4", State: "package_transaction", Pending: true}
	if err = createGeneration(g); err != nil {
		t.Fatal(err)
	}
	if err = m.packageTransaction(context.Background(), o, &g); err == nil || !strings.Contains(err.Error(), "coordination") {
		t.Fatal("old serialized generation bypassed coordination", err)
	}
	if err = m.RunTransaction(id); err == nil || !strings.Contains(err.Error(), "coordination") {
		t.Fatal("privileged package entry bypassed coordination", err)
	}
	g.SchedulerManaged = true
	if err = saveGeneration(g); err != nil {
		t.Fatal(err)
	}
	if err = m.packageTransaction(context.Background(), o, &g); err == nil {
		t.Fatal("missing Scheduler hold admitted package execution")
	}
	if err = m.RunTransaction(id); err == nil {
		t.Fatal("privileged package entry trusted a missing Scheduler hold")
	}
	// No package transaction runs in this fixture. Establish and release the
	// trusted hold, proving cleanup restores the general mutation admission gate.
	if err = scheduler.Reclose(context.Background(), "8.4", id); err != nil {
		t.Fatal(err)
	}
	if err = scheduler.AssertDrained(context.Background(), "8.4", id, nil); err != nil {
		t.Fatal(err)
	}
	if err = scheduler.Release(context.Background(), "8.4", id, nil); err != nil {
		t.Fatal(err)
	}
	g.Pending = false
	g.State = "succeeded"
	if err = saveGeneration(g); err != nil {
		t.Fatal(err)
	}
	if err = finish(id, "succeeded", "package-entry admission fixture complete; no packages changed"); err != nil {
		t.Fatal(err)
	}
}
