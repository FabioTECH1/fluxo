package phpupdate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/syscmd"
	"golang.org/x/sys/unix"
)

// This child deliberately exits between the same durable intent/delivery steps
// used by activation. It never kills FPM and never invokes a package manager.
func TestVMFPMDeliveryCrashChild(t *testing.T) {
	requireRecoveryVM(t)
	if os.Getenv("FLUXO_FPM_CRASH_CHILD") != "1" {
		t.Skip("acceptance subprocess only")
	}
	if err := database.InitDB(os.Getenv("FLUXO_FPM_CRASH_DB")); err != nil {
		t.Fatal(err)
	}
	g, err := getGeneration(os.Getenv("FLUXO_FPM_CRASH_GENERATION"))
	if err != nil {
		t.Fatal(err)
	}
	o, err := GetOperation(g.Head)
	if err != nil {
		t.Fatal(err)
	}
	name := "php8.4-fpm.service"
	g.Attempt.Signalled = map[string]bool{name: true}
	g.Attempt.SignalDelivery = map[string]string{name: "intent"}
	g.Attempt.SignalAttempts = map[string]string{name: g.Head}
	m := NewManager(filepath.Dir(os.Getenv("FLUXO_FPM_CRASH_DB")))
	if err = m.saveActivation(o, g.Attempt, []*recoveryGeneration{&g}); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("FLUXO_FPM_CRASH_BOUNDARY") == "delivered" {
		u := g.Attempt.Units[0]
		if err = signalRecordedProcess(context.Background(), u, u.MainPID, unix.SIGUSR2); err != nil {
			t.Fatal(err)
		}
	}
	os.Exit(73)
}

func vmReplaceFPMInode(t *testing.T) {
	t.Helper()
	path := "/usr/sbin/php-fpm8.4"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".fluxo-fpm-acceptance-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(info.Mode().Perm()); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err = syncDirectory(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
}

func vmFPMCurrent(t *testing.T) unitSnapshot {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		u, err := unitState(context.Background(), "php8.4-fpm.service")
		if err == nil && u.Active {
			ready := true
			for pid := range u.PIDs {
				if processCurrent(pid) != nil {
					ready = false
				}
			}
			if ready {
				return u
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("FPM did not adopt the byte-identical package executable inode")
	return unitSnapshot{}
}

func TestVMFPMIntentAndUnrecordedDeliveryRecoverSafely(t *testing.T) {
	requireRecoveryVM(t)
	for _, boundary := range []string{"intent", "delivered"} {
		t.Run(boundary, func(t *testing.T) {
			m := testStore(t)
			vmCloneExistingConsumers(t)
			before, err := unitState(context.Background(), "php8.4-fpm.service")
			if err != nil || !before.Active {
				t.Fatalf("FPM prerequisite: %v", err)
			}
			vmReplaceFPMInode(t)
			t.Cleanup(func() {
				_, _ = syscmd.Run(context.Background(), 15*time.Second, "/usr/bin/systemctl", "reload", "php8.4-fpm.service")
			})
			if processCurrent(before.MainPID) == nil {
				t.Fatal("old FPM master was not genuinely stale")
			}
			g := vmGeneration(t, m, []unitSnapshot{before})
			defaultCLI, err := filepath.EvalSymlinks("/usr/bin/php")
			if err != nil {
				t.Fatal(err)
			}
			g.Original.DefaultCLI = defaultCLI
			g.Attempt.DefaultCLI = defaultCLI
			if err = saveGeneration(g); err != nil {
				t.Fatal(err)
			}
			var dbpath string
			if err = database.DB.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&dbpath); err != nil {
				t.Fatal(err)
			}
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			var output strings.Builder
			err = syscmd.RunMaintenance(context.Background(), "", &output, []string{"FLUXO_PHP_UPDATE_VM_TEST=1", "FLUXO_FPM_CRASH_CHILD=1", "FLUXO_FPM_CRASH_DB=" + dbpath, "FLUXO_FPM_CRASH_GENERATION=" + g.ID, "FLUXO_FPM_CRASH_BOUNDARY=" + boundary}, exe, "-test.run", "^TestVMFPMDeliveryCrashChild$", "-test.v")
			if err == nil || !strings.Contains(err.Error(), "73") {
				t.Fatalf("fault child did not exit at boundary: %v %s", err, output.String())
			}
			g, err = getGeneration(g.ID)
			if err != nil {
				t.Fatal(err)
			}
			if boundary == "delivered" {
				vmFPMCurrent(t)
			}
			retry, err := m.fpmRetryNeeded(context.Background(), g)
			if err != nil {
				t.Fatal(err)
			}
			if retry != (boundary == "intent") {
				t.Fatalf("unsafe reviewed retry decision: %v", retry)
			}
			vmNativeRepairHead(t, &g)
			if retry {
				g.Attempt.ReloadApprovedHead = g.Head
			}
			if err = saveGeneration(g); err != nil {
				t.Fatal(err)
			}
			alreadyCurrent := unitSnapshot{}
			if boundary == "delivered" {
				alreadyCurrent = vmFPMCurrent(t)
			}
			o, err := GetOperation(g.Head)
			if err != nil {
				t.Fatal(err)
			}
			impact, err := inspectImpact(context.Background(), "8.4")
			if err != nil {
				t.Fatal(err)
			}
			p := Plan{Version: "8.4", Impact: impact}
			if err = m.activate(context.Background(), o, p, g.Attempt, &g); err != nil {
				t.Fatal(err)
			}
			current := vmFPMCurrent(t)
			if !sameOriginalMain(before, current) {
				t.Fatal("FPM PID/start identity was replaced instead of reexecuted")
			}
			if boundary == "delivered" && digest(alreadyCurrent.PIDs) != digest(current.PIDs) {
				t.Fatal("Repair replayed a reload after master had already adopted current code")
			}
			if done, err := GetOperation(o.ID); err != nil || done.Status != "succeeded" {
				t.Fatalf("FPM repair did not finish: %+v %v", done, err)
			}
		})
	}
}
