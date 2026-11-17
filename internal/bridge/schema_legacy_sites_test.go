package bridge

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/database"
)

func TestSchemaPreservesRetiredPostDeployStorage(t *testing.T) {
	db := fixture(t)
	if _, err := db.Exec(`ALTER TABLE sites ADD COLUMN post_deploy_script TEXT DEFAULT '';
		INSERT INTO sites (id, domain, path, deploy_script, post_deploy_script)
		VALUES (1, 'retained.test', '/srv/retained', 'printf active', 'printf retired'),
		(2, 'null.test', '/srv/null', 'printf second', NULL)`); err != nil {
		t.Fatal(err)
	}
	var beforeDDL string
	var beforeChanges int
	if err := db.QueryRow("SELECT sql FROM sqlite_master WHERE name='sites'").Scan(&beforeDDL); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT total_changes()").Scan(&beforeChanges); err != nil {
		t.Fatal(err)
	}
	if err := checkSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var afterDDL, active, retired string
	var afterChanges int
	var nullValue sql.NullString
	if err := db.QueryRow("SELECT sql FROM sqlite_master WHERE name='sites'").Scan(&afterDDL); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT total_changes()").Scan(&afterChanges); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT deploy_script, post_deploy_script FROM sites WHERE id=1").Scan(&active, &retired); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT post_deploy_script FROM sites WHERE id=2").Scan(&nullValue); err != nil {
		t.Fatal(err)
	}
	if beforeDDL != afterDDL || beforeChanges != afterChanges || active != "printf active" || retired != "printf retired" || nullValue.Valid {
		t.Fatal("inspection changed historical storage or promoted a retired script")
	}
}

func TestPublished036SchemaPreparesWithoutDroppingRetiredCommands(t *testing.T) {
	raw, err := os.ReadFile("testdata/published-v0.3.6-schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(raw)) != "51e413cf93e5627987763d694a67a0a0ff63a18fef254f0be5aba940af09955c" {
		t.Fatal("published 0.3.6 DDL fixture changed")
	}
	path := filepath.Join(t.TempDir(), "historical.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = legacy.Exec(string(raw)); err == nil {
		_, err = legacy.Exec(`INSERT INTO sites (domain, path, deploy_script, post_deploy_script)
			VALUES ('historical.test', '/srv/historical', 'printf active', 'printf retained')`)
	}
	closeErr := legacy.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	previous := database.DB
	if err := database.InitDB(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.DB.Close(); database.DB = previous })
	if err := checkSchema(context.Background(), database.DB); err != nil {
		t.Fatal(err)
	}
	var active, retired string
	if err := database.DB.QueryRow("SELECT deploy_script, post_deploy_script FROM sites WHERE domain='historical.test'").Scan(&active, &retired); err != nil {
		t.Fatal(err)
	}
	if active != "printf active" || retired != "printf retained" {
		t.Fatal("historical script storage was rewritten during preparation")
	}
}

func TestRetiredPostDeployAllowanceRejectsAlteredAuthority(t *testing.T) {
	for _, tc := range []struct{ name, column string }{
		{"wrong-type", "post_deploy_script INTEGER DEFAULT ''"},
		{"wrong-default", "post_deploy_script TEXT DEFAULT 'changed'"},
		{"missing-default", "post_deploy_script TEXT"},
		{"not-null", "post_deploy_script TEXT NOT NULL DEFAULT ''"},
		{"virtual", "post_deploy_script TEXT GENERATED ALWAYS AS (deploy_script) VIRTUAL"},
		{"stored", "post_deploy_script TEXT GENERATED ALWAYS AS (deploy_script) STORED"},
		{"collation", "post_deploy_script TEXT COLLATE NOCASE DEFAULT ''"},
		{"check", "post_deploy_script TEXT DEFAULT '' CHECK (length(post_deploy_script)<100)"},
		{"unique", "post_deploy_script TEXT DEFAULT '' UNIQUE"},
		{"foreign-key", "post_deploy_script TEXT DEFAULT '' REFERENCES users(username)"},
		{"other-name", "post_deploy_script_shadow TEXT DEFAULT ''"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(1)
			const target = "deploy_script_mode TEXT DEFAULT 'managed',"
			ddl := strings.Replace(database.CurrentSchemaSQL(), target, target+"\n"+tc.column+",", 1)
			if _, err := db.Exec(ddl); err != nil {
				t.Fatal(err)
			}
			if err := checkSchema(context.Background(), db); err == nil {
				t.Fatal("altered historical authority was admitted")
			}
		})
	}
	for _, tc := range []struct{ name, ddl string }{
		{"another-table", "ALTER TABLE users ADD COLUMN post_deploy_script TEXT DEFAULT ''"},
		{"missing-live-column", "ALTER TABLE sites ADD COLUMN post_deploy_script TEXT DEFAULT ''; ALTER TABLE sites DROP COLUMN deploy_script"},
		{"unknown-column", "ALTER TABLE sites ADD COLUMN post_deploy_script TEXT DEFAULT ''; ALTER TABLE sites ADD COLUMN unexpected TEXT"},
		{"trigger", "ALTER TABLE sites ADD COLUMN post_deploy_script TEXT DEFAULT ''; CREATE TRIGGER restore_retired AFTER INSERT ON sites BEGIN UPDATE sites SET deploy_script=post_deploy_script; END"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := fixture(t)
			if _, err := db.Exec(tc.ddl); err != nil {
				t.Fatal(err)
			}
			if err := checkSchema(context.Background(), db); err == nil {
				t.Fatal("unsupported table, column or execution trigger was admitted")
			}
		})
	}
}
