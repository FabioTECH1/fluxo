package database

import (
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/config"
)

func bridgeDB(t *testing.T) {
	t.Helper()
	previous := DB
	if err := InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { DB.Close(); DB = previous })
	if err := config.InitEncryption(t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryConflictKeepsBothOriginalTables(t *testing.T) {
	bridgeDB(t)
	if _, err := DB.Exec(`INSERT INTO databases(id,site_id,engine,name,username,password) VALUES(1,0,'mysql','app','owner','first'); CREATE TABLE databases_migrate AS SELECT * FROM databases; UPDATE databases_migrate SET password='different'`); err != nil {
		t.Fatal(err)
	}
	if err := migrateDatabaseRegistryUniqueness(); err == nil {
		t.Fatal("conflicting conversion succeeded")
	}
	var a, b string
	if err := DB.QueryRow("SELECT password FROM databases WHERE id=1").Scan(&a); err != nil {
		t.Fatal(err)
	}
	if err := DB.QueryRow("SELECT password FROM databases_migrate WHERE id=1").Scan(&b); err != nil {
		t.Fatal(err)
	}
	if a != "first" || b != "different" {
		t.Fatal("original data changed")
	}
}

func TestSecretConversionRollbackAndPATAccountBinding(t *testing.T) {
	bridgeDB(t)
	if _, err := DB.Exec("INSERT INTO users(username,token_hash,github_pat,fluxo_sudo_password) VALUES('admin','hash','new-token','sudo-secret'); INSERT INTO github_accounts(id,name,token) VALUES(1,'existing','other-token'); INSERT INTO sites(id,domain,path,repository,github_account_id) VALUES(1,'one.test','/home/fluxo/one.test','owner/one',1),(2,'two.test','/home/fluxo/two.test','owner/two',0); CREATE TRIGGER refuse_secret BEFORE UPDATE ON github_accounts BEGIN SELECT RAISE(ABORT,'fixture'); END;"); err != nil {
		t.Fatal(err)
	}
	if err := EncryptExistingSecrets(); err == nil {
		t.Fatal("conversion ignored a failed update")
	}
	var value string
	DB.QueryRow("SELECT fluxo_sudo_password FROM users").Scan(&value)
	if value != "sudo-secret" {
		t.Fatal("partial secret conversion committed")
	}
	if _, err := DB.Exec("DROP TRIGGER refuse_secret"); err != nil {
		t.Fatal(err)
	}
	if err := EncryptExistingSecrets(); err != nil {
		t.Fatal(err)
	}
	var first, second int
	if err := DB.QueryRow("SELECT github_account_id FROM sites WHERE id=1").Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := DB.QueryRow("SELECT github_account_id FROM sites WHERE id=2").Scan(&second); err != nil {
		t.Fatal(err)
	}
	if first != 1 || second <= 1 {
		t.Fatal("assigned account overwritten or unbound site lost", first, second)
	}
	DB.QueryRow("SELECT github_pat FROM users").Scan(&value)
	if value != "" {
		t.Fatal("legacy PAT was not cleared")
	}
	DB.QueryRow("SELECT fluxo_sudo_password FROM users").Scan(&value)
	if !strings.HasPrefix(value, "enc:") {
		t.Fatal("secret not encrypted")
	}
	var before int
	DB.QueryRow("SELECT count(*) FROM github_accounts").Scan(&before)
	if err := EncryptExistingSecrets(); err != nil {
		t.Fatal(err)
	}
	var after int
	DB.QueryRow("SELECT count(*) FROM github_accounts").Scan(&after)
	if before != after {
		t.Fatal("retry duplicated an account")
	}
}

func TestOrphanHistoryAndCustomScriptsArePreserved(t *testing.T) {
	bridgeDB(t)
	script := "echo custom\nsudo systemctl reload php$FLUXO_PHP_VERSION-fpm\necho \"Deployment complete.\""
	if _, err := DB.Exec("PRAGMA foreign_keys=OFF; INSERT INTO sites(id,domain,path,deploy_script,deploy_script_mode) VALUES(1,'custom.test','/home/fluxo/custom.test',?,'legacy'); INSERT INTO commands(id,site_id,command,output) VALUES(3,999,'do something','original output'); PRAGMA foreign_keys=ON", script); err != nil {
		t.Fatal(err)
	}
	if err := archiveOrphanedRows("commands"); err != nil {
		t.Fatal(err)
	}
	var payload string
	if err := DB.QueryRow("SELECT payload FROM bridge_orphan_history WHERE source_table='commands' AND source_id=3").Scan(&payload); err != nil || !strings.Contains(payload, "original output") {
		t.Fatal("original orphan data lost", err)
	}
	var count int
	DB.QueryRow("SELECT count(*) FROM commands WHERE id=3").Scan(&count)
	if count != 0 {
		t.Fatal("orphan stayed in current relationship table")
	}
	var actual string
	if err := DB.QueryRow("SELECT deploy_script FROM sites WHERE id=1").Scan(&actual); err != nil || actual != script {
		t.Fatal("custom script changed", err)
	}
	if err := archiveOrphanedRows("commands"); err != nil {
		t.Fatal(err)
	}
	if _, err := DB.Exec("PRAGMA foreign_keys=OFF; INSERT INTO commands(id,site_id,command,output) VALUES(3,999,'do something','conflict'); PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if err := archiveOrphanedRows("commands"); err == nil {
		t.Fatal("conflicting retained original overwritten")
	}
	if err := DB.QueryRow("SELECT output FROM commands WHERE id=3").Scan(&actual); err != nil || actual != "conflict" {
		t.Fatal("conflicting source was lost", err)
	}
}
