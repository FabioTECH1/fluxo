package nodeupdate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/nodetoolchain"
	"fluxo/internal/services/phpupdate"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

// Consumer-only smoke: no Node activation, package transaction, FPM action,
// original fixture modification or production database access occurs here.
func TestVMNodePHPConsumerLifecycleSmoke(t *testing.T) {
	host, _ := os.Hostname()
	if host != "fluxo-php-patch-test" || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("explicit root reusable acceptance VM only")
	}
	if _, err := os.Stat("/home/fluxo/php-recovery-acceptance/vendor/autoload.php"); err != nil {
		t.Skip("requires retained Laravel fixture")
	}
	m := testManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	run := func(binary string, args ...string) string {
		t.Helper()
		out, err := syscmd.Run(ctx, 15*time.Second, binary, args...)
		if err != nil {
			t.Fatalf("%s: %v", binary, err)
		}
		return out
	}
	root, err := filepath.EvalSymlinks(filepath.Join(nodetoolchain.NodeRoot(), "current"))
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("/home/fluxo", "node-php-smoke-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	program := `<?php
$key=$argv[1]==='queue:work'?'queue':'natural';$base=__DIR__.'/'.$key;
if(file_exists($base.'.fail')){$n=(int)@file_get_contents($base.'.fail-runs')+1;file_put_contents($base.'.fail-runs',(string)$n);exit(75);}
$runs=(int)@file_get_contents($base.'.runs')+1;file_put_contents($base.'.runs',(string)$runs);
if($key==='queue'){
 require '/home/fluxo/php-recovery-acceptance/vendor/autoload.php';
 $worker=(new ReflectionClass(Illuminate\Queue\Worker::class))->newInstanceWithoutConstructor();
 (new ReflectionProperty($worker,'events'))->setValue($worker,new Illuminate\Events\Dispatcher(new Illuminate\Container\Container));
 (new ReflectionMethod($worker,'listenForSignals'))->invoke($worker);
 file_put_contents($base.'.ready','ready');while(!$worker->shouldQuit){
  if($runs>1){
   $foreign=file_exists($base.'.foreign-node');$binary=$foreign?trim(file_get_contents($base.'.foreign-node')):'node';
   $job=proc_open([$binary,'-e','console.log(process.version);setTimeout(()=>process.exit(0),'.($foreign?5000:1200).')'],[0=>['file','/dev/null','r'],1=>['file',$base.($foreign?'.foreign-jobs':'.jobs'),'a'],2=>['file','/dev/null','w']],$pipes);
   proc_close($job);if($foreign)file_put_contents($base.'.foreign-finished','yes');usleep(200000);
  }else usleep(50000);
 }
}else{
 pcntl_async_signals(true);pcntl_signal(SIGTERM,function()use($base){file_put_contents($base.'.signal','unexpected');exit(73);});
 if($runs>1)exit(0);
 file_put_contents($base.'.ready','ready');while(!file_exists($base.'.exit'))usleep(50000);
}
if($runs===1){
 $child=proc_open(['node','-e','setTimeout(()=>process.exit(0),2500)'],[0=>['file','/dev/null','r'],1=>['file',$base.'.child','w'],2=>['file','/dev/null','w']],$pipes);
 file_put_contents($base.'.child-started','yes');proc_close($child);file_put_contents($base.'.done','natural');
}
`
	if err = os.WriteFile(filepath.Join(dir, "artisan"), []byte(program), 0644); err != nil {
		t.Fatal(err)
	}
	run("/usr/bin/chown", "-R", "fluxo:fluxo", dir)
	if _, err = database.DB.Exec("INSERT INTO sites(id,domain,path,app_type,php_version) VALUES(195000,'node-php-smoke.test',?,'laravel','8.4')", dir); err != nil {
		t.Fatal(err)
	}
	names := []string{"fluxo-daemon-195001.service", "fluxo-daemon-195002.service", "fluxo-daemon-195002-2.service", "fluxo-daemon-195003.service"}
	id := uuid.NewString()
	for _, name := range names {
		path := filepath.Join("/etc/systemd/system", name)
		if _, e := os.Lstat(path); !os.IsNotExist(e) {
			t.Fatalf("preserve existing %s", path)
		}
		if _, e := os.Lstat(path + ".d"); !os.IsNotExist(e) {
			t.Fatalf("preserve existing %s.d", path)
		}
		command := "/usr/bin/php8.4 artisan queue:work --sleep=3 --tries=3 --timeout=90"
		if strings.Contains(name, "195002") {
			command = "/usr/bin/php8.4 artisan reports:generate --format=json"
		}
		content := "[Unit]\nDescription=Fluxo Node PHP consumer smoke fixture\nStartLimitIntervalSec=60\nStartLimitBurst=5\n[Service]\nType=simple\nUser=fluxo\nWorkingDirectory=" + dir + "\nExecStart=" + command + "\nRestart=always\nRestartSec=1\nTimeoutStopSec=2\nKillSignal=SIGTERM\n[Install]\nWantedBy=multi-user.target\n"
		if err = os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = os.WriteFile(filepath.Join(dir, "natural.exit"), []byte("finish"), 0644)
			_ = os.Remove(barrierPath(phpBarrierUnit(name)))
			_ = os.Remove(permitPath(phpBarrierUnit(name), id))
			_, _ = syscmd.Run(context.Background(), 10*time.Second, "/usr/bin/systemctl", "daemon-reload")
			_, _ = syscmd.Run(context.Background(), 10*time.Second, "/usr/bin/systemctl", "stop", name)
			_, _ = syscmd.Run(context.Background(), 10*time.Second, "/usr/bin/systemctl", "disable", name)
			_ = os.Remove(path)
			_ = os.Remove(path + ".d")
			_, _ = syscmd.Run(context.Background(), 10*time.Second, "/usr/bin/systemctl", "daemon-reload")
		})
	}
	for _, row := range []struct {
		id, instances int
		kind, command string
	}{{195001, 1, "", "php8.4 artisan queue:work --sleep=3 --tries=3 --timeout=90"}, {195002, 2, "", "php8.4 artisan reports:generate --format=json"}, {195003, 1, "laravel_queue", "php8.4 artisan queue:work --sleep=3 --tries=3 --timeout=90"}} {
		if _, err = database.DB.Exec("INSERT INTO daemons(id,site_id,name,managed_kind,command,directory,user,instances) VALUES(?,195000,?,?,?,?,'fluxo',?)", row.id, fmt.Sprint("Worker ", row.id), row.kind, row.command, dir, row.instances); err != nil {
			t.Fatal(err)
		}
	}
	run("/usr/bin/systemctl", "daemon-reload")
	for _, name := range names[2:3] {
		run("/usr/bin/systemctl", "enable", name)
	}
	for _, name := range names[:2] {
		run("/usr/bin/systemctl", "start", name)
	}
	waitFile := func(name string) {
		t.Helper()
		until := time.Now().Add(10 * time.Second)
		for {
			if _, e := os.Stat(filepath.Join(dir, name)); e == nil {
				return
			}
			if time.Now().After(until) {
				t.Fatal("waiting for ", name)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	waitFile("queue.ready")
	waitFile("natural.ready")
	set, reasons, err := inspectPHPConsumers(ctx, []string{root}, "", nil, nil)
	if err != nil || len(reasons) > 0 || len(set) != 3 {
		t.Fatal("inspection", err, reasons, len(set))
	}
	impact := Impact{PHPPolicy: 1, PHP: set}
	g := Generation{ID: id, Head: id, State: "prepared", Coordination: &impact, Plan: Plan{Impact: impact, Before: nodetoolchain.OwnedRuntime{Root: root}, After: nodetoolchain.Release{Version: strings.TrimPrefix(filepath.Base(root), "v")}}}
	if err = saveGeneration(g); err != nil {
		t.Fatal(err)
	}
	if err = m.preparePHPState(ctx, &g); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if e := phpupdate.ReleaseMaintenanceEvidence(context.Background(), "node", g.ID, g.PHPState.Evidence, nil, func() error { return saveGeneration(g) }); e != nil {
			t.Error("clean own smoke reference", e)
		}
	})
	if err = m.holdPHP(ctx, &g); err != nil {
		t.Fatal(err)
	}
	drained := make(chan error, 1)
	go func() { drained <- m.drainPHP(ctx, &g) }()
	waitFile("queue.child-started")
	if _, e := os.Stat(filepath.Join(dir, "natural.signal")); e == nil {
		t.Fatal("natural command was signalled")
	}
	select {
	case e := <-drained:
		t.Fatal("natural work did not hold drainage", e)
	default:
	}
	if err = os.WriteFile(filepath.Join(dir, "natural.exit"), []byte("finish"), 0644); err != nil {
		t.Fatal(err)
	}
	if err = <-drained; err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"queue.done", "natural.done"} {
		waitFile(key)
	}
	if err = m.resumePHP(ctx, &g); err != nil {
		t.Fatal(err)
	}
	// Let the disabled finite command exit well before verification, then force
	// configuration reload. Success must use retained results, not a polling race.
	time.Sleep(3 * time.Second)
	run("/usr/bin/systemctl", "daemon-reload")
	if g.PHPState.Evidence == nil || g.PHPState.Evidence.State != "held" {
		t.Fatal("missing independent evidence retention")
	}
	controller := run("/usr/bin/systemctl", "show", names[0], "--property=MainPID,NRestarts")
	if err = m.verifyPHP(ctx, &g, root); err != nil {
		t.Fatal(err)
	}
	if after := run("/usr/bin/systemctl", "show", names[0], "--property=MainPID,NRestarts"); after != controller || strings.Contains(after, "MainPID=0\n") {
		t.Fatal("busy queue controller restarted during verification", controller, after)
	}
	jobs, _ := os.ReadFile(filepath.Join(dir, "queue.jobs"))
	if len(strings.Fields(string(jobs))) < 2 {
		t.Fatal("verification did not cover real child turnover", string(jobs))
	}
	for _, version := range strings.Fields(string(jobs)) {
		if version != filepath.Base(root) {
			t.Fatal("queue child used an unexpected Node runtime", version, root)
		}
	}
	// A retained authentic release is still the wrong post-switch child. Keep
	// that child alive long enough to prove provenance rejection, then let it
	// finish naturally before checking healthy turnover again.
	foreignRoot := filepath.Join(nodetoolchain.NodeRoot(), "v24.22.1")
	if foreignRoot == root {
		foreignRoot = filepath.Join(nodetoolchain.NodeRoot(), "v24.18.0")
	}
	if _, e := os.Stat(filepath.Join(foreignRoot, "bin/node")); e == nil {
		flag := filepath.Join(dir, "queue.foreign-node")
		if e = os.WriteFile(flag, []byte(filepath.Join(foreignRoot, "bin/node")), 0644); e != nil {
			t.Fatal(e)
		}
		until := time.Now().Add(10 * time.Second)
		for {
			data, _ := os.ReadFile(filepath.Join(dir, "queue.foreign-jobs"))
			if strings.Contains(string(data), filepath.Base(foreignRoot)) {
				break
			}
			if time.Now().After(until) {
				t.Fatal("foreign runtime fixture did not start")
			}
			time.Sleep(50 * time.Millisecond)
		}
		if e = m.verifyPHP(ctx, &g, root); e == nil || !strings.Contains(e.Error(), "unauthenticated Node.js child") {
			t.Fatal("busy worker bypassed child runtime provenance", e)
		}
		if e = os.Remove(flag); e != nil {
			t.Fatal(e)
		}
		waitFile("queue.foreign-finished")
		if e = m.verifyPHP(ctx, &g, root); e != nil {
			t.Fatal("healthy workload did not verify after foreign child ended", e)
		}
	} else {
		t.Log("retained-release negative case unavailable on this test host")
	}
	if !g.PHPState.Resumes[names[1]].Completed {
		t.Fatal("finite completion not persisted")
	}
	if err = m.releasePHP(ctx, &g); err != nil {
		t.Fatal(err)
	}
	// Re-enter recovery after cleanup: completion must prevent another dispatch.
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
	if err = m.verifyPHP(ctx, &g, root); err != nil {
		t.Fatal(err)
	}
	if err = m.releasePHP(ctx, &g); err != nil {
		t.Fatal(err)
	}
	runs, _ := os.ReadFile(filepath.Join(dir, "natural.runs"))
	if string(runs) != "2" {
		t.Fatal("finite command replayed", string(runs))
	}
	for _, name := range names[2:] {
		out := run("/usr/bin/systemctl", "show", name, "--property=ActiveState")
		if strings.TrimSpace(out) != "ActiveState=inactive" {
			t.Fatal("stopped instance started", name, out)
		}
	}
	// Both original active consumers are disabled. Explicitly let systemd
	// unload their inactive units before recovery, without changing enablement.
	for _, name := range names[:2] {
		if strings.TrimSpace(run("/usr/bin/systemctl", "show", name, "--property=UnitFileState")) != "UnitFileState=disabled" {
			t.Fatal("active fixture was enabled", name)
		}
	}
	// Exercise genuine failure/start-limit state in the queue fixture. The
	// production restart path must clear it while still preserving disabled.
	if err = m.preparePHPState(ctx, &g); err != nil {
		t.Fatal(err)
	}
	if err = m.holdPHP(ctx, &g); err != nil {
		t.Fatal(err)
	}
	if err = m.drainPHP(ctx, &g); err != nil {
		t.Fatal(err)
	}
	unit := phpBarrierUnit(names[0])
	if err = durableDirectory(filepath.Dir(permitPath(unit, g.ID)), 0755); err != nil {
		t.Fatal(err)
	}
	if err = durableWrite(permitPath(unit, g.ID), []byte(g.ID+"\n"), 0644, false); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "queue.fail"), []byte("fixture failure"), 0644); err != nil {
		t.Fatal(err)
	}
	refused := 0
	for i := 0; i < 8; i++ {
		if _, e := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "start", names[0]); e != nil {
			refused++
		}
		time.Sleep(150 * time.Millisecond)
	}
	state := run("/usr/bin/systemctl", "show", names[0], "--property=ActiveState,Result")
	failRuns, e := os.ReadFile(filepath.Join(dir, "queue.fail-runs"))
	// systemd255 preserves the preceding exit-code Result on manually rate-
	// limited starts. Prove refused dispatches using the fixture's invocation
	// counter as well; do not confuse that Result spelling with lack of a limit.
	if !strings.Contains(state, "ActiveState=failed") || refused < 3 || e != nil || string(failRuns) != "5" {
		t.Fatal("start-limit fixture did not fail as intended", state, refused, string(failRuns), e)
	}
	if err = os.Remove(filepath.Join(dir, "queue.fail")); err != nil {
		t.Fatal(err)
	}
	if err = m.resumePHP(ctx, &g); err != nil {
		t.Fatal(err)
	}
	if err = m.verifyPHP(ctx, &g, root); err != nil {
		t.Fatal(err)
	}
	if err = m.releasePHP(ctx, &g); err != nil {
		t.Fatal(err)
	}
	current, e := filepath.EvalSymlinks(filepath.Join(nodetoolchain.NodeRoot(), "current"))
	if e != nil || current != root {
		t.Fatal("consumer smoke changed Node runtime", current, e)
	}
	if err = phpupdate.ValidateMaintenancePHP(ctx, set); err != nil {
		t.Fatal(err)
	}
}
