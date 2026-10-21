package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"fluxo/internal/database"
	"fluxo/internal/services/cron"
	"fluxo/internal/services/processlog"
	"fluxo/internal/services/scheduler"
)

func TestVMSchedulerLogRepairPreservesAbsentTrigger(t *testing.T) {
	hostname, _ := os.Hostname()
	if os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" || os.Geteuid() != 0 || hostname != "fluxo-php-patch-test" {
		t.Skip("guarded reusable VM only")
	}
	dir, err := os.MkdirTemp("/var/lib", "fluxo-scheduler-log-acceptance-")
	if err != nil {
		t.Fatal(err)
	}
	if err = database.InitDB(filepath.Join(dir, "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	defer database.DB.Close()
	for _, id := range []int{9277001, 9277002} {
		for _, path := range []string{scheduler.CronPath(id), filepath.Join("/var/log/fluxo", fmt.Sprintf("cron-%d.log", id))} {
			if _, err = os.Lstat(path); !os.IsNotExist(err) {
				t.Fatal("refusing preexisting fixture path", path)
			}
		}
	}
	if _, err = database.DB.Exec("INSERT INTO sites(id,domain,path,php_version,app_type) VALUES(9277001,'absent-scheduler.test','/home/fluxo/absent-scheduler','8.4','laravel')"); err != nil {
		t.Fatal(err)
	}
	if _, err = database.DB.Exec(`INSERT INTO crons(id,site_id,name,expression,command,user) VALUES(9277001,9277001,'Laravel Scheduler','* * * * *','php8.4 /home/fluxo/absent-scheduler/artisan schedule:run','fluxo'),(9277002,0,'Log repair fixture','0 0 1 1 *','/usr/bin/true','fluxo')`); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target")
	if err = os.WriteFile(target, []byte("unchanged\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{9277001, 9277002} {
		log := filepath.Join("/var/log/fluxo", fmt.Sprintf("cron-%d.log", id))
		if err = os.Symlink(target, log); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Remove(log) })
	}
	enabled := "# Fluxo Cron ID: 9277002\n0 0 1 1 * fluxo /usr/bin/true >> /var/log/fluxo/cron-9277002.log 2>&1\n"
	if err = os.WriteFile(scheduler.CronPath(9277002), []byte(enabled), 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cron.Delete(9277002) })
	repairManagedCronLogs()
	if _, err = os.Lstat(scheduler.CronPath(9277001)); !os.IsNotExist(err) {
		t.Fatal("log repair enabled an absent Scheduler trigger", err)
	}
	actual, err := os.ReadFile(scheduler.CronPath(9277002))
	if err != nil || string(actual) != enabled {
		t.Fatal("enabled cron was not restored", err)
	}
	for _, id := range []int{9277001, 9277002} {
		safe, err := processlog.IsSafe(filepath.Join("/var/log/fluxo", fmt.Sprintf("cron-%d.log", id)))
		if err != nil || !safe {
			t.Fatal("log was not repaired safely", id, err)
		}
	}
	if actual, err = os.ReadFile(target); err != nil || string(actual) != "unchanged\n" {
		t.Fatal("repair followed unsafe log symlink", err)
	}
	t.Log("Preserved bootstrap log repair evidence:", dir)
}
