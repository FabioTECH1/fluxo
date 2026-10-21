package phpupdate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/services/daemon"
	"fluxo/internal/services/octaneruntime"
	"github.com/google/uuid"
)

const octaneCleanupRebootSpecPath = "/var/lib/fluxo-octane-cleanup-reboot.json"
const octaneCleanupRebootID = 9107000
const octaneCleanupRebootPort = 18892

type octaneCleanupRebootSpec struct {
	Boot, UnitContent, CleanupID string
}

func readCleanupAcceptanceJournal(t *testing.T, name string) octaneCleanup {
	t.Helper()
	path, _, _ := cleanupPaths(name)
	if err := rootOwned(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var j octaneCleanup
	if err = json.Unmarshal(raw, &j); err != nil {
		t.Fatal(err)
	}
	return j
}

func assertCleanupAcceptanceRemoved(t *testing.T, id int) {
	t.Helper()
	name := unitName(id, 1)
	journal, override, marker := cleanupPaths(name)
	for _, path := range []string{filepath.Join("/etc/systemd/system", name), filepath.Join("/etc/systemd/system/multi-user.target.wants", name), journal, override, marker} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("cleanup retained fixture path", path, err)
		}
	}
	u, err := observeUnit(context.Background(), name, "")
	if err != nil || u.Active || u.MainPID != 0 || len(u.PIDs) != 0 {
		t.Fatal("cleanup retained fixture processes", name, err)
	}
	// Only remove an empty, exact fixture drop-in directory.
	if err = os.Remove(filepath.Join("/etc/systemd/system", name+".d")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// The unit's provider failure produces a real auto-restart transition. Cleanup
// must durably hold that transition before it tries to prove native processes.
// Stage deliberately preserves only its held unit and spec across a real reboot.
func TestVMStageRebootDuringOctaneCleanupAutoRestart(t *testing.T) {
	requireRebootVM(t)
	for _, path := range []string{octaneCleanupRebootSpecPath, rebootSpecPath, activationRebootSpecPath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("finish existing reboot acceptance first", path)
		}
	}
	name := unitName(octaneCleanupRebootID, 1)
	unitPath := filepath.Join("/etc/systemd/system", name)
	for _, path := range []string{unitPath, unitPath + ".d", filepath.Join("/etc/systemd/system/multi-user.target.wants", name), "/var/log/fluxo/fluxo-daemon-9107000.log"} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("refusing pre-existing acceptance fixture", path)
		}
	}
	existing := unitProperties(vmRun(t, "/usr/bin/systemctl", "show", name, "--property=LoadState,ActiveState"))
	if existing["LoadState"] != "not-found" || existing["ActiveState"] != "inactive" {
		t.Fatal("fixture name already belongs to a service", existing)
	}
	if err := octaneruntime.Install(); err != nil {
		t.Fatal(err)
	}
	// Restore application files before requesting reboot, in addition to the
	// ordinary helper cleanup. No changed application fixture crosses reboot.
	type savedFile struct {
		path   string
		data   []byte
		mode   os.FileMode
		exists bool
	}
	var saved []savedFile
	write := func(path, data string) {
		t.Helper()
		old, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		entry := savedFile{path: path, data: old, exists: err == nil}
		if entry.exists {
			info, e := os.Stat(path)
			if e != nil {
				t.Fatal(e)
			}
			entry.mode = info.Mode().Perm()
		}
		saved = append(saved, entry)
		vmApplicationFile(t, path, data)
	}
	write(filepath.Join(recoveryVMApp, ".rr.yaml"), "")
	write(filepath.Join(recoveryVMApp, "config/octane.php"), "<?php return ['server'=>'roadrunner'];\n")
	write(filepath.Join(recoveryVMApp, "app/Providers/RecoveryCleanupProvider.php"), `<?php
namespace App\Providers;
class RecoveryCleanupProvider extends \Illuminate\Support\ServiceProvider {
 public function register(): void { throw new \RuntimeException('Fluxo isolated cleanup auto-restart acceptance'); }
}
`)
	write(filepath.Join(recoveryVMApp, "bootstrap/providers.php"), "<?php return [App\\Providers\\AppServiceProvider::class, App\\Providers\\RecoveryCleanupProvider::class];\n")
	cache := filepath.Join(recoveryVMApp, "bootstrap/cache/config.php")
	write(cache, "<?php return [];\n")
	if err := os.Remove(cache); err != nil {
		t.Fatal(err)
	}
	if err := daemon.GenerateServiceFile(octaneCleanupRebootID, octaneruntime.Command("8.4", octaneCleanupRebootPort), recoveryVMApp, "fluxo", 60, 15, "SIGTERM"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "ExecStart=/usr/bin/"+octaneruntime.Command("8.4", octaneCleanupRebootPort)+"\n") || !strings.Contains(string(content), "RestartSec=60\n") {
		t.Fatal("fixture is not the generated qualified managed wrapper")
	}
	vmRun(t, "/usr/bin/systemctl", "daemon-reload")
	vmRun(t, "/usr/bin/systemctl", "enable", name)
	vmRun(t, "/usr/bin/systemctl", "start", name)
	deadline := time.Now().Add(20 * time.Second)
	for {
		p := unitProperties(vmRun(t, "/usr/bin/systemctl", "show", name, "--property=ActiveState,SubState,MainPID"))
		if p["ActiveState"] == "activating" && p["SubState"] == "auto-restart" && p["MainPID"] == "0" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("provider failure did not produce auto-restart", p)
		}
		time.Sleep(50 * time.Millisecond)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- CleanupManagedOctane(ctx, "8.4", octaneCleanupRebootID, recoveryVMApp, octaneCleanupRebootPort)
	}()
	journalPath, _, _ := cleanupPaths(name)
	var held octaneCleanup
	deadline = time.Now().Add(20 * time.Second)
	for {
		select {
		case e := <-done:
			t.Fatalf("cleanup ended before an observable held transition: %v", e)
		default:
		}
		if raw, e := os.ReadFile(journalPath); e == nil && json.Unmarshal(raw, &held) == nil && verifyOctaneCleanupHold(context.Background(), held) == nil {
			// Cancellation is deliberately after real durable/effective protection.
			cancel()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cleanup did not protect auto-restart before runtime proof")
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cleanup completed instead of retaining interrupted hold")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("cleanup ignored cancellation after establishing hold")
	}
	held = readCleanupAcceptanceJournal(t, name)
	if held.Delivery != "" || verifyOctaneCleanupHold(context.Background(), held) != nil {
		t.Fatal("transition cleanup signalled unproved runtime or lost protection")
	}
	for _, old := range saved {
		if old.exists {
			err = os.WriteFile(old.path, old.data, old.mode)
		} else {
			err = os.Remove(old.path)
			if os.IsNotExist(err) {
				err = nil
			}
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		t.Fatal(err)
	}
	spec := octaneCleanupRebootSpec{Boot: strings.TrimSpace(string(boot)), UnitContent: string(content), CleanupID: held.ID}
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err = writeDurableExclusive(octaneCleanupRebootSpecPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = verifyOctaneCleanupHold(context.Background(), held); err != nil {
		t.Fatal(err)
	}
	t.Log("REBOOT_REQUESTED with real auto-restart cleanup hold; preserved", octaneCleanupRebootSpecPath)
	vmRun(t, "/usr/bin/systemctl", "reboot")
}

func TestVMResumeAfterRealOctaneCleanupReboot(t *testing.T) {
	requireRebootVM(t)
	if err := rootOwned(octaneCleanupRebootSpecPath); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(octaneCleanupRebootSpecPath)
	if err != nil {
		t.Fatal(err)
	}
	var spec octaneCleanupRebootSpec
	if err = json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	if _, err = uuid.Parse(spec.CleanupID); err != nil {
		t.Fatal(err)
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil || spec.Boot == "" || strings.TrimSpace(string(boot)) == spec.Boot {
		t.Fatal("VM did not actually reboot")
	}
	name := unitName(octaneCleanupRebootID, 1)
	if err = verifyBarrierFile(filepath.Join("/etc/systemd/system", name), spec.UnitContent); err != nil {
		t.Fatal(err)
	}
	j := readCleanupAcceptanceJournal(t, name)
	if j.ID != spec.CleanupID || j.DaemonID != octaneCleanupRebootID || j.Directory != recoveryVMApp || j.Branch != "8.4" || j.Port != octaneCleanupRebootPort {
		t.Fatal("cleanup fixture identity changed")
	}
	if err = verifyOctaneCleanupHold(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	vmRun(t, "/usr/bin/systemctl", "start", name)
	u, err := observeUnit(context.Background(), name, j.Snapshot.CGroup)
	if err != nil || u.Active || len(u.PIDs) != 0 || u.MainPID != 0 {
		t.Fatal("boot/manual start bypassed cleanup hold", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = CleanupManagedOctane(ctx, "8.4", octaneCleanupRebootID, recoveryVMApp, octaneCleanupRebootPort); err != nil {
		t.Fatal(err)
	}
	assertCleanupAcceptanceRemoved(t, octaneCleanupRebootID)
	if err = os.Rename(octaneCleanupRebootSpecPath, octaneCleanupRebootSpecPath+".completed-"+uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	t.Log("Auto-restart cleanup persisted across real reboot and exact resumed removal verified; fixture log retained")
}

// Explicit operator utility for the one historical failed-enable acceptance
// fixture. Refuse any other unit, app directory, command or listener identity.
func TestVMResumeFailedOctaneEnablementAcceptance(t *testing.T) {
	requireRecoveryVM(t)
	if os.Getenv("FLUXO_PHP_UPDATE_CLEANUP_VM_TEST") != "1" {
		t.Skip("explicit orphan acceptance cleanup opt-in required")
	}
	const id, port = 9108001, 18890
	name := unitName(id, 1)
	path := filepath.Join("/etc/systemd/system", name)
	if err := rootOwned(path); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	command := octaneruntime.Command("8.4", port)
	if !strings.Contains(string(content), "Description=Fluxo Daemon 9108001 (1/1)\n") || !strings.Contains(string(content), "WorkingDirectory="+recoveryVMApp+"\n") || (!strings.Contains(string(content), "ExecStart="+command+"\n") && !strings.Contains(string(content), "ExecStart=/usr/bin/"+command+"\n")) {
		t.Fatal("unit is not the exact historical failed-enable acceptance fixture")
	}
	p := unitProperties(vmRun(t, "/usr/bin/systemctl", "show", name, "--property="+inspectedUnitProperties))
	c := Consumer{ID: id, Kind: "laravel_octane", Name: "failed acceptance fixture", Command: command, Directory: recoveryVMApp, Port: port, Instances: 1}
	journal, _, _ := cleanupPaths(name)
	if _, err = os.Lstat(journal); os.IsNotExist(err) {
		if err = validateUnitDefinition("8.4", &c, p); err != nil {
			t.Fatal(err)
		}
	} else if err != nil {
		t.Fatal(err)
	} else {
		j := readCleanupAcceptanceJournal(t, name)
		if j.DaemonID != id || j.Directory != recoveryVMApp || j.Branch != "8.4" || j.Port != port || j.Content != string(content) {
			t.Fatal("orphan cleanup journal identity differs")
		}
	}
	vmApplicationFile(t, filepath.Join(recoveryVMApp, ".rr.yaml"), "")
	vmApplicationFile(t, filepath.Join(recoveryVMApp, "config/octane.php"), "<?php return ['server'=>'roadrunner'];\n")
	cache := filepath.Join(recoveryVMApp, "bootstrap/cache/config.php")
	vmApplicationFile(t, cache, "<?php return [];\n")
	if err = os.Remove(cache); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err = CleanupManagedOctane(ctx, "8.4", id, recoveryVMApp, port); err != nil {
		t.Fatal(err)
	}
	assertCleanupAcceptanceRemoved(t, id)
	t.Log("Removed only historical acceptance unit 9108001 with verified native cleanup; application fixtures restored")
}
