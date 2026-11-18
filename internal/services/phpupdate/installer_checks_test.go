package phpupdate

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestInstallerCheckCannotMaskMaintenanceInAnotherTable(t *testing.T) {
	for _, tc := range []struct {
		name, extra string
		pending     bool
	}{
		{"checks-only", "INSERT INTO node_update_operations VALUES (NULL,'running','check')", true},
		{"node-apply", "INSERT INTO node_update_operations VALUES (NULL,'queued','apply')", false},
		{"unknown-node-operation", "INSERT INTO node_update_operations VALUES (NULL,'running',NULL)", false},
		{"php-generation", "INSERT INTO php_update_generations VALUES ('active',NULL,NULL)", false},
		{"node-generation", "INSERT INTO node_update_generations VALUES ('active',NULL,NULL)", false},
		{"php-barrier", "INSERT INTO php_update_barriers VALUES (NULL,NULL,NULL)", false},
		{"node-barrier", "INSERT INTO node_update_barriers VALUES (NULL,NULL,NULL)", false},
		{"node-php-barrier", "INSERT INTO node_update_php_barriers VALUES (NULL,NULL,NULL)", false},
		{"repository-migration", "INSERT INTO php_repository_migrations VALUES ('pending',NULL,NULL)", false},
		{"resolved-generation", "INSERT INTO node_update_generations VALUES ('succeeded',NULL,NULL)", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(1)
			for _, table := range []string{"php_update_operations", "node_update_operations", "php_update_generations", "node_update_generations", "php_update_barriers", "node_update_barriers", "node_update_php_barriers", "php_repository_migrations"} {
				if _, err = db.Exec("CREATE TABLE " + table + " (state TEXT,status TEXT,kind TEXT)"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = db.Exec("INSERT INTO php_update_operations VALUES (NULL,'running','check')"); err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(tc.extra); err != nil {
				t.Fatal(err)
			}
			err = checkInstallerMaintenanceDatabase(context.Background(), db)
			if err == nil || errors.Is(err, ErrInstallerChecksPending) != tc.pending {
				t.Fatal("read-only check masked a cross-table maintenance blocker", err)
			}
		})
	}
}

func TestInstallerReadOnlyCheckClassification(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"php_update_operations", "node_update_operations"} {
		if _, err = db.Exec("CREATE TABLE " + table + " (status TEXT,kind TEXT)"); err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"check", "apply", "repair", "rollback", "verify", "repository_migrate", ""} {
			if _, err = db.Exec("DELETE FROM " + table); err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec("INSERT INTO "+table+" VALUES ('running',?),('queued','check'),('failed','apply')", kind); err != nil {
				t.Fatal(err)
			}
			got, err := installerOnlyChecks(context.Background(), db, table)
			if err != nil || got != (kind == "check") {
				t.Fatal("mutation classified as read-only", table, kind, got, err)
			}
		}
	}
	for _, table := range []string{"php_update_generations", "node_update_generations", "php_update_barriers", "node_update_php_barriers"} {
		if got, err := installerOnlyChecks(context.Background(), db, table); err != nil || got {
			t.Fatal("generation/barrier treated as a check", table, got, err)
		}
	}
}
