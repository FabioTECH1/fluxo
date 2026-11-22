package bridge

import "runtime"

// PreviewBridgeAdmissionTag is set only by the official release workflow for
// this exact opt-in base tag. Default builds retain stable bridge admission.
// Neither an environment variable nor the predecessor's version enables it.
var PreviewBridgeAdmissionTag string

const previewBaseTag = "v0.4.54-base-test.1"
const previewBridgeTag = "v0.4.54-bridge-test.7"
const previewBridgeSource = "bec82832c07a5a7dbd64a318a965b3831edb00d5"
const previewBridgeInstallCommand = "curl -fsSL https://github.com/FabioTECH1/fluxo/releases/download/v0.4.54-bridge-test.7/install.sh | sudo env FLUXO_VERSION=v0.4.54-bridge-test.7 bash"

func previewBridgeSHAForArchitecture(arch string) string {
	return map[string]string{
		"amd64": "d3b10d64f15ee64326c9c63528c08749531d229ea967014210bb531d23cb00cd",
		"arm64": "0cb7b04e266848dd2c64976280cb6c7755e62aa37b175d8a02dae2cc444862a0",
	}[arch]
}

func admittedBridgeImage(arch, image string) bool {
	stable := publishedBridgeSHAForArchitecture(arch)
	if stable == "" {
		return false
	}
	return image == stable || PreviewBridgeAdmissionTag == previewBaseTag && image == previewBridgeSHAForArchitecture(arch)
}

// BasePreviewBridgeAdmission exposes the exact additional subject in the signed
// release contract; it never replaces stable policy or host/operation checks.
func BasePreviewBridgeAdmission() map[string]string {
	if PreviewBridgeAdmissionTag != previewBaseTag {
		return nil
	}
	return map[string]string{
		"base_tag": previewBaseTag, "bridge_tag": previewBridgeTag,
		"bridge_source": previewBridgeSource,
		"amd64_sha256":  previewBridgeSHAForArchitecture("amd64"),
		"arm64_sha256":  previewBridgeSHAForArchitecture("arm64"),
	}
}

func bridgeInstallCommandForImage(arch, image string) string {
	if PreviewBridgeAdmissionTag == previewBaseTag && image != "" && image == previewBridgeSHAForArchitecture(arch) {
		return previewBridgeInstallCommand
	}
	return BridgeInstallCommand
}

// BaseBridgeInstallCommand is a read-only installer diagnostic. In particular,
// unfinished preview setup must point back to its original immutable installer.
func BaseBridgeInstallCommand() string {
	raw, err := readProtected("/usr/local/bin/fluxo", 128<<20)
	if err != nil {
		return BridgeInstallCommand
	}
	return bridgeInstallCommandForImage(runtime.GOARCH, digest(raw))
}
