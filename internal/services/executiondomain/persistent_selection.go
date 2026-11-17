package executiondomain

import (
	"context"
	"runtime"
	"strings"
)

// This bounded selection is not a host/profile qualification shortcut. Exact
// stock provider/PAM/package policies and current loader receipts still gate
// every caller. No other systemd249 distribution inherits Jammy's contract.
func requireQualifiedPersistentSelection(ctx context.Context) error {
	raw, err := readProtected("/usr/lib/os-release", 64<<10)
	if err != nil {
		return err
	}
	if !persistentSelectionPlatform(string(raw), runtime.GOARCH) {
		return ErrUnsupportedHost
	}
	return RequireAdapterHost(ctx, PersistentV1)
}
func persistentSelectionPlatform(raw, arch string) bool {
	if arch != "amd64" {
		return false
	}
	values := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if key == "ID" || key == "VERSION_ID" {
			if _, duplicate := values[key]; duplicate {
				return false
			}
			values[key] = strings.Trim(value, "\"")
		}
	}
	return values["ID"] == "ubuntu" && values["VERSION_ID"] == "22.04"
}
