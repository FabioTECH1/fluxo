package maintenancetask

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestInstallerExecutionAdmissionStillRefusesUnboundAuthority(t *testing.T) {
	// A future host-profile exception must never accept an incomplete or
	// missing guarded publication. Neither path writes a receipt or trigger.
	for _, r := range []registration{{}, acknowledgedLaunchFixture()} {
		if err := bridgeExecutionRegistration(context.Background(), r); err == nil {
			t.Fatal("execution-only inspection accepted unbound trigger/helper")
		}
		if err := bridgeReadyRegistration(context.Background(), r); err == nil {
			t.Fatal("certification accepted unbound trigger/helper")
		}
	}
}

func TestInstallerExecutionAdmissionKeepsCertificationSeparate(t *testing.T) {
	// A source boundary protects the two call sites: admission must retain
	// durable execution checks without acquiring future runtime approval.
	raw, err := os.ReadFile("bridge.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "func bridgeExecutionRegistration(")
	end := strings.Index(source, "func bridgeReadyRegistration(")
	if start < 0 || end <= start {
		t.Fatal("execution admission boundary missing")
	}
	for _, check := range []string{"verifyRegistrationFiles", "verifyHelperReaderContracts", "requireCurrentLaunchAcknowledgement", "readyBackend"} {
		if !strings.Contains(source[start:end], check+"(") {
			t.Fatalf("execution safety check lost: %s", check)
		}
	}
	if strings.Contains(source[start:end], "qualifyContractProfile(") || !strings.Contains(source[end:], "qualifyContractProfile(") {
		t.Fatal("future profile approval/certification boundary changed")
	}
}
