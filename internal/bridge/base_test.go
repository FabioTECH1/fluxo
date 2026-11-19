package bridge

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBaseReceiptRequiresExactIdentityAndContract(t *testing.T) {
	saved := BaseAdmission{Contract: BaseContract, Mode: "base", InstallationID: strings.Repeat("a", 32), KeySHA256: strings.Repeat("b", 64)}
	raw, _ := json.Marshal(saved)
	if _, err := decodeBaseAdmission(raw); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*BaseAdmission){
		func(r *BaseAdmission) { r.Contract = Contract },
		func(r *BaseAdmission) { r.Mode = "fresh" },
		func(r *BaseAdmission) { r.InstallationID = "" },
		func(r *BaseAdmission) { r.InstallationID = strings.Repeat("A", 32) },
		func(r *BaseAdmission) { r.KeySHA256 = "0.4.51" },
	} {
		changed := saved
		mutate(&changed)
		raw, _ := json.Marshal(changed)
		if _, err := decodeBaseAdmission(raw); err == nil {
			t.Fatal("invalid base identity admitted", changed)
		}
	}
	raw, _ = json.Marshal(saved)
	if _, err := decodeBaseAdmission(append(raw, []byte(` {}`)...)); err == nil {
		t.Fatal("trailing receipt admitted")
	}
	if _, err := decodeBaseAdmission([]byte(strings.TrimSuffix(string(raw), "}") + `,"unknown":true}`)); err == nil {
		t.Fatal("unknown receipt contract admitted")
	}
}

func TestBaseBridgeSubjectIsPinnedToPublishedArtifact(t *testing.T) {
	// The gate's code pins real public artifact bytes, never a claimed version or
	// a completion phase. Both supported architectures remain explicit.
	if len(publishedBridgeSHA()) != 64 {
		t.Fatal("bridge artifact missing for build architecture")
	}
	if BridgeInstallCommand != "curl -fsSL https://github.com/FabioTECH1/fluxo/releases/download/v0.4.51/install.sh | sudo env FLUXO_VERSION=v0.4.51 bash" {
		t.Fatal("bridge instruction is not the pinned published installer")
	}
}
