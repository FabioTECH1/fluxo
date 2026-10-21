package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"fluxo/internal/services/processlog"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

// Only the test executable reads this identity. Production adapters never
// accept fixture units or paths. Cronie itself is the exact packaged binary.
type vmBackendAdapter struct {
	Unit, Group, Binary, Fragment, EnvironmentFile, CommandLine string
	Files                                                       map[string]string
}

func loadVMBackendAdapter() {
	host, _ := os.Hostname()
	if host != "fluxo-php-patch-test" || os.Geteuid() != 0 {
		return
	}
	b, err := os.ReadFile(filepath.Join(stateDir, "backend-test-adapter.json"))
	if err != nil {
		return
	}
	if err = owned(filepath.Join(stateDir, "backend-test-adapter.json"), false); err != nil {
		panic(err)
	}
	var a vmBackendAdapter
	if err = json.Unmarshal(b, &a); err != nil {
		panic(err)
	}
	cronieAdapter.unit = a.Unit
	cronieAdapter.group = a.Group
	cronieAdapter.executable = a.Binary
	cronieAdapter.fragment = a.Fragment
	cronieAdapter.environmentFile = a.EnvironmentFile
	cronieAdapter.commandLine = a.CommandLine
	cronieAdapter.files = a.Files
	backendObserver = func(ctx context.Context) BackendObservation {
		out, err := syscmd.Run(ctx, 5*time.Second, "systemctl", "show", a.Unit, "--property=MainPID,ActiveState")
		if err != nil {
			panic(err)
		}
		p := properties(out)
		pid, _ := strconv.Atoi(p["MainPID"])
		return BackendObservation{Engine: "cronie", State: p["ActiveState"], PID: pid, Version: "1.7.1-1", Architecture: runtime.GOARCH, Unit: a.Unit}
	}
}

func cronieVMFixture(t *testing.T, polling bool) (Entry, string, vmBackendAdapter) {
	t.Helper()
	e, dir := vmFixture(t)
	oldAdapter, oldObserver := cronieAdapter, backendObserver
	t.Cleanup(func() { cronieAdapter = oldAdapter; backendObserver = oldObserver; InvalidateBackendStatus() })
	root := stateDir
	unit := "fluxo-cronie-acceptance-" + uuid.NewString() + ".service"
	fragment := "/etc/systemd/system/" + unit
	a := vmBackendAdapter{Unit: unit, Group: "/system.slice/" + unit, Binary: filepath.Join(root, "crond"), Fragment: fragment, EnvironmentFile: filepath.Join(root, "cronie.default"), Files: map[string]string{}}
	copyFile := func(source, target string, mode os.FileMode) {
		b, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(target, b, mode); err != nil {
			t.Fatal(err)
		}
	}
	copyFile("/root/fluxo-cronie-package/usr/sbin/crond", a.Binary, 0755)
	for _, name := range []string{"pam", "spool"} {
		if err := os.Mkdir(filepath.Join(root, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir("/etc/pam.d")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			copyFile(filepath.Join("/etc/pam.d", entry.Name()), filepath.Join(root, "pam", entry.Name()), 0644)
		}
	}
	copyFile("/root/fluxo-cronie-package/etc/pam.d/cronie", filepath.Join(root, "pam/cronie"), 0644)
	copyFile("/root/fluxo-cronie-package/etc/default/cronie", a.EnvironmentFile, 0644)
	if polling {
		if err = os.WriteFile(a.EnvironmentFile, []byte("DAEMON_ARGS=\"-i\"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"pid", "reboot", "empty-crontab"} {
		if err = os.WriteFile(filepath.Join(root, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	content := fmt.Sprintf(`[Unit]
Description=Isolated packaged Cronie acceptance
[Service]
EnvironmentFile=-%s
ExecStart=%s -n $DAEMON_ARGS
ExecReload=/bin/kill -URG $MAINPID
KillMode=process
Restart=no
BindPaths=%s:/etc/cron.d %s:/etc/crontab %s:/var/spool/cron/crontabs %s:/run/crond.pid %s:/run/cron.reboot %s:/etc/pam.d
`, a.EnvironmentFile, a.Binary, cronDir, filepath.Join(root, "empty-crontab"), filepath.Join(root, "spool"), filepath.Join(root, "pid"), filepath.Join(root, "reboot"), filepath.Join(root, "pam"))
	if _, err = os.Lstat(fragment); !os.IsNotExist(err) {
		t.Fatal("fixture unit already exists", err)
	}
	if err = os.WriteFile(fragment, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = syscmd.Run(context.Background(), 5*time.Second, "systemctl", "stop", unit)
		_ = os.Remove(fragment)
		_, _ = syscmd.Run(context.Background(), 5*time.Second, "systemctl", "daemon-reload")
	})
	for _, p := range []string{fragment, a.EnvironmentFile, filepath.Join(root, "pam/cronie"), filepath.Join(root, "pam/common-auth"), filepath.Join(root, "pam/common-account"), filepath.Join(root, "pam/common-session-noninteractive")} {
		a.Files[p], err = fileDigest(p)
		if err != nil {
			t.Fatal(err)
		}
	}
	a.CommandLine = a.Binary + "\x00-n\x00"
	if polling {
		a.CommandLine += "-i\x00"
	}
	b, _ := json.Marshal(a)
	if err = os.WriteFile(filepath.Join(root, "backend-test-adapter.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	loadVMBackendAdapter()
	for _, args := range [][]string{{"daemon-reload"}, {"start", unit}} {
		if out, err := syscmd.Run(context.Background(), 10*time.Second, "systemctl", args...); err != nil {
			t.Fatal(err, out)
		}
	}
	if _, err = qualifyBackendHost(context.Background()); err != nil {
		t.Fatal("isolated package qualification", err)
	}
	return e, dir, a
}

func TestVMCronieInitialFailureRecoveryAndNaturalDrain(t *testing.T) {
	e, dir, a := cronieVMFixture(t, false)
	ctx := context.Background()
	host, err := qualifyBackendHost(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = Disable(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(CronPath(e.ID))
	e.ID = 1910061
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("INSERT INTO crons VALUES(?,1,?,'* * * * *','fluxo')", e.ID, e.Command); err != nil {
		t.Fatal(err)
	}
	legacy := fmt.Sprintf("# Fluxo Cron ID: %d\n* * * * * fluxo cd %s && %s >> /var/log/fluxo/cron-%d.log 2>&1\n", e.ID, dir, e.Command, e.ID)
	if err = processlog.Prepare(fmt.Sprintf("/var/log/fluxo/cron-%d.log", e.ID), "fluxo"); err != nil {
		t.Fatal(err)
	}
	script := `<?php
file_put_contents(__DIR__.'/legacy-started','yes');
$p=pcntl_fork();if($p===0){posix_setsid();$p=pcntl_fork();if($p>0)exit(0);pcntl_exec('/bin/sh',['-c','sleep 90; echo done > '.escapeshellarg(__DIR__.'/detached-complete')]);exit(1);}
usleep(75000000);file_put_contents(__DIR__.'/parent-complete','yes');
`
	if err = os.WriteFile(filepath.Join(dir, "artisan"), []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	if err = publishCron(CronPath(e.ID), []byte(legacy)); err != nil {
		t.Fatal(err)
	}
	vmWait(t, 80*time.Second, "real Cronie legacy tick", func() bool { _, err := os.Stat(filepath.Join(dir, "legacy-started")); return err == nil })
	if err = os.WriteFile(filepath.Join(dir, "artisan"), []byte("<?php file_put_contents(__DIR__.'/managed-runs','yes\\n',FILE_APPEND); file_put_contents(__DIR__.'/caps',file_get_contents('/proc/self/status'));"), 0644); err != nil {
		t.Fatal(err)
	}
	if err = Materialize(ctx, e, legacy, false); err != nil {
		t.Fatal(err)
	}
	u, err := lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	j, err := read()
	if err != nil {
		t.Fatal(err)
	}
	if j.Migrations[e.Version].Host != nil {
		t.Fatal("Cronie was written as an Ubuntu core host")
	}
	// Same deployed v0.4.36 failure shape, independently qualified in this test.
	m := j.Migrations[e.Version]
	m.Reason = "Fluxo cannot verify earlier Scheduler jobs with this cron configuration; review Scheduler migration before updating PHP"
	j.Migrations[e.Version] = m
	exe, _ := os.Executable()
	hash, _ := fileDigest(exe)
	j.PanelProof = &panelProof{Boot: host.Boot, Binary: hash}
	if err = save(j); err != nil {
		t.Fatal(err)
	}
	identity := j.Entries[e.ID].Identity
	u()
	if err = ReconcileBackendMigrations(ctx); err != nil {
		t.Fatal("initial-failure recovery", err)
	}
	u, _ = lock(ctx)
	j, err = read()
	if err != nil {
		t.Fatal(err)
	}
	if j.Entries[e.ID].Identity != identity {
		t.Fatal("registration UUID changed")
	}
	proof, err := readBackendProofs()
	if err != nil {
		t.Fatal(err)
	}
	nonce := proof.Branches[e.Version].Migration.Replacements[e.ID].Nonce
	u()
	if err = EntryReceipt(ctx, e.ID, nonce); err == nil {
		t.Fatal("manual receipt accepted")
	}
	vmWait(t, 80*time.Second, "real authenticated Cronie receipt", func() bool {
		u, err := lock(ctx)
		if err != nil {
			return false
		}
		defer u()
		p, err := readBackendProofs()
		return err == nil && p.Branches[e.Version].Migration.Replacements[e.ID].Received
	})
	processes, err := schedulerProcesses()
	if err != nil {
		t.Fatal(err)
	}
	group, err := groupPIDs(a.Group)
	if err != nil {
		t.Fatal(err)
	}
	sameDomain := map[int]bool{}
	for pid := range processes {
		if group[pid] {
			sameDomain[pid] = true
		}
	}
	if len(sameDomain) < 2 {
		t.Fatal("fixture missed the still-alive Cronie job supervisor")
	}
	independent, err := independentSchedulerProcesses(host.PID, sameDomain)
	if err != nil || len(independent) != 1 || !independent[host.PID] {
		t.Fatal("job supervisor misclassified", independent, err)
	}
	out, err := syscmd.Run(ctx, time.Second, "systemctl", "show", "cron.service", "--property=MainPID", "--value")
	if err != nil {
		t.Fatal(err)
	}
	other, _ := strconv.Atoi(strings.TrimSpace(out))
	sameDomain[other] = true
	independent, err = independentSchedulerProcesses(host.PID, sameDomain)
	if err != nil || len(independent) != 2 {
		t.Fatal("independent standard cron was hidden", independent, err)
	}
	if err = BranchReady(ctx, e.Version); err == nil {
		t.Fatal("old jobs and detached child were ignored")
	}
	vmWait(t, 55*time.Second, "natural parent and detached child completion", func() bool { return BranchReady(ctx, e.Version) == nil })
	if _, err = os.Stat(filepath.Join(dir, "parent-complete")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, "detached-complete")); err != nil {
		t.Fatal(err)
	}
	if after, err := qualifyBackendHost(ctx); err != nil || after != host {
		t.Fatal("daemon identity or boot changed", after, err)
	}
	e, err = Inspect(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Dispatch(ctx, e.ID); err != nil {
		t.Fatal("Run now", err)
	}
	vmWait(t, 5*time.Second, "unprivileged managed start", func() bool { _, err := os.Stat(filepath.Join(dir, "caps")); return err == nil })
	caps, _ := os.ReadFile(filepath.Join(dir, "caps"))
	if !strings.Contains(string(caps), "CapEff:\t0000000000000000\n") {
		t.Fatal("application retained capabilities")
	}
	generation := uuid.NewString()
	if err = Drain(ctx, e.Version, generation, []Entry{e}); err != nil {
		t.Fatal(err)
	}
	if out, err := Dispatch(ctx, e.ID); err != nil || !strings.Contains(out, "skipped") {
		t.Fatal("held tick admitted", out, err)
	}
	if err = Release(ctx, e.Version, generation, []Entry{e}); err != nil {
		t.Fatal(err)
	}
	if support, err := RequiredSupport(); err != nil || support != 3 {
		t.Fatal("backend proof permitted incompatible rollback", support, err)
	}
	originalDefaults, err := os.ReadFile(a.EnvironmentFile)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(a.EnvironmentFile, append(append([]byte{}, originalDefaults...), []byte("\n# qualification drift fixture\n")...), 0644); err != nil {
		t.Fatal(err)
	}
	_, driftErr := qualifyBackendHost(ctx)
	if err = os.WriteFile(a.EnvironmentFile, originalDefaults, 0644); err != nil {
		t.Fatal(err)
	}
	if driftErr == nil {
		t.Fatal("changed backend configuration reused qualification")
	}
	if restored, err := qualifyBackendHost(ctx); err != nil || restored != host {
		t.Fatal("restored qualification changed daemon identity", restored, err)
	}
	if err = Disable(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(CronPath(e.ID))
	t.Log("Real packaged Cronie same-boot receipts, initial Host:nil recovery, supervisor classification, natural legacy/descendant drain, Run now and pause/resume verified")
}

func TestVMCronieDeletionReceiptsAndBranchIsolation(t *testing.T) {
	for _, polling := range []bool{false, true} {
		t.Run(fmt.Sprintf("polling-%t", polling), func(t *testing.T) {
			e, dir, _ := cronieVMFixture(t, polling)
			ctx := context.Background()
			host, err := qualifyBackendHost(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, "artisan"), []byte("<?php $key=($argv[1]??'')==='custom'?'custom':PHP_MAJOR_VERSION.'.'.PHP_MINOR_VERSION;file_put_contents(__DIR__.'/runs-'.$key,\"run\\n\",FILE_APPEND);"), 0644); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", databasePath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			e3 := e
			e3.ID = 1910063
			e3.Identity = ""
			e3.LegacyBoot = ""
			e3.MigrationNonce = ""
			e3.Version = "8.3"
			e3.Command = "php8.3 " + filepath.Join(dir, "artisan") + " schedule:run"
			if _, err = db.Exec("INSERT INTO crons VALUES(?,1,?,'* * * * *','fluxo')", e3.ID, e3.Command); err != nil {
				t.Fatal(err)
			}
			if err = Materialize(ctx, e3, "", true); err != nil {
				t.Fatal("new 8.3 registration", err)
			}
			customPath := filepath.Join(cronDir, "custom-acceptance")
			custom := []byte("* * * * * fluxo /usr/bin/php8.4 " + filepath.Join(dir, "artisan") + " custom\n")
			if err = publishCron(customPath, custom); err != nil {
				t.Fatal(err)
			}
			deleted := 1910062
			legacy := fmt.Sprintf("# Fluxo Cron ID: %d\n* * * * * fluxo cd %s && %s >> /var/log/fluxo/cron-%d.log 2>&1\n", deleted, dir, e.Command, deleted)
			if err = publishCron(CronPath(deleted), []byte(legacy)); err != nil {
				t.Fatal(err)
			}
			if err = RetireLegacy(ctx, legacy); err != nil {
				t.Fatal(err)
			}
			if err = os.Remove(CronPath(deleted)); err != nil {
				t.Fatal(err)
			}
			u, _ := lock(ctx)
			j, err := read()
			if err != nil {
				t.Fatal(err)
			}
			exe, _ := os.Executable()
			hash, _ := fileDigest(exe)
			j.PanelProof = &panelProof{Boot: host.Boot, Binary: hash}
			if err = save(j); err != nil {
				t.Fatal(err)
			}
			u()
			if err = ReconcileBackendMigrations(ctx); err != nil {
				t.Fatal(err)
			}
			vmWait(t, 80*time.Second, "Cronie deletion A", func() bool {
				u, err := lock(ctx)
				if err != nil {
					return false
				}
				defer u()
				p, err := readBackendProofs()
				return err == nil && p.Branches[e.Version].Migration.MarkerReceived
			})
			if err = BranchReady(ctx, e.Version); err == nil {
				t.Fatal("one deletion receipt certified migration")
			}
			if err = ReconcileBackendMigrations(ctx); err != nil {
				t.Fatal("publish deletion B", err)
			}
			vmWait(t, 80*time.Second, "Cronie deletion B", func() bool { return BranchReady(ctx, e.Version) == nil })
			if _, err = os.Lstat(CronPath(deleted)); !os.IsNotExist(err) {
				t.Fatal("deleted trigger was recreated", err)
			}
			if got, err := os.ReadFile(customPath); err != nil || string(got) != string(custom) {
				t.Fatal("custom trigger changed", err)
			}
			length := func(key string) int { b, _ := os.ReadFile(filepath.Join(dir, "runs-"+key)); return len(b) }
			vmWait(t, 5*time.Second, "new and custom schedules", func() bool { return length("8.4") > 0 && length("8.3") > 0 && length("custom") > 0 })
			e, err = Inspect(ctx, e.ID)
			if err != nil {
				t.Fatal(err)
			}
			generation := uuid.NewString()
			if err = Drain(ctx, e.Version, generation, []Entry{e}); err != nil {
				t.Fatal(err)
			}
			before4, before3, beforeCustom := length("8.4"), length("8.3"), length("custom")
			vmWait(t, 80*time.Second, "other branch continues while 8.4 held", func() bool { return length("8.3") > before3 && length("custom") > beforeCustom })
			if length("8.4") != before4 {
				t.Fatal("held branch still ran")
			}
			if err = Release(ctx, e.Version, generation, []Entry{e}); err != nil {
				t.Fatal(err)
			}
			if _, err = Dispatch(ctx, e.ID); err != nil {
				t.Fatal(err)
			}
			vmWait(t, 5*time.Second, "released branch Run now", func() bool { return length("8.4") > before4 })
			if after, err := qualifyBackendHost(ctx); err != nil || after != host {
				t.Fatal("daemon was restarted", err)
			}
			if err = Disable(ctx, e.ID); err != nil {
				t.Fatal(err)
			}
			_ = os.Remove(CronPath(e.ID))
			if err = Disable(ctx, e3.ID); err != nil {
				t.Fatal(err)
			}
			_ = os.Remove(CronPath(e3.ID))
			_ = os.Remove(customPath)
			t.Log("Real A/B deletion receipts, absent-file preservation, new 8.3/8.4 schedules, unchanged custom schedule, branch isolation and resume verified")
		})
	}
}
