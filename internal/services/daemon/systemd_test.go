package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteServiceFileAtomicallyReplacesContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fluxo-daemon.service")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeServiceFile(path, []byte("new")); err != nil {
		t.Fatalf("writeServiceFile() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("service content = %q, want new", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0644 {
		t.Fatalf("service mode = %o, want 0644", info.Mode().Perm())
	}
}

func TestDaemonServiceNamesUseStablePrimaryAndNumberedInstances(t *testing.T) {
	if got := daemonServiceName(42, 1); got != "fluxo-daemon-42.service" {
		t.Fatalf("primary service name = %q", got)
	}
	if got := daemonServiceName(42, 3); got != "fluxo-daemon-42-3.service" {
		t.Fatalf("numbered service name = %q", got)
	}
}

func TestSameServiceNameSetDetectsLegacyAndStaleUnits(t *testing.T) {
	desired := []string{"fluxo-daemon-42.service", "fluxo-daemon-42-2.service"}
	if sameServiceNameSet([]string{"fluxo-daemon-42.service"}, desired) {
		t.Fatal("legacy primary-only service set was treated as reconciled")
	}
	if !sameServiceNameSet([]string{"fluxo-daemon-42-2.service", "fluxo-daemon-42.service"}, desired) {
		t.Fatal("equivalent service sets with different ordering were not equal")
	}
	if sameServiceNameSet(append(desired, "fluxo-daemon-42-3.service"), desired) {
		t.Fatal("stale numbered service was treated as reconciled")
	}
}
