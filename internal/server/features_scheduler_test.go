package server

import (
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"testing"

	"fluxo/internal/database"
)

func TestLaravelSchedulerCommitsBeforeIndependentPublicationRead(t *testing.T) {
	original := database.DB
	path := filepath.Join(t.TempDir(), "fluxo.db")
	if err := database.InitDB(path); err != nil {
		t.Fatal(err)
	}
	defer func() { database.DB.Close(); database.DB = original }()
	if _, err := database.DB.Exec("INSERT INTO sites(id,domain,path,php_version,app_type,deployment_strategy) VALUES(1,'scheduler.test','/home/fluxo/scheduler.test','8.4','laravel','zero-downtime')"); err != nil {
		t.Fatal(err)
	}
	uri := url.URL{Scheme: "file", Path: path}
	independent, err := sql.Open("sqlite", uri.String()+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		t.Fatal(err)
	}
	defer independent.Close()
	command := "php8.4 /home/fluxo/scheduler.test/current/artisan schedule:run"
	// Establish the old ordering's failure using the same independent reader
	// used by the loader: uncommitted rows cannot authenticate publication.
	tx, err := database.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	res, err := tx.Exec("INSERT INTO crons(site_id,name,expression,command,user) VALUES(1,'Laravel Scheduler','* * * * *',?,'fluxo')", command)
	if err != nil {
		t.Fatal(err)
	}
	oldID, _ := res.LastInsertId()
	var count int
	if err := independent.QueryRow("SELECT COUNT(*) FROM crons WHERE id=?", oldID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("uncommitted row visible: %d %v", count, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	published := false
	err = createLaravelScheduler(1, command, func(id int) error {
		var siteID int
		var path, strategy, app, expression, saved, user string
		err := independent.QueryRow(`SELECT c.site_id,s.path,s.deployment_strategy,s.app_type,c.expression,c.command,c.user FROM crons c JOIN sites s ON s.id=c.site_id WHERE c.id=?`, id).Scan(&siteID, &path, &strategy, &app, &expression, &saved, &user)
		if err != nil {
			return err
		}
		if siteID != 1 || path != "/home/fluxo/scheduler.test" || strategy != "zero-downtime" || app != "laravel" || expression != "* * * * *" || saved != command || user != "fluxo" {
			t.Fatal("publication saw different binding")
		}
		published = true
		return nil
	})
	if err != nil || !published {
		t.Fatalf("committed row unavailable to loader: %v", err)
	}
}

func TestLaravelSchedulerPublicationFailurePreservesRecoveryBinding(t *testing.T) {
	original := database.DB
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	defer func() { database.DB.Close(); database.DB = original }()
	if _, err := database.DB.Exec("INSERT INTO sites(id,domain,path,app_type) VALUES(1,'scheduler.test','/home/fluxo/scheduler.test','laravel')"); err != nil {
		t.Fatal(err)
	}
	refused := errors.New("publication interrupted after durable intent")
	id := 0
	command := "php8.4 /home/fluxo/scheduler.test/artisan schedule:run"
	err := createLaravelScheduler(1, command, func(created int) error { id = created; return refused })
	if !errors.Is(err, refused) || id == 0 {
		t.Fatalf("publication failure lost: %v", err)
	}
	var saved string
	if err := database.DB.QueryRow("SELECT command FROM crons WHERE id=? AND site_id=1", id).Scan(&saved); err != nil || saved != command {
		t.Fatalf("exact pending binding removed: %v", err)
	}
	if err := createLaravelScheduler(1, command, func(int) error { t.Fatal("duplicate job published"); return nil }); !errors.Is(err, errSchedulerAlreadyConfigured) {
		t.Fatalf("retry created a second registration: %v", err)
	}
	var count int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM crons WHERE site_id=1").Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate saved jobs", count, err)
	}
}
