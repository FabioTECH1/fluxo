package nodeupdate

import (
	"context"
	"fluxo/internal/database"
	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/scheduler"
	"testing"
)

func TestWordPressCronUsesExistingNodeTaskInventory(t *testing.T) {
	testManager(t)
	if _, err := database.DB.Exec("INSERT INTO sites(id,domain,path,app_type,php_version,deployment_strategy) VALUES(7,'wp.test','/home/fluxo/wp.test','wordpress','8.4','standard')"); err != nil {
		t.Fatal(err)
	}
	cmd := "/usr/bin/flock -n -E 0 '/home/fluxo/wp.test/.fluxo-wordpress-cron.lock' /usr/bin/php8.4 -f '/home/fluxo/wp.test/public/wp-cron.php'"
	if _, err := database.DB.Exec("INSERT INTO crons(id,site_id,name,expression,command,user) VALUES(9,7,'WordPress Cron','* * * * *',?,'fluxo')", cmd); err != nil {
		t.Fatal(err)
	}
	calls := 0
	reasons, tasks, err := inspectCronInventoryDetailed(context.Background(), func(context.Context, int, scheduler.Entry) error {
		t.Fatal("WordPress became a Laravel scheduler")
		return nil
	}, func(_ context.Context, c maintenancetask.Contract, db string) (maintenancetask.Receipt, error) {
		calls++
		if c.Kind != maintenancetask.ApplicationJob || c.User != "fluxo" || c.Command != cmd || c.Directory != "/home/fluxo/wp.test" {
			t.Fatal("changed WordPress contract", c)
		}
		return maintenancetask.Receipt{Contract: c, Database: db, Identity: "verified-in-fixture", Helper: "bound", Profile: "fixed-dispatch-v1"}, nil
	})
	if err != nil || len(reasons) != 0 || len(tasks) != 1 || calls != 1 {
		t.Fatal(reasons, tasks, calls, err)
	}
}
