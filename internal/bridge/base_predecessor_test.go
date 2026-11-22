package bridge

import (
	"strings"
	"testing"
)

func TestHistoricalPreviewCannotBeAdmittedOrHiddenByBaseReceipt(t *testing.T) {
	for arch, want := range map[string]string{
		"amd64": "d3b10d64f15ee64326c9c63528c08749531d229ea967014210bb531d23cb00cd",
		"arm64": "0cb7b04e266848dd2c64976280cb6c7755e62aa37b175d8a02dae2cc444862a0",
	} {
		if historicalPreviewBridgeSHAForArchitecture(arch) != want || !knownBridgePredecessor(arch, want) {
			t.Fatal("receipt beside the historical preview could bypass handoff", arch)
		}
		for _, image := range []string{want, "", "v0.4.54-bridge-test.7", strings.Repeat("a", 64), strings.ToUpper(want)} {
			if admittedBridgeImage(arch, image) {
				t.Fatal("preview or unbound subject admitted", arch, image)
			}
		}
		if !admittedBridgeImage(arch, publishedBridgeSHAForArchitecture(arch)) {
			t.Fatal("qualified primary bridge refused", arch)
		}
	}
	if admittedBridgeImage("386", historicalPreviewBridgeSHAForArchitecture("amd64")) || knownBridgePredecessor("386", "") {
		t.Fatal("unsupported architecture gained admission")
	}
	if BaseBridgeInstallCommand() != BridgeInstallCommand {
		t.Fatal("base diagnostic recommends a historical preview")
	}
}

func TestPriorBaseImagesRemainDistinctFromBridgeAdmission(t *testing.T) {
	for arch, image := range map[string]string{
		"amd64": "f2b322c29555ea3818c8399c1eddd678525c9e66d01ced59719ce337af187e57",
		"arm64": "b0e55e9b9b2da8a16efd913c486277fe2125861b00bb5aa1c8dceb0645b54bc8",
	} {
		// A prior base must still present its protected identity and floor. Its
		// version or image never becomes authority for a bridge handoff.
		if knownBridgePredecessor(arch, image) || admittedBridgeImage(arch, image) {
			t.Fatal("completed prior base was confused with bridge admission", arch)
		}
	}
}

func TestBase4ImageNeverBecomesBridgeAuthority(t *testing.T) {
	for arch, image := range map[string]string{"amd64": "70e7ceb688771ce46249cf3eb368020b6ad1c667bee773dfbf567607fa41549a", "arm64": "fc6e7d8af6f8059ad953bcb824e1a355f54aec4ae0c9fbfcfc075b0cbfe4b549"} {
		if knownBridgePredecessor(arch, image) || admittedBridgeImage(arch, image) {
			t.Fatal("base4 was routed back through bridge", arch)
		}
	}
}
