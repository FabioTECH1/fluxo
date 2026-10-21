package phpupdate

import (
	"context"
	"os"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/syscmd"
)

// This opt-in acceptance test creates only a short-lived sleep unit, never a
// package transaction. It is restricted to the disposable acceptance VM.
func TestVMReconcileInterruptedSupervisorWithoutReplay(t *testing.T) {
	hostname, _ := os.Hostname()
	if os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" || hostname != "fluxo-php-patch-test" || os.Geteuid() != 0 {
		t.Skip("disposable root/systemd acceptance VM only")
	}
	m := testStore(t)
	o, err := m.Enqueue(context.Background(), "check", "", "", "", "acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	name := "fluxo-php-update-" + o.ID + ".service"
	if _, err := syscmd.Run(context.Background(), 10*time.Second, "/usr/bin/systemd-run", "--quiet", "--unit="+name, "/bin/sleep", "60"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = syscmd.Run(context.Background(), 10*time.Second, "/usr/bin/systemctl", "stop", name) })
	_, err = database.DB.Exec("UPDATE php_update_operations SET kind='apply',status='running',checkpoint='package_transaction',updated_at=? WHERE id=?", time.Now().Add(-2*time.Minute).Unix(), o.ID)
	if err != nil {
		t.Fatal(err)
	}
	m.reconcile(context.Background())
	current, _ := GetOperation(o.ID)
	if current.Status != "running" {
		t.Fatal("independent active supervisor was mistaken for panel interruption")
	}
	if _, err := syscmd.Run(context.Background(), 10*time.Second, "/usr/bin/systemctl", "stop", name); err != nil {
		t.Fatal(err)
	}
	calls := 0
	m.Launch = func(context.Context, Operation) error { calls++; return nil }
	m.reconcile(context.Background())
	current, _ = GetOperation(o.ID)
	if current.Status != "interrupted" || current.Checkpoint != "package_transaction" || calls != 0 {
		t.Fatalf("interruption was not preserved without replay: %+v calls=%d", current, calls)
	}
}

func TestVMReconcileKeepsGateWhileDescendantDeactivates(t *testing.T) {
	hostname, _ := os.Hostname()
	if os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" || hostname != "fluxo-php-patch-test" || os.Geteuid() != 0 {
		t.Skip("disposable root/systemd acceptance VM only")
	}
	m := testStore(t)
	o, err := m.Enqueue(context.Background(), "check", "", "", "", "acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	name := "fluxo-php-update-" + o.ID + ".service"
	program := "import os,signal,time\nif os.fork()==0:\n signal.signal(signal.SIGTERM,signal.SIG_IGN)\n time.sleep(8)\n os._exit(0)\ntime.sleep(0.2)\nos._exit(0)\n"
	if _, err := syscmd.Run(context.Background(), 10*time.Second, "/usr/bin/systemd-run", "--quiet", "--unit="+name, "--property=Type=exec", "--property=TimeoutStopSec=infinity", "/usr/bin/python3", "-I", "-c", program); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = syscmd.Run(context.Background(), 5*time.Second, "/usr/bin/systemctl", "stop", "--no-block", name)
	})
	_, err = database.DB.Exec("UPDATE php_update_operations SET kind='apply',status='running',checkpoint='package_transaction',updated_at=? WHERE id=?", time.Now().Add(-2*time.Minute).Unix(), o.ID)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	observed := false
	for time.Now().Before(deadline) {
		out, _ := syscmd.Run(context.Background(), 2*time.Second, "/usr/bin/systemctl", "show", name, "--property=ActiveState")
		if unitProperties(out)["ActiveState"] == "deactivating" {
			observed = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !observed {
		t.Fatal("test descendant never entered deactivating")
	}
	m.reconcile(context.Background())
	current, _ := GetOperation(o.ID)
	busy, _ := Applying()
	if current.Status != "running" || !busy {
		t.Fatal("maintenance gate released while descendant alive")
	}
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		m.reconcile(context.Background())
		current, _ = GetOperation(o.ID)
		if current.Status == "interrupted" {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("terminal empty supervisor was not reconciled")
}
