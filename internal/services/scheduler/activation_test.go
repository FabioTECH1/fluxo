package scheduler

import (
	"encoding/json"
	"fluxo/internal/services/executiondomain"
	"testing"
)

func TestActivationCannotRewriteSchedulerCommandOrHistory(t *testing.T) {
	old := journal{Schema: 2, Entries: map[int]Entry{4: {ID: 4, Command: "php8.4 /home/fluxo/site/artisan schedule:run", Enabled: false}}, Invocations: map[string]invocation{}, Migrations: map[string]legacyMigration{}}
	raw, _ := json.Marshal(old)
	next := old
	next.Schema = executiondomain.CurrentSchedulerSchema
	intended, err := executiondomain.MergeActivationJSON(raw, next)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateExecutionActivationPlan(raw, intended); err != nil {
		t.Fatal(err)
	}
	entry := next.Entries[4]
	entry.Command = "php8.4 /different/artisan schedule:run"
	next.Entries[4] = entry
	changed, _ := executiondomain.MergeActivationJSON(raw, next)
	if err = ValidateExecutionActivationPlan(raw, changed); err == nil {
		t.Fatal("activation silently changed Scheduler intent")
	}
}

func TestCleanAbsencePlanRequiresEmptyCurrentCoreAndOperationMarker(t *testing.T) {
	operation := "1af605c7-82ca-4245-9fc5-1240cae7d8ab"
	changes := executionCreationPlan(operation)
	var core []byte
	for _, change := range changes {
		if change.Path == stateDir+"/state.json" {
			core = change.Data
		}
		if len(change.Fence) > 0 && string(change.Fence) != "activation:"+operation+"\n" {
			t.Fatal("marker not operation-bound")
		}
		if change.Before != "absent" {
			t.Fatal("absence creation silently reused existing state")
		}
	}
	if err := ValidateExecutionActivationPlan(nil, core); err != nil {
		t.Fatal(err)
	}
	if err := ValidateExecutionActivationPlan(nil, []byte(`{"schema":4,"entries":{"1":{"command":"custom"}},"invocations":{}}`)); err == nil {
		t.Fatal("absence invented registration authority")
	}
}
