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
	"github.com/google/uuid"
)

func managedVMGeneration(t *testing.T, units []unitSnapshot) recoveryGeneration {
	t.Helper()
	// This fixture exercises service lifecycle only. It does not manufacture a
	// Scheduler receipt or claim package-installation admission.
	id := uuid.NewString()
	if _, err := database.DB.Exec("INSERT INTO php_update_operations(id,kind,status,created_at,updated_at) VALUES(?,'apply','running',1,1)", id); err != nil {
		t.Fatal(err)
	}
	r := runtimeSnapshot{Units: units}
	g := recoveryGeneration{ID: id, Head: id, Version: "8.4", State: "prepared", Pending: true, Goal: "after", Original: r, Attempt: r}
	if err := createGeneration(g); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if retained, e := getGeneration(id); e == nil {
			if e = ReleaseMaintenanceEvidence(context.Background(), "php", id, retained.NaturalEvidence, nil, func() error { return saveGeneration(retained) }); e != nil {
				t.Error("release own lifecycle evidence", e)
			}
		}
	})
	return g
}

// Native service lifecycle only: no package transaction, customer database,
// Composer, ACME or real application command is executed by this fixture.
func TestVMManagedDeclaredShutdownGroupTimeoutAndOnceOnlyResume(t *testing.T) {
	requireRecoveryVM(t)
	if os.Getenv("FLUXO_SCHEDULER_ISOLATED_MOUNTS") != "1" {
		t.Skip("private task/scheduler journals required")
	}
	m := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("/home/fluxo", "managed-stop-acceptance-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	program := `<?php
$key=$argv[1];$base=__DIR__.'/'.$key;
$runs=(int)@file_get_contents($base.'.runs')+1;file_put_contents($base.'.runs',(string)$runs);
if($runs>1){file_put_contents($base.'.resumed','yes');exit(0);}
pcntl_async_signals(true);
if($key==='parent'){
 $child=pcntl_fork();
 if($child===0){
  pcntl_signal(SIGINT,function()use($base){file_put_contents($base.'.child-signal','INT');});
  file_put_contents($base.'.child-ready','yes');
  while(!file_exists($base.'.release'))usleep(50000);
  file_put_contents($base.'.child-done','yes');exit(0);
 }
 pcntl_signal(SIGINT,function()use($base){file_put_contents($base.'.signal','INT');exit(0);});
}else{
 pcntl_signal(SIGTERM,function()use($base){$n=(int)@file_get_contents($base.'.signals')+1;file_put_contents($base.'.signals',(string)$n);});
}
file_put_contents($base.'.ready','yes');
while(!file_exists($base.'.release'))usleep(50000);
exit(0);
`
	if err = os.WriteFile(filepath.Join(dir, "worker.php"), []byte(program), 0644); err != nil {
		t.Fatal(err)
	}
	vmRun(t, "/usr/bin/chown", "-R", "fluxo:fluxo", dir)
	if _, err = database.DB.Exec("INSERT INTO sites(id,domain,path,app_type,php_version) VALUES(8,'managed-stop.example.test',?,'laravel','8.4')", dir); err != nil {
		t.Fatal(err)
	}
	rows := []struct {
		id          int
		key, signal string
	}{{194101, "parent", "SIGINT"}, {194102, "ignore", "SIGTERM"}, {194103, "stopped", "SIGTERM"}}
	var names []string
	for _, row := range rows {
		name := unitName(row.id, 1)
		names = append(names, name)
		if _, e := os.Lstat(filepath.Join("/etc/systemd/system", name+".d")); !os.IsNotExist(e) {
			t.Fatal("preserve existing fixture drop-in", name, e)
		}
		t.Cleanup(func() { _ = os.Remove(filepath.Join("/etc/systemd/system", name+".d")) })
		command := "php8.4 worker.php " + row.key
		if _, err = database.DB.Exec("INSERT INTO daemons(id,site_id,name,command,directory,user,instances,stop_signal,stop_seconds) VALUES(?,8,?,?,?,'fluxo',1,?,2)", row.id, "Managed "+row.key, command, dir, row.signal); err != nil {
			t.Fatal(err)
		}
		vmUnit(t, name, "/usr/bin/"+command, dir)
		path := filepath.Join("/etc/systemd/system", name)
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		raw = []byte(strings.Replace(string(raw), "KillSignal=SIGTERM", "KillSignal="+row.signal, 1))
		if err = os.WriteFile(path, raw, 0644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = os.WriteFile(filepath.Join(dir, row.key+".release"), []byte("finish"), 0644)
			time.Sleep(150 * time.Millisecond)
		})
	}
	vmRun(t, "/usr/bin/systemctl", "daemon-reload")
	for _, name := range names[:2] {
		vmRun(t, "/usr/bin/systemctl", "start", name)
	}
	exists := func(name string) bool { _, e := os.Stat(filepath.Join(dir, name)); return e == nil }
	wait := func(label string, check func() bool) {
		t.Helper()
		end := time.Now().Add(12 * time.Second)
		for !check() {
			if time.Now().After(end) {
				t.Fatal("timeout: ", label)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	for _, key := range []string{"parent.ready", "parent.child-ready", "ignore.ready"} {
		wait(key, func() bool { return exists(key) })
	}
	set, reasons, err := InspectMaintenanceConsumers(ctx, nil, MaintenanceInspection{Policy: ManagedProcessPolicy})
	if err != nil || len(reasons) > 0 || len(set) != 3 {
		t.Fatal("actual managed inventory refused", reasons, err)
	}
	var units []unitSnapshot
	for _, c := range set {
		if c.Consumer.Protocol != managedProcessProtocol || c.Consumer.Kind != "" {
			t.Fatal("custom ownership/protocol changed", c.Consumer)
		}
		for _, u := range c.Units {
			if !u.Runtime.Natural || u.Runtime.ManagedStop == nil {
				t.Fatal("missing retained shutdown authority", u)
			}
			units = append(units, u.Runtime)
		}
	}
	before := digest(set)
	g := managedVMGeneration(t, units)
	err = m.drainConsumers(ctx, &g)
	if err == nil || !strings.Contains(err.Error(), "did not stop within 2 seconds") {
		t.Fatal("ignored shutdown admitted mutation", err)
	}
	for _, key := range []string{"parent.signal", "parent.child-signal", "ignore.signals"} {
		if !exists(key) {
			t.Fatal("declared signal did not reach group", key)
		}
	}
	if exists("parent.child-done") || exists("stopped.runs") {
		t.Fatal("live child was killed or stopped service was started")
	}
	var held int
	if e := database.DB.QueryRow("SELECT COUNT(*) FROM php_update_barriers WHERE generation_id=?", g.ID).Scan(&held); e != nil || held != len(units) || !g.Pending {
		t.Fatal("timeout lost durable holds", held, e)
	}
	// Wait past the original native TimeoutStopSec: the maintenance override must
	// prevent systemd's SIGKILL escalation while retaining the exact child.
	time.Sleep(2300 * time.Millisecond)
	for _, key := range []string{"parent.release", "ignore.release"} {
		if err = os.WriteFile(filepath.Join(dir, key), []byte("finish"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	g, err = getGeneration(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.drainConsumers(ctx, &g); err != nil {
		t.Fatal("fresh recovery failed", err)
	}
	count, e := os.ReadFile(filepath.Join(dir, "ignore.signals"))
	if e != nil || string(count) != "1" {
		t.Fatal("same-head recovery resent signal", string(count), e)
	}
	if !exists("parent.child-done") {
		t.Fatal("child did not complete naturally")
	}
	g.State = "activating"
	if err = saveGeneration(g); err != nil {
		t.Fatal(err)
	}
	if err = m.startConsumers(ctx, &g); err != nil {
		t.Fatal(err)
	}
	for _, u := range g.Original.Units[:2] {
		wait(u.Name+" completion", func() bool {
			ready, _, e := naturalActivationReady(ctx, &g, u)
			if e != nil {
				t.Fatal(e)
			}
			return ready
		})
	}
	if err = m.releaseBarriers(ctx, g); err != nil {
		t.Fatal(err)
	}
	g, err = getGeneration(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.startConsumers(ctx, &g); err != nil {
		t.Fatal("completion was replayed on retry", err)
	}
	for _, key := range []string{"parent.runs", "ignore.runs"} {
		data, e := os.ReadFile(filepath.Join(dir, key))
		if e != nil || string(data) != "2" {
			t.Fatal("resumed invocation replayed", key, string(data), e)
		}
	}
	if exists("stopped.runs") {
		t.Fatal("stopped process started during recovery")
	}
	after, reasons, e := InspectMaintenanceConsumers(ctx, nil, MaintenanceInspection{Policy: ManagedProcessPolicy, Recovery: true})
	if e != nil || len(reasons) > 0 || !SameMaintenanceConsumers(set, after) {
		t.Fatal("saved invocation/settings changed", before, digest(after), reasons, e)
	}
	for _, row := range rows {
		var signal, command string
		var seconds int
		if e = database.DB.QueryRow("SELECT stop_signal,stop_seconds,command FROM daemons WHERE id=?", row.id).Scan(&signal, &seconds, &command); e != nil || signal != row.signal || seconds != 2 || command != "php8.4 worker.php "+row.key {
			t.Fatal("record mutated", row, e)
		}
	}
	t.Log(fmt.Sprintf("declared group signal, child drainage, bounded timeout/no SIGKILL, stopped preservation and once-only recovery passed for %d units", len(units)))
}

func TestVMLegacyCustomRecoveryNeverAcquiresNewSignalAuthority(t *testing.T) {
	requireRecoveryVM(t)
	if os.Getenv("FLUXO_SCHEDULER_ISOLATED_MOUNTS") != "1" {
		t.Skip("private journals required")
	}
	m := testStore(t)
	ctx := context.Background()
	dir, err := os.MkdirTemp("/home/fluxo", "legacy-managed-stop-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	code := `<?php
$n=(int)@file_get_contents(__DIR__.'/runs')+1;file_put_contents(__DIR__.'/runs',(string)$n);
if($n>1)exit(0);
pcntl_async_signals(true);pcntl_signal(SIGTERM,function(){file_put_contents(__DIR__.'/unexpected-signal','TERM');exit(73);});
file_put_contents(__DIR__.'/ready','yes');while(!file_exists(__DIR__.'/finish'))usleep(50000);
`
	if err = os.WriteFile(filepath.Join(dir, "artisan"), []byte(code), 0644); err != nil {
		t.Fatal(err)
	}
	vmRun(t, "/usr/bin/chown", "-R", "fluxo:fluxo", dir)
	if _, err = database.DB.Exec("INSERT INTO sites(id,domain,path,app_type,php_version) VALUES(8,'legacy-stop.example.test',?,'laravel','8.4')", dir); err != nil {
		t.Fatal(err)
	}
	if _, err = database.DB.Exec("INSERT INTO daemons(id,site_id,name,command,directory,user,instances) VALUES(194104,8,'Earlier custom worker','php8.4 artisan custom:continuous',?,'fluxo',1)", dir); err != nil {
		t.Fatal(err)
	}
	name := unitName(194104, 1)
	if _, e := os.Lstat(filepath.Join("/etc/systemd/system", name+".d")); !os.IsNotExist(e) {
		t.Fatal("preserve existing drop-in", e)
	}
	t.Cleanup(func() { _ = os.Remove(filepath.Join("/etc/systemd/system", name+".d")) })
	vmUnit(t, name, "/usr/bin/php8.4 artisan custom:continuous", dir)
	t.Cleanup(func() {
		_ = os.WriteFile(filepath.Join(dir, "finish"), []byte("finish"), 0644)
		time.Sleep(150 * time.Millisecond)
	})
	vmRun(t, "/usr/bin/systemctl", "start", name)
	end := time.Now().Add(8 * time.Second)
	for {
		if _, e := os.Stat(filepath.Join(dir, "ready")); e == nil {
			break
		}
		if time.Now().After(end) {
			t.Fatal("legacy fixture not ready")
		}
		time.Sleep(50 * time.Millisecond)
	}
	set, reasons, err := InspectMaintenanceConsumers(ctx, nil, MaintenanceInspection{Policy: 0})
	if err != nil || len(reasons) > 0 || len(set) != 1 {
		t.Fatal(reasons, err)
	}
	u := set[0].Units[0].Runtime
	if set[0].Consumer.Protocol != "artisan_natural" || !u.Natural || u.ManagedStop != nil {
		t.Fatal("old authority changed", set)
	}
	g := managedVMGeneration(t, []unitSnapshot{u})
	short, cancel := context.WithTimeout(ctx, time.Second)
	err = m.drainConsumers(short, &g)
	cancel()
	if err == nil {
		t.Fatal("still-running old custom work was ignored")
	}
	if _, e := os.Stat(filepath.Join(dir, "unexpected-signal")); !os.IsNotExist(e) {
		t.Fatal("old generation received new automatic signal", e)
	}
	if err = os.WriteFile(filepath.Join(dir, "finish"), []byte("finish"), 0644); err != nil {
		t.Fatal(err)
	}
	g, err = getGeneration(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.drainConsumers(ctx, &g); err != nil {
		t.Fatal(err)
	}
	g.State = "activating"
	if err = saveGeneration(g); err != nil {
		t.Fatal(err)
	}
	if err = m.startConsumers(ctx, &g); err != nil {
		t.Fatal(err)
	}
	end = time.Now().Add(8 * time.Second)
	for {
		ready, _, e := naturalActivationReady(ctx, &g, u)
		if e != nil {
			t.Fatal(e)
		}
		if ready {
			break
		}
		if time.Now().After(end) {
			t.Fatal("legacy resume did not complete")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err = m.releaseBarriers(ctx, g); err != nil {
		t.Fatal(err)
	}
	raw, e := os.ReadFile(filepath.Join(dir, "runs"))
	if e != nil || string(raw) != "2" {
		t.Fatal("legacy recovery replayed", string(raw), e)
	}
	t.Log("saved legacy natural-only recovery sent no signal, waited for real completion, and resumed once")
}
