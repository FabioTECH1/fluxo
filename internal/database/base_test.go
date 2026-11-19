package database

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"fluxo/internal/config"
)

func baseDB(t *testing.T) string {
	t.Helper()
	previous := DB
	path := filepath.Join(t.TempDir(), "fluxo.db")
	if err := InitBaseDB(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		DB.Close()
		DB = previous
	})
	return path
}

func TestBaseFreshSchemaAndOrdinaryReopenPreserveIdentityAndHistory(t *testing.T) {
	path := baseDB(t)
	id, err := InstallationID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DB.Exec(`
		INSERT INTO users(id,username,token_hash,github_pat) VALUES(7,'admin','unchanged-hash','unchanged-token');
		INSERT INTO sites(id,domain,path,deploy_script,deploy_script_mode) VALUES(42,'base.test','/home/fluxo/base.test','echo user-owned','legacy');
		INSERT INTO commands(id,site_id,command,output) VALUES(51,42,'echo retained','historical output');
		INSERT INTO bridge_orphan_history(source_table,source_id,source_schema,payload) VALUES('commands',91,'saved original DDL','saved original data');
	`); err != nil {
		t.Fatal(err)
	}
	if err = DB.Close(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = InitBaseDB(path); err != nil {
			t.Fatal(err)
		}
		got, err := InstallationID()
		if err != nil || got != id {
			t.Fatal("installation identity changed", got, err)
		}
		var hash, token, script, history, archive string
		if err = DB.QueryRow(`SELECT token_hash,github_pat FROM users WHERE id=7`).Scan(&hash, &token); err != nil {
			t.Fatal(err)
		}
		if err = DB.QueryRow(`SELECT deploy_script FROM sites WHERE id=42`).Scan(&script); err != nil {
			t.Fatal(err)
		}
		if err = DB.QueryRow(`SELECT output FROM commands WHERE id=51 AND site_id=42`).Scan(&history); err != nil {
			t.Fatal(err)
		}
		if err = DB.QueryRow(`SELECT payload FROM bridge_orphan_history WHERE source_id=91`).Scan(&archive); err != nil {
			t.Fatal(err)
		}
		if hash != "unchanged-hash" || token != "unchanged-token" || script != "echo user-owned" || history != "historical output" || archive != "saved original data" {
			t.Fatal("base startup performed historical conversion or changed user history")
		}
		DB.Close()
	}
}

func TestBaseRefusesDamagedOrLegacyDatabaseWithoutRepair(t *testing.T) {
	for _, change := range []string{
		"DROP TABLE commands",
		"DROP INDEX idx_sites_app_port",
		"ALTER TABLE deployments DROP COLUMN branch",
		"DELETE FROM system_metadata WHERE key='installation_id'",
		"UPDATE system_metadata SET value='damaged' WHERE key='installation_id'",
	} {
		t.Run(change, func(t *testing.T) {
			path := baseDB(t)
			if _, err := DB.Exec(change); err != nil {
				t.Fatal(err)
			}
			DB.Close()
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err = InitBaseDB(path); err == nil {
				t.Fatal("damaged database was admitted")
			}
			after, err := os.ReadFile(path)
			if err != nil || sha256.Sum256(before) != sha256.Sum256(after) {
				t.Fatal("refusal rewrote original database", err)
			}
		})
	}
}

func TestBaseRefusesExistingEmptyCorruptAndSymlinkFiles(t *testing.T) {
	for _, contents := range []string{"", "not a SQLite database"} {
		path := filepath.Join(t.TempDir(), "fluxo.db")
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		if err := InitBaseDB(path); err == nil {
			t.Fatal("invalid database admitted")
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != contents {
			t.Fatal("invalid database replaced", err)
		}
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "original")
	if err := os.WriteFile(target, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "fluxo.db")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := InitBaseDB(link); err == nil {
		t.Fatal("symlink database admitted")
	}
}

func TestBaseAcceptsCompletedBridgeSchemaWithoutReplayingConversions(t *testing.T) {
	previous := DB
	t.Cleanup(func() { DB.Close(); DB = previous })
	path := filepath.Join(t.TempDir(), "fluxo.db")
	if err := InitDB(path); err != nil {
		t.Fatal(err)
	}
	id, err := InstallationID()
	if err != nil {
		t.Fatal(err)
	}
	DB.Close()
	if err = InitBaseDB(path); err != nil {
		t.Fatal(err)
	}
	got, err := InstallationID()
	if err != nil || got != id {
		t.Fatal("bridge identity changed", got, err)
	}
}

func TestBaseSecretsValidateWithoutConversionOrCiphertextChanges(t *testing.T) {
	baseDB(t)
	if err := config.InitEncryption(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	secret, err := config.EncryptSecret("preserved credential")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DB.Exec("INSERT INTO users(username,token_hash,fluxo_sudo_password) VALUES('admin','hash',?)", secret); err != nil {
		t.Fatal(err)
	}
	if err = ValidateBaseSecrets(); err != nil {
		t.Fatal(err)
	}
	var got string
	if err = DB.QueryRow("SELECT fluxo_sudo_password FROM users").Scan(&got); err != nil || got != secret {
		t.Fatal("validation rewrote original ciphertext", err)
	}
	for _, bad := range []string{"plaintext legacy secret", "enc:damaged"} {
		if _, err = DB.Exec("UPDATE users SET fluxo_sudo_password=?", bad); err != nil {
			t.Fatal(err)
		}
		if err = ValidateBaseSecrets(); err == nil {
			t.Fatal("invalid secret admitted")
		}
		if err = DB.QueryRow("SELECT fluxo_sudo_password FROM users").Scan(&got); err != nil || got != bad {
			t.Fatal("refusal converted original value", err)
		}
	}
	if _, err = DB.Exec("UPDATE users SET fluxo_sudo_password=?,github_pat=?", secret, secret); err != nil {
		t.Fatal(err)
	}
	if err = ValidateBaseSecrets(); err == nil {
		t.Fatal("unconverted legacy PAT admitted")
	}
	var accounts int
	if err = DB.QueryRow("SELECT count(*) FROM github_accounts").Scan(&accounts); err != nil || accounts != 0 {
		t.Fatal("validation imported PAT", err)
	}
}
