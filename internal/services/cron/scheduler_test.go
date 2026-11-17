package cron

import (
	"context"
	"database/sql"
	"testing"

	"fluxo/internal/services/maintenancetask"
)

func TestSchedulerRoutingUsesCommittedSiteLayoutAndType(t *testing.T) {
	for _, tc := range []struct {
		name, app, path, strategy, appDir, dir string
		wantScheduler, wantError               bool
	}{
		{"laravel", "laravel", "/home/fluxo/app", "standard", ".", "/home/fluxo/app", true, false},
		{"laravel current release", "laravel", "/home/fluxo/app", "zero-downtime", ".", "/home/fluxo/app/current", true, false},
		{"php application job", "php", "/home/fluxo/app", "standard", ".", "/home/fluxo/app", false, false},
		{"python application directory", "python", "/home/fluxo/app", "standard", "src", "/home/fluxo/app/src", false, false},
		{"stale release", "laravel", "/home/fluxo/app", "zero-downtime", ".", "/home/fluxo/app", false, true},
		{"moved PHP site", "php", "/home/fluxo/moved", "standard", ".", "/home/fluxo/app", false, true},
		{"missing application type", "", "/home/fluxo/app", "standard", ".", "/home/fluxo/app", false, true},
		{"missing site path", "laravel", "", "standard", ".", "/home/fluxo/app", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err = db.Exec(`CREATE TABLE sites(id INTEGER PRIMARY KEY,path TEXT,deployment_strategy TEXT,app_type TEXT,app_directory TEXT,php_version TEXT); INSERT INTO sites VALUES(1,?,?,?,?, '8.4')`, tc.path, tc.strategy, tc.app, tc.appDir); err != nil {
				t.Fatal(err)
			}
			// The pinned command branch survives a different site default.
			command := "php8.3 " + tc.dir + "/artisan schedule:run"
			e, scheduler, err := qualifyScheduler(context.Background(), db, 2, 1, tc.dir, "* * * * *", command, "fluxo")
			if (err != nil) != tc.wantError || scheduler != tc.wantScheduler {
				t.Fatalf("scheduler=%v error=%v", scheduler, err)
			}
			if tc.wantScheduler && (e.Directory != tc.dir || e.Version != "8.3") {
				t.Fatalf("incorrect scheduler contract: %+v", e)
			}
			if !tc.wantScheduler && !tc.wantError {
				contract, ok := taskContract(2, 1, tc.dir, "* * * * *", command, "fluxo")
				if !ok || contract.Kind != maintenancetask.ApplicationJob || contract.Directory != tc.dir {
					t.Fatalf("non-Laravel command lost guarded application routing: %+v %v", contract, ok)
				}
			}
			if _, _, err = qualifyScheduler(context.Background(), db, 2, 99, tc.dir, "* * * * *", command, "fluxo"); err == nil {
				t.Fatal("missing site did not fail closed")
			}
			db.Close()
			if _, _, err = qualifyScheduler(context.Background(), db, 2, 1, tc.dir, "* * * * *", command, "fluxo"); err == nil {
				t.Fatal("unavailable database did not fail closed")
			}
		})
	}
}
