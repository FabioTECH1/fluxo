package maintenancetask

import (
	"context"
	"errors"
	"testing"
)

func TestWordPressApplicationJobHonorsEveryExistingMaintenanceBoundary(t *testing.T) {
	for _, pending := range []string{"",
		"INSERT INTO php_update_operations VALUES('apply','queued')",
		"INSERT INTO php_update_operations VALUES('repair','running')",
		"INSERT INTO php_update_generations VALUES('recovery_required')",
		"INSERT INTO php_update_barriers VALUES('worker')",
		"INSERT INTO php_repository_migrations VALUES('pending')",
		"INSERT INTO node_update_operations VALUES('apply','queued')",
		"INSERT INTO node_update_generations VALUES('recovery_required')",
		"INSERT INTO node_update_barriers VALUES('worker')",
		"INSERT INTO node_update_php_barriers VALUES('worker')",
	} {
		t.Run(pending, func(t *testing.T) {
			db, _ := admissionDB(t)
			if _, err := db.Exec(`CREATE TABLE sites(id INTEGER,path TEXT,deployment_strategy TEXT,app_type TEXT,app_directory TEXT);
INSERT INTO sites VALUES(7,'/home/fluxo/wp.test','standard','wordpress','.');`); err != nil {
				t.Fatal(err)
			}
			cmd := "/usr/bin/flock -n -E 0 '/home/fluxo/wp.test/.fluxo-wordpress-cron.lock' /usr/bin/php8.4 -f '/home/fluxo/wp.test/public/wp-cron.php'"
			c, ok := QualifyApplication(1, 7, "/home/fluxo/wp.test", "* * * * *", cmd, "fluxo")
			if !ok {
				t.Fatal("not a guarded application contract")
			}
			if _, err := db.Exec("UPDATE crons SET site_id=7,expression=?,command=?,user='fluxo' WHERE id=1", c.Expression, c.Command); err != nil {
				t.Fatal(err)
			}
			if pending != "" {
				if _, err := db.Exec(pending); err != nil {
					t.Fatal(err)
				}
			}
			err := queryAdmission(context.Background(), db, c)
			if pending == "" && err != nil {
				t.Fatal("ordinary WordPress task falsely blocked", err)
			}
			if pending != "" && !errors.Is(err, ErrMaintenance) {
				t.Fatal("launch escaped runtime gate", err)
			}
		})
	}
}
