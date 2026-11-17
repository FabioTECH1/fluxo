package bridge

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/config"
	"fluxo/internal/database"
	"golang.org/x/crypto/bcrypt"
)

func fixture(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err = db.Exec(database.CurrentSchemaSQL()); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestServiceContextMatchesInstalledDataAndTransport(t *testing.T) {
	base := map[string]string{"LoadState": "loaded", "NeedDaemonReload": "no", "FragmentPath": "/etc/systemd/system/fluxo.service", "ProtectHome": "no", "ProtectSystem": "no", "PrivateUsers": "no", "DynamicUser": "no", "ExecStart": "{ path=/usr/local/bin/fluxo ; argv[]=/usr/local/bin/fluxo ; ignore_errors=no ; }", "Environment": `FLUXO_ENV=prod "FLUXO_PORT=9595"`}
	if err := serviceContext(base, "/var/lib/fluxo", false); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"EnvironmentFiles", "PassEnvironment", "UnsetEnvironment"} {
		p := map[string]string{"LoadState": "loaded", "NeedDaemonReload": "no", "FragmentPath": "/etc/systemd/system/fluxo.service", "ProtectHome": "no", "ProtectSystem": "no", "PrivateUsers": "no", "DynamicUser": "no", "ExecStart": "{ path=/usr/local/bin/fluxo ; argv[]=/usr/local/bin/fluxo ; ignore_errors=no ; }", "Environment": base["Environment"], field: "custom"}
		if serviceContext(p, "/var/lib/fluxo", false) == nil {
			t.Fatalf("accepted ambiguous %s", field)
		}
	}
	for _, env := range []string{`FLUXO_ENV=prod FLUXO_USE_HTTP=1`, `FLUXO_ENV=prod FLUXO_DATA_DIR=/other`, `FLUXO_ENV=dev`, `"FLUXO_ENV=prod`, `FLUXO_ENV='prod'`} {
		if serviceContext(map[string]string{"LoadState": "loaded", "NeedDaemonReload": "no", "FragmentPath": "/etc/systemd/system/fluxo.service", "ProtectHome": "no", "ProtectSystem": "no", "PrivateUsers": "no", "DynamicUser": "no", "ExecStart": "{ path=/usr/local/bin/fluxo ; argv[]=/usr/local/bin/fluxo ; ignore_errors=no ; }", "Environment": env}, "/var/lib/fluxo", false) == nil {
			t.Fatalf("accepted mismatched or unsupported environment %q", env)
		}
	}
	custom := map[string]string{"LoadState": "loaded", "NeedDaemonReload": "no", "FragmentPath": "/etc/systemd/system/fluxo.service", "ProtectHome": "no", "ProtectSystem": "no", "PrivateUsers": "no", "DynamicUser": "no", "ExecStart": "{ path=/usr/local/bin/fluxo ; argv[]=/usr/local/bin/fluxo ; ignore_errors=no ; }", "User": "root", "Environment": `FLUXO_ENV=prod "FLUXO_DATA_DIR=/root/custom directory" FLUXO_USE_HTTP=1`}
	if err := serviceContext(custom, "/root/custom directory", true); err != nil {
		t.Fatal(err)
	}
	custom["User"] = "fluxo"
	if serviceContext(custom, "/root/custom directory", true) == nil {
		t.Fatal("accepted non-root service")
	}
}

func TestCanonicalSchemaNeedsNoHistoricalPreparation(t *testing.T) {
	db := fixture(t)
	if err := checkSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"SELECT github_deploy_key_id,github_webhook_id,github_webhook_url,github_account_id FROM sites LIMIT 0", "SELECT branch,trigger_source,target_commit_hash FROM deployments LIMIT 0", "SELECT webhook_secret,token_version FROM users LIMIT 0", "SELECT username,ip_address FROM activity LIMIT 0", "SELECT expires_at FROM certificates LIMIT 0"} {
		rows, err := db.Query(q)
		if err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
}

func TestSchemaRejectsLostColumnsAndWeakenedConstraints(t *testing.T) {
	for _, tc := range []struct{ name, sql string }{{"column", "ALTER TABLE deployments DROP COLUMN branch"}, {"port_index", "DROP INDEX idx_sites_app_port"}, {"domain_trigger", "DROP TRIGGER prevent_panel_domain_conflict"}, {"extra_name_uniqueness", "CREATE UNIQUE INDEX old_name ON databases(name)"}, {"changed_port_filter", "DROP INDEX idx_sites_app_port; CREATE UNIQUE INDEX idx_sites_app_port ON sites(app_port) WHERE app_port>=0"}} {
		t.Run(tc.name, func(t *testing.T) {
			db := fixture(t)
			if _, err := db.Exec(tc.sql); err != nil {
				t.Fatal(err)
			}
			if err := checkSchema(context.Background(), db); err == nil {
				t.Fatal("incompatible schema was admitted")
			}
		})
	}
}

func TestSchemaRejectsChecksAllocationCollationsAndForeignTriggers(t *testing.T) {
	for _, tc := range []struct{ name, old, new string }{
		{"check", "id INTEGER PRIMARY KEY CHECK (id = 1)", "id INTEGER PRIMARY KEY"},
		{"allocation", "id INTEGER PRIMARY KEY AUTOINCREMENT", "id INTEGER PRIMARY KEY"},
		{"collation", "username TEXT NOT NULL UNIQUE", "username TEXT NOT NULL COLLATE NOCASE UNIQUE"},
		{"literal_case", "status IN ('queued', 'running')", "status IN ('QUEUED', 'RUNNING')"},
		{"replace_conflict", "username TEXT NOT NULL UNIQUE", "username TEXT NOT NULL UNIQUE ON CONFLICT REPLACE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sqlText := database.CurrentSchemaSQL()
			if !strings.Contains(sqlText, tc.old) {
				t.Fatal("fixture target absent")
			}
			db, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(1)
			if _, err = db.Exec(strings.Replace(sqlText, tc.old, tc.new, 1)); err != nil {
				t.Fatal(err)
			}
			if err = checkSchema(context.Background(), db); err == nil {
				t.Fatal("incompatible semantics admitted")
			}
		})
	}
	db := fixture(t)
	if _, err := db.Exec("CREATE TRIGGER foreign_effect AFTER INSERT ON activity BEGIN UPDATE users SET credentials_copied=1; END"); err != nil {
		t.Fatal(err)
	}
	if err := checkSchema(context.Background(), db); err == nil {
		t.Fatal("unknown trigger admitted")
	}
	db = fixture(t)
	if _, err := db.Exec("ALTER TABLE users ADD COLUMN shadow TEXT GENERATED ALWAYS AS (username) VIRTUAL"); err != nil {
		t.Fatal(err)
	}
	if err := checkSchema(context.Background(), db); err == nil {
		t.Fatal("hidden generated column admitted")
	}
}

func TestPreparedDatabaseMatchesFreshContract(t *testing.T) {
	previous := database.DB
	if err := database.InitDB(filepath.Join(t.TempDir(), "prepared.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.DB.Close(); database.DB = previous })
	if err := checkSchema(context.Background(), database.DB); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateSnapshotIncludesWALWithoutTouchingSources(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "fluxo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec("PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE proof(value TEXT); INSERT INTO proof VALUES('committed-in-wal')"); err != nil {
		t.Fatal(err)
	}
	var sources []snapshotSource
	before := map[string]string{}
	for _, name := range []string{"fluxo.db", "fluxo.db-wal"} {
		path := filepath.Join(dir, name)
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, snapshotSource{path, f, info})
	}
	for _, name := range []string{"fluxo.db", "fluxo.db-wal", "fluxo.db-shm"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		before[name] = digest(b)
	}
	path, cleanup, err := copySnapshot(context.Background(), dir, sources)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	copied, err := openSQLite(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer copied.Close()
	var value string
	if err = copied.QueryRow("SELECT value FROM proof").Scan(&value); err != nil || value != "committed-in-wal" {
		t.Fatal("WAL commit was not observed", value, err)
	}
	for name, want := range before {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || digest(b) != want {
			t.Fatal("source was modified", name, err)
		}
	}
}

func TestTerminalOperationsDoNotDischargeHolds(t *testing.T) {
	db := fixture(t)
	ctx := context.Background()
	if err := checkPending(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO node_update_operations VALUES('op','apply','','request','succeeded',1,'{}'); INSERT INTO node_update_generations VALUES('gen','op','succeeded','{}'); INSERT INTO node_update_barriers VALUES('unit','gen')"); err != nil {
		t.Fatal(err)
	}
	if err := checkPending(ctx, db); err == nil {
		t.Fatal("terminal operation bypassed a retained barrier")
	}
	if _, err := db.Exec("DELETE FROM node_update_barriers"); err != nil {
		t.Fatal(err)
	}
	if err := checkPending(ctx, db); err != nil {
		t.Fatal(err)
	}
}

func TestSecretsNeedAuthenticatedCiphertextAndConfiguredAccount(t *testing.T) {
	db := fixture(t)
	dir := t.TempDir()
	if err := config.InitEncryption(dir); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(dir, "encryption.key"))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("fixture-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO users(username,token_hash) VALUES('admin',?)", string(hash)); err != nil {
		t.Fatal(err)
	}
	value, err := config.EncryptSecret("fixture-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO github_accounts(name,token) VALUES('test',?)", value); err != nil {
		t.Fatal(err)
	}
	if err = checkSecrets(context.Background(), db, key); err != nil {
		t.Fatal(err)
	}
	wrong := append([]byte{}, key...)
	wrong[0] ^= 1
	if err = checkSecrets(context.Background(), db, wrong); err == nil || strings.Contains(err.Error(), "fixture-secret") {
		t.Fatal("wrong key admitted or secret exposed")
	}
	if _, err = db.Exec("UPDATE github_accounts SET token='plaintext'"); err != nil {
		t.Fatal(err)
	}
	if err = checkSecrets(context.Background(), db, key); err == nil {
		t.Fatal("plaintext admitted")
	}
	if _, err = db.Exec("DELETE FROM github_accounts; UPDATE users SET token_hash=?", strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if err = checkSecrets(context.Background(), db, key); err == nil {
		t.Fatal("legacy password admitted")
	}
}

func TestInspectionDoesNotCreateMissingState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	_, _ = Inspect(context.Background(), dir)
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatal("inspection created persistent state", err)
	}
}

func TestOldPATAndUnfinishedConversionCannotBeCertified(t *testing.T) {
	db := fixture(t)
	if _, err := db.Exec("INSERT INTO users(username,token_hash,github_pat) VALUES('admin','hash','old-pat')"); err != nil {
		t.Fatal(err)
	}
	if err := checkConversions(context.Background(), db); err == nil {
		t.Fatal("legacy PAT writer was ignored")
	}
	if _, err := db.Exec("UPDATE users SET github_pat=''; CREATE TABLE databases_next(id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if err := checkConversions(context.Background(), db); err == nil {
		t.Fatal("unfinished conversion was ignored")
	}
}
