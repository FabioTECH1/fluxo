package phpupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/syscmd"
)

func requireRecoveryVM(t *testing.T) {
	t.Helper()
	hostname, _ := os.Hostname()
	if hostname != "fluxo-php-patch-test" || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("explicit root disposable Ubuntu acceptance VM only")
	}
}
func vmRun(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := syscmd.Run(context.Background(), 30*time.Second, name, args...)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return out
}
func vmUnit(t *testing.T, name, command, directory string) {
	t.Helper()
	path := filepath.Join("/etc/systemd/system", name)
	data := "[Unit]\nDescription=Fluxo isolated recovery acceptance fixture\n[Service]\nType=simple\nUser=fluxo\nWorkingDirectory=" + directory + "\nExecStart=" + command + "\nRestart=always\nRestartSec=1\nTimeoutStopSec=2\nKillSignal=SIGTERM\n"
	if err := writeDurableExclusive(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = syscmd.Run(context.Background(), 20*time.Second, "/usr/bin/systemctl", "stop", name)
		// Remove only the exact files this guarded acceptance test owns.
		if b, err := getBarrier(name); err == nil {
			_ = os.Remove(b.Marker)
			_ = os.Remove(b.Path)
		}
		_ = os.Remove(path)
		_, _ = syscmd.Run(context.Background(), 10*time.Second, "/usr/bin/systemctl", "daemon-reload")
	})
	vmRun(t, "/usr/bin/systemctl", "daemon-reload")
}
func vmGeneration(t *testing.T, m *Manager, units []unitSnapshot) recoveryGeneration {
	t.Helper()
	o, err := m.Enqueue(context.Background(), "check", "", "", "", "acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.DB.Exec("UPDATE php_update_operations SET kind='apply',status='running' WHERE id=?", o.ID); err != nil {
		t.Fatal(err)
	}
	r := runtimeSnapshot{Units: units}
	g := recoveryGeneration{ID: o.ID, Head: o.ID, Version: "8.4", State: "prepared", Pending: true, Goal: "after", Original: r, Attempt: r}
	if err = createGeneration(g); err != nil {
		t.Fatal(err)
	}
	return g
}

func TestVMBarrierWaitsForDescendantWithoutForceKillAndPreservesStoppedUnit(t *testing.T) {
	requireRecoveryVM(t)
	m := testStore(t)
	directory, err := os.MkdirTemp("/home/fluxo", "php-recovery-process-")
	if err != nil {
		t.Fatal(err)
	}
	u, err := user.Lookup("fluxo")
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	if err = os.Chown(directory, uid, gid); err != nil {
		t.Fatal(err)
	}
	program := `import os,signal,time
from pathlib import Path
signal.signal(signal.SIGTERM,lambda *_:os._exit(0))
if os.fork()==0:
 signal.signal(signal.SIGTERM,signal.SIG_IGN)
 Path("started").write_text(str(os.getpid()))
 until=time.monotonic()+9
 while time.monotonic()<until: time.sleep(0.05)
 Path("finished").write_text("finished naturally")
 os._exit(0)
while True: time.sleep(0.05)
`
	path := filepath.Join(directory, "job.py")
	if err = os.WriteFile(path, []byte(program), 0644); err != nil {
		t.Fatal(err)
	}
	first, second := "fluxo-daemon-9100001.service", "fluxo-daemon-9100001-2.service"
	vmUnit(t, first, "/usr/bin/python3 -I "+path, directory)
	vmUnit(t, second, "/usr/bin/python3 -I "+path, directory)
	vmRun(t, "/usr/bin/systemctl", "start", first)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err = os.Stat(filepath.Join(directory, "started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture job did not start")
		}
		time.Sleep(100 * time.Millisecond)
	}
	active, err := unitState(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := unitState(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	g := vmGeneration(t, m, []unitSnapshot{active, stopped})
	for _, u := range g.Attempt.Units {
		if err = m.prepareBarrier(context.Background(), g, u); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	err = m.drainConsumers(ctx, &g)
	cancel()
	if err == nil {
		t.Fatal("drain completed while long job remained")
	}
	current, err := observeUnit(context.Background(), first, active.CGroup)
	if err != nil {
		t.Fatal(err)
	}
	if current.MainPID != 0 || len(current.PIDs) == 0 {
		t.Fatalf("expected exited master and live descendant: %+v", current)
	}
	if _, err = os.Stat(filepath.Join(directory, "finished")); err == nil {
		t.Fatal("fixture finished before interrupted drain was observed")
	}
	if err = finish(g.Head, "interrupted", "fixture interruption during draining"); err != nil {
		t.Fatal(err)
	}
	if busy, err := Applying(); err != nil || !busy {
		t.Fatalf("interruption released persistent maintenance gate: %v %v", busy, err)
	}
	if err = m.drainConsumers(context.Background(), &g); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(directory, "finished")); err != nil {
		t.Fatal("job did not finish naturally")
	}
	vmRun(t, "/usr/bin/systemctl", "start", "--no-block", first)
	time.Sleep(200 * time.Millisecond)
	if state, err := unitState(context.Background(), first); err != nil || state.Active || len(state.PIDs) > 0 {
		t.Fatalf("persistent barrier allowed start: %+v %v", state, err)
	}
	if err = m.startConsumers(context.Background(), &g); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		state, e := unitState(context.Background(), first)
		if e == nil && state.Active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("active consumer did not restart: %v", e)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if state, err := unitState(context.Background(), second); err != nil || state.Active {
		t.Fatalf("original stopped instance was started: %+v %v", state, err)
	}
	if err = m.releaseBarriers(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	// Let the new synthetic job finish before cleaning its test unit.
	time.Sleep(10 * time.Second)
}

func TestVMJournaledBarrierCrashBoundariesResume(t *testing.T) {
	requireRecoveryVM(t)
	m := testStore(t)
	for boundary := 0; boundary < 7; boundary++ {
		t.Run(fmt.Sprint(boundary), func(t *testing.T) {
			name := fmt.Sprintf("fluxo-daemon-%d.service", 9100100+boundary)
			vmUnit(t, name, "/usr/bin/sleep infinity", "/")
			u, err := unitState(context.Background(), name)
			if err != nil {
				t.Fatal(err)
			}
			g := vmGeneration(t, m, []unitSnapshot{u})
			original := unitProperties(vmRun(t, "/usr/bin/systemctl", "show", name, "--property="+inspectedUnitProperties))
			path := filepath.Join("/etc/systemd/system", name+".d", "90-fluxo-php-maintenance.conf")
			marker := filepath.Join(filepath.Dir(path), "90-fluxo-php-"+g.ID+".hold")
			content, err := barrierContent(marker)
			if err != nil {
				t.Fatal(err)
			}
			b := maintenanceBarrier{Unit: name, GenerationID: g.ID, Path: path, Marker: marker, Content: content, Original: original, State: "preparing"}
			if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(b)
			if _, err = database.DB.Exec("INSERT INTO php_update_barriers(unit_name,generation_id,payload) VALUES (?,?,?)", name, g.ID, string(raw)); err != nil {
				t.Fatal(err)
			}
			if boundary >= 1 {
				if err = writeDurableExclusive(marker, []byte(g.ID+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if boundary >= 2 {
				if err = writeDurableExclusive(path, []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if boundary >= 3 {
				vmRun(t, "/usr/bin/systemctl", "daemon-reload")
			}
			if boundary >= 4 {
				b.State = "releasing"
				if err = saveBarrier(b); err != nil {
					t.Fatal(err)
				}
				if err = os.Remove(marker); err != nil {
					t.Fatal(err)
				}
				if boundary >= 5 {
					if err = os.Remove(path); err != nil {
						t.Fatal(err)
					}
				}
				if boundary >= 6 {
					vmRun(t, "/usr/bin/systemctl", "daemon-reload")
				}
			}
			actual := unitProperties(vmRun(t, "/usr/bin/systemctl", "show", name, "--property="+inspectedUnitProperties))
			normalized, err := normalizeBarrierDefinition(name, actual)
			if err != nil {
				t.Fatalf("review cannot recognize journaled boundary %d: %v", boundary, err)
			}
			if canonicalDefinition(normalized) != canonicalDefinition(original) {
				t.Fatal("recovery accepted a changed service definition")
			}
			if boundary < 4 {
				if err = m.prepareBarrier(context.Background(), g, u); err != nil {
					t.Fatalf("prepare did not resume: %v", err)
				}
			}
			if err = m.releaseBarriers(context.Background(), g); err != nil {
				t.Fatalf("release did not resume: %v", err)
			}
			if err = finish(g.Head, "succeeded", "fixture complete"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
