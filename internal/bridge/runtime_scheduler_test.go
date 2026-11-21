package bridge

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"fluxo/internal/database"
)

func TestRuntimeSchedulerDatabaseGatePreservesAllPendingRuntimeEvidence(t *testing.T) {
	old := database.DB
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	defer func() { database.DB.Close(); database.DB = old }()
	ctx := context.Background()
	if err := runtimeSchedulerDatabaseIdle(ctx, database.DB); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ table, insert string }{
		{"php_update_operations", `INSERT INTO php_update_operations(id,kind,version,status,created_at,updated_at) VALUES('pending','apply','8.4','running',1,1)`},
		{"node_update_operations", `INSERT INTO node_update_operations(id,kind,status,updated_at,payload) VALUES('pending','migrate','queued',1,'{}')`},
		{"php_update_generations", `INSERT INTO php_update_operations(id,kind,version,status,created_at,updated_at) VALUES('pending','apply','8.4','succeeded',1,1);INSERT INTO php_update_generations(id,version,head_operation_id,state,payload,created_at) VALUES('pending','8.4','pending','recovery_pending','{}',1)`},
		{"node_update_generations", `INSERT INTO node_update_generations(id,head,state,payload) VALUES('pending','pending','recovery_required','{}')`},
		{"php_repository_migrations", `INSERT INTO php_repository_migrations(plan_id,state,before_policy,created_at) VALUES('pending','pending','{}',1)`},
		{"php_update_barriers", `INSERT INTO php_update_operations(id,kind,version,status,created_at,updated_at) VALUES('pending','apply','8.4','succeeded',1,1);INSERT INTO php_update_generations(id,version,head_operation_id,state,payload,created_at) VALUES('pending','8.4','pending','succeeded','{}',1);INSERT INTO php_update_barriers(unit_name,generation_id,payload) VALUES('held.service','pending','{}')`},
		{"node_update_barriers", `INSERT INTO node_update_barriers(unit,generation_id) VALUES('held.service','pending')`},
		{"node_update_php_barriers", `INSERT INTO node_update_php_barriers(unit,generation_id,payload) VALUES('held.service','pending','{}')`},
	} {
		t.Run(tc.table, func(t *testing.T) {
			if _, err := database.DB.Exec(tc.insert); err != nil {
				t.Fatal(err)
			}
			defer func() {
				for _, table := range []string{"php_update_barriers", "node_update_barriers", "node_update_php_barriers", "php_update_generations", "node_update_generations", "php_repository_migrations", "php_update_operations", "node_update_operations"} {
					if _, err := database.DB.Exec("DELETE FROM " + table); err != nil {
						t.Error(err)
					}
				}
			}()
			if err := runtimeSchedulerDatabaseIdle(ctx, database.DB); err == nil {
				t.Fatal("pending evidence ignored")
			}
			var count int
			if err := database.DB.QueryRow("SELECT COUNT(*) FROM " + tc.table).Scan(&count); err != nil || count != 1 {
				t.Fatal("gate mutated evidence", count, err)
			}
		})
	}
}

// The Scheduler package separately verifies that only its single typed drain
// observation is retryable. Same-text and joined errors must not prolong a
// request or hide an integrity/configuration failure at this boundary.
func TestRuntimeSchedulerRetryKeepsHardFailuresAndCancellation(t *testing.T) {
	for _, blocked := range []error{errors.New("Scheduler setup is waiting for earlier cron jobs to finish. PHP has not been changed"), errors.Join(errors.New("busy"), errors.New("unsafe provider"))} {
		calls := 0
		got := retryRuntimeSchedulerPolicy(t.Context(), func() error { calls++; return blocked }, func(context.Context) error { t.Fatal("hard error retried"); return nil })
		if !errors.Is(got, blocked) || calls != 1 {
			t.Fatal(got, calls)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got := retryRuntimeSchedulerPolicy(ctx, func() error { t.Fatal("canceled request attempted renewal"); return nil }, func(context.Context) error { t.Fatal("canceled request paused"); return nil })
	if !errors.Is(got, context.Canceled) {
		t.Fatal(got)
	}
	calls := 0
	if err := retryRuntimeSchedulerPolicy(t.Context(), func() error { calls++; return nil }, func(context.Context) error { t.Fatal("successful renewal retried"); return nil }); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
}
