package scheduler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fluxo/internal/syscmd"
)

// Native acceptance only: the existing fixture requires root, the exact named
// VM and FLUXO_PHP_UPDATE_VM_TEST=1. This never touches the installed provider.
// It uses packaged Cronie, its real PAM/fork path and an isolated service/group.
func TestVMInstallerCronieGenerationRetirement(t *testing.T) {
	_, _, a := cronieVMFixture(t, false)
	ctx := context.Background()
	saved, err := qualifyBackendHost(ctx)
	if err != nil {
		t.Fatal(err)
	}
	trigger := filepath.Join(cronDir, "fluxo-installer-retirement-child")
	started := filepath.Join(stateDir, "retirement-child-started")
	t.Cleanup(func() { _ = os.Remove(trigger) })
	script := fmt.Sprintf("* * * * * root /bin/sh -c 'echo started > %s; /usr/bin/setsid /bin/sleep 20'\n", started)
	if err = publishCron(trigger, []byte(script)); err != nil {
		t.Fatal(err)
	}
	vmWait(t, 80*time.Second, "real Cronie child", func() bool { _, err := os.Stat(started); return err == nil })
	if err = os.Remove(trigger); err != nil {
		t.Fatal(err)
	}
	run := func(action string) {
		t.Helper()
		if out, err := syscmd.Run(ctx, 20*time.Second, "systemctl", action, a.Unit); err != nil {
			t.Fatal(action, err, out)
		}
	}
	run("restart")
	replacement, err := qualifyBackendHost(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.PID == saved.PID && replacement.Start == saved.Start {
		t.Fatal("daemon generation did not change")
	}
	if VerifyInstallerHistoricalRetirement(ctx, CronBackendRetirement{Saved: saved, Current: replacement}) == nil {
		t.Fatal("surviving old child was ignored")
	}
	vmWait(t, 30*time.Second, "natural child drain", func() bool { return VerifyCronBackendEmpty(ctx, replacement) == nil })
	// Start a genuinely new empty domain only after natural drainage. The test
	// restarts its isolated service; the production installer never restarts cron.
	run("stop")
	vmWait(t, 10*time.Second, "old cgroup removal", func() bool { _, err := os.Stat("/sys/fs/cgroup" + a.Group); return os.IsNotExist(err) })
	run("start")
	current, err := qualifyBackendHost(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if current.GroupInode == saved.GroupInode {
		t.Fatal("kernel reused old domain identity; cannot qualify retirement")
	}
	if err = VerifyInstallerHistoricalRetirement(ctx, CronBackendRetirement{Saved: saved, Current: current}); err != nil {
		t.Fatal(err)
	}
}
