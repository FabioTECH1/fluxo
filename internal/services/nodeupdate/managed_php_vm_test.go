package nodeupdate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/phpupdate"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

// Exercise the actual Node-owned PHP coordination, without activating a Node
// runtime or changing PHP packages. No real application/ACME command runs.
func TestVMNodeManagedPHPDeclaredGroupShutdownAndResume(t *testing.T) {
	host, _ := os.Hostname()
	if host != "fluxo-php-patch-test" || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" || os.Getenv("FLUXO_SCHEDULER_ISOLATED_MOUNTS") != "1" {
		t.Skip("explicit isolated root acceptance VM only")
	}
	m := testManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	run := func(binary string, args ...string) string {
		t.Helper()
		out, e := syscmd.Run(ctx, 10*time.Second, binary, args...)
		if e != nil {
			t.Fatalf("%s: %v", binary, e)
		}
		return out
	}
	dir, err := os.MkdirTemp("/home/fluxo", "node-managed-shutdown-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	program := `<?php
$key=$argv[1];$base=__DIR__.'/'.$key;$runs=(int)@file_get_contents($base.'.runs')+1;file_put_contents($base.'.runs',(string)$runs);
if($runs>1){file_put_contents($base.'.resumed','yes');exit(0);}
pcntl_async_signals(true);
pcntl_signal(SIGQUIT,function()use($base){file_put_contents($base.'.main-signal','QUIT');exit(0);});
$pid=pcntl_fork();if($pid===0){pcntl_signal(SIGQUIT,function()use($base){file_put_contents($base.'.child-signal','QUIT');usleep(300000);exit(0);});file_put_contents($base.'.child-ready','yes');}
file_put_contents($base.'.ready','yes');while(!file_exists($base.'.finish'))usleep(50000);
`
	if err = os.WriteFile(filepath.Join(dir, "worker.php"), []byte(program), 0644); err != nil {
		t.Fatal(err)
	}
	run("/usr/bin/chown", "-R", "fluxo:fluxo", dir)
	if _, err = database.DB.Exec("INSERT INTO sites(id,domain,path,app_type,php_version) VALUES(8,'node-managed.example.test',?,'laravel','8.4')", dir); err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	names := []string{"fluxo-daemon-196101.service", "fluxo-daemon-196102.service"}
	for i, name := range names {
		path := filepath.Join("/etc/systemd/system", name)
		for _, p := range []string{path, path + ".d"} {
			if _, e := os.Lstat(p); !os.IsNotExist(e) {
				t.Fatal("preserve existing fixture path", p, e)
			}
		}
		command := "php8.4 worker.php " + strings.TrimSuffix(name, ".service")
		if _, err = database.DB.Exec("INSERT INTO daemons(id,site_id,name,command,directory,user,instances,stop_signal,stop_seconds) VALUES(?,8,'Node PHP process',?,?,'fluxo',1,'SIGQUIT',3)", 196101+i, command, dir); err != nil {
			t.Fatal(err)
		}
		content := "[Unit]\nDescription=Fluxo isolated Node PHP managed shutdown fixture\n[Service]\nType=simple\nUser=fluxo\nWorkingDirectory=" + dir + "\nExecStart=/usr/bin/" + command + "\nRestart=always\nRestartSec=1\nKillSignal=SIGQUIT\nTimeoutStopSec=3\n"
		if err = os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = os.WriteFile(filepath.Join(dir, strings.TrimSuffix(name, ".service")+".finish"), []byte("finish"), 0644)
			_ = os.Remove(barrierPath(phpBarrierUnit(name)))
			_ = os.Remove(permitPath(phpBarrierUnit(name), id))
			_, _ = syscmd.Run(context.Background(), 10*time.Second, "/usr/bin/systemctl", "daemon-reload")
			_, _ = syscmd.Run(context.Background(), 10*time.Second, "/usr/bin/systemctl", "stop", name)
			_ = os.Remove(path)
			_ = os.Remove(path + ".d")
			_, _ = syscmd.Run(context.Background(), 10*time.Second, "/usr/bin/systemctl", "daemon-reload")
		})
	}
	run("/usr/bin/systemctl", "daemon-reload")
	run("/usr/bin/systemctl", "start", names[0])
	wait := func(file string) {
		t.Helper()
		end := time.Now().Add(8 * time.Second)
		for {
			if _, e := os.Stat(filepath.Join(dir, file)); e == nil {
				return
			}
			if time.Now().After(end) {
				t.Fatal("timeout", file)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	key := strings.TrimSuffix(names[0], ".service")
	wait(key + ".ready")
	wait(key + ".child-ready")
	set, reasons, err := inspectPHPConsumersAt(ctx, nil, "", nil, nil)
	if err != nil || len(reasons) > 0 || len(set) != 2 {
		t.Fatal("new policy inventory", reasons, err)
	}
	impact := Impact{PHPPolicy: phpupdate.ManagedProcessPolicy, PHP: set}
	g := Generation{ID: id, Head: id, State: "prepared", Coordination: &impact, Plan: Plan{Impact: impact}}
	if err = saveGeneration(g); err != nil {
		t.Fatal(err)
	}
	if err = m.preparePHPState(ctx, &g); err != nil {
		t.Fatal(err)
	}
	if g.PHPState.Schema != 2 {
		t.Fatal("missing versioned coordination")
	}
	t.Cleanup(func() {
		if e := phpupdate.ReleaseMaintenanceEvidence(context.Background(), "node", g.ID, g.PHPState.Evidence, nil, func() error { return saveGeneration(g) }); e != nil {
			t.Error("release own evidence", e)
		}
	})
	if err = m.holdPHP(ctx, &g); err != nil {
		t.Fatal(err)
	}
	if err = m.drainPHP(ctx, &g); err != nil {
		t.Fatal(err)
	}
	wait(key + ".main-signal")
	wait(key + ".child-signal")
	g, err = getGeneration(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.preparePHPState(ctx, &g); err != nil {
		t.Fatal("fresh recovery", err)
	}
	if err = m.drainPHP(ctx, &g); err != nil {
		t.Fatal("idempotent drain", err)
	}
	if err = m.resumePHP(ctx, &g); err != nil {
		t.Fatal(err)
	}
	if err = m.verifyPHPAt(ctx, &g, nodeExecutable{}); err != nil {
		t.Fatal(err)
	}
	if err = m.releasePHP(ctx, &g); err != nil {
		t.Fatal(err)
	}
	g, err = getGeneration(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.preparePHPState(ctx, &g); err != nil {
		t.Fatal(err)
	}
	if err = m.holdPHP(ctx, &g); err != nil {
		t.Fatal(err)
	}
	if err = m.drainPHP(ctx, &g); err != nil {
		t.Fatal(err)
	}
	if err = m.resumePHP(ctx, &g); err != nil {
		t.Fatal("completion replayed", err)
	}
	if err = m.verifyPHPAt(ctx, &g, nodeExecutable{}); err != nil {
		t.Fatal(err)
	}
	if err = m.releasePHP(ctx, &g); err != nil {
		t.Fatal(err)
	}
	raw, e := os.ReadFile(filepath.Join(dir, key+".runs"))
	if e != nil || string(raw) != "2" {
		t.Fatal("duplicate resume", string(raw), e)
	}
	if _, e = os.Stat(filepath.Join(dir, strings.TrimSuffix(names[1], ".service")+".runs")); !os.IsNotExist(e) {
		t.Fatal("stopped consumer started", e)
	}
	t.Log("actual Node-owned schema2/group signal/child drainage/stopped intent/once-only recovery passed; no Node or PHP package mutation")
}
