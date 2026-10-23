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
	"fluxo/internal/syscmd"
)

func TestVMOtherArtisanCommandsWaitWithoutSignalsAndDoNotReplay(t *testing.T) {
	requireRecoveryVM(t)
	if os.Getenv("FLUXO_SCHEDULER_ISOLATED_MOUNTS") != "1" {
		t.Skip("private journal required")
	}
	m := testStore(t)
	ctx := context.Background()
	dir, err := os.MkdirTemp("/home/fluxo", "php-natural-artisan-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	program := `<?php
$key=($argv[1]??'')==='reports:generate'?'report':'server';
$base=__DIR__.'/'.$key;
$runs=(int)@file_get_contents($base.'.runs')+1;file_put_contents($base.'.runs',(string)$runs);
pcntl_async_signals(true);pcntl_signal(SIGTERM,function()use($base){file_put_contents($base.'.signal','unexpected');exit(73);});
if($runs===1){
 if($key==='report'){
  $pid=pcntl_fork();if($pid===0){file_put_contents($base.'.child','ready');while(!file_exists($base.'.child-exit'))usleep(50000);file_put_contents($base.'.child-done','natural');exit(0);}
 }
 file_put_contents($base.'.ready','ready');while(!file_exists($base.'.exit'))usleep(50000);
 file_put_contents($base.'.done','natural');exit(0);
}

file_put_contents($base.'.resumed','resumed');
if($key==='server')while(!file_exists($base.'.resumed-exit'))usleep(50000);
exit(0);
`
	if err = os.WriteFile(filepath.Join(dir, "artisan"), []byte(program), 0644); err != nil {
		t.Fatal(err)
	}
	vmRun(t, "/usr/bin/chown", "-R", "fluxo:fluxo", dir)
	if _, err = database.DB.Exec("INSERT INTO sites(id,domain,path,app_type,php_version) VALUES(8,'natural.example.test',?,'laravel','8.4')", dir); err != nil {
		t.Fatal(err)
	}
	names := []string{unitName(193201, 1), unitName(193202, 1), unitName(193203, 1)}
	commands := []string{"php8.4 artisan reports:generate --format=json", "/usr/bin/php8.4 artisan reverb:start --port=8080", "php8.4 artisan custom:unstarted"}
	unitFiles := map[string]string{}
	for i, name := range names {
		if _, e := os.Lstat(filepath.Join("/etc/systemd/system", name+".d")); !os.IsNotExist(e) {
			t.Fatalf("preserve existing drop-in %s", name)
		}
		t.Cleanup(func() { _ = os.Remove(filepath.Join("/etc/systemd/system", name+".d")) })
		if _, err = database.DB.Exec("INSERT INTO daemons(id,site_id,name,command,directory,user,instances,status) VALUES(?,8,?,?,?,'fluxo',1,'active')", 193201+i, fmt.Sprintf("Custom Artisan %d", i), commands[i], dir); err != nil {
			t.Fatal(err)
		}
		command := commands[i]
		if strings.HasPrefix(command, "php8.4 ") {
			command = "/usr/bin/" + command
		}
		vmUnit(t, name, command, dir)
		path := filepath.Join("/etc/systemd/system", name)
		data, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		data = append(data, []byte("\n[Install]\nWantedBy=multi-user.target\n")...)
		if err = os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
		unitFiles[name] = string(data)
	}
	vmRun(t, "/usr/bin/systemctl", "daemon-reload")
	for _, i := range []int{0, 2} {
		name := names[i]
		vmRun(t, "/usr/bin/systemctl", "enable", name)
		t.Cleanup(func() { _, _ = syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "disable", name) })
	}
	for _, name := range names[:2] {
		vmRun(t, "/usr/bin/systemctl", "start", name)
	}
	wait := func(label string, check func() bool) {
		t.Helper()
		until := time.Now().Add(12 * time.Second)
		for !check() {
			if time.Now().After(until) {
				t.Fatal("timeout: ", label)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	exists := func(name string) bool { _, e := os.Stat(filepath.Join(dir, name)); return e == nil }
	touch := func(name string) {
		t.Helper()
		if e := os.WriteFile(filepath.Join(dir, name), []byte("finish naturally"), 0644); e != nil {
			t.Fatal(e)
		}
	}
	for _, key := range []string{"report.ready", "report.child", "server.ready"} {
		wait(key, func() bool { return exists(key) })
	}
	impact, err := inspectImpact(ctx, "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if len(impact.Workers) != 3 || len(impact.BlockedReasons) > 0 {
		t.Fatal("custom Artisan participation", impact)
	}
	for _, c := range impact.Workers {
		if c.Kind != "" || c.Protocol != "artisan_natural" {
			t.Fatal("custom ownership/protocol changed", c)
		}
	}
	runtime, err := captureRuntime(ctx, Plan{Version: "8.4", Impact: impact})
	if err != nil {
		t.Fatal(err)
	}
	units := []unitSnapshot{}
	for _, u := range runtime.Units {
		if managedUnitPattern.MatchString(u.Name) {
			if !u.Natural {
				t.Fatal("drain protocol missing from durable runtime", u)
			}
			units = append(units, u)
		}
	}
	g := vmGeneration(t, m, units)
	short, cancel := context.WithTimeout(ctx, 3*time.Second)
	err = m.drainConsumers(short, &g)
	cancel()
	if err == nil {
		t.Fatal("live custom work admitted package mutation")
	}
	for _, key := range []string{"report.signal", "server.signal"} {
		if exists(key) {
			t.Fatal("unknown Artisan received automatic stop signal", key)
		}
	}
	if busy, e := Applying(); e != nil || !busy {
		t.Fatal("interrupted natural drain lost durable admission", busy, e)
	}
	// Even after its main exits, the retained PHP child must finish naturally.
	touch("report.exit")
	wait("main exited", func() bool { return exists("report.done") })
	short, cancel = context.WithTimeout(ctx, time.Second)
	err = m.drainConsumers(short, &g)
	cancel()
	if err == nil {
		t.Fatal("detached child or continuous command was ignored")
	}
	if exists("report.child-done") {
		t.Fatal("child ended before its own completion")
	}
	touch("report.child-exit")
	touch("server.exit")
	g, err = getGeneration(g.ID)
	if err != nil {
		t.Fatal(err)
	} // Fresh recovery observation.
	if err = m.drainConsumers(ctx, &g); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"report.child-done", "report.done", "server.done"} {
		if !exists(key) {
			t.Fatal("missing natural completion", key)
		}
	}
	current, err := inspectImpact(ctx, "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if err = recoveryCompatible(impact, current); err != nil {
		t.Fatal(err)
	}
	g.State = "activating"
	if err = saveGeneration(g); err != nil {
		t.Fatal(err)
	}
	if err = m.startConsumers(ctx, &g); err != nil {
		t.Fatal(err)
	}
	for _, u := range g.Original.Units[:2] {
		wait(u.Name+" activation", func() bool {
			ok, _, e := naturalActivationReady(ctx, &g, u)
			if e != nil {
				t.Fatal(e)
			}
			return ok
		})
	}
	if !g.NaturalResumes[names[0]].Completed {
		t.Fatal("successful short-lived invocation was not retained")
	}
	for _, marker := range []string{"report.resumed", "server.resumed"} {
		wait(marker, func() bool { return exists(marker) })
	}
	assertRuns := func() {
		t.Helper()
		for _, key := range []string{"report.runs", "server.runs"} {
			b, e := os.ReadFile(filepath.Join(dir, key))
			if e != nil || string(b) != "2" {
				t.Fatal("custom invocation was replayed", key, string(b), e)
			}
		}
	}
	assertRuns()
	if err = m.releaseBarriers(ctx, g); err != nil {
		t.Fatal(err)
	}
	g, err = getGeneration(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.startConsumers(ctx, &g); err != nil {
		t.Fatal("recovery after cleanup replayed an invocation", err)
	}
	assertRuns()
	// Deliberately lost/uncertain start evidence is never an excuse to replay.
	completed := g.NaturalResumes[names[0]]
	unconfirmed, err := naturalBoundary()
	if err != nil {
		t.Fatal(err)
	}
	g.NaturalResumes[names[0]] = unconfirmed
	if err = m.startConsumers(ctx, &g); err != nil {
		t.Fatal(err)
	}
	assertRuns()
	g.NaturalResumes[names[0]] = naturalResume{Boot: "b3c393f3-1f3b-44b0-80b3-c989efb2126e", After: completed.After}
	if err = m.startConsumers(ctx, &g); err == nil {
		t.Fatal("unproven rebooted activation accepted")
	}
	assertRuns()
	g.NaturalResumes[names[0]] = completed
	// Once its original Restart=always policy is restored, the continuous
	// service stays running. Stop only this idle fixture during test cleanup.
	for i, name := range names {
		data, e := os.ReadFile(filepath.Join("/etc/systemd/system", name))
		if e != nil || string(data) != unitFiles[name] {
			t.Fatal("original service changed", name, e)
		}
		p := unitProperties(vmRun(t, "/usr/bin/systemctl", "show", name, "-p", "UnitFileState", "-p", "ActiveState"))
		want := "enabled"
		if i == 1 {
			want = "disabled"
		}
		wantActive := "inactive"
		if i == 1 {
			wantActive = "active"
		}
		if p["UnitFileState"] != want || p["ActiveState"] != wantActive {
			t.Fatal("enabled/stopped intent changed", name, p)
		}
		var command, kind, status string
		if e = database.DB.QueryRow("SELECT command,managed_kind,status FROM daemons WHERE id=?", 193201+i).Scan(&command, &kind, &status); e != nil || command != commands[i] || kind != "" || status != "active" {
			t.Fatal("daemon record was changed", name, e)
		}
	}
	for _, key := range []string{"report.signal", "server.signal"} {
		if exists(key) {
			t.Fatal("custom command was signalled", key)
		}
	}
	g.Pending = false
	g.State = "succeeded"
	if err = saveGeneration(g); err != nil {
		t.Fatal(err)
	}
}
