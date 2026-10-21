package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/services/processlog"
	"github.com/google/uuid"
)

func vmWait(t *testing.T, limit time.Duration, what string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("timed out: " + what)
}

// Uses the real daemon, an old direct cron command and a detached non-PHP child.
// No cron/VPS restart, signal or artificial ready flag retires the legacy work.
func TestVMLegacyMigrationAndSchedulerLifecycleWithoutReboot(t *testing.T) {
	e, dir := vmFixture(t)
	ctx := context.Background()
	host, err := qualifyLegacyHost(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = Disable(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	cronDir = "/etc/cron.d"
	if err = os.WriteFile(filepath.Join(stateDir, "real-cron"), []byte("1"), 0600); err != nil {
		t.Fatal(err)
	}
	e.ID = 1910031
	if _, err = os.Lstat(CronPath(e.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("reserved test cron already exists", err)
	}
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("INSERT INTO crons VALUES(?,1,?,'* * * * *','fluxo')", e.ID, e.Command); err != nil {
		t.Fatal(err)
	}
	// A protected stand-in for the independently tested installer boundary. It
	// certifies this fixture has no historical panel-origin manual executions.
	unlock, err := lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
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
	unlock()
	legacy := "# Fluxo Cron ID: 1910031\n* * * * * fluxo cd " + dir + " && " + e.Command + " >> /var/log/fluxo/cron-1910031.log 2>&1\n"
	if err = processlog.Prepare("/var/log/fluxo/cron-1910031.log", "fluxo"); err != nil {
		t.Fatal(err)
	}
	script := `<?php
file_put_contents(__DIR__.'/legacy-started',getmypid()."\n");
$p=pcntl_fork(); if($p>0)exit(0); posix_setsid(); $p=pcntl_fork(); if($p>0)exit(0);
pcntl_exec('/bin/sh',['-c','sleep 80; echo complete >> '.escapeshellarg(__DIR__.'/legacy-complete')]);
`
	if err = os.WriteFile(filepath.Join(dir, "artisan"), []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	if err = publishCron(CronPath(e.ID), []byte(legacy)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Disable(ctx, e.ID); _ = os.Remove(CronPath(e.ID)); _ = os.Remove(markerPath(e.Version)) })
	vmWait(t, 75*time.Second, "old Scheduler tick", func() bool { _, err := os.Stat(filepath.Join(dir, "legacy-started")); return err == nil })
	// Future ticks get the short fixture; the already detached legacy child
	// continues its old work independently and must still prevent certification.
	script = `<?php
file_put_contents(__DIR__.'/managed-runs',PHP_MAJOR_VERSION.'.'.PHP_MINOR_VERSION."\n",FILE_APPEND);
file_put_contents(__DIR__.'/capabilities',file_get_contents('/proc/self/status'));
`
	if err = os.WriteFile(filepath.Join(dir, "artisan"), []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	if err = Materialize(ctx, e, legacy, false); err != nil {
		t.Fatal(err)
	}
	if _, err = Inspect(ctx, e.ID); err == nil {
		t.Fatal("migration skipped receipt/drain evidence")
	}
	if err = EntryReceipt(ctx, e.ID, uuid.NewString()); err == nil {
		t.Fatal("manual stale receipt certified migration")
	}
	vmWait(t, 75*time.Second, "replacement cron receipt", func() bool {
		unlock, err := lock(ctx)
		if err != nil {
			return false
		}
		defer unlock()
		j, err := read()
		return err == nil && j.Migrations[e.Version].Replacements[e.ID].Received
	})
	if _, err = os.Stat(filepath.Join(dir, "legacy-complete")); err == nil {
		t.Fatal("fixture did not retain old work through its replacement receipt")
	}
	if _, err = Inspect(ctx, e.ID); err == nil {
		t.Fatal("detached non-PHP legacy child was ignored")
	}
	vmWait(t, 90*time.Second, "natural legacy drain and certification", func() bool { _, err := Inspect(ctx, e.ID); return err == nil })
	e, err = Inspect(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := qualifyLegacyHost(ctx)
	if err != nil || after != host {
		t.Fatal("cron or boot identity changed", after, err)
	}
	capabilities, err := os.ReadFile(filepath.Join(dir, "capabilities"))
	if err != nil || !strings.Contains(string(capabilities), "CapEff:\t0000000000000000\n") {
		t.Fatal("application retained root capabilities", err)
	}
	if _, err = Dispatch(ctx, e.ID); err != nil {
		t.Fatal("Run now after migration", err)
	}
	generation := uuid.NewString()
	if err = Drain(ctx, e.Version, generation, []Entry{e}); err != nil {
		t.Fatal(err)
	}
	if out, err := Dispatch(ctx, e.ID); err != nil || !strings.Contains(out, "skipped") {
		t.Fatal("paused Scheduler admitted work", out, err)
	}
	if err = Release(ctx, e.Version, generation, []Entry{e}); err != nil {
		t.Fatal(err)
	}
	if err = Disable(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = Dispatch(ctx, e.ID); err == nil {
		t.Fatal("disabled entry ran")
	}
	if err = os.Remove(CronPath(e.ID)); err != nil {
		t.Fatal(err)
	}
	if err = Materialize(ctx, e, "", true); err != nil {
		t.Fatal("re-enable", err)
	}
	e, err = Inspect(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Dispatch(ctx, e.ID); err != nil {
		t.Fatal("re-enabled Scheduler did not run", err)
	}
	finalGeneration := uuid.NewString()
	if err = Drain(ctx, e.Version, finalGeneration, []Entry{e}); err != nil {
		t.Fatal(err)
	}
	if err = Release(ctx, e.Version, finalGeneration, []Entry{e}); err != nil {
		t.Fatal(err)
	}
	if err = Disable(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("DELETE FROM crons WHERE id=?", e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = Dispatch(ctx, e.ID); err == nil {
		t.Fatal("deleted Scheduler ran")
	}
	t.Log("Same-boot legacy migration, natural detached-child completion, Run now, pause/resume, disable/re-enable, deletion and unprivileged execution verified")
}

func TestVMLegacyDeletionRequiresTwoReceiptsAndPreservesAbsence(t *testing.T) {
	e, dir := vmFixture(t)
	ctx := context.Background()
	if err := Disable(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	cronDir = "/etc/cron.d"
	if err := os.WriteFile(filepath.Join(stateDir, "real-cron"), []byte("1"), 0600); err != nil {
		t.Fatal(err)
	}
	id := 1910032
	if _, err := os.Lstat(CronPath(id)); !os.IsNotExist(err) {
		t.Fatal("reserved deletion test cron exists", err)
	}
	legacy := "# Fluxo Cron ID: 1910032\n* * * * * fluxo cd " + dir + " && " + e.Command + " >> /var/log/fluxo/cron-1910032.log 2>&1\n"
	if err := publishCron(CronPath(id), []byte(legacy)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(CronPath(id)); _ = os.Remove(markerPath(e.Version)) })
	if err := RetireLegacy(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(CronPath(id)); err != nil {
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
	j.Entries = map[int]Entry{}
	exe, _ := os.Executable()
	hash, _ := fileDigest(exe)
	boot, _ := bootID()
	j.PanelProof = &panelProof{Boot: boot, Binary: hash}
	if err = save(j); err != nil {
		t.Fatal(err)
	}
	unlock()
	if required, err := CompatibilityRequired(); err != nil || !required {
		t.Fatal("deletion-only state allowed incompatible rollback", err)
	}
	vmWait(t, 75*time.Second, "deletion marker A", func() bool {
		u, err := lock(ctx)
		if err != nil {
			return false
		}
		defer u()
		j, err := read()
		return err == nil && j.Migrations[e.Version].MarkerReceived
	})
	if err = BranchReady(ctx, e.Version); err == nil {
		t.Fatal("one deletion receipt certified the incomplete reload")
	}
	u, err := lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	j, err = read()
	if err != nil {
		t.Fatal(err)
	}
	m := j.Migrations[e.Version]
	u()
	if m.MarkerPhase != 2 || m.MarkerReceived {
		t.Fatal("marker B was not required")
	}
	// Crash after journal publication but before marker B file publication.
	previous := m
	previous.Marker = m.PreviousMarker
	if err = publishCron(markerPath(e.Version), []byte(markerContent(e.Version, previous))); err != nil {
		t.Fatal(err)
	}
	_ = BranchReady(ctx, e.Version)
	b, err := os.ReadFile(markerPath(e.Version))
	if err != nil || string(b) != markerContent(e.Version, m) {
		t.Fatal("marker B crash publication did not recover", err)
	}
	if err = Receipt(ctx, e.Version, m.Marker, 0); err == nil {
		t.Fatal("manual marker receipt accepted")
	}
	vmWait(t, 80*time.Second, "deletion marker B and natural certificate", func() bool { return BranchReady(ctx, e.Version) == nil })
	if _, err = os.Lstat(CronPath(id)); !os.IsNotExist(err) {
		t.Fatal("deleted application trigger was recreated", err)
	}
	t.Log("Two real cron receipts, incomplete-reload rejection, crash recovery and absence preservation verified")
}
