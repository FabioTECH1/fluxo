package maintenancetask

import (
	"context"
	"testing"
)

func TestInactiveReviewRequiresActualGuardedRetirement(t *testing.T) {
	r := acknowledgedLaunchFixture()
	r.Enabled = false
	if err := validateRetiredRegistration(r); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*registration)
	}{
		{"enabled", func(r *registration) { r.Enabled = true }},
		{"unpublished", func(r *registration) { r.Published = false }},
		{"unacknowledged", func(r *registration) { r.Received = false }},
		{"unfinished helper refresh", func(r *registration) { r.RefreshFrom = &helperRefresh{Helper: "old", Nonce: "pending"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := r
			tc.change(&bad)
			if err := validateRetiredRegistration(bad); err == nil {
				t.Fatal("uncertain inactivity accepted")
			}
		})
	}
	// Synthetic metadata is only the first gate. A missing/unbound helper must
	// still refuse before issuing any inactive receipt; no authority is saved.
	r.Helper = "/does/not/exist"
	if _, err := inspectRetiredRegistration(context.Background(), &journal{}, r); err == nil {
		t.Fatal("metadata alone authorized inactivity")
	}
	// Existing execution admission continues to reject the disabled registration.
	if err := validateLaunchRegistration(r, &r, r.Boot); err == nil {
		t.Fatal("inactive review enabled a future launch")
	}
}
