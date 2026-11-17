package updatecheck

import (
	"os/exec"
	"testing"
)

func TestInstallerGuardedCronRollbackSnapshot(t *testing.T) {
	if out, err := exec.Command("python3", "installer_cron_rollback_test.py").CombinedOutput(); err != nil {
		t.Fatalf("guarded cron rollback fixtures: %v\n%s", err, out)
	}
}
