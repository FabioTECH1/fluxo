package phpupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/syscmd"
)

func TestVMIndependentCustomQueuesDrainAndResumeWithoutRecordChanges(t *testing.T) {
	requireRecoveryVM(t)
	if os.Getenv("FLUXO_SCHEDULER_ISOLATED_MOUNTS") != "1" {
		t.Skip("requires private /var/lib mount")
	}
	if _, err := os.Stat(recoveryVMApp + "/vendor/autoload.php"); err != nil {
		t.Skip("requires the retained Laravel acceptance fixture")
	}
	m := testStore(t)
	ctx := context.Background()
	directory, err := os.MkdirTemp("/home/fluxo", "php-custom-queues-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	program := `<?php
require '/home/fluxo/php-recovery-acceptance/vendor/autoload.php';
// Exercise the installed Laravel worker's actual signal initialization without
// booting or changing the retained application or connecting to its queues.
$worker=(new ReflectionClass(Illuminate\Queue\Worker::class))->newInstanceWithoutConstructor();
// Newer Laravel versions dispatch interruption events from these handlers.
(new ReflectionProperty($worker,'events'))->setValue($worker,new Illuminate\Events\Dispatcher(new Illuminate\Container\Container));
(new ReflectionMethod($worker,'listenForSignals'))->invoke($worker);
$queue='alpha'; foreach($argv as $arg){if(str_starts_with($arg,'--queue='))$queue=substr($arg,8);}
$marker=__DIR__.'/'.$queue;
file_put_contents($marker.'.ready',(string)getmypid());
if(!is_file($marker.'.finished')){
 while(!is_file($marker.'.begin')&&!$worker->shouldQuit)usleep(50000);
 if($worker->shouldQuit)exit(0);
 file_put_contents($marker.'.started',(string)getmypid());
 $until=microtime(true)+30;while(microtime(true)<$until)usleep(50000);
 file_put_contents($marker.'.finished','job completed naturally');
}

while(!$worker->shouldQuit)usleep(50000);
`
	if err = os.WriteFile(filepath.Join(directory, "artisan"), []byte(program), 0644); err != nil {
		t.Fatal(err)
	}
	vmRun(t, "/usr/bin/chown", "-R", "fluxo:fluxo", directory)
	if _, err = database.DB.Exec("INSERT INTO sites(id,domain,path,app_type,php_version) VALUES(8,'queues.example.test',?,'laravel','8.4')", directory); err != nil {
		t.Fatal(err)
	}
	rows := []struct {
		id, instances         int
		kind, command, status string
	}{
		{193091, 2, "", "php8.4 artisan queue:work --queue=alpha --sleep=3 --tries=3 --timeout=90", "active"},
		{193092, 1, "laravel_queue", "php8.4 artisan queue:work redis --queue=managed --sleep=3 --tries=3 --timeout=90", "stopped"},
		{193093, 1, "", "/usr/bin/php8.4 artisan queue:work database --queue=beta --sleep=3 --tries=3 --timeout=90", "stopped"},
	}
	names := []string{}
	active := map[string]bool{unitName(193091, 1): true, unitName(193093, 1): true}
	enabled := map[string]bool{unitName(193091, 1): true, unitName(193092, 1): true}
	files := map[string]string{}
	for _, row := range rows {
		if _, err = database.DB.Exec(`INSERT INTO daemons(id,site_id,name,managed_kind,command,directory,user,instances,status,stop_seconds) VALUES(?,8,?,?,?,?,'fluxo',?,?,2)`, row.id, fmt.Sprintf("Independent worker %d", row.id), row.kind, row.command, directory, row.instances, row.status); err != nil {
			t.Fatal(err)
		}
		for i := 1; i <= row.instances; i++ {
			name := unitName(row.id, i)
			names = append(names, name)
			if _, e := os.Lstat(filepath.Join("/etc/systemd/system", name+".d")); !os.IsNotExist(e) {
				t.Fatalf("preserve existing drop-in directory %s: %v", name, e)
			}
			t.Cleanup(func() { _ = os.Remove(filepath.Join("/etc/systemd/system", name+".d")) })
			command := row.command
			if strings.HasPrefix(command, "php8.4 ") {
				command = "/usr/bin/" + command
			}
			vmUnit(t, name, command, directory)
			path := filepath.Join("/etc/systemd/system", name)
			content, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			content = append(content, []byte("\n[Install]\nWantedBy=multi-user.target\n")...)
			if e = os.WriteFile(path, content, 0644); e != nil {
				t.Fatal(e)
			}
			files[name] = string(content)
		}
	}
	vmRun(t, "/usr/bin/systemctl", "daemon-reload")
	for _, name := range names {
		if enabled[name] {
			vmRun(t, "/usr/bin/systemctl", "enable", name)
		}
		t.Cleanup(func() { _, _ = syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "disable", name) })
		if active[name] {
			vmRun(t, "/usr/bin/systemctl", "start", name)
		}
	}
	if _, err = database.DB.Exec("INSERT INTO laravel_queue_workers(site_id,daemon_id,enabled,connection,queues) VALUES(8,193092,1,'redis','managed')"); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		result := []map[string]any{}
		r, e := database.DB.Query("SELECT * FROM daemons ORDER BY id")
		if e != nil {
			t.Fatal(e)
		}
		columns, e := r.Columns()
		if e != nil {
			t.Fatal(e)
		}
		for r.Next() {
			values := make([]any, len(columns))
			refs := make([]any, len(columns))
			for i := range values {
				refs[i] = &values[i]
			}
			if e = r.Scan(refs...); e != nil {
				t.Fatal(e)
			}
			item := map[string]any{}
			for i, key := range columns {
				item[key] = values[i]
			}
			result = append(result, item)
		}
		if e = r.Err(); e != nil {
			t.Fatal(e)
		}
		r.Close()
		var id, enabled int
		var connection, queues string
		if e = database.DB.QueryRow("SELECT daemon_id,enabled,connection,queues FROM laravel_queue_workers WHERE site_id=8").Scan(&id, &enabled, &connection, &queues); e != nil {
			t.Fatal(e)
		}
		b, e := json.Marshal(map[string]any{"daemons": result, "feature": []any{id, enabled, connection, queues}})
		if e != nil {
			t.Fatal(e)
		}
		return string(b)
	}
	beforeRows := snapshot()
	wait := func(label string, check func() bool) {
		t.Helper()
		until := time.Now().Add(8 * time.Second)
		for !check() {
			if time.Now().After(until) {
				t.Fatal("timeout: ", label)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	for _, q := range []string{"alpha", "beta"} {
		wait(q+" initialization", func() bool { _, e := os.Stat(filepath.Join(directory, q+".ready")); return e == nil })
	}
	impact, err := inspectImpact(ctx, "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if len(impact.BlockedReasons) != 0 || len(impact.Workers) != 3 {
		t.Fatalf("queues not independently qualified: %+v", impact)
	}
	for _, c := range impact.Workers {
		if c.ID != 193092 && (c.Kind != "" || c.Protocol != "laravel_queue") {
			t.Fatal("custom ownership changed", c)
		}
	}
	plan := Plan{Version: "8.4", Impact: impact}
	runtime, err := captureRuntime(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	consumers := []unitSnapshot{}
	for _, u := range runtime.Units {
		if managedUnitPattern.MatchString(u.Name) {
			consumers = append(consumers, u)
		}
	}
	g := vmGeneration(t, m, consumers)
	for _, u := range g.Attempt.Units {
		if err = m.prepareBarrier(ctx, g, u); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{"alpha", "beta"} {
		if err = os.WriteFile(filepath.Join(directory, q+".begin"), []byte("start job"), 0644); err != nil {
			t.Fatal(err)
		}
		wait(q+" job start", func() bool { _, e := os.Stat(filepath.Join(directory, q+".started")); return e == nil })
	}
	drainCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = m.drainConsumers(drainCtx, &g)
	cancel()
	if err == nil {
		t.Fatal("maintenance admitted before in-flight jobs completed")
	}
	for _, q := range []string{"alpha", "beta"} {
		if _, e := os.Stat(filepath.Join(directory, q+".finished")); !os.IsNotExist(e) {
			t.Fatal("job ended before interruption was observed", q, e)
		}
	}
	if busy, e := Applying(); e != nil || !busy {
		t.Fatalf("interruption lost durable admission: %v %v", busy, e)
	}
	if err = m.drainConsumers(ctx, &g); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"alpha", "beta"} {
		data, e := os.ReadFile(filepath.Join(directory, q+".finished"))
		if e != nil || string(data) != "job completed naturally" {
			t.Fatal("job was not drained naturally", q, e)
		}
	}
	current, err := inspectImpact(ctx, "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if err = recoveryCompatible(impact, current); err != nil {
		t.Fatal("unchanged custom queues lost recovery compatibility", err)
	}
	if err = m.startConsumers(ctx, &g); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		wait(name+" activation", func() bool { u, e := unitState(ctx, name); return e == nil && u.Active == active[name] })
	}
	if err = m.releaseBarriers(ctx, g); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		data, e := os.ReadFile(filepath.Join("/etc/systemd/system", name))
		if e != nil || string(data) != files[name] {
			t.Fatal("original unit was rewritten", name, e)
		}
		state := unitProperties(vmRun(t, "/usr/bin/systemctl", "show", name, "-p", "UnitFileState"))
		want := "disabled"
		if enabled[name] {
			want = "enabled"
		}
		if state["UnitFileState"] != want {
			t.Fatalf("enabled intent changed for %s: %v", name, state)
		}
	}
	if snapshot() != beforeRows {
		t.Fatal("maintenance rewrote or deleted daemon/feature records")
	}
	// A wrapper remains unsupported even alongside otherwise supported workers.
	if _, err = database.DB.Exec("UPDATE daemons SET command='env php8.4 artisan queue:work' WHERE id=193091"); err != nil {
		t.Fatal(err)
	}
	blocked, err := inspectImpact(ctx, "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked.BlockedReasons) != 1 || !strings.Contains(blocked.BlockedReasons[0], "daemon #193091") {
		t.Fatal("unsupported wrapper was silently qualified", blocked.BlockedReasons)
	}
	if _, err = database.DB.Exec("UPDATE daemons SET command=? WHERE id=193091", rows[0].command); err != nil {
		t.Fatal(err)
	}
	g.Pending = false
	g.State = "succeeded"
	if err = saveGeneration(g); err != nil {
		t.Fatal(err)
	}
}

func TestVMCustomQueueWithoutGracefulHandlerRemainsBlocked(t *testing.T) {
	requireRecoveryVM(t)
	if os.Getenv("FLUXO_SCHEDULER_ISOLATED_MOUNTS") != "1" {
		t.Skip("requires private journal")
	}
	testStore(t)
	directory, err := os.MkdirTemp("/home/fluxo", "php-custom-handler-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	if err = os.WriteFile(filepath.Join(directory, "artisan"), []byte("<?php file_put_contents(__DIR__.'/ready','ready');while(true)usleep(100000);"), 0644); err != nil {
		t.Fatal(err)
	}
	vmRun(t, "/usr/bin/chown", "-R", "fluxo:fluxo", directory)
	if _, err = database.DB.Exec("INSERT INTO sites(id,domain,path,app_type,php_version) VALUES(8,'handler.example.test',?,'laravel','8.4')", directory); err != nil {
		t.Fatal(err)
	}
	if _, err = database.DB.Exec("INSERT INTO daemons(id,site_id,name,command,directory,user,instances) VALUES(193094,8,'Queue Worker','php8.4 artisan queue:work',?,'fluxo',1)", directory); err != nil {
		t.Fatal(err)
	}
	name := unitName(193094, 1)
	vmUnit(t, name, "/usr/bin/php8.4 artisan queue:work", directory)
	vmRun(t, "/usr/bin/systemctl", "start", name)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err = os.Stat(filepath.Join(directory, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture did not initialize")
		}
		time.Sleep(100 * time.Millisecond)
	}
	_, err = inspectImpact(context.Background(), "8.4")
	if err == nil || !strings.Contains(err.Error(), "no verified Laravel signal initialization") {
		t.Fatal("unsafe custom process was accepted", err)
	}
}
