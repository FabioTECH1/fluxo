package bridge

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMaintenanceCandidateHandoffIsCommandImageAndDeadlineScoped(t *testing.T) {
	id := "4406908b-29af-47d7-a714-c52b5557a94b"
	args := []string{strings.Repeat("a", 64), strings.Repeat("b", 64)}
	fixture := maintenanceCandidateHandoff{Format: 1, Authority: "verified-release", ID: id, ArtifactSHA256: strings.Repeat("c", 64), Version: "0.4.46-bridge.1", Architecture: runtime.GOARCH, InstallationID: strings.Repeat("d", 32), SourceFloorSHA256: strings.Repeat("e", 64), OperationID: "2974294a-9e46-46d1-ac43-f66d7f6b75aa", ParentPID: 40, ParentStart: "400", Boot: "f5edfc94-6880-4f03-b3ab-71db97046e65", Expires: 1090, Action: "requalify", Expectations: append([]string(nil), args...)}
	if err := validateMaintenanceHandoffScope(fixture, id, fixture.Version, "requalify", args, 1000); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*maintenanceCandidateHandoff){
		"self hash as provenance":   func(h *maintenanceCandidateHandoff) { h.Authority = "self-sha" },
		"unknown wire":              func(h *maintenanceCandidateHandoff) { h.Format++ },
		"different image":           func(h *maintenanceCandidateHandoff) { h.ArtifactSHA256 = "invalid" },
		"different version":         func(h *maintenanceCandidateHandoff) { h.Version = "0.4.47" },
		"different architecture":    func(h *maintenanceCandidateHandoff) { h.Architecture = "unreviewed" },
		"missing source floor":      func(h *maintenanceCandidateHandoff) { h.SourceFloorSHA256 = "" },
		"missing installation":      func(h *maintenanceCandidateHandoff) { h.InstallationID = "" },
		"wrong handoff":             func(h *maintenanceCandidateHandoff) { h.ID = "2974294a-9e46-46d1-ac43-f66d7f6b75aa" },
		"different command":         func(h *maintenanceCandidateHandoff) { h.Action = "activate-historical" },
		"different expectation":     func(h *maintenanceCandidateHandoff) { h.Expectations[0] = strings.Repeat("f", 64) },
		"missing parent":            func(h *maintenanceCandidateHandoff) { h.ParentPID = 1 },
		"invalid parent generation": func(h *maintenanceCandidateHandoff) { h.ParentStart = "invalid" },
		"missing operation":         func(h *maintenanceCandidateHandoff) { h.OperationID = "" },
		"expired":                   func(h *maintenanceCandidateHandoff) { h.Expires = 1000 },
		"excessive lifetime":        func(h *maintenanceCandidateHandoff) { h.Expires = 1121 },
	} {
		t.Run(name, func(t *testing.T) {
			h := fixture
			h.Expectations = append([]string(nil), fixture.Expectations...)
			change(&h)
			if validateMaintenanceHandoffScope(h, id, fixture.Version, "requalify", args, 1000) == nil {
				t.Fatal("unscoped candidate authority accepted")
			}
		})
	}
	h := fixture
	h.Authority = "explicit-local-sha256"
	if err := validateMaintenanceHandoffScope(h, id, fixture.Version, "requalify", args, 1000); err != nil {
		t.Fatal("explicit root local-build selection rejected", err)
	}
	h.Action, h.Expectations = "recover", []string{id}
	if validateMaintenanceHandoffScope(h, id, fixture.Version, "recover", h.Expectations, 1000) == nil {
		t.Fatal("recovery changed operation UUID")
	}
	h.Expectations = []string{h.OperationID}
	if err := validateMaintenanceHandoffScope(h, id, fixture.Version, "recover", h.Expectations, 1000); err != nil {
		t.Fatal(err)
	}
}

func TestMaintenanceCandidateCannotCommitAfterAuthorizationExpiry(t *testing.T) {
	admission := maintenanceCandidateAdmission{Handoff: maintenanceCandidateHandoff{Expires: time.Now().Unix() - 1}}
	if err := admission.revalidate("0.4.46-bridge.1"); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatal("expiry did not precede privileged/file probes", err)
	}
}
