package bridge

// historicalPreviewBridgeSHAForArchitecture recognizes the exact public trial
// previously admitted only by base-test.1. Recognition forces a bridge image
// beside a surviving base receipt back through admission; it grants no entry.
func historicalPreviewBridgeSHAForArchitecture(arch string) string {
	return map[string]string{
		"amd64": "d3b10d64f15ee64326c9c63528c08749531d229ea967014210bb531d23cb00cd",
		"arm64": "0cb7b04e266848dd2c64976280cb6c7755e62aa37b175d8a02dae2cc444862a0",
	}[arch]
}

func admittedBridgeImage(arch, image string) bool {
	stable := publishedBridgeSHAForArchitecture(arch)
	return stable != "" && image == stable
}

// BaseBridgeInstallCommand is the read-only diagnostic for the current stable
// predecessor. Interrupted work still requires its original immutable installer.
func BaseBridgeInstallCommand() string { return BridgeInstallCommand }
