package syscmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTinkerAdmissionRevocationAndMarkerSafety(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned marker checks")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "admission")
	if err := os.WriteFile(marker, []byte("active"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := tinkerAdmitted(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := tinkerAdmitted(dir); err == nil {
		t.Fatal("revoked admission accepted")
	}
	if err := os.Symlink("/dev/null", marker); err != nil {
		t.Fatal(err)
	}
	if err := tinkerAdmitted(dir); err == nil {
		t.Fatal("symlink admission accepted")
	}
}
func TestTinkerLaunchRejectsExecutableOverrides(t *testing.T) {
	for _, version := range []string{"8.4;id", "../../bin/sh", "8.4 -r", "/usr/bin/php8.4"} {
		if _, err := StartTinker("12345678901234567890123456789012", version, "/home/fluxo/app"); err == nil {
			t.Errorf("accepted %q", version)
		}
	}
}
