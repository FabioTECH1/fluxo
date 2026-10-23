package nodeupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/nodetoolchain"
	"fluxo/internal/services/phpupdate"
	"github.com/google/uuid"
)

func phpTestConsumer() phpupdate.MaintenanceConsumer {
	return phpupdate.MaintenanceConsumer{Branch: "8.4", Domain: "app.test", PHP: phpupdate.MaintenancePHP{Path: "/usr/bin/php8.4", Version: "8.4.22", SHA256: "receipt"}, Consumer: phpupdate.Consumer{ID: 10, SiteID: 1, Kind: "", Protocol: "artisan_natural", Command: "php8.4 artisan reports:generate", Instances: 1}, Units: []phpupdate.MaintenanceUnit{{Enabled: "enabled", Definition: map[string]string{"ExecStart": "fixed"}, Runtime: phpupdate.MaintenanceRuntime{Name: "fluxo-daemon-10.service", Active: true, Natural: true}}}}
}

func TestNodePHPLegacyWorkerRejectsReviewedIntentDriftBeforeHolds(t *testing.T) {
	for _, active := range []bool{true, false} {
		t.Run(fmt.Sprint(active), func(t *testing.T) {
			m, original := retainedTestManager(t)
			g := Generation{ID: uuid.NewString(), Head: uuid.NewString(), State: "succeeded", Plan: original}
			if err := saveGeneration(g); err != nil {
				t.Fatal(err)
			}
			owned := nodetoolchain.OwnedRuntime{Root: "/opt/fluxo/node/v24.20.0", Release: original.After}
			m.Inspect = func(context.Context) (nodetoolchain.OwnedRuntime, error) { return owned, nil }
			approved := phpTestConsumer()
			approved.Units[0].Runtime.Active = active
			observed := phpTestConsumer()
			observed.Units[0].Runtime.Active = !active
			m.RecoveryImpact = func(context.Context, Generation) (Impact, error) {
				return Impact{PHPPolicy: 1, PHP: []phpupdate.MaintenanceConsumer{observed}}, nil
			}
			p := Plan{ConsumerPolicy: consumerPolicy, ID: uuid.NewString(), Kind: "rollback", CreatedAt: time.Now().Unix(), GenerationID: g.ID, Head: g.Head, Before: owned, After: original.Before.Release, Impact: Impact{PHPPolicy: 1, PHP: []phpupdate.MaintenanceConsumer{approved}}}
			if err := savePlan(p); err != nil {
				t.Fatal(err)
			}
			o, err := m.Enqueue(context.Background(), "rollback", p.ID, uuid.NewString(), "admin", "")
			if err != nil {
				t.Fatal(err)
			}
			if err = m.apply(context.Background(), o); !errors.Is(err, ErrChanged) {
				t.Fatal("worker adopted changed PHP running intent", err)
			}
			actual, err := getGeneration(g.ID)
			if err != nil || actual.PHPState != nil || actual.Coordination != nil || actual.State != "succeeded" || digest(actual.Plan) != digest(original) {
				t.Fatal("intent rejection mutated retained generation", err)
			}
			var count int
			if err = database.DB.QueryRow("SELECT COUNT(*) FROM node_update_php_barriers").Scan(&count); err != nil || count != 0 {
				t.Fatal("intent drift installed holds", err, count)
			}
		})
	}
}
func TestNodePHPRecoveryBindsContractAndOriginalIntent(t *testing.T) {
	a := Impact{PHPPolicy: 1, PHP: []phpupdate.MaintenanceConsumer{phpTestConsumer()}}
	b := Impact{PHPPolicy: 1, PHP: []phpupdate.MaintenanceConsumer{phpTestConsumer()}}
	b.PHP[0].Units[0].Runtime.PIDs = map[int]string{99: "new observation"}
	b.PHP[0].Units[0].Runtime.State = "failed"
	if !sameOriginalConsumers(a, b) {
		t.Fatal("health became a prerequisite for recovery")
	}
	b.PHP[0].Units[0].Runtime.Active = false
	if sameOriginalConsumers(a, b) {
		t.Fatal("original running intent was replaced")
	}
	b.PHP[0].Units[0].Runtime.Active = true
	b.PHP[0].Units[0].Enabled = "disabled"
	if sameOriginalConsumers(a, b) {
		t.Fatal("enablement drift was accepted")
	}
	b.PHP[0].Units[0].Enabled = "enabled"
	b.PHP[0].Consumer.Command = "php8.4 artisan another:command"
	if sameOriginalConsumers(a, b) {
		t.Fatal("changed invocation was accepted")
	}
	if !sameOriginalConsumers(a, Impact{}) {
		t.Fatal("fresh reviewed legacy PHP additions were blocked")
	}
}
func TestNodePHPMissingNaturalModeCannotAuthorizeSignal(t *testing.T) {
	c := phpTestConsumer()
	if err := phpupdate.ValidateMaintenanceResumes([]phpupdate.MaintenanceConsumer{c}, nil); err != nil {
		t.Fatal(err)
	}
	c.Units[0].Runtime.Natural = false
	if err := phpupdate.ValidateMaintenanceResumes([]phpupdate.MaintenanceConsumer{c}, nil); err == nil {
		t.Fatal("missing natural mode authorized a signal")
	}
	c = phpTestConsumer()
	c.Units = nil
	if err := phpupdate.ValidateMaintenanceResumes([]phpupdate.MaintenanceConsumer{c}, nil); err == nil {
		t.Fatal("missing instance accepted")
	}
	c = phpTestConsumer()
	if err := phpupdate.ValidateMaintenanceResumes([]phpupdate.MaintenanceConsumer{c}, map[string]phpupdate.MaintenanceResume{"fluxo-daemon-10.service": {Completed: true}}); err == nil {
		t.Fatal("invalid completion evidence accepted")
	}
}
func TestNodePHPHoldsCloseBothAdmissions(t *testing.T) {
	m := testManager(t)
	ctx := context.Background()
	generation := uuid.NewString()
	if _, err := database.DB.Exec("INSERT INTO node_update_php_barriers(unit,generation_id,payload) VALUES('fluxo-daemon-10.service',?,'{}')", generation); err != nil {
		t.Fatal(err)
	}
	if pending, err := database.PHPMaintenancePending(); err != nil || !pending {
		t.Fatal("PHP ignored Node-owned PHP hold", pending, err)
	}
	if _, err := m.Enqueue(ctx, "check", "", uuid.NewString(), "test", ""); !errors.Is(err, ErrBusy) {
		t.Fatal("Node ignored orphan hold", err)
	}
	php := phpupdate.NewManager(t.TempDir())
	php.Launch = func(context.Context, phpupdate.Operation) error { return nil }
	if _, err := php.Enqueue(ctx, "check", "", "", uuid.NewString(), "test", ""); !errors.Is(err, phpupdate.ErrBusy) {
		t.Fatal("PHP check ignored hold", err)
	}
	if _, err := database.DB.Exec("DELETE FROM node_update_php_barriers"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Enqueue(ctx, "check", "", uuid.NewString(), "test", ""); err != nil {
		t.Fatal("settled history blocked admission", err)
	}
}
func TestNodeGenerationCannotOverwriteNewRecoveryHead(t *testing.T) {
	testManager(t)
	g := Generation{ID: uuid.NewString(), Head: uuid.NewString(), State: "prepared"}
	if err := saveGeneration(g); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("UPDATE node_update_generations SET head=? WHERE id=?", uuid.NewString(), g.ID); err != nil {
		t.Fatal(err)
	}
	if err := saveGeneration(g); !errors.Is(err, ErrChanged) {
		t.Fatal("stale generation writer replaced recovery head", err)
	}
}

func TestNodePHPRecoveryRetainsHistoricalDomainUntilExit(t *testing.T) {
	pid := os.Getpid()
	key, err := processKey(pid)
	if err != nil {
		t.Fatal(err)
	}
	initial := phpupdate.MaintenanceRuntime{PIDs: map[int]string{pid: key}}
	empty := phpupdate.MaintenanceRuntime{PIDs: map[int]string{}, State: "inactive"}
	first, err := retainPHPDomain(empty, initial)
	if err != nil || first.PIDs[pid] != key {
		t.Fatal("initial descendant identity lost", err)
	}
	again, err := retainPHPDomain(empty, first)
	if err != nil || again.PIDs[pid] != key {
		t.Fatal("prior-attempt identity lost", err)
	}
	if drained, err := phpupdate.MaintenanceDrained(again, empty); err != nil || drained {
		t.Fatal("live historical descendant counted as drained", err)
	}
	replaced := phpupdate.MaintenanceRuntime{PIDs: map[int]string{pid: key + "-different-boot-or-start"}}
	done, err := retainPHPDomain(empty, replaced)
	if err != nil || len(done.PIDs) != 0 {
		t.Fatal("replaced identity retained", err)
	}
}
