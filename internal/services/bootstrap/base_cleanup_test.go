package bootstrap

import (
	"os"
	"strings"
	"testing"
)

// This is a source boundary: base startup may seed canonical fresh defaults,
// but historical command conversion belongs to the predecessor bridge.
func TestBaseStartupDoesNotConvertHistoricalCleanup(t *testing.T) {
	raw, err := os.ReadFile("bootstrap.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"ensurePackageCleanupLock",
		"command='apt-get autoremove -y && apt-get autoclean'",
	} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("historical cleanup conversion remains in base startup: %s", forbidden)
		}
	}
	if !strings.Contains(string(raw), `{"System Cleanup", "0 0 * * 0", systemCleanupCommand, "root", ""}`) {
		t.Fatal("canonical fresh cleanup default lost")
	}
	if !strings.Contains(string(raw), "maintenancetask.DefaultRetired(c.Kind)") {
		t.Fatal("explicit default retirement protection lost")
	}
}
