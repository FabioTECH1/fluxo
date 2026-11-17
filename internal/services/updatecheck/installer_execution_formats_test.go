package updatecheck

import (
	"os/exec"
	"testing"
)

func TestInstallerExecutionFormatPolicy(t *testing.T) {
	if out, err := exec.Command("python3", "installer_execution_formats_test.py").CombinedOutput(); err != nil {
		t.Fatalf("installer execution format fixtures: %v\n%s", err, out)
	}
}

func TestInstallerReaderSupport(t *testing.T) {
	if out, err := exec.Command("python3", "installer_reader_support_test.py").CombinedOutput(); err != nil {
		t.Fatalf("installer retained reader fixtures: %v\n%s", err, out)
	}
}
