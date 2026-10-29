package scheduler

import (
	"context"
	"database/sql"
	"testing"
)

func TestSiteContractBindsLayoutAndPreservesCommandBranch(t *testing.T) {
	e, ok := QualifySite(1, 2, "/home/fluxo/app", "zero-downtime", "laravel", "* * * * *", "php8.3 /home/fluxo/app/current/artisan schedule:run", "fluxo")
	if !ok || e.Version != "8.3" || e.Directory != "/home/fluxo/app/current" {
		t.Fatal(e, ok)
	}
	for _, tc := range []struct {
		path, strategy, app, expr, cmd, user string
		ok                                   bool
	}{
		{"/home/fluxo/app", "zero-downtime", "laravel", "* * * * *", e.Command, "fluxo", true},
		{"/home/fluxo/app", "standard", "laravel", "* * * * *", e.Command, "fluxo", false},
		{"/home/fluxo/moved", "zero-downtime", "laravel", "* * * * *", e.Command, "fluxo", false},
		{"/home/fluxo/app", "zero-downtime", "php", "* * * * *", e.Command, "fluxo", false},
		{"/home/fluxo/app", "zero-downtime", "laravel", "*/2 * * * *", e.Command, "fluxo", false},
		{"/home/fluxo/app", "zero-downtime", "laravel", "* * * * *", e.Command + " && node job.js", "fluxo", false},
		{"/home/fluxo/app", "zero-downtime", "laravel", "* * * * *", e.Command, "root", false},
		{"/home/fluxo/app", "zero-downtime", "laravel", "* * * * *", "php8.4 /home/fluxo/app/current/artisan schedule:run", "fluxo", false},
	} {
		if got := MatchesSite(e, 2, tc.path, tc.strategy, tc.app, tc.expr, tc.cmd, tc.user); got != tc.ok {
			t.Fatalf("%+v accepted=%v", tc, got)
		}
	}
	if MatchesSite(e, 3, "/home/fluxo/app", "zero-downtime", "laravel", "* * * * *", e.Command, "fluxo") {
		t.Fatal("changed site accepted")
	}
}

func TestCommittedSiteRejectsChangedDatabaseLayout(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE sites(id INTEGER PRIMARY KEY,path TEXT,deployment_strategy TEXT,app_type TEXT,php_version TEXT);CREATE TABLE crons(id INTEGER PRIMARY KEY,site_id INTEGER,expression TEXT,command TEXT,user TEXT);INSERT INTO sites VALUES(2,'/home/fluxo/app','zero-downtime','laravel','8.4');INSERT INTO crons VALUES(1,2,'* * * * *','php8.3 /home/fluxo/app/current/artisan schedule:run','fluxo')`)
	if err != nil {
		t.Fatal(err)
	}
	e, _ := QualifySite(1, 2, "/home/fluxo/app", "zero-downtime", "laravel", "* * * * *", "php8.3 /home/fluxo/app/current/artisan schedule:run", "fluxo")
	if err = validateCommittedSite(context.Background(), db, e); err != nil {
		t.Fatal("actual command branch should survive site default change", err)
	}
	for _, q := range []string{"UPDATE sites SET deployment_strategy='standard'", "UPDATE sites SET path='/home/fluxo/moved'", "UPDATE crons SET user='root'"} {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(q); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if err = validateCommittedSite(context.Background(), db, e); err == nil {
			t.Fatal("database contract drift accepted", q)
		}
		if _, err = db.Exec("UPDATE sites SET path='/home/fluxo/app',deployment_strategy='zero-downtime';UPDATE crons SET user='fluxo'"); err != nil {
			t.Fatal(err)
		}
	}
}
