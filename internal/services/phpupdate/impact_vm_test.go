package phpupdate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/syscmd"
)

func TestVMCustomQueueAndManagedQueueParticipateWithoutAdoption(t *testing.T) {
	requireRecoveryVM(t)
	if os.Getenv("FLUXO_SCHEDULER_ISOLATED_MOUNTS") != "1" {
		t.Skip("requires private /var/lib mount to preserve the VM journal")
	}
	testStore(t)
	directory, err := os.MkdirTemp("/home/fluxo", "php-blocker-queue-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	if _, err = database.DB.Exec("INSERT INTO sites(id,domain,path,app_type,php_version) VALUES(8,'blocker.example.test',?,'laravel','8.4')", directory); err != nil {
		t.Fatal(err)
	}
	command := "php8.4 artisan queue:work --sleep=3 --tries=3 --timeout=90"
	for _, row := range []struct {
		id         int
		name, kind string
	}{{193081, "Queue Worker", ""}, {193082, "Laravel Queue Worker", "laravel_queue"}} {
		if _, err = database.DB.Exec(`INSERT INTO daemons(id,site_id,name,managed_kind,command,directory,user,instances,status) VALUES(?,8,?,?,?,?, 'fluxo',1,'stopped')`, row.id, row.name, row.kind, command, directory); err != nil {
			t.Fatal(err)
		}
	}
	names := []string{unitName(193081, 1), unitName(193082, 1)}
	for _, name := range names {
		vmUnit(t, name, "/usr/bin/"+command, directory)
		path := filepath.Join("/etc/systemd/system", name)
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, append(content, []byte("\n[Install]\nWantedBy=multi-user.target\n")...), 0644); err != nil {
			t.Fatal(err)
		}
		vmRun(t, "/usr/bin/systemctl", "daemon-reload")
		vmRun(t, "/usr/bin/systemctl", "enable", name)
		t.Cleanup(func() { _, _ = syscmd.Run(context.Background(), 10*time.Second, "/usr/bin/systemctl", "disable", name) })
	}
	impact, err := inspectImpact(context.Background(), "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if len(impact.BlockedReasons) != 0 {
		t.Fatalf("supported custom queue was blocked: %+v", impact.BlockedReasons)
	}
	if len(impact.Workers) != 2 || impact.Workers[0].ID != 193081 || impact.Workers[0].Kind != "" || impact.Workers[0].Protocol != "laravel_queue" || impact.Workers[1].ID != 193082 || impact.Workers[1].Kind != "laravel_queue" {
		t.Fatalf("independent queue ownership changed: %+v", impact.Workers)
	}
	var kind, status string
	if err = database.DB.QueryRow("SELECT managed_kind,status FROM daemons WHERE id=193081").Scan(&kind, &status); err != nil || kind != "" || status != "stopped" {
		t.Fatalf("custom configuration changed: %q %q %v", kind, status, err)
	}
	for _, name := range names {
		state := vmRun(t, "/usr/bin/systemctl", "show", name, "-p", "ActiveState", "-p", "UnitFileState", "-p", "MainPID")
		if !strings.Contains(state, "ActiveState=inactive") || !strings.Contains(state, "UnitFileState=enabled") || !strings.Contains(state, "MainPID=0") {
			t.Fatal("stopped/enabled intent changed: ", state)
		}
	}
	if _, err = database.DB.Exec("UPDATE sites SET php_version='8.3' WHERE id=8"); err != nil {
		t.Fatal(err)
	}
	impact, err = inspectImpact(context.Background(), "8.4")
	if err != nil || len(impact.Workers) != 2 || len(impact.Sites) != 1 || impact.Sites[0].Domain != "blocker.example.test" {
		t.Fatal("custom worker's actual interpreter lost its affected site", impact, err)
	}
}
