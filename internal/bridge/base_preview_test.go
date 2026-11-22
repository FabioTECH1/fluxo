package bridge

import (
	"strings"
	"testing"
)

func TestPreviewBridgeAdmissionRequiresExactBuildMarkerAndSubject(t *testing.T) {
	saved := PreviewBridgeAdmissionTag
	t.Cleanup(func() { PreviewBridgeAdmissionTag = saved })
	for _, marker := range []string{"", "v0.4.54", "0.4.54-base-test.1", "v0.4.54-base-test.2", previewBaseTag} {
		PreviewBridgeAdmissionTag = marker
		for _, arch := range []string{"amd64", "arm64"} {
			trial := previewBridgeSHAForArchitecture(arch)
			if !knownBridgePredecessor(arch, trial) {
				t.Fatal("receipt beside trial bridge would bypass handoff", arch, marker)
			}
			if admittedBridgeImage(arch, trial) != (marker == previewBaseTag) {
				t.Fatal("trial admitted without exact preview marker", arch, marker)
			}
			if !admittedBridgeImage(arch, publishedBridgeSHAForArchitecture(arch)) {
				t.Fatal("stable bridge policy changed", arch, marker)
			}
			for _, image := range []string{"", "v0.4.54-bridge-test.7", strings.Repeat("a", 64), strings.ToUpper(trial)} {
				if admittedBridgeImage(arch, image) {
					t.Fatal("unbound predecessor admitted", arch, marker, image)
				}
			}
			wantCommand := BridgeInstallCommand
			if marker == previewBaseTag {
				wantCommand = previewBridgeInstallCommand
			}
			if bridgeInstallCommandForImage(arch, trial) != wantCommand {
				t.Fatal("unfinished preview would receive the wrong installer", marker, arch)
			}
		}
		if admittedBridgeImage("386", previewBridgeSHAForArchitecture("amd64")) || knownBridgePredecessor("386", "") {
			t.Fatal("unsupported architecture gained admission")
		}
		metadata := BasePreviewBridgeAdmission()
		if marker != previewBaseTag {
			if metadata != nil {
				t.Fatal("default release advertises experimental admission")
			}
			continue
		}
		if len(metadata) != 5 || metadata["base_tag"] != previewBaseTag || metadata["bridge_tag"] != previewBridgeTag || metadata["bridge_source"] != previewBridgeSource || metadata["amd64_sha256"] != previewBridgeSHAForArchitecture("amd64") || metadata["arm64_sha256"] != previewBridgeSHAForArchitecture("arm64") {
			t.Fatal("signed preview metadata is not the exact qualified subject", metadata)
		}
	}
}

func TestPreviewBridgePinsCannotBecomeVersionOnlyAdmission(t *testing.T) {
	for arch, want := range map[string]string{
		"amd64": "d3b10d64f15ee64326c9c63528c08749531d229ea967014210bb531d23cb00cd",
		"arm64": "0cb7b04e266848dd2c64976280cb6c7755e62aa37b175d8a02dae2cc444862a0",
	} {
		if previewBridgeSHAForArchitecture(arch) != want || knownBridgePredecessor(arch, "v0.4.54-bridge-test.7") {
			t.Fatal("trial subject changed or a version claim replaced its image", arch)
		}
	}
	if previewBridgeSource != "bec82832c07a5a7dbd64a318a965b3831edb00d5" {
		t.Fatal("preview source changed")
	}
}
