package phpupdate

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"fluxo/internal/database"
)

func TestUpdateWithoutRollbackRequiresDurableExplicitAcknowledgement(t *testing.T) {
	m := testStore(t)
	p := testPlan(t)
	p.Rollback = RollbackCapability{Reason: "Previous package is no longer published"}
	raw, _ := json.Marshal(p)
	if _, err := database.DB.Exec("UPDATE php_update_plans SET payload=? WHERE id=?", string(raw), p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Enqueue(context.Background(), "apply", p.Version, p.ID, "", "tester", ""); !errors.Is(err, ErrRollbackAcknowledgement) {
		t.Fatalf("missing acknowledgement accepted: %v", err)
	}
	var n int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM php_update_operations").Scan(&n); err != nil || n != 0 {
		t.Fatalf("unapproved update was queued: %d %v", n, err)
	}
	o, err := m.Enqueue(context.Background(), "apply", p.Version, p.ID, "", "tester", "", true)
	if err != nil {
		t.Fatal(err)
	}
	var acknowledged bool
	if err = database.DB.QueryRow("SELECT acknowledge_no_rollback FROM php_update_approvals WHERE operation_id=?", o.ID).Scan(&acknowledged); err != nil || !acknowledged {
		t.Fatalf("approval was not retained: %v %v", acknowledged, err)
	}
}

func TestGenerationCannotOverwriteANewerAttempt(t *testing.T) {
	m := testStore(t)
	o, err := m.Enqueue(context.Background(), "check", "", "", "", "tester", "")
	if err != nil {
		t.Fatal(err)
	}
	g := recoveryGeneration{ID: o.ID, Head: o.ID, Version: "8.4", State: "prepared", Goal: "after"}
	if err = createGeneration(g); err != nil {
		t.Fatal(err)
	}
	if err = finish(o.ID, "succeeded", "fixture"); err != nil {
		t.Fatal(err)
	}
	newer, err := m.Enqueue(context.Background(), "check", "", "", "", "tester", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.DB.Exec("UPDATE php_update_generations SET head_operation_id=? WHERE id=?", newer.ID, g.ID); err != nil {
		t.Fatal(err)
	}
	if err = saveGeneration(g); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("stale attempt overwrote generation: %v", err)
	}
}
