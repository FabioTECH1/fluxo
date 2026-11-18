package bridge

import (
	"encoding/json"
	"strings"
	"testing"

	"fluxo/internal/services/scheduler"
)

func historicalTaskArchiveFixture() executionActivation {
	p := generationUpgradeFixture()
	p.Format, p.Kind = 4, activationInstallerHistoricalTasks
	p.SourceFloor, p.SourceFloorSHA256 = nil, ""
	saved := scheduler.CronBackendIdentity{Backend: "cronie-noble-1", Boot: "083c861b-86fb-405d-9484-09eada7ce31c", PID: 758, Start: "600", GroupInode: 2818, Group: "/system.slice/cronie.service", Configuration: strings.Repeat("a", 64)}
	current := saved
	current.PID, current.Start, current.GroupInode = 961231, "900", 65668
	p.HistoricalTaskRetirements = []scheduler.CronBackendRetirement{{Saved: saved, Current: current}}
	return p
}

func TestHistoricalInstallerRecoveryRetainsExactWitnessAndFirstConversion(t *testing.T) {
	p := historicalTaskArchiveFixture()
	if err := validateActivationPlan(p); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(p)
	var recovered executionActivation
	if err := json.Unmarshal(raw, &recovered); err != nil {
		t.Fatal(err)
	}
	if err := validateActivationPlan(recovered); err != nil {
		t.Fatal(err)
	}
	intent := installerExecutionIntent{Plan: digest(raw), Operation: p.ID, Image: p.BinarySHA256, Version: p.Version, InstallationID: p.InstallationID}
	if err := validateInstallerPlan(intent, raw); err != nil {
		t.Fatal("exact installer recovery rejected", err)
	}
	for _, name := range []string{"lost-witness", "lost-kind", "old-format", "replaced-floor", "other-kind", "changed-witness"} {
		t.Run(name, func(t *testing.T) {
			var changed executionActivation
			_ = json.Unmarshal(raw, &changed)
			switch name {
			case "lost-witness":
				changed.HistoricalTaskRetirements = nil
			case "lost-kind":
				changed.Kind = activationFresh
			case "old-format":
				changed.Format = 3
			case "replaced-floor":
				changed.SourceFloor = []byte("unrelated")
			case "other-kind":
				changed.Kind = activationTaskHelperRefresh
			case "changed-witness":
				changed.HistoricalTaskRetirements[0].Current.GroupInode++
			}
			bad, _ := json.Marshal(changed)
			if validateInstallerPlan(intent, bad) == nil {
				t.Fatal("recovery regenerated archived identity")
			}
			if name != "changed-witness" && validateActivationPlan(changed) == nil {
				t.Fatal("unscoped retirement admitted")
			}
		})
	}
}
