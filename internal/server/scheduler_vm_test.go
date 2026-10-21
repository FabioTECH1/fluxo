package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/cron"
	"fluxo/internal/services/scheduler"
	"fluxo/internal/syscmd"
	"golang.org/x/sys/unix"
)

// Run only inside the isolated mount namespace described below. The real VM
// journal, cron files and panel remain outside this test's mounts.
func TestVMLegacySchedulerHTTPFallbackAfterAdoptionFailureAndRetry(t *testing.T) {
	host, _ := os.Hostname()
	if host != "fluxo-php-patch-test" || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" || os.Getenv("FLUXO_SCHEDULER_ISOLATED_MOUNTS") != "1" {
		t.Skip("guarded isolated reusable VM only")
	}
	root := os.Getenv("FLUXO_SCHEDULER_HTTP_EVIDENCE")
	if root == "" {
		t.Fatal("missing evidence directory")
	}
	app := "/home/fluxo/scheduler-http-acceptance"
	if err := os.Mkdir(app, 0755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(app)
	if err := os.WriteFile(filepath.Join(app, "artisan"), []byte("<?php file_put_contents(__DIR__.'/started','yes'); echo 'legacy HTTP completed';"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := syscmd.Run(context.Background(), time.Second, "chown", "-R", "fluxo:fluxo", app); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(root, "fluxo.db")
	if err := database.InitDB(dbPath); err != nil {
		t.Fatal(err)
	}
	defer database.DB.Close()
	scheduler.SetDatabasePath(dbPath)
	command := "php8.4 " + app + "/artisan schedule:run"
	if _, err := database.DB.Exec("INSERT INTO sites(id,domain,path,php_version,app_type) VALUES(1,'http.test',?,'8.4','laravel')", app); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("INSERT INTO crons(id,site_id,name,expression,command,user) VALUES(1910052,1,'Laravel Scheduler','* * * * *',?,'fluxo')", command); err != nil {
		t.Fatal(err)
	}
	legacy := "# Fluxo Cron ID: 1910052\n* * * * * fluxo cd " + app + " && " + command + " >> /var/log/fluxo/cron-1910052.log 2>&1\n"
	if err := os.WriteFile(scheduler.CronPath(1910052), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := scheduler.InvalidateLegacyManual(ctx, "8.4"); err != nil {
		t.Fatal(err)
	}
	gate, err := os.OpenFile("/var/lib/fluxo-scheduler/gate.lock", os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Close()
	if err = unix.Flock(int(gate.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	statePath := "/var/lib/fluxo-scheduler/state.json"
	read := func() map[string]any {
		b, e := os.ReadFile(statePath)
		if e != nil {
			t.Fatal(e)
		}
		var j map[string]any
		if e = json.Unmarshal(b, &j); e != nil {
			t.Fatal(e)
		}
		return j
	}
	j := read()
	boot, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	j["panel_proof"] = map[string]string{"boot": strings.TrimSpace(string(boot)), "binary": strings.Repeat("a", 64)}
	b, _ := json.Marshal(j)
	if err = os.WriteFile(statePath, b, 0600); err != nil {
		t.Fatal(err)
	}
	blocked, cancel := context.WithTimeout(ctx, 60*time.Millisecond)
	err = cron.ReconcileSchedulers(blocked)
	cancel()
	if err == nil {
		t.Fatal("injected startup adoption failure did not occur")
	}
	if err = unix.Flock(int(gate.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	managed, err := cron.ManagedScheduler(ctx, 1910052, app, "* * * * *", command, "fluxo")
	if err != nil || managed {
		t.Fatal("failed adoption did not preserve legacy Run now", managed, err)
	}
	s := NewServer(nil, root, false)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/sites/{id}/crons/{cron_id}/run", s.handleRunCron())
	httpServer := httptest.NewServer(s.phpMaintenanceGuard(mux))
	defer httpServer.Close()
	response, err := http.Post(httpServer.URL+"/api/v1/sites/1/crons/1910052/run", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !strings.Contains(string(body), "legacy HTTP completed") {
		t.Fatal(response.StatusCode, string(body))
	}
	if _, err = os.Stat(filepath.Join(app, "started")); err != nil {
		t.Fatal("actual HTTP application was not executed", err)
	}
	if got := read(); got["panel_proof"] != nil || got["legacy"].(map[string]any)["8.4"] != strings.TrimSpace(string(boot)) {
		t.Fatal("HTTP fallback retained obsolete panel certificate", got)
	}
	if err = cron.ReconcileSchedulers(ctx); err != nil {
		t.Fatal("adoption retry failed", err)
	}
	managed, err = cron.ManagedScheduler(ctx, 1910052, app, "* * * * *", command, "fluxo")
	if err != nil || !managed {
		t.Fatal("retry did not adopt exact legacy Scheduler", managed, err)
	}
	if read()["panel_proof"] != nil {
		t.Fatal("retry fabricated replacement panel proof")
	}
	if _, err = scheduler.Inspect(ctx, 1910052); err == nil {
		t.Fatal("retry became ready without fresh proof/receipt")
	}
	t.Log("PASS: real HTTP fallback after failed startup adoption runs normally, invalidates earlier proof; successful adoption retry stays unready without fresh evidence")
}
