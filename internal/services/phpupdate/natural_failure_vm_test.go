package phpupdate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
)

func TestVMNaturalFailedActivationIsNotReplayed(t *testing.T) {
	requireRecoveryVM(t)
	if os.Getenv("FLUXO_SCHEDULER_ISOLATED_MOUNTS") != "1" {
		t.Skip("private journal required")
	}
	m := testStore(t)
	ctx := context.Background()
	dir, err := os.MkdirTemp("/home/fluxo", "php-natural-failure-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	program := `<?php
$n=(int)@file_get_contents(__DIR__.'/runs')+1;file_put_contents(__DIR__.'/runs',(string)$n);
if($n===1){while(!file_exists(__DIR__.'/finish'))usleep(50000);exit(0);}exit(23);
`
	if err = os.WriteFile(filepath.Join(dir, "artisan"), []byte(program), 0644); err != nil {
		t.Fatal(err)
	}
	vmRun(t, "/usr/bin/chown", "-R", "fluxo:fluxo", dir)
	if _, err = database.DB.Exec("INSERT INTO sites(id,domain,path,app_type,php_version) VALUES(8,'failed-artisan.example.test',?,'laravel','8.4')", dir); err != nil {
		t.Fatal(err)
	}
	if _, err = database.DB.Exec("INSERT INTO daemons(id,site_id,name,command,directory,user,instances) VALUES(193204,8,'Custom failure','php8.4 artisan custom:failure',?,'fluxo',1)", dir); err != nil {
		t.Fatal(err)
	}
	name := unitName(193204, 1)
	if _, e := os.Lstat(filepath.Join("/etc/systemd/system", name+".d")); !os.IsNotExist(e) {
		t.Fatal("preserve existing fixture drop-in", e)
	}
	t.Cleanup(func() { _ = os.Remove(filepath.Join("/etc/systemd/system", name+".d")) })
	vmUnit(t, name, "/usr/bin/php8.4 artisan custom:failure", dir)
	vmRun(t, "/usr/bin/systemctl", "start", name)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err = os.Stat(filepath.Join(dir, "runs")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("initial command did not initialize")
		}
		time.Sleep(100 * time.Millisecond)
	}
	impact, err := inspectImpact(ctx, "8.4")
	if err != nil {
		t.Fatal(err)
	}
	r, err := captureRuntime(ctx, Plan{Version: "8.4", Impact: impact})
	if err != nil {
		t.Fatal(err)
	}
	var units []unitSnapshot
	for _, u := range r.Units {
		if u.Name == name {
			units = append(units, u)
		}
	}
	if len(units) != 1 || !units[0].Natural {
		t.Fatal("natural protocol not captured")
	}
	g := vmGeneration(t, m, units)
	// Publish the hold before permitting the initial command's natural finish.
	if err = m.prepareBarrier(ctx, g, units[0]); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "finish"), []byte("finish"), 0644); err != nil {
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
	deadline = time.Now().Add(10 * time.Second)
	for {
		_, _, err = naturalActivationReady(ctx, &g, units[0])
		if err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("failed command was not observed")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(err.Error(), "did not verify a successful completion") {
		t.Fatal(err)
	}
	g, err = getGeneration(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.startConsumers(ctx, &g); err == nil {
		t.Fatal("failed invocation was silently replayed")
	}
	count, err := os.ReadFile(filepath.Join(dir, "runs"))
	if err != nil || string(count) != "2" {
		t.Fatal("failed invocation replayed", string(count), err)
	}
	if busy, e := Applying(); e != nil || !busy {
		t.Fatal("failed activation lost its recovery hold", busy, e)
	}
}
