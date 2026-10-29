package maintenancetask

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func admissionDB(t *testing.T) (*sql.DB, Contract) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`CREATE TABLE crons(id INTEGER,site_id INTEGER,expression TEXT,command TEXT,user TEXT);
CREATE TABLE node_update_operations(kind TEXT,status TEXT);CREATE TABLE node_update_generations(state TEXT);
CREATE TABLE node_update_barriers(unit TEXT);CREATE TABLE node_update_php_barriers(unit TEXT);
CREATE TABLE php_update_operations(kind TEXT,status TEXT);CREATE TABLE php_update_generations(state TEXT);
CREATE TABLE php_update_barriers(unit TEXT);CREATE TABLE php_repository_migrations(state TEXT);`)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := Qualify(1, 0, "0 0 * * 0", PackageCleanupCommand, "root")
	if _, err = db.Exec("INSERT INTO crons VALUES(1,0,?,?, 'root')", c.Expression, c.Command); err != nil {
		t.Fatal(err)
	}
	return db, c
}

func TestAdmissionRetainsAllDurableRecoveryBoundaries(t *testing.T) {
	for _, q := range []string{
		"INSERT INTO node_update_operations VALUES('apply','queued')",
		"INSERT INTO node_update_operations VALUES('rollback','running')",
		"INSERT INTO node_update_generations VALUES('recovery_required')",
		"INSERT INTO node_update_barriers VALUES('node.service')",
		"INSERT INTO node_update_php_barriers VALUES('php.service')",
		"INSERT INTO php_update_operations VALUES('repair','queued')",
		"INSERT INTO php_update_generations VALUES('recovery_required')",
		"INSERT INTO php_update_barriers VALUES('worker.service')",
		"INSERT INTO php_repository_migrations VALUES('pending')",
	} {
		t.Run(q, func(t *testing.T) {
			db, c := admissionDB(t)
			if _, err := db.Exec(q); err != nil {
				t.Fatal(err)
			}
			if err := queryAdmission(context.Background(), db, c); !errors.Is(err, ErrMaintenance) {
				t.Fatal(err)
			}
		})
	}
}

func TestAdmissionAllowsChecksAndTerminalHistoryButNotLostSchemaOrDrift(t *testing.T) {
	db, c := admissionDB(t)
	if _, err := db.Exec("INSERT INTO node_update_operations VALUES('check','running');INSERT INTO node_update_generations VALUES('rolled_back');INSERT INTO php_update_generations VALUES('succeeded')"); err != nil {
		t.Fatal(err)
	}
	if err := queryAdmission(context.Background(), db, c); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE crons SET command='node /root/job.js'"); err != nil {
		t.Fatal(err)
	}
	if err := queryAdmission(context.Background(), db, c); err == nil {
		t.Fatal("changed command admitted")
	}
	if _, err := db.Exec("UPDATE crons SET command=?", c.Command); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP TABLE node_update_php_barriers"); err != nil {
		t.Fatal(err)
	}
	if err := queryAdmission(context.Background(), db, c); err == nil {
		t.Fatal("missing recovery schema admitted")
	}
}
