package bridge

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

// Use the real protected fsynced record writer/readback in a private root
// directory. Only the subprocess dies: no live service, journal, nonce or floor
// is mutated. This complements the unprivileged stop-boundary orchestration test.
func TestVMInstallerSchedulerStopIntentSurvivesProcessDeath(t *testing.T) {
	host, _ := os.Hostname()
	if os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" || host != "fluxo-bridge-noble-stale-proof-20261117" {
		t.Skip("named root fixture and explicit opt-in required")
	}
	if root := os.Getenv("FLUXO_VM_SCHEDULER_STOP_CRASH_TEST"); root != "" {
		v := preparingSchedulerFixture()
		err := pauseInstallerSchedulerPanel(v, true, func() error { return nil }, func() error {
			return writeActivationJSON(filepath.Join(root, installerSchedulerRecord), v)
		}, func() error {
			if err := os.WriteFile(filepath.Join(root, "stopped"), []byte("private fake stop"), 0600); err != nil {
				return err
			}
			return syscall.Kill(os.Getpid(), syscall.SIGKILL)
		})
		t.Fatalf("process-death boundary returned: %v", err)
	}
	before, err := readProtected("/var/lib/fluxo/execution-readers.json", 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp("/root", "fluxo-scheduler-stop-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	cmd := exec.Command(os.Args[0], "-test.run=^TestVMInstallerSchedulerStopIntentSurvivesProcessDeath$")
	cmd.Env = append(os.Environ(), "FLUXO_VM_SCHEDULER_STOP_CRASH_TEST="+root)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("subprocess survived: %s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "stopped")); err != nil {
		t.Fatal("stop boundary missed", err)
	}
	v, err := readInstallerSchedulerIntent(root)
	if err != nil || v == nil || *v != preparingSchedulerFixture() || !installerSchedulerPending(v) {
		t.Fatal("durable restart binding lost", v, err)
	}
	after, err := readProtected("/var/lib/fluxo/execution-readers.json", 64<<10)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("live reader floor changed", err)
	}
}
