package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"fluxo/internal/services/processlog"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

func init() {
	if len(os.Args) >= 3 && strings.HasPrefix(os.Args[1], "scheduler-") {
		exe, _ := os.Executable()
		stateDir = filepath.Dir(exe)
		cronDir = filepath.Join(stateDir, "cron")
		if _, err := os.Stat(filepath.Join(stateDir, "real-cron")); err == nil {
			cronDir = "/etc/cron.d"
		}
		loadVMBackendAdapter()
		var err error
		switch os.Args[1] {
		case "scheduler-execute":
			err = Execute(context.Background(), os.Args[2])
		case "scheduler-dispatch":
			id, _ := strconv.Atoi(os.Args[2])
			if len(os.Args) == 4 {
				_ = EntryReceipt(context.Background(), id, os.Args[3])
			}
			_, err = Dispatch(context.Background(), id)
		case "scheduler-migration":
			if len(os.Args) == 4 {
				err = Receipt(context.Background(), os.Args[2], os.Args[3], 0)
			}
		default:
			os.Exit(2)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
}

func vmFixture(t *testing.T) (Entry, string) {
	t.Helper()
	hostname, _ := os.Hostname()
	if os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" || os.Geteuid() != 0 || hostname != "fluxo-php-patch-test" {
		t.Skip("guarded reusable VM only")
	}
	oldState, oldCron, oldDB := stateDir, cronDir, databasePath
	stateDir = "/var/lib/fluxo-scheduler-acceptance-" + uuid.NewString()
	cronDir = filepath.Join(stateDir, "cron")
	if err := os.MkdirAll(cronDir, 0700); err != nil {
		t.Fatal(err)
	}
	dir := "/home/fluxo/scheduler-acceptance-" + uuid.NewString()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// Parent exits after a double fork. A session-detached, non-PHP grandchild
	// remains in the invocation cgroup; no inherited flock FD is involved.
	script := `<?php
file_put_contents(__DIR__.'/started',posix_geteuid()."\n", FILE_APPEND);
$p=pcntl_fork(); if($p>0)exit(0); posix_setsid();
$p=pcntl_fork(); if($p>0)exit(0);
pcntl_exec('/bin/sh',['-c','sleep 3; echo complete >> '.escapeshellarg(__DIR__.'/completed')]);
`
	if err := os.WriteFile(filepath.Join(dir, "artisan"), []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := syscmd.Run(context.Background(), 5*time.Second, "/usr/bin/chown", "-R", "fluxo:fluxo", dir); err != nil {
		t.Fatal(err)
	}
	databasePath = filepath.Join(stateDir, "registration.db")
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE sites(id INTEGER PRIMARY KEY,app_type TEXT);CREATE TABLE crons(id INTEGER PRIMARY KEY,site_id INTEGER,command TEXT,expression TEXT,user TEXT);INSERT INTO sites VALUES(1,'laravel')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO crons VALUES(910001,1,?,'* * * * *','fluxo')", "php8.4 "+dir+"/artisan schedule:run"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	e, ok := Qualify(910001, 1, dir, "* * * * *", "php8.4 "+dir+"/artisan schedule:run", "fluxo")
	if !ok {
		t.Fatal("fixture qualification")
	}
	if err := Materialize(context.Background(), e, "", true); err != nil {
		t.Fatal(err)
	}
	e, err = Inspect(context.Background(), e.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Preserved Scheduler evidence: %s, app %s", stateDir, dir)
	t.Cleanup(func() { stateDir, cronDir, databasePath = oldState, oldCron, oldDB })
	return e, dir
}

func TestVMSchedulerNaturalDescendantDrainAndAdmission(t *testing.T) {
	e, dir := vmFixture(t)
	ctx := context.Background()
	if _, err := Dispatch(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("guarded application did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}
	generation := uuid.NewString()
	if err := Hold(ctx, e.Version, generation, []Entry{e}); err != nil {
		t.Fatal(err)
	}
	if err := AssertDrained(ctx, e.Version, generation, []Entry{e}); err == nil {
		t.Fatal("detached background child was ignored")
	}
	if out, err := Dispatch(ctx, e.ID); err != nil || !strings.Contains(out, "skipped") {
		t.Fatalf("held tick: %s %v", out, err)
	}
	if pending, err := Pending(); err != nil || !pending {
		t.Fatalf("hold did not survive journal reopen: %v %v", pending, err)
	}
	if err := Disable(ctx, e.ID); err == nil {
		t.Fatal("disable bypassed maintenance hold")
	}
	if err := Release(ctx, e.Version, uuid.NewString(), []Entry{e}); err == nil {
		t.Fatal("foreign generation released hold")
	}
	if err := Drain(ctx, e.Version, generation, []Entry{e}); err != nil {
		t.Fatal(err)
	}
	started, err := os.ReadFile(filepath.Join(dir, "started"))
	if err != nil {
		t.Fatal(err)
	}
	cred, err := syscmd.ResolveCredential("fluxo")
	if err != nil {
		t.Fatal(err)
	}
	if string(started) != fmt.Sprintln(cred.Uid) {
		t.Fatalf("wrong user or held tick executed: %q", started)
	}
	if _, err = os.Stat(filepath.Join(dir, "completed")); err != nil {
		t.Fatal("descendant did not finish naturally", err)
	}
	if err = Release(ctx, e.Version, generation, []Entry{e}); err != nil {
		t.Fatal(err)
	}
	if _, err = Dispatch(ctx, e.ID); err != nil {
		t.Fatal("Scheduler was not restored", err)
	}
	if err = Drain(ctx, e.Version, generation, []Entry{e}); err != nil {
		t.Fatal(err)
	}
	if err = Release(ctx, e.Version, generation, []Entry{e}); err != nil {
		t.Fatal(err)
	}
	if err = Disable(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = Dispatch(ctx, e.ID); err == nil {
		t.Fatal("disabled Scheduler launched")
	}
}

func TestVMSchedulerUncertainGuardedSubmissionRetiresWithoutReboot(t *testing.T) {
	e, dir := vmFixture(t)
	ctx := context.Background()
	unlock, err := lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	j, err := read()
	if err != nil {
		t.Fatal(err)
	}
	boot, _ := bootID()
	name := "fluxo-scheduler-" + uuid.NewString() + ".service"
	j.Invocations[name] = invocation{Unit: name, Entry: e, Boot: boot, Guarded: true}
	if err = save(j); err != nil {
		t.Fatal(err)
	}
	unlock()
	if _, err = KnownPIDs(ctx, e.Version); err != nil {
		t.Fatal("guarded missing submission could not be retired", err)
	}
	// Reproduce the originally uncertain request arriving after reconciliation.
	// No entry remains to authorize its application, even outside maintenance.
	_, _ = syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemd-run", "--quiet", "--wait", "--collect", "--unit="+name,
		"--", filepath.Join(stateDir, "dispatcher"), "scheduler-execute", name)
	if _, err = os.Stat(filepath.Join(dir, "started")); !os.IsNotExist(err) {
		t.Fatal("retired late submission ran application code", err)
	}
	if err = Hold(ctx, e.Version, uuid.NewString(), []Entry{e}); err != nil {
		t.Fatal("retired uncertainty still blocked maintenance", err)
	}
}

func TestVMSchedulerDelayedAdmissionRejectsDisabledAndRecreatedIdentities(t *testing.T) {
	for _, mode := range []string{"disabled", "recreated"} {
		t.Run(mode, func(t *testing.T) {
			e, dir := vmFixture(t)
			ctx := context.Background()
			unlock, err := lock(ctx)
			if err != nil {
				t.Fatal(err)
			}
			j, err := read()
			if err != nil {
				t.Fatal(err)
			}
			boot, _ := bootID()
			name := "fluxo-scheduler-" + uuid.NewString() + ".service"
			j.Invocations[name] = invocation{Unit: name, Entry: e, Boot: boot, Guarded: true}
			if err = save(j); err != nil {
				t.Fatal(err)
			}
			logPath := fmt.Sprintf("/var/log/fluxo/cron-%d.log", e.ID)
			if err = processlog.Prepare(logPath, "fluxo"); err != nil {
				t.Fatal(err)
			}
			if _, err = submitInvocation(ctx, name, e, logPath); err != nil {
				t.Fatal(err)
			}
			current := e
			if mode == "disabled" {
				current.Enabled = false
			} else {
				current.Identity = uuid.NewString()
			}
			j.Entries[e.ID] = current
			if err = save(j); err != nil {
				t.Fatal(err)
			}
			unlock()
			vmWait(t, 10*time.Second, "revoked helper exit", func() bool {
				_, err := KnownPIDs(ctx, e.Version)
				if err != nil {
					return false
				}
				u, err := lock(ctx)
				if err != nil {
					return false
				}
				defer u()
				j, err := read()
				return err == nil && len(j.Invocations) == 0
			})
			if _, err = os.Stat(filepath.Join(dir, "started")); !os.IsNotExist(err) {
				t.Fatal("revoked invocation ran", err)
			}
			if mode == "disabled" {
				if err = Materialize(ctx, e, "", true); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = Dispatch(ctx, e.ID); err != nil {
				t.Fatal("fresh invocation failed", err)
			}
			vmWait(t, 10*time.Second, "fresh invocation", func() bool { _, err := os.Stat(filepath.Join(dir, "started")); return err == nil })
			current, err = Inspect(ctx, e.ID)
			if err != nil {
				t.Fatal(err)
			}
			gen := uuid.NewString()
			if err = Drain(ctx, e.Version, gen, []Entry{current}); err != nil {
				t.Fatal(err)
			}
			if err = Release(ctx, e.Version, gen, []Entry{current}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestVMSchedulerLegacyManualInvalidatesEarlierPanelProof(t *testing.T) {
	e, _ := vmFixture(t)
	ctx := context.Background()
	u, err := lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	j, err := read()
	if err != nil {
		t.Fatal(err)
	}
	boot, _ := bootID()
	j.PanelProof = &panelProof{Boot: boot, Binary: strings.Repeat("a", 64)}
	if err = save(j); err != nil {
		t.Fatal(err)
	}
	u()
	if err = InvalidateLegacyManual(ctx, e.Version); err != nil {
		t.Fatal(err)
	}
	u, err = lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	j, err = read()
	u()
	if err != nil || j.PanelProof != nil || j.Legacy[e.Version] != boot {
		t.Fatal("untracked manual execution retained its earlier proof", err)
	}
	if err = BranchReady(ctx, e.Version); err == nil {
		t.Fatal("untracked manual execution did not invalidate migration")
	}
}

func TestVMSchedulerMigrationAndFailClosedJournal(t *testing.T) {
	e, _ := vmFixture(t)
	ctx := context.Background()
	legacy := "legacy generated bytes\n"
	if err := os.WriteFile(CronPath(e.ID), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Materialize(ctx, e, legacy, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(ctx, e.ID); err == nil {
		t.Fatal("same-boot migration authorized")
	}
	if err := Hold(ctx, e.Version, uuid.NewString(), nil); err == nil {
		t.Fatal("empty consumer list bypassed legacy branch gate")
	}
	if err := Disable(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(CronPath(e.ID)); err != nil {
		t.Fatal(err)
	}
	if err := Materialize(ctx, e, legacy, true); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(ctx, e.ID); err == nil {
		t.Fatal("re-enable erased legacy history")
	}
	// A different, recorded boot permits readiness only after reconciliation.
	unlock, err := lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	j, err := read()
	if err != nil {
		t.Fatal(err)
	}
	old := j.Entries[e.ID]
	old.LegacyBoot = uuid.NewString()
	old.Ready = false
	j.Entries[e.ID] = old
	j.Legacy[e.Version] = old.LegacyBoot
	if err = save(j); err != nil {
		t.Fatal(err)
	}
	unlock()
	if err = Materialize(ctx, old, legacy, false); err != nil {
		t.Fatal(err)
	}
	e, err = Inspect(ctx, e.ID)
	if err != nil {
		t.Fatal("boot-boundary reconciliation", err)
	}
	// Journal a submission with no confirmed response. An absent unit does not
	// prove a delayed D-Bus start cannot arrive later on the current boot.
	unlock, err = lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	j, err = read()
	if err != nil {
		t.Fatal(err)
	}
	boot, _ := bootID()
	name := "fluxo-scheduler-" + uuid.NewString() + ".service"
	j.Invocations[name] = invocation{Unit: name, Entry: e, Boot: boot, Submitted: false}
	if err = save(j); err != nil {
		t.Fatal(err)
	}
	unlock()
	if _, err = KnownPIDs(ctx, e.Version); err == nil {
		t.Fatal("uncertain submission was discarded")
	}
	if err = os.WriteFile(filepath.Join(stateDir, "state.json"), []byte(`{"schema":9000}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Pending(); err == nil {
		t.Fatal("unsupported journal admitted maintenance")
	}
}

func TestVMSchedulerMissingStateRegistrationAndRecreation(t *testing.T) {
	e, dir := vmFixture(t)
	ctx := context.Background()
	if err := Disable(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(CronPath(e.ID)); err != nil {
		t.Fatal(err)
	}
	if err := Materialize(ctx, e, "", true); err != nil {
		t.Fatal(err)
	}
	var err error
	e, err = Inspect(ctx, e.ID)
	if err != nil {
		t.Fatal("guarded identity recreation lost readiness", err)
	}
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("DELETE FROM crons WHERE id=?", e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = Dispatch(ctx, e.ID); err == nil {
		t.Fatal("orphaned/uncommitted registration executed")
	}
	if err = ReconcileRegistrations(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = Inspect(ctx, e.ID); err == nil {
		t.Fatal("orphan retained admission")
	}
	if _, err = db.Exec("INSERT INTO crons VALUES(910001,1,?,'* * * * *','fluxo')", e.Command); err != nil {
		t.Fatal(err)
	}
	if err = Materialize(ctx, e, "", true); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(databasePath, databasePath+".missing"); err != nil {
		t.Fatal(err)
	}
	if _, err = Dispatch(ctx, e.ID); err == nil {
		t.Fatal("missing database admitted work")
	}
	if err = os.Rename(databasePath+".missing", databasePath); err != nil {
		t.Fatal(err)
	}
	// A live detached non-PHP child must not disappear when the row and journal
	// are lost. The independent initialization anchor makes loss fail closed.
	if _, err = Dispatch(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	vmWait(t, 10*time.Second, "application admitted before registration loss", func() bool { _, err := os.Stat(filepath.Join(dir, "started")); return err == nil })
	if _, err = db.Exec("DELETE FROM crons WHERE id=?", e.ID); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(stateDir, "state.json")); err != nil {
		t.Fatal(err)
	}
	if err = Hold(ctx, e.Version, uuid.NewString(), nil); err == nil {
		t.Fatal("missing established journal was initialized empty")
	}
	if err = Release(ctx, e.Version, uuid.NewString(), nil); err == nil {
		t.Fatal("missing journal counted as released")
	}
	if _, err = Pending(); err == nil {
		t.Fatal("missing journal admitted mutations")
	}
	for _, invalid := range []string{`{}`, `{"schema":1}`, `{"schema":1,"entries":{},"invocations":{},"holds":{},"legacy":{},"released":{}}`} {
		if err = os.WriteFile(filepath.Join(stateDir, "state.json"), []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = Pending(); err == nil {
			t.Fatalf("incomplete journal accepted: %s", invalid)
		}
	}
	if err = os.WriteFile(filepath.Join(stateDir, "state.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = ReconcileRegistrations(ctx); err != nil {
		t.Fatal(err)
	}
	gen := uuid.NewString()
	if err = Drain(ctx, e.Version, gen, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, "completed")); err != nil {
		t.Fatal("background descendant was not allowed to finish")
	}
	if err = Release(ctx, e.Version, gen, nil); err != nil {
		t.Fatal(err)
	}
	if err = Release(ctx, e.Version, gen, nil); err != nil {
		t.Fatal("journaled idempotent release failed", err)
	}
	if err = Release(ctx, e.Version, uuid.NewString(), nil); err == nil {
		t.Fatal("absent hold accepted without release evidence")
	}
	renamed := stateDir + ".moved"
	if err = os.Rename(stateDir, renamed); err != nil {
		t.Fatal(err)
	}
	if _, err = Pending(); err == nil {
		t.Fatal("lost directory bypassed independent anchor")
	}
	if err = os.Rename(renamed, stateDir); err != nil {
		t.Fatal(err)
	}
}

func TestVMSchedulerBranchIsolationAndAdmissionLease(t *testing.T) {
	e, dir := vmFixture(t)
	ctx := context.Background()
	if _, err := os.Stat("/usr/bin/php8.3"); err != nil {
		t.Fatal("acceptance needs the preserved Ubuntu PHP 8.3 fixture")
	}
	other, ok := Qualify(e.ID+1, e.SiteID, dir, "* * * * *", "php8.3 "+dir+"/artisan schedule:run", "fluxo")
	if !ok {
		t.Fatal("other branch qualification")
	}
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO crons VALUES(?,1,?,'* * * * *','fluxo')", other.ID, other.Command); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err = Materialize(ctx, other, "", true); err != nil {
		t.Fatal(err)
	}
	other, err = Inspect(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	gen := uuid.NewString()
	if err = Hold(ctx, e.Version, gen, []Entry{e}); err != nil {
		t.Fatal(err)
	}
	if _, err = Dispatch(ctx, other.ID); err != nil {
		t.Fatal("unaffected branch was blocked", err)
	}
	if err = AssertDrained(ctx, e.Version, gen, []Entry{e}); err != nil {
		t.Fatal("other branch prevented package readiness", err)
	}
	if err = Release(ctx, e.Version, gen, []Entry{e}); err != nil {
		t.Fatal(err)
	}
	_, unlock, err := RuntimeLease(ctx, e.Version)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan error, 1)
	go func() { _, err := Dispatch(ctx, e.ID); started <- err }()
	select {
	case err := <-started:
		unlock()
		t.Fatal("dispatch overtook snapshot admission gate", err)
	case <-time.After(100 * time.Millisecond):
	}
	unlock()
	if err = <-started; err != nil {
		t.Fatal(err)
	}
	if err = Drain(ctx, e.Version, gen, []Entry{e}); err != nil {
		t.Fatal(err)
	}
	if err = Release(ctx, e.Version, gen, []Entry{e}); err != nil {
		t.Fatal(err)
	}
	if err = Drain(ctx, other.Version, gen, []Entry{other}); err != nil {
		t.Fatal(err)
	}
	if err = Release(ctx, other.Version, gen, []Entry{other}); err != nil {
		t.Fatal(err)
	}
}

type rebootFixture struct {
	Entry      Entry  `json:"entry"`
	Legacy     Entry  `json:"legacy"`
	Generation string `json:"generation"`
}

func TestVMSchedulerRebootPrepare(t *testing.T) {
	if os.Getenv("FLUXO_SCHEDULER_REBOOT_PREPARE") != "1" {
		t.Skip("explicit reboot preparation only")
	}
	e, _ := vmFixture(t)
	ctx := context.Background()
	legacy, ok := Qualify(e.ID+1, e.SiteID, e.Directory, "* * * * *", "php8.3 "+e.Directory+"/artisan schedule:run", "fluxo")
	if !ok {
		t.Fatal("legacy fixture")
	}
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO crons VALUES(?,1,?,'* * * * *','fluxo')", legacy.ID, legacy.Command); err != nil {
		t.Fatal(err)
	}
	db.Close()
	old := "legacy generated cron\n"
	if err = os.WriteFile(CronPath(legacy.ID), []byte(old), 0644); err != nil {
		t.Fatal(err)
	}
	if err = Materialize(ctx, legacy, old, false); err != nil {
		t.Fatal(err)
	}
	if _, err = Inspect(ctx, legacy.ID); err == nil {
		t.Fatal("migration was ready before boot")
	}
	gen := uuid.NewString()
	if err = Hold(ctx, e.Version, gen, []Entry{e}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(rebootFixture{Entry: e, Legacy: legacy, Generation: gen})
	if err = os.WriteFile(filepath.Join(stateDir, "reboot-fixture.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("REBOOT_FIXTURE=%s", stateDir)
}

func TestVMSchedulerRebootVerify(t *testing.T) {
	dir := os.Getenv("FLUXO_SCHEDULER_REBOOT_FIXTURE")
	if dir == "" {
		t.Skip("explicit reboot verification only")
	}
	hostname, _ := os.Hostname()
	if os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" || os.Geteuid() != 0 || hostname != "fluxo-php-patch-test" || !strings.HasPrefix(dir, "/var/lib/fluxo-scheduler-acceptance-") {
		t.Fatal("invalid reboot guard")
	}
	prior, priorCron, priorDB := stateDir, cronDir, databasePath
	stateDir, cronDir, databasePath = dir, filepath.Join(dir, "cron"), filepath.Join(dir, "registration.db")
	defer func() { stateDir, cronDir, databasePath = prior, priorCron, priorDB }()
	raw, err := os.ReadFile(filepath.Join(dir, "reboot-fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture rebootFixture
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = Materialize(ctx, fixture.Legacy, "legacy generated cron\n", false); err == nil {
		t.Fatal("migration mutation bypassed another branch's hold")
	}
	if pending, err := Pending(); err != nil || !pending {
		t.Fatal("reboot cleared the maintenance hold", pending, err)
	}
	if out, err := Dispatch(ctx, fixture.Entry.ID); err != nil || !strings.Contains(out, "skipped") {
		t.Fatal("held scheduler launched after reboot", out, err)
	}
	if err = AssertDrained(ctx, fixture.Entry.Version, fixture.Generation, []Entry{fixture.Entry}); err != nil {
		t.Fatal(err)
	}
	if err = Release(ctx, fixture.Entry.Version, fixture.Generation, []Entry{fixture.Entry}); err != nil {
		t.Fatal(err)
	}
	if err = Materialize(ctx, fixture.Legacy, "legacy generated cron\n", false); err != nil {
		t.Fatal(err)
	}
	if _, err = Inspect(ctx, fixture.Legacy.ID); err != nil {
		t.Fatal("real boot did not permit legacy reconciliation", err)
	}
	if _, err = Dispatch(ctx, fixture.Entry.ID); err != nil {
		t.Fatal(err)
	}
	if err = Drain(ctx, fixture.Entry.Version, fixture.Generation, []Entry{fixture.Entry}); err != nil {
		t.Fatal(err)
	}
	if err = Release(ctx, fixture.Entry.Version, fixture.Generation, []Entry{fixture.Entry}); err != nil {
		t.Fatal(err)
	}
	t.Log("Real reboot preserved the hold and cleared only legacy readiness after reconciliation")
}

func TestVMSchedulerDatabaseGateCanonicalBranchesAndManagedRouting(t *testing.T) {
	e, _ := vmFixture(t)
	ctx := context.Background()
	if managed, err := Managed(ctx, e.ID, "plain"); err != nil || !managed {
		t.Fatal("managed trigger not recognized", managed, err)
	}
	plainID := e.ID + 1
	plain := "verified generated plain cron\n"
	if err := os.WriteFile(CronPath(plainID), []byte(plain), 0644); err != nil {
		t.Fatal(err)
	}
	if managed, err := Managed(ctx, plainID, plain); err != nil || managed {
		t.Fatal("plain legacy fallback routed through dispatcher", managed, err)
	}
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("CREATE TABLE php_update_generations(id TEXT PRIMARY KEY,version TEXT,state TEXT); INSERT INTO php_update_generations VALUES('pending','8.4','recovery_pending')"); err != nil {
		t.Fatal(err)
	}
	gen := uuid.NewString()
	if err = Hold(ctx, e.Version, gen, []Entry{e}); err != nil {
		t.Fatal(err)
	}
	if err = Release(ctx, e.Version, gen, []Entry{e}); err != nil {
		t.Fatal(err)
	}
	if out, err := Dispatch(ctx, e.ID); err != nil || !strings.Contains(out, "skipped") {
		t.Fatal("final-save crash window admitted a new tick", out, err)
	}
	if _, err = db.Exec("UPDATE crons SET command='changed' WHERE id=?", e.ID); err != nil {
		t.Fatal(err)
	}
	if err = Reclose(ctx, e.Version, gen); err != nil {
		t.Fatal("registration drift prevented closing admission", err)
	}
	if err = Hold(ctx, e.Version, gen, []Entry{e}); err == nil {
		t.Fatal("registration drift escaped validation")
	}
	if pending, err := Pending(); err != nil || !pending {
		t.Fatal("drift reopened Scheduler admission", pending, err)
	}
	if _, err = db.Exec("UPDATE crons SET command=? WHERE id=?", e.Command, e.ID); err != nil {
		t.Fatal(err)
	}
	if err = Release(ctx, e.Version, gen, []Entry{e}); err != nil {
		t.Fatal(err)
	}
	unlock, err := lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	j, err := read()
	if err != nil {
		t.Fatal(err)
	}
	valid, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	bad := j.Entries[e.ID]
	bad.Version = "8.3"
	j.Entries[e.ID] = bad
	if err = save(j); err != nil {
		t.Fatal(err)
	}
	unlock()
	if _, err = Dispatch(ctx, e.ID); err == nil {
		t.Fatal("malformed branch selected another PHP interpreter")
	}
	if err = os.WriteFile(filepath.Join(stateDir, "state.json"), valid, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE php_update_generations SET state='succeeded'"); err != nil {
		t.Fatal(err)
	}
	if _, err = Dispatch(ctx, e.ID); err != nil {
		t.Fatal("completed generation did not restore future ticks", err)
	}
	if err = Drain(ctx, e.Version, gen, []Entry{e}); err != nil {
		t.Fatal(err)
	}
	if err = Release(ctx, e.Version, gen, []Entry{e}); err != nil {
		t.Fatal(err)
	}
}
