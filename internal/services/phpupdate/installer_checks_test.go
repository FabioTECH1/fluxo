package phpupdate

import (
	"context"
	"database/sql"
	"testing"
)

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
