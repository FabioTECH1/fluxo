package phpupdate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
)

// Exercise the PHP caller without taking ownership of unrelated preserved VM
// consumers or running a package/FPM transaction. The production per-row/unit
// inspector still qualifies every fixture in this test's private database.
func TestVMNaturalEvidenceDisabledFiniteAndFailedNoReplay(t *testing.T) {
	requireRecoveryVM(t)
	for _, code := range []int{0, 23} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			m := testStore(t)
			ctx := context.Background()
			dir, err := os.MkdirTemp("/home/fluxo", "php-natural-evidence-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			program := fmt.Sprintf(`<?php
$n=(int)@file_get_contents(__DIR__.'/runs')+1;file_put_contents(__DIR__.'/runs',(string)$n);
pcntl_async_signals(true);pcntl_signal(SIGTERM,function(){file_put_contents(__DIR__.'/signal','unexpected');exit(73);});
if($n===1){while(!file_exists(__DIR__.'/finish'))usleep(50000);exit(0);}exit(%d);
`, code)
			if err = os.WriteFile(filepath.Join(dir, "artisan"), []byte(program), 0644); err != nil {
				t.Fatal(err)
			}
			vmRun(t, "/usr/bin/chown", "-R", "fluxo:fluxo", dir)
			id := 197001 + code
			if _, err = database.DB.Exec("INSERT INTO sites(id,domain,path,app_type,php_version) VALUES(8,'evidence.example.test',?,'laravel','8.4')", dir); err != nil {
				t.Fatal(err)
			}
			if _, err = database.DB.Exec("INSERT INTO daemons(id,site_id,name,command,directory,user,instances) VALUES(?,8,'Evidence test','php8.4 artisan reports:generate',?,'fluxo',2)", id, dir); err != nil {
				t.Fatal(err)
			}
			for _, instance := range []int{1, 2} {
				name := unitName(id, instance)
				if _, e := os.Lstat(filepath.Join("/etc/systemd/system", name+".d")); !os.IsNotExist(e) {
					t.Fatal("preserve existing drop-in", name, e)
				}
				t.Cleanup(func() { _ = os.Remove(filepath.Join("/etc/systemd/system", name+".d")) })
				vmUnit(t, name, "/usr/bin/php8.4 artisan reports:generate", dir)
				path := filepath.Join("/etc/systemd/system", name)
				data, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				if err = os.WriteFile(path, append(data, []byte("\n[Install]\nWantedBy=multi-user.target\n")...), 0644); err != nil {
					t.Fatal(err)
				}
			}
			vmRun(t, "/usr/bin/systemctl", "daemon-reload")
			name := unitName(id, 1)
			vmRun(t, "/usr/bin/systemctl", "start", name)
			wait := func(check func() bool) {
				t.Helper()
				until := time.Now().Add(10 * time.Second)
				for !check() {
					if time.Now().After(until) {
						t.Fatal("fixture did not settle")
					}
					time.Sleep(100 * time.Millisecond)
				}
			}
			wait(func() bool { raw, _ := os.ReadFile(filepath.Join(dir, "runs")); return string(raw) == "1" })
			consumers, reasons, err := InspectMaintenanceConsumers(ctx, nil, MaintenanceInspection{})
			if err != nil || len(reasons) != 0 || len(consumers) != 1 {
				t.Fatal(consumers, reasons, err)
			}
			var units []unitSnapshot
			for _, u := range consumers[0].Units {
				if !u.Runtime.Natural {
					t.Fatal("fixture protocol was not qualified")
				}
				units = append(units, u.Runtime)
			}
			g := vmGeneration(t, m, units)
			for _, u := range units {
				if err = m.prepareBarrier(ctx, g, u); err != nil {
					t.Fatal(err)
				}
			}
			if err = os.WriteFile(filepath.Join(dir, "finish"), []byte("natural exit"), 0644); err != nil {
				t.Fatal(err)
			}
			if err = m.drainConsumers(ctx, &g); err != nil {
				t.Fatal(err)
			}
			if err = m.startConsumers(ctx, &g); err != nil {
				t.Fatal(err)
			}
			wait(func() bool { raw, _ := os.ReadFile(filepath.Join(dir, "runs")); return string(raw) == "2" })
			time.Sleep(3 * time.Second)
			vmRun(t, "/usr/bin/systemctl", "daemon-reload")
			// A new caller loads only durable state. A journal fault must not
			// clear the independent reference or turn missing proof into success.
			g, err = getGeneration(g.ID)
			if err != nil {
				t.Fatal(err)
			}
			evidence := vmRun(t, "/usr/bin/systemctl", "show", name, "--property=ExecMainPID,ExecMainStartTimestampMonotonic,ExecMainCode,ExecMainStatus,UnitFileState")
			if strings.Contains(evidence, "ExecMainPID=0\n") || !strings.Contains(evidence, fmt.Sprintf("ExecMainStatus=%d", code)) || !strings.Contains(evidence, "UnitFileState=disabled") {
				t.Fatal("disabled finite execution evidence vanished", evidence)
			}
			if code == 0 {
				_, _, err = MaintenanceNaturalReady(ctx, g.NaturalEvidence, g.NaturalResumes, units[0], func(map[string]MaintenanceResume) error { return fmt.Errorf("injected completion journal failure") })
				if err == nil {
					t.Fatal("journal failure accepted")
				}
				g, err = getGeneration(g.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			ready, completed, err := naturalActivationReady(ctx, &g, units[0])
			if code == 0 {
				if err != nil || !ready || !completed || !g.NaturalResumes[name].Completed {
					t.Fatal("successful finite completion not retained", ready, completed, err)
				}
				if err = m.releaseBarriers(ctx, g); err != nil {
					t.Fatal(err)
				}
			} else if err == nil || g.NaturalResumes[name].Completed {
				t.Fatal("failed finite command accepted", ready, completed, err)
			}
			g, err = getGeneration(g.ID)
			if err != nil {
				t.Fatal(err)
			}
			err = m.startConsumers(ctx, &g)
			if code == 0 && err != nil || code != 0 && err == nil {
				t.Fatal("recovery changed the retained outcome", err)
			}
			raw, _ := os.ReadFile(filepath.Join(dir, "runs"))
			if string(raw) != "2" {
				t.Fatal("finite work replayed", string(raw))
			}
			if _, err = os.Stat(filepath.Join(dir, "signal")); !os.IsNotExist(err) {
				t.Fatal("natural command was signalled", err)
			}
			if state := vmRun(t, "/usr/bin/systemctl", "show", unitName(id, 2), "--property=ActiveState"); strings.TrimSpace(state) != "ActiveState=inactive" {
				t.Fatal("reference started stopped service", state)
			}
		})
	}
}
