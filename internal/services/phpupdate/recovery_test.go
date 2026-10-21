package phpupdate

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"fluxo/internal/database"
	"github.com/google/uuid"
)

func TestRecoveryHeadAdvancesOnceAndRejectsStaleReview(t *testing.T) {
	m := testStore(t)
	p := testPlan(t)
	o, err := m.Enqueue(context.Background(), "apply", "8.4", p.ID, uuid.NewString(), "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = finish(o.ID, "partial", "fixture"); err != nil {
		t.Fatal(err)
	}
	g := recoveryGeneration{ID: o.ID, Head: o.ID, Version: "8.4", PlanID: p.ID, Goal: "after", State: "package_transaction"}
	if err = createGeneration(g); err != nil {
		t.Fatal(err)
	}
	plan := RecoveryPlan{ID: uuid.NewString(), GenerationID: g.ID, Head: g.Head, Version: g.Version, Kind: "repair", Goal: "after", CreatedAt: time.Now().Unix()}
	raw, _ := json.Marshal(plan)
	if _, err = database.DB.Exec("INSERT INTO php_update_recovery_plans(id,generation_id,payload,created_at) VALUES (?,?,?,?)", plan.ID, g.ID, string(raw), plan.CreatedAt); err != nil {
		t.Fatal(err)
	}
	request := uuid.NewString()
	first, err := m.EnqueueRecovery(context.Background(), plan.ID, request, "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := m.EnqueueRecovery(context.Background(), plan.ID, request, "admin", "")
	if err != nil || duplicate.ID != first.ID {
		t.Fatalf("lost reply created duplicate recovery: %+v %v", duplicate, err)
	}
	if err = finish(first.ID, "failed", "fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err = m.EnqueueRecovery(context.Background(), plan.ID, uuid.NewString(), "admin", ""); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("stale review accepted: %v", err)
	}
	if err = saveGeneration(g); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("old attempt overwrote new head: %v", err)
	}
	if _, err = m.EnqueueVerification(context.Background(), first.ID, "admin", ""); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("legacy verification bypassed recovery generation: %v", err)
	}
}

func TestCompletedRecoveryUnblocksNextUpdateButFailedRecoveryDoesNot(t *testing.T) {
	for _, goal := range []string{"after", "before"} {
		t.Run(goal, func(t *testing.T) {
			m := testStore(t)
			p := testPlan(t)
			o, err := m.Enqueue(context.Background(), "apply", "8.4", p.ID, uuid.NewString(), "admin", "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = database.DB.Exec("UPDATE php_update_operations SET checkpoint='package_transaction' WHERE id=?", o.ID); err != nil {
				t.Fatal(err)
			}
			if err = finish(o.ID, "partial", "fixture"); err != nil {
				t.Fatal(err)
			}
			g := recoveryGeneration{ID: o.ID, Head: o.ID, Version: "8.4", PlanID: p.ID, Goal: goal, State: "package_transaction"}
			if err = createGeneration(g); err != nil {
				t.Fatal(err)
			}
			if _, err = m.Enqueue(context.Background(), "apply", "8.4", p.ID, uuid.NewString(), "admin", ""); !errors.Is(err, ErrRecoveryRequired) {
				t.Fatalf("unresolved generation did not block new update: %v", err)
			}
			g.State = "succeeded"
			if goal == "before" {
				g.State = "rolled_back"
			}
			if err = saveGeneration(g); err != nil {
				t.Fatal(err)
			}
			if _, err = m.Enqueue(context.Background(), "apply", "8.4", p.ID, uuid.NewString(), "admin", ""); err != nil {
				t.Fatalf("completed generation left historical failure blocking updates: %v", err)
			}
		})
	}
}

func TestScopedRecoveryRejectsUnrelatedDamageAndExternalChanges(t *testing.T) {
	p := Package{Name: "php8.4-fpm", Architecture: "amd64", Installed: "8.4.1-1", Candidate: "8.4.1-2", Automatic: true}
	g := recoveryGeneration{Version: "8.4", Archives: archiveManifest{PolicyHash: "policy", Packages: []Package{p}, After: []PackageArchive{{Name: p.Name, Architecture: p.Architecture, Version: p.Candidate}}}}
	base := packageObservation{Version: "8.4", PolicyHash: "policy", Packages: []observedPackage{{Name: p.Name, Architecture: p.Architecture, Installed: p.Candidate, State: 6, Automatic: true}}}
	if matched, err := validateRecoveryObservation(g, base, "after"); err != nil || !matched {
		t.Fatalf("valid completed transaction rejected: %v %v", matched, err)
	}
	for _, change := range []func(*packageObservation){
		func(o *packageObservation) { o.JournalDirty = true },
		func(o *packageObservation) { o.UnrelatedPending = []string{"nginx"} },
		func(o *packageObservation) { o.PolicyHash = "changed" },
		func(o *packageObservation) { o.Packages[0].Installed = "8.4.9-1" },
		func(o *packageObservation) { o.Packages[0].Automatic = false },
		func(o *packageObservation) { o.Packages[0].Held = true },
	} {
		copy := base
		copy.Packages = append([]observedPackage{}, base.Packages...)
		change(&copy)
		if _, err := validateRecoveryObservation(g, copy, "after"); err == nil {
			t.Fatalf("external state accepted: %+v", copy)
		}
	}
	base.Packages[0].State = 1
	if matched, err := validateRecoveryObservation(g, base, "after"); err != nil || matched {
		t.Fatalf("branch partial configuration falsely completed: %v %v", matched, err)
	}
}

func TestQueuedRollbackKeepsGenerationPendingBeforeAnyBarrierExists(t *testing.T) {
	m := testStore(t)
	p := testPlan(t)
	o, err := m.Enqueue(context.Background(), "apply", "8.4", p.ID, uuid.NewString(), "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = finish(o.ID, "succeeded", "fixture"); err != nil {
		t.Fatal(err)
	}
	g := recoveryGeneration{ID: o.ID, Head: o.ID, Version: "8.4", PlanID: p.ID, Goal: "after", State: "succeeded", Archives: archiveManifest{RollbackAvailable: true}}
	if err = createGeneration(g); err != nil {
		t.Fatal(err)
	}
	plan := RecoveryPlan{ID: uuid.NewString(), GenerationID: g.ID, Head: g.Head, Version: g.Version, Kind: "rollback", Goal: "before", CreatedAt: time.Now().Unix()}
	raw, _ := json.Marshal(plan)
	if _, err = database.DB.Exec("INSERT INTO php_update_recovery_plans(id,generation_id,payload,created_at) VALUES (?,?,?,?)", plan.ID, g.ID, string(raw), plan.CreatedAt); err != nil {
		t.Fatal(err)
	}
	next, err := m.EnqueueRecovery(context.Background(), plan.ID, uuid.NewString(), "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = finish(next.ID, "interrupted", "before any files were written"); err != nil {
		t.Fatal(err)
	}
	current, err := currentGeneration(next.ID)
	if err != nil || !current.Pending || current.State != "succeeded" {
		t.Fatalf("lost resumable phase or pending guard: %+v %v", current, err)
	}
	if busy, err := Applying(); err != nil || !busy {
		t.Fatalf("interrupted rollback lost maintenance gate: %v %v", busy, err)
	}
	if _, err = m.Enqueue(context.Background(), "apply", "8.4", p.ID, uuid.NewString(), "admin", ""); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("new apply eclipsed interrupted rollback: %v", err)
	}
}

func TestAdmissionRechecksAfterAnInterveningFailureOrNewerApply(t *testing.T) {
	m := testStore(t)
	p := testPlan(t)
	if err := admitApply(database.DB, "8.4", p.ID); err != nil {
		t.Fatal(err)
	}
	o, err := m.Enqueue(context.Background(), "apply", "8.4", p.ID, uuid.NewString(), "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = finish(o.ID, "partial", "intervening failure"); err != nil {
		t.Fatal(err)
	}
	g := recoveryGeneration{ID: o.ID, Head: o.ID, Version: "8.4", PlanID: p.ID, Goal: "after", State: "prepared", Pending: true}
	if err = createGeneration(g); err != nil {
		t.Fatal(err)
	}
	tx, err := database.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = admitApply(tx, "8.4", p.ID); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("transaction used stale apply admission: %v", err)
	}
	_ = tx.Rollback()
	g.State = "succeeded"
	g.Pending = false
	if err = saveGeneration(g); err != nil {
		t.Fatal(err)
	}
	review := RecoveryPlan{GenerationID: g.ID, Head: g.Head, Version: g.Version, Kind: "repair", Goal: "after"}
	if _, err = admitRecovery(database.DB, review); err != nil {
		t.Fatal(err)
	}
	newer, err := m.Enqueue(context.Background(), "apply", "8.4", p.ID, uuid.NewString(), "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = finish(newer.ID, "failed", "intervening newer attempt"); err != nil {
		t.Fatal(err)
	}
	tx, err = database.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = admitRecovery(tx, review); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("transaction admitted recovery eclipsed by newer apply: %v", err)
	}
}
