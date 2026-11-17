package bridge

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"fluxo/internal/database"
)

const currentBackupPrefixColumn = "prefix TEXT NOT NULL DEFAULT 'fluxo-backups'"

func TestSchemaAcceptsHistoricalBackupDefaultWithoutChangingStoredPaths(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ddl := strings.Replace(database.CurrentSchemaSQL(), currentBackupPrefixColumn, "prefix TEXT NOT NULL DEFAULT 'fluxo'", 1)
	if _, err = db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	// Omit prefix to exercise the actual historical SQL default, not a row
	// whose explicit value only resembles it. Credentials are test sentinels.
	if _, err = db.Exec(`INSERT INTO backup_destinations
		(name, provider, bucket, server_id, access_key, secret_key)
		VALUES ('historical', 's3', 'test-bucket', 'existing-installation', 'test-access', 'test-secret');
		INSERT INTO backup_runs
		(id, plan_id, plan_name, destination_id, destination_name, site_id, site_domain, status, manifest_key)
		VALUES ('historical-run', 1, 'historical', 1, 'historical', 1, 'example.test', 'completed',
		'fluxo/servers/existing-installation/sites/1-example.test/2026/07/historical-run/manifest.json');
		INSERT INTO backup_artifacts (run_id, kind, object_key, filename, sha256)
		VALUES ('historical-run', 'files',
		'fluxo/servers/existing-installation/sites/1-example.test/2026/07/historical-run/files.tar.gz',
		'files.tar.gz', 'test-checksum')`); err != nil {
		t.Fatal(err)
	}
	queries := []string{
		"SELECT sql FROM sqlite_master WHERE name='backup_destinations'",
		"SELECT prefix || '|' || server_id || '|' || access_key || '|' || secret_key FROM backup_destinations WHERE id=1",
		"SELECT manifest_key FROM backup_runs WHERE id='historical-run'",
		"SELECT object_key FROM backup_artifacts WHERE run_id='historical-run'",
	}
	read := func() []string {
		t.Helper()
		var values []string
		for _, query := range queries {
			var value string
			if err := db.QueryRow(query).Scan(&value); err != nil {
				t.Fatal(err)
			}
			values = append(values, value)
		}
		return values
	}
	before := read()
	if !strings.HasPrefix(before[1], "fluxo|") {
		t.Fatal("fixture did not use the historical SQL default")
	}
	if err := checkSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if after := read(); !reflect.DeepEqual(before, after) {
		t.Fatal("schema inspection changed historical DDL, credentials or backup paths")
	}
}

func TestPreparedHistoricalBackupDefaultMatchesBridgeContract(t *testing.T) {
	path := filepath.Join(t.TempDir(), "historical.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Published 0.4.4 used this default. CREATE IF NOT EXISTS in subsequent
	// releases preserves it, including an installation now running 0.4.40.
	const oldTable = `CREATE TABLE backup_destinations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE, provider TEXT NOT NULL, bucket TEXT NOT NULL,
		region TEXT DEFAULT '', account_id TEXT DEFAULT '', jurisdiction TEXT DEFAULT 'default',
		prefix TEXT NOT NULL DEFAULT 'fluxo', server_id TEXT NOT NULL,
		access_key TEXT DEFAULT '', secret_key TEXT DEFAULT '',
		use_instance_role INTEGER NOT NULL DEFAULT 0, is_default INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`
	_, err = legacy.Exec(oldTable)
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
	// Current creation writes the selected prefix explicitly and therefore
	// remains independent of the retained historical SQL default.
	destination := database.BackupDestination{
		Name: "current", Provider: "s3", Bucket: "test-bucket",
		Prefix: "fluxo-backups", ServerID: "existing-installation",
	}
	if err := database.CreateBackupDestination(&destination); err != nil {
		t.Fatal(err)
	}
	var prefix string
	if err := database.DB.QueryRow("SELECT prefix FROM backup_destinations WHERE id=?", destination.ID).Scan(&prefix); err != nil || prefix != destination.Prefix {
		t.Fatal("current explicit prefix was not preserved", prefix, err)
	}
}

func TestHistoricalBackupDefaultDoesNotRelaxOtherSchemaChecks(t *testing.T) {
	for _, tc := range []struct{ name, column string }{
		{"unknown-default", "prefix TEXT NOT NULL DEFAULT 'custom'"},
		{"case-sensitive-default", "prefix TEXT NOT NULL DEFAULT 'FLUXO'"},
		{"expression-default", "prefix TEXT NOT NULL DEFAULT ('flu' || 'xo')"},
		{"nullable-prefix", "prefix TEXT DEFAULT 'fluxo'"},
		{"changed-type", "prefix BLOB NOT NULL DEFAULT 'fluxo'"},
		{"extra-constraint", "prefix TEXT NOT NULL DEFAULT 'fluxo' CHECK (prefix != '')"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(1)
			if _, err := db.Exec(strings.Replace(database.CurrentSchemaSQL(), currentBackupPrefixColumn, tc.column, 1)); err != nil {
				t.Fatal(err)
			}
			if err := checkSchema(context.Background(), db); err == nil {
				t.Fatal("incompatible backup schema was admitted")
			}
		})
	}
	for _, statement := range []string{
		"ALTER TABLE backup_destinations ADD COLUMN hidden_prefix TEXT GENERATED ALWAYS AS (prefix) VIRTUAL",
		"CREATE TRIGGER altered_backup AFTER INSERT ON backup_destinations BEGIN UPDATE backup_destinations SET prefix='elsewhere'; END",
		"CREATE UNIQUE INDEX changed_backup_uniqueness ON backup_destinations(bucket)",
	} {
		t.Run(statement, func(t *testing.T) {
			db := fixture(t)
			if _, err := db.Exec(statement); err != nil {
				t.Fatal(err)
			}
			if err := checkSchema(context.Background(), db); err == nil {
				t.Fatal("unreviewed backup authority was admitted")
			}
		})
	}
}
