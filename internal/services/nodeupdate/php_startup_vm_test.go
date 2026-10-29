package nodeupdate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/nodetoolchain"
	"fluxo/internal/services/packages"
	"fluxo/internal/services/phpupdate"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

// A consumer-only readiness regression. No runtime selection, APT/FPM action,
// production database change or existing application unit is involved.
func TestVMNodeWaitsForRestartedLaravelSignalInitialization(t *testing.T) {
	host, _ := os.Hostname()
	if host != "fluxo-php-patch-test" || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("explicit root reusable VM only")
	}
	if _, err := os.Stat("/home/fluxo/php-recovery-acceptance/vendor/autoload.php"); err != nil {
		t.Fatal("retained Laravel fixture missing", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	unlock, err := packages.Lifecycle(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	m := testManager(t)
	run := func(args ...string) string {
		t.Helper()
		out, e := syscmd.Run(ctx, 15*time.Second, args[0], args[1:]...)
		if e != nil {
			t.Fatal(args, e)
		}
		return out
	}
	const unit = "fluxo-daemon-212601.service"
	unitPath := "/etc/systemd/system/" + unit
	for _, p := range []string{unitPath, unitPath + ".d"} {
		if _, e := os.Lstat(p); !os.IsNotExist(e) {
			t.Fatal("preserve existing fixture", p, e)
		}
	}
	dir, err := os.MkdirTemp("/home/fluxo", "node-php-startup-")
	if err != nil {
		t.Fatal(err)
	}
	g := Generation{ID: uuid.NewString(), Head: uuid.NewString(), State: "prepared"}
	t.Cleanup(func() {
		_ = os.WriteFile(filepath.Join(dir, "allow-init"), []byte("finish startup"), 0644)
		_ = os.Remove(barrierPath(phpBarrierUnit(unit)))
		_ = os.Remove(permitPath(phpBarrierUnit(unit), g.ID))
		_, _ = syscmd.Run(context.Background(), 10*time.Second, "/usr/bin/systemctl", "daemon-reload")
		if _, e := syscmd.Run(context.Background(), 20*time.Second, "/usr/bin/systemctl", "stop", unit); e != nil {
			t.Error("fixture stop", e)
		}
		if g.PHPState != nil {
			if e := phpupdate.ReleaseMaintenanceEvidence(context.Background(), "node", g.ID, g.PHPState.Evidence, nil, func() error { return saveGeneration(g) }); e != nil {
				t.Error("fixture evidence", e)
			}
		}
		_ = os.Remove(unitPath)
		_ = os.Remove(unitPath + ".d")
		_, _ = syscmd.Run(context.Background(), 10*time.Second, "/usr/bin/systemctl", "daemon-reload")
		_ = os.RemoveAll(dir)
	})
	program := `<?php
$runs=(int)@file_get_contents(__DIR__.'/runs')+1;file_put_contents(__DIR__.'/runs',(string)$runs);
if($runs>1){file_put_contents(__DIR__.'/starting','yes');while(!file_exists(__DIR__.'/allow-init'))usleep(50000);}
require '/home/fluxo/php-recovery-acceptance/vendor/autoload.php';
$worker=(new ReflectionClass(Illuminate\Queue\Worker::class))->newInstanceWithoutConstructor();
(new ReflectionProperty($worker,'events'))->setValue($worker,new Illuminate\Events\Dispatcher(new Illuminate\Container\Container));
(new ReflectionMethod($worker,'listenForSignals'))->invoke($worker);
file_put_contents(__DIR__.'/ready',(string)$runs);
while(!$worker->shouldQuit)usleep(50000);
`
	if err = os.WriteFile(filepath.Join(dir, "artisan"), []byte(program), 0644); err != nil {
		t.Fatal(err)
	}
	run("/usr/bin/chown", "-R", "fluxo:fluxo", dir)
	content := "[Unit]\nDescription=Fluxo isolated Laravel startup regression\n[Service]\nType=simple\nUser=fluxo\nWorkingDirectory=" + dir + "\nExecStart=/usr/bin/php8.4 artisan queue:work --sleep=3 --tries=3 --timeout=90\nRestart=always\nRestartSec=1\nKillSignal=SIGTERM\nTimeoutStopSec=15\nSendSIGKILL=no\n[Install]\nWantedBy=multi-user.target\n"
	if err = os.WriteFile(unitPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = database.DB.Exec("INSERT INTO sites(id,domain,path,app_type,php_version) VALUES(212600,'startup.test',?,'laravel','8.4')", dir); err != nil {
		t.Fatal(err)
	}
	if _, err = database.DB.Exec("INSERT INTO daemons(id,site_id,name,managed_kind,command,directory,user,instances) VALUES(212601,212600,'Delayed queue','laravel_queue','php8.4 artisan queue:work --sleep=3 --tries=3 --timeout=90',?,'fluxo',1)", dir); err != nil {
		t.Fatal(err)
	}
	run("/usr/bin/systemctl", "daemon-reload")
	run("/usr/bin/systemctl", "start", unit)
	waitFile := func(name, want string) {
		t.Helper()
		end := time.Now().Add(10 * time.Second)
		for {
			b, e := os.ReadFile(filepath.Join(dir, name))
			if e == nil && string(b) == want {
				return
			}
			if time.Now().After(end) {
				t.Fatal("fixture readiness", name, want)
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	waitFile("ready", "1")
	root, err := filepath.EvalSymlinks(filepath.Join(nodetoolchain.NodeRoot(), "current"))
	if err != nil {
		t.Fatal(err)
	}
	set, reasons, err := inspectPHPConsumers(ctx, []string{root}, "", nil, nil)
	if err != nil || len(reasons) != 0 || len(set) != 1 {
		t.Fatal("fixture inspection", err, reasons, len(set))
	}
	impact := Impact{PHPPolicy: 1, PHP: set}
	g.Coordination = &impact
	g.Plan = Plan{Impact: impact, Before: nodetoolchain.OwnedRuntime{Root: root}}
	if err = saveGeneration(g); err != nil {
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
		t.Fatal(err)
	}
	waitFile("starting", "yes")
	controller := run("/usr/bin/systemctl", "show", unit, "--property=MainPID,NRestarts")
	// No Node server applications are present to accidentally delay this check.
	// Hold Laravel initialization back so an immediate failure is deterministic.
	done := make(chan error, 1)
	go func() { done <- m.verifyPHP(ctx, &g, root) }()
	select {
	case e := <-done:
		t.Fatal("verification failed/passed before Laravel initialized", e)
	case <-time.After(1500 * time.Millisecond):
	}
	short, stop := context.WithTimeout(ctx, 1200*time.Millisecond)
	if e := m.verifyPHP(short, &g, root); e == nil {
		stop()
		t.Fatal("uninitialized worker passed readiness")
	}
	stop()
	if err = m.verifyPHPHold(ctx, &g, set[0].Units[0]); err != nil {
		t.Fatal("readiness refusal released hold", err)
	}
	if err = os.WriteFile(filepath.Join(dir, "allow-init"), []byte("ready"), 0644); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal("initialized Laravel worker did not verify", err)
	}
	if after := run("/usr/bin/systemctl", "show", unit, "--property=MainPID,NRestarts"); after != controller {
		t.Fatal("verification restarted controller", controller, after)
	}
	if err = m.releasePHP(ctx, &g); err != nil {
		t.Fatal(err)
	}
	var holds int
	if err = database.DB.QueryRow("SELECT count(*) FROM node_update_php_barriers").Scan(&holds); err != nil || holds != 0 {
		t.Fatal("own hold cleanup", holds, err)
	}
	if _, e := os.Lstat(permitPath(phpBarrierUnit(unit), g.ID)); !os.IsNotExist(e) {
		t.Fatal("own permit retained", e)
	}
	t.Log(fmt.Sprintf("Real Laravel restart waited for handlers; context-bounded refusal kept hold; same controller verified; own holds/permits released"))
}
