package scheduler

import (
	"bytes"
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

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

// This is the real Materialize/receipt/Dispatch/Execute/application/recovery
// path. Only platform selection uses a private test seam: ordinary production
// selectors remain disabled. The DB rows are unique real installed-DB rows;
// the Scheduler journal and helper are protected per-run test namespaces.
func TestVMDistributionSchedulerFullCaller(t *testing.T) {
	backend := backendProtocolHost()
	if backend == "" || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("explicit dedicated22/24/26 root opt-in only")
	}
	if executiondomain.CurrentSchedulerSchema != loaderEpochSchema {
		t.Skip("schema5 bootstrap/loader producers are not enabled yet")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()
	if err := executiondomain.GuardBackendWrite("/var/lib/fluxo", backend, executiondomain.Capabilities()); err != nil {
		t.Fatal("actual committed installed5/3 floor required", err)
	}
	global, err := read()
	if err != nil || global.Schema != executiondomain.CurrentSchedulerSchema || len(global.Entries) != 0 || len(global.Invocations) != 0 {
		t.Fatal("dedicated native empty current Scheduler core required; preserve existing state", err)
	}
	if _, err = os.Stat("/usr/bin/php8.4"); err != nil {
		t.Fatal("existing PHP8.4 fixture required; test never installs packages", err)
	}
	oldState, oldCron, oldDB, oldEnabled, oldSelector := stateDir, cronDir, databasePath, distributionCronQualificationEnabled, invocationAdapterSelector
	t.Cleanup(func() {
		stateDir, cronDir, databasePath, distributionCronQualificationEnabled, invocationAdapterSelector = oldState, oldCron, oldDB, oldEnabled, oldSelector
		InvalidateBackendStatus()
	})
	id := uuid.NewString()
	stateDir = backendProtocolRootPrefix + id
	cronDir = "/etc/cron.d"
	databasePath = "/var/lib/fluxo/fluxo.db"
	app, public := "/home/fluxo/cron-backend-acceptance-"+id, "/var/lib/fluxo-cron-backend-bin-"+id
	for path, mode := range map[string]os.FileMode{stateDir: 0700, public: 0755} {
		if err = os.Mkdir(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	credential, err := syscmd.ResolveCredential("fluxo")
	if err != nil || credential.Uid == 0 {
		t.Fatal("nonroot fixture identity", err)
	}
	if err = makeAcceptanceDirectoryAsFluxo(app); err != nil {
		t.Fatal(err)
	}
	c := backendProtocolFixture{App: app, Payload: filepath.Join(public, "payload"), Database: databasePath, FullCaller: true}
	if err = distributionAtomic(filepath.Join(stateDir, "protocol-fixture.json"), c, 0600); err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	image, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(c.Payload, image, 0755); err != nil {
		t.Fatal(err)
	}
	// The PHP parent exits; a session-detached non-PHP child retains progress
	// and release handshakes in its invocation hierarchy without inherited FD
	// locks. Only the root helper is intentionally killed later.
	script := fmt.Sprintf(`<?php
file_put_contents(__DIR__.'/application-starts',posix_geteuid()."\n",FILE_APPEND);
$p=pcntl_fork();if($p>0)exit(0);posix_setsid();
pcntl_exec(%q,['distribution-backend-payload',__DIR__]);exit(92);
`, c.Payload)
	if err = writeAcceptanceFileAsFluxo(filepath.Join(app, "artisan"), []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec("INSERT INTO sites(domain,path,app_type,php_version,deployment_strategy) VALUES(?,?,'laravel','8.4','standard')", "scheduler-caller-"+id+".invalid", app)
	if err != nil {
		t.Fatal(err)
	}
	siteID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	command := "php8.4 " + filepath.Join(app, "artisan") + " schedule:run"
	result, err = db.Exec("INSERT INTO crons(site_id,name,expression,command,user) VALUES(?,?,'* * * * *',?,'fluxo')", siteID, "guarded-fullcaller-"+id, command)
	if err != nil {
		t.Fatal(err)
	}
	cronID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	e, ok := Qualify(int(cronID), int(siteID), app, "* * * * *", command, "fluxo")
	if !ok {
		t.Fatal("real row qualification")
	}
	if _, err = os.Lstat(CronPath(e.ID)); !os.IsNotExist(err) {
		t.Fatal("allocated row cron path already occupied; preserve it", err)
	}
	invocationAdapterSelector = func(context.Context) (executiondomain.AdapterID, error) { return backendFullCallerAdapter(), nil }
	distributionCronQualificationEnabled = true
	t.Cleanup(func() {
		_ = writeAcceptanceFileAsFluxo(filepath.Join(app, "release"), []byte("release\n"), 0644)
		// Retain rows, app and journals for review. Remove only a launcher whose
		// bytes still match this private registration, preserving custom edits.
		if j, e := read(); e == nil {
			if own, found := j.Entries[int(cronID)]; found {
				if raw, e := os.ReadFile(CronPath(own.ID)); e == nil && string(raw) == CronContent(own) {
					_ = os.Remove(CronPath(own.ID))
				}
			}
		}
	})
	if err = Materialize(ctx, e, "", true); err != nil {
		t.Fatal("real Materialize", err)
	}
	j, err := read()
	if err != nil {
		t.Fatal(err)
	}
	e = j.Entries[e.ID]
	if e.LoaderNonce == "" || j.Schema != loaderEpochSchema || !j.LoaderEpochEstablished {
		t.Fatal("current producer omitted receipt authority")
	}
	if _, err = Dispatch(ctx, e.ID); err == nil {
		t.Fatal("direct dispatch admitted before actual receipt")
	}
	if err = Execute(ctx, "fluxo-scheduler-"+uuid.NewString()+".service"); err == nil {
		t.Fatal("unrecorded execution admitted")
	}
	if j, err = read(); err != nil || len(j.Invocations) != 0 {
		t.Fatal("pre-receipt call saved launch authority", err)
	}
	if _, err = os.Stat(filepath.Join(app, "application-starts")); !os.IsNotExist(err) {
		t.Fatal("pre-receipt application ran", err)
	}
	helper := filepath.Join(stateDir, "dispatcher")
	if _, err = syscmd.Run(ctx, 15*time.Second, helper, "scheduler-dispatch", strconv.Itoa(e.ID), uuid.NewString()); err == nil {
		t.Fatal("stale receipt nonce admitted")
	}
	// Temporarily replace ONLY our own trigger with a wrong epoch nonce.
	// This negative fixture proves genuine cron cannot save authority or run
	// the app before its exact current epoch target has been acknowledged.
	validTrigger := []byte(CronContent(e))
	expectedNonce := uuid.NewString()
	badTrigger := []byte(strings.Replace(string(validTrigger), e.LoaderNonce, expectedNonce, 1))
	if raw, readErr := os.ReadFile(CronPath(e.ID)); readErr != nil || !bytes.Equal(raw, validTrigger) {
		t.Fatal("own newly published epoch trigger changed", readErr)
	}
	if err = publishCron(CronPath(e.ID), badTrigger); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if raw, err := os.ReadFile(CronPath(e.ID)); err == nil && bytes.Equal(raw, badTrigger) {
			_ = os.Remove(CronPath(e.ID))
		}
	})
	if err = os.Remove(filepath.Join(stateDir, "caller-receipt-denied.json")); err != nil {
		t.Fatal(err)
	}
	backendProtocolWait(t, ctx, "genuine wrong-epoch cron refusal", func() bool {
		raw, readErr := os.ReadFile(filepath.Join(stateDir, "caller-receipt-denied.json"))
		return readErr == nil && strings.Contains(string(raw), expectedNonce)
	})
	if j, err = read(); err != nil || len(j.Invocations) != 0 {
		t.Fatal("wrong epoch cron saved launch authority", err)
	}
	if _, err = os.Stat(filepath.Join(app, "application-starts")); !os.IsNotExist(err) {
		t.Fatal("wrong epoch cron ran application", err)
	}
	if err = publishCron(CronPath(e.ID), validTrigger); err != nil {
		t.Fatal(err)
	}
	backendProtocolWait(t, ctx, "genuine receipt and real PHP application", func() bool { _, err := os.Stat(filepath.Join(app, "old-child-pid")); return err == nil })
	j, err = read()
	if err != nil || len(j.Invocations) != 1 {
		t.Fatal("expected exactly one real invocation", err)
	}
	e = j.Entries[e.ID]
	var inv invocation
	for _, value := range j.Invocations {
		inv = value
	}
	if inv.Adapter != backendFullCallerAdapter() || !inv.Started || inv.ServiceIdentity == nil || inv.Bootstrap == nil || inv.Bootstrap.Phase != "consumed" || !inv.Bootstrap.Loader.Valid() {
		t.Fatal("application started without saved adapter/service identity")
	}
	var before map[string]any
	raw, err := os.ReadFile(filepath.Join(stateDir, "caller-before-execute.json"))
	if err != nil || json.Unmarshal(raw, &before) != nil || before["unit"] != inv.Unit || before["adapter"] != string(backendFullCallerAdapter()) || before["started"] != false {
		t.Fatal("saved authority was not visible before execution", err)
	}
	starts, err := os.ReadFile(filepath.Join(app, "application-starts"))
	if err != nil || string(starts) != strconv.Itoa(int(credential.Uid))+"\n" {
		t.Fatal("application was root or ran more than once", err)
	}
	pidRaw, _ := os.ReadFile(filepath.Join(app, "old-child-pid"))
	child, err := strconv.Atoi(string(pidRaw))
	if err != nil || child <= 1 {
		t.Fatal("detached child identity", err)
	}
	group, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", child))
	sid, sidErr := unix.Getsid(child)
	if err != nil || strings.TrimSpace(string(group)) != "0::/system.slice/"+inv.Unit || sidErr != nil || sid != child {
		t.Fatal("detached non-PHP child escaped recorded hierarchy", err, sidErr)
	}
	if digest, err := fileDigest(fmt.Sprintf("/proc/%d/exe", child)); err != nil || digest != hashBytes(image) {
		t.Fatal("payload is not the actual non-PHP test executable", err)
	}
	// This fixture qualifies one minute. Retire its own cron trigger before a
	// later legitimate minute can create another independent invocation.
	raw, err = os.ReadFile(CronPath(e.ID))
	if err != nil || string(raw) != CronContent(e) {
		t.Fatal("own launcher changed", err)
	}
	if err = os.Remove(CronPath(e.ID)); err != nil {
		t.Fatal(err)
	}
	p, err := inspectInvocationUnit(ctx, inv.Unit, inv)
	if err != nil {
		t.Fatal(err)
	}
	mainPID, err := strconv.Atoi(p["MainPID"])
	if err != nil || mainPID <= 1 {
		t.Fatal("protected helper PID", err)
	}
	status, statusErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", mainPID))
	if statusErr != nil {
		t.Fatal(statusErr)
	}
	for _, expected := range []string{"CapPrm:\t00000000000000c4", "CapEff:\t00000000000000c4", "CapBnd:\t00000000000000c4", "CapAmb:\t0000000000000000", "NoNewPrivs:\t1"} {
		if !strings.Contains(string(status), expected+"\n") {
			t.Fatal("actual worker privilege boundary differs", expected)
		}
	}
	fd, err := unix.PidfdOpen(mainPID, 0)
	if err != nil {
		t.Fatal(err)
	}
	p, err = inspectInvocationUnit(ctx, inv.Unit, inv)
	if err == nil && p["MainPID"] == strconv.Itoa(mainPID) {
		err = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
	} else if err == nil {
		err = fmt.Errorf("helper changed before signal")
	}
	unix.Close(fd)
	if err != nil {
		t.Fatal(err)
	}
	progress, _ := os.ReadFile(filepath.Join(app, "old-child-progress"))
	backendProtocolWait(t, ctx, "detached child progress after helper loss", func() bool {
		raw, err := os.ReadFile(filepath.Join(app, "old-child-progress"))
		return err == nil && string(raw) != string(progress)
	})
	if err = Quiescent(ctx); err == nil {
		t.Fatal("helper loss hid populated invocation hierarchy")
	}
	known, err := KnownPIDs(ctx, e.Version)
	if err != nil || !known[child] {
		t.Fatal("actual recovery lost the live detached child", err)
	}
	if err = Execute(ctx, inv.Unit); err == nil {
		t.Fatal("already-started invocation replayed after helper loss")
	}
	j, err = read()
	if err != nil || len(j.Invocations) != 1 {
		t.Fatal("live descendant authority/evidence was retired", err)
	}
	if err = writeAcceptanceFileAsFluxo(filepath.Join(app, "release"), []byte("release\n"), 0644); err != nil {
		t.Fatal(err)
	}
	backendProtocolWait(t, ctx, "natural descendant drain", func() bool { return Quiescent(ctx) == nil })
	if _, err = KnownPIDs(ctx, e.Version); err != nil {
		t.Fatal("actual recovery/empty retirement", err)
	}
	j, err = read()
	if err != nil || len(j.Invocations) != 0 {
		t.Fatal("empty invocation not retired", err)
	}
	if err = Execute(ctx, inv.Unit); err == nil {
		t.Fatal("retired delayed authority replayed")
	}
	starts, err = os.ReadFile(filepath.Join(app, "application-starts"))
	if err != nil || string(starts) != strconv.Itoa(int(credential.Uid))+"\n" {
		t.Fatal("application replay detected", err)
	}
	proof := map[string]any{"status": "PASS", "backend": backend, "adapter": inv.Adapter, "site_id": siteID, "cron_id": cronID, "unit": inv.Unit, "real_materialize": true, "real_cron_receipt_dispatch_bootstrap_worker_application": true, "pre_receipt_direct_and_wrong_epoch_cron_denied": true, "stale_nonce_denied": true, "worker_caps_0xc4_no_new_privs": true, "saved_adapter_before_execute": true, "saved_service_identity_before_application": true, "helper_killed_only": true, "detached_non_php_progress_after_helper_loss": true, "readiness_blocked_until_natural_drain": true, "exactly_one_application": true, "no_late_or_replayed_authority": true, "actual_empty_recovery_retired": true, "private_journal_with_unique_installed_db_rows": true, "production_selectors_enabled": false, "new_boot_generation_qualified": false}
	if err = distributionAtomic(filepath.Join(stateDir, "fullcaller-evidence.json"), proof, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("fullcaller evidence retained at", stateDir, "with real installed DB rows", siteID, cronID)
}
