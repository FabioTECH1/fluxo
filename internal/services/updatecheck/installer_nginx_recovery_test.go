package updatecheck

import (
	"os/exec"
	"testing"
)

func TestInstallerNginxRecoveryRollbackAdmission(t *testing.T) {
	if out, err := exec.Command("python3", "installer_nginx_recovery_test.py").CombinedOutput(); err != nil {
		t.Fatalf("installer Nginx recovery fixtures: %v\n%s", err, out)
	}
}
