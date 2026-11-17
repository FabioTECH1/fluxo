package scheduler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"fluxo/internal/services/executiondomain"
)

// The immutable projection is checked by fresh plans, Format3 upgrade plans,
// archived-plan replay and exact recovery BEFORE applying either core. Even an
// unsubmitted/unloaded old invocation remains one-use authority, not absence.
func TestSchema5ProjectionRefusesEveryPendingHistoricalAuthority(t *testing.T) {
	for _, adapter := range []executiondomain.AdapterID{"", executiondomain.CgroupExitV1, executiondomain.PersistentV1} {
		for _, submitted := range []bool{false, true} {
			old := emptyExecutionJournalForSchema(4)
			if adapter == "" {
				old.Schema = 2
			}
			v := invocation{Adapter: adapter, Unit: "fluxo-scheduler-1af605c7-82ca-4245-9fc5-1240cae7d8ab.service", Submitted: submitted, Started: false}
			old.Invocations[v.Unit] = v
			original, _ := json.Marshal(old)
			next := old
			next.Schema = 5
			if v.Adapter == "" {
				v.Adapter = executiondomain.CgroupExitV1
				next.Invocations[v.Unit] = v
			}
			intended, _ := executiondomain.MergeActivationJSON(original, next)
			if err := ValidateExecutionActivationPlanForSchema(original, intended, 5); err == nil || !strings.Contains(err.Error(), "pending historical") {
				t.Fatalf("pending %q submitted=%v crossed core fence: %v", adapter, submitted, err)
			}
			// Runtime qualification cannot infer revoked authority from a missing unit;
			// refusal precedes every PID1/cgroup probe, even with a cancelled context.
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := invocationQuiescent(ctx, v); err == nil || !strings.Contains(err.Error(), "outstanding") {
				t.Fatal("unconsumed invocation treated as drained", err)
			}
		}
	}
}
func TestSchema5ProjectionPreservesConsumedLegacyTombstones(t *testing.T) {
	old := emptyExecutionJournalForSchema(4)
	v := invocation{Unit: "fluxo-scheduler-1af605c7-82ca-4245-9fc5-1240cae7d8ab.service", Started: true, Submitted: true, Guarded: true, Entry: Entry{ID: 9, MigrationNonce: "historical-nonce"}}
	old.Invocations[v.Unit] = v
	original, _ := json.Marshal(old)
	next := old
	next.Schema = 5
	v.Adapter = executiondomain.CgroupExitV1
	next.Invocations[v.Unit] = v
	intended, _ := executiondomain.MergeActivationJSON(original, next)
	if err := ValidateExecutionActivationPlanForSchema(original, intended, 5); err != nil {
		t.Fatal(err)
	}
	var saved journal
	_ = json.Unmarshal(intended, &saved)
	got := saved.Invocations[v.Unit]
	if !got.Started || !got.Submitted || got.ExecutionContract != "" || got.Bootstrap != nil || got.Helper != "" || got.Entry.MigrationNonce != "historical-nonce" {
		t.Fatal("historical tombstone acquired new launch or bootstrap semantics", got)
	}
	got.Started = false
	saved.Invocations[v.Unit] = got
	bad, _ := json.Marshal(saved)
	if ValidateExecutionActivationPlanForSchema(original, bad, 5) == nil {
		t.Fatal("recovery projection revived consumed historical authority")
	}
}
