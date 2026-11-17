package maintenancetask

import (
	"context"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/packages"
)

// This checks the existing profile and real fixed renew command with zero
// lineages. It proves neither ACME issuance/renewal nor cron dispatch end to end.
func TestVMExistingCertbotRenewalProfileAndCommand(t *testing.T) {
	host, _ := os.Hostname()
	known := host == "fluxo-bridge-jammy-20261101" || host == "fluxo-bridge-resolute-20261101" || host == "fluxo-bridge-noble-final-20261102"
	if os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" || !known {
		t.Skip("explicit root dedicated 22/26/fresh24 renewal acceptance only")
	}
	expected := os.Getenv("FLUXO_CERTBOT_RENEWAL_CANDIDATE_SHA256")
	decoded, err := hex.DecodeString(expected)
	if err != nil || len(decoded) != 32 || strings.ToLower(expected) != expected {
		t.Fatal("exact installed candidate SHA256 is required")
	}
	currentFloor := func() executiondomain.ReaderFloor {
		t.Helper()
		report, err := executiondomain.InspectCompatibility("/var/lib/fluxo")
		if err != nil || !report.Compatible || report.Floor == nil {
			t.Fatal("actual compatible execution floor required", err)
		}
		floor := *report.Floor
		if floor.Phase != "committed" || floor.SchedulerSchema != executiondomain.CurrentSchedulerSchema || floor.SystemTaskSchema != executiondomain.CurrentSystemTaskSchema {
			t.Fatal("current committed compatible floor required")
		}
		return floor
	}
	floor := currentFloor() // Admission before ordinary lock creation.
	image, err := readRegular("/usr/local/bin/fluxo", 128<<20)
	if err != nil || digest(image) != expected {
		t.Fatal("installed image differs from explicit candidate", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	lifecycle, err := packages.Lifecycle(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer lifecycle()
	packageLock, err := packages.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer packageLock()
	native, err := packages.NativeDPKG(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer native()
	lockedFloor := currentFloor()
	if lockedFloor.ActivationID != floor.ActivationID || lockedFloor.PlanSHA256 != floor.PlanSHA256 {
		t.Fatal("execution floor changed while waiting for maintenance ownership")
	}
	zeroLineages := func() {
		t.Helper()
		directory, err := os.Open("/etc/letsencrypt/renewal")
		if os.IsNotExist(err) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		defer directory.Close()
		names, err := directory.Readdirnames(1)
		if err != nil && err != io.EOF || len(names) != 0 {
			t.Fatal("zero-lineage private fixture required; existing certificate storage is preserved", err)
		}
	}
	before, err := qualifyProfile(ctx, CertificateRenewal)
	if err != nil {
		t.Fatal("existing renewal profile refused", err)
	}
	zeroLineages()
	contract, ok := Qualify(1, 0, "0 0 1 1 *", CertificateRenewalCommand, "root")
	if !ok {
		t.Fatal("fixed existing renewal contract unavailable")
	}
	// No cron rows, registrations, receipts or invocation outcomes are invented.
	if err = runFixed(ctx, invocation{Registration: registration{Contract: contract}}); err != nil {
		t.Fatal("actual fixed renew startup command failed", err)
	}
	zeroLineages()
	after, err := qualifyProfile(ctx, CertificateRenewal)
	if err != nil || !compatibleProfiles(before, after, false, false) {
		t.Fatal("renewal execution/configuration inputs changed", err)
	}
	t.Log("existing renewal profile and actual zero-lineage renew startup PASS", host, before.External["certbot-platform-profile"], "candidate", expected, "protected inputs", len(before.Files))
}
