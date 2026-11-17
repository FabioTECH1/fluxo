package maintenancetask

import (
	"context"
	"strings"
	"testing"

	"fluxo/internal/services/scheduler"
	"github.com/google/uuid"
)

func acknowledgedLaunchFixture() registration {
	boot := uuid.NewString()
	return registration{Identity: uuid.NewString(), Nonce: uuid.NewString(), Boot: boot,
		Enabled: true, Published: true, Received: true, Helper: "/protected/helper", Database: "/protected/fluxo.db",
		Backend: scheduler.CronBackendIdentity{Boot: boot, PID: 100, Start: "1000", GroupInode: 77}}
}

func TestLaunchRequiresExactAcknowledgedRegistration(t *testing.T) {
	original := acknowledgedLaunchFixture()
	if err := validateLaunchRegistration(original, &original, original.Boot); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		change func(*registration)
	}{
		{"disabled", func(r *registration) { r.Enabled = false }},
		{"unpublished", func(r *registration) { r.Published = false }},
		{"unreceived", func(r *registration) { r.Received = false }},
		{"invalid nonce", func(r *registration) { r.Nonce = "" }},
		{"refreshed nonce", func(r *registration) { r.Nonce = uuid.NewString() }},
		{"identity", func(r *registration) { r.Identity = uuid.NewString() }},
		{"helper", func(r *registration) { r.Helper = "/protected/other" }},
		{"database", func(r *registration) { r.Database = "/protected/other.db" }},
		{"command", func(r *registration) { r.Contract.Command = "different" }},
		{"predecessor", func(r *registration) { r.Predecessor = "different" }},
		{"predecessor absence", func(r *registration) { r.PredecessorAbsent = true }},
		{"old receipt boot", func(r *registration) { r.Boot = uuid.NewString() }},
		{"provider boot", func(r *registration) { r.Backend.Boot = uuid.NewString() }},
		{"provider generation", func(r *registration) { r.Backend.PID++ }},
		{"provider domain", func(r *registration) { r.Backend.GroupInode++ }},
		{"unknown backend", func(r *registration) { r.Backend.Backend = "unknown" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := original
			tt.change(&r)
			if err := validateLaunchRegistration(r, &original, original.Boot); err == nil {
				t.Fatal("changed registration admitted an old launch authority")
			}
		})
	}
	for _, field := range []string{"published", "received"} {
		t.Run("snapshot "+field, func(t *testing.T) {
			saved := original
			if field == "published" {
				saved.Published = false
			} else {
				saved.Received = false
			}
			if err := validateLaunchRegistration(original, &saved, original.Boot); err == nil {
				t.Fatal("unacknowledged saved authority admitted")
			}
		})
	}
}

func TestUnacknowledgedLaunchRefusesBeforeFileOrProviderInspection(t *testing.T) {
	// No root, files, live provider or executable fixture: acknowledgement must
	// fail first in the shared boundary used by dispatch and both helper stages.
	r := acknowledgedLaunchFixture()
	boot, err := bootID()
	if err != nil {
		t.Fatal(err)
	}
	r.Boot = boot
	r.Backend.Boot = boot
	r.Received = false
	err = requireLaunchRegistration(context.Background(), r, nil)
	if err == nil || !strings.Contains(err.Error(), "real loader acknowledgement") {
		t.Fatalf("wrong admission boundary: %v", err)
	}
	// A late helper submitted before refresh must fail even if the replacement
	// registration has now obtained its own valid receipt.
	saved := r
	r.Received = true
	r.Nonce = uuid.NewString()
	err = requireLaunchRegistration(context.Background(), r, &saved)
	if err == nil || !strings.Contains(err.Error(), "older or different") {
		t.Fatalf("late helper crossed refresh: %v", err)
	}
}

func TestBridgeAcknowledgementRejectsPriorBootWithoutProviderInspection(t *testing.T) {
	r := acknowledgedLaunchFixture()
	current, err := bootID()
	if err != nil {
		t.Fatal(err)
	}
	if r.Boot == current {
		t.Fatal("fixture accidentally uses current boot")
	}
	err = requireCurrentLaunchAcknowledgement(context.Background(), r)
	if err == nil || !strings.Contains(err.Error(), "current-boot") {
		t.Fatalf("reboot was treated as new loader acknowledgement: %v", err)
	}
}
