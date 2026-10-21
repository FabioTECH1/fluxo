package scheduler

import (
	"errors"
	"testing"
)

func TestBranchStatusPreservesHoldsAndExplainsMigrationBlockers(t *testing.T) {
	h := legacyHost{PID: 10, Start: "100", GroupInode: 20}
	backend := BackendObservation{State: "active", Qualification: "qualified", Identity: &h}
	for _, tc := range []struct {
		name        string
		j           journal
		p           backendProofs
		state, code string
	}{
		{"certified with hold", journal{Holds: map[string]string{"8.4": "operation"}}, backendProofs{}, "paused", "maintenance_active"},
		{"missing core migration boundary", journal{}, backendProofs{Branches: map[string]backendAttempt{"8.4": {Host: h}}}, "blocked", "legacy_origin_unproven"},
		{"companion identity changed", journal{Legacy: map[string]string{"8.4": "boot"}}, backendProofs{Branches: map[string]backendAttempt{"8.4": {Host: legacyHost{PID: 11}}}}, "blocked", "backend_identity_changed"},
		{"standard identity changed", journal{Legacy: map[string]string{"8.4": "boot"}, Migrations: map[string]legacyMigration{"8.4": {Host: &legacyHost{PID: 11}}}}, backendProofs{}, "blocked", "backend_identity_changed"},
		{"hold despite changed identity", journal{Legacy: map[string]string{"8.4": "boot"}, Holds: map[string]string{"8.4": "operation"}}, backendProofs{Branches: map[string]backendAttempt{"8.4": {Host: legacyHost{PID: 11}}}}, "paused", "maintenance_active"},
		{"contradictory initial evidence", journal{Legacy: map[string]string{"8.4": "boot"}, Migrations: map[string]legacyMigration{"8.4": {MarkerReceived: true}}}, backendProofs{}, "blocked", "legacy_origin_unproven"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := branchObservations(tc.j, tc.p, backend)
			if len(got) != 1 || got[0].State != tc.state || got[0].Code != tc.code {
				t.Fatalf("unexpected status: %+v", got)
			}
		})
	}
}

func TestReconciliationFailureIsVisibleAndClearedOnSuccess(t *testing.T) {
	defer recordBackendReconcileIssue("8.4", nil)
	j := journal{Legacy: map[string]string{"8.4": "boot"}}
	b := BackendObservation{State: "active", Qualification: "qualified"}
	recordBackendReconcileIssue("8.4", errors.New("Scheduler trigger changed before reload retry"))
	got := branchObservations(j, backendProofs{}, b)
	if len(got) != 1 || got[0].State != "blocked" || got[0].Code != "reconciliation_failed" {
		t.Fatal(got)
	}
	recordBackendReconcileIssue("8.4", nil)
	if _, ok := backendReconcileIssue("8.4"); ok {
		t.Fatal("successful retry retained a stale blocker")
	}
}
