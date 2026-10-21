package cron

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"fluxo/internal/services/scheduler"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

func init() {
	if len(os.Args) >= 3 && strings.HasPrefix(os.Args[1], "scheduler-") {
		ctx := context.Background()
		var err error
		switch os.Args[1] {
		case "scheduler-execute":
			err = scheduler.Execute(ctx, os.Args[2])
		case "scheduler-dispatch":
			id, _ := strconv.Atoi(os.Args[2])
			_, err = scheduler.Dispatch(ctx, id)
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

func TestVMNewAndCustomCronCreationBranchIsolationAndLifecycle(t *testing.T) {
	host, _ := os.Hostname()
	if host != "fluxo-php-patch-test" || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("guarded reusable VM only")
	}
	ctx := context.Background()
	root := "/var/lib/fluxo-cron-creation-" + uuid.NewString()
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	dir := "/home/fluxo/cron-creation-" + uuid.NewString()
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	script := `<?php $kind=($argv[1]??'')==='custom'?'custom':PHP_MAJOR_VERSION.'.'.PHP_MINOR_VERSION; file_put_contents(__DIR__.'/'.$kind,"run\n",FILE_APPEND);`
	if err := os.WriteFile(filepath.Join(dir, "artisan"), []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/chown", "-R", "fluxo:fluxo", dir); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(root, "registration.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("CREATE TABLE sites(id INTEGER PRIMARY KEY,app_type TEXT);CREATE TABLE crons(id INTEGER PRIMARY KEY,site_id INTEGER,command TEXT,expression TEXT,user TEXT);INSERT INTO sites VALUES(1,'laravel')"); err != nil {
		t.Fatal(err)
	}
	scheduler.SetDatabasePath(dbPath)
	defer scheduler.SetDatabasePath("/var/lib/fluxo/fluxo.db")
	ids := []int{1910041, 1910042, 1910043}
	for _, id := range ids {
		if _, err := os.Lstat(scheduler.CronPath(id)); !os.IsNotExist(err) {
			t.Fatal("reserved cron exists", id, err)
		}
	}
	oldDispatcher, oldErr := os.ReadFile("/var/lib/fluxo-scheduler/dispatcher")
	t.Cleanup(func() {
		for _, id := range ids {
			_ = Delete(id)
		}
		if oldErr == nil {
			_ = os.WriteFile("/var/lib/fluxo-scheduler/dispatcher", oldDispatcher, 0700)
		}
	})
	commands := []string{"php8.4 " + dir + "/artisan schedule:run", "php8.3 " + dir + "/artisan schedule:run", "/usr/bin/php8.4 " + dir + "/artisan custom"}
	for i, id := range ids {
		if _, err = db.Exec("INSERT INTO crons VALUES(?,1,?,'* * * * *','fluxo')", id, commands[i]); err != nil {
			t.Fatal(err)
		}
		if err = Create(id, dir, "* * * * *", commands[i], "fluxo", 1); err != nil {
			t.Fatal("create", id, err)
		}
	}
	readCount := func(name string) int {
		b, _ := os.ReadFile(filepath.Join(dir, name))
		return strings.Count(string(b), "run\n")
	}
	wait := func(what string, ready func() bool) {
		t.Helper()
		end := time.Now().Add(80 * time.Second)
		for time.Now().Before(end) {
			if ready() {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatal("timed out: " + what)
	}
	wait("new managed PHP8.4/PHP8.3 and custom cron ticks", func() bool { return readCount("8.4") > 0 && readCount("8.3") > 0 && readCount("custom") > 0 })
	raw, err := os.ReadFile(scheduler.CronPath(ids[2]))
	if err != nil || strings.Contains(string(raw), "scheduler-dispatch") {
		t.Fatal("custom cron was converted", err)
	}
	e, err := scheduler.Inspect(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	gen := uuid.NewString()
	if err = scheduler.Drain(ctx, "8.4", gen, []scheduler.Entry{e}); err != nil {
		t.Fatal(err)
	}
	n4, n3, nc := readCount("8.4"), readCount("8.3"), readCount("custom")
	wait("other branch and custom cron continue through PHP8.4 hold", func() bool { return readCount("8.3") > n3 && readCount("custom") > nc })
	if readCount("8.4") != n4 {
		t.Fatal("held PHP8.4 Scheduler tick executed")
	}
	if err = scheduler.Release(ctx, "8.4", gen, []scheduler.Entry{e}); err != nil {
		t.Fatal(err)
	}
	if err = Delete(ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err = scheduler.Dispatch(ctx, ids[0]); err == nil {
		t.Fatal("disabled Scheduler ran")
	}
	if err = Create(ids[0], dir, "* * * * *", commands[0], "fluxo", 1); err != nil {
		t.Fatal("re-enable", err)
	}
	if _, err = scheduler.Dispatch(ctx, ids[0]); err != nil {
		t.Fatal("Run now", err)
	}
	wait("re-enabled Run now", func() bool { return readCount("8.4") > n4 })
	for _, id := range ids {
		if err = Delete(id); err != nil {
			t.Fatal("delete", id, err)
		}
		if _, err = db.Exec("DELETE FROM crons WHERE id=?", id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = scheduler.Dispatch(ctx, ids[0]); err == nil {
		t.Fatal("deleted Scheduler ran")
	}
	t.Log("Preserved creation/lifecycle evidence", root, dir)
}
