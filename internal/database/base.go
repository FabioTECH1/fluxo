package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"fluxo/internal/config"
)

// InitBaseDB creates the current schema directly on a genuinely absent database.
// An existing database must already satisfy the supported schema and retain its
// original installation identity. Historical conversion stays in InitDB for the
// bridge; base startup never repairs missing tables or invents a replacement ID.
func InitBaseDB(path string) (resultErr error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(absolute)
	fresh := os.IsNotExist(err)
	if err != nil && !fresh {
		return err
	}
	if !fresh && (!info.Mode().IsRegular() || info.Size() == 0) {
		return errors.New("existing base database is missing valid state; restore its original database")
	}
	if fresh {
		// Exclusive creation also distinguishes a concurrent install from a new
		// database. Keep interrupted bytes for recovery rather than deleting them.
		file, err := os.OpenFile(absolute, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err != nil {
			return err
		}
		if err = file.Close(); err != nil {
			return err
		}
	} else {
		readOnly, err := sql.Open("sqlite", baseConnectionString(absolute, "ro"))
		if err != nil {
			return err
		}
		err = validateBaseDatabase(context.Background(), readOnly)
		closeErr := readOnly.Close()
		if err != nil {
			return fmt.Errorf("existing database requires the supported bridge or recovery: %w", err)
		}
		if closeErr != nil {
			return closeErr
		}
	}

	db, err := sql.Open("sqlite", baseConnectionString(absolute, "rw"))
	if err != nil {
		return err
	}
	defer func() {
		if resultErr != nil {
			db.Close()
		}
	}()
	if err = db.Ping(); err != nil {
		return err
	}
	if fresh {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err = tx.Exec(CurrentSchemaSQL()); err != nil {
			return err
		}
		identity := make([]byte, 16)
		if _, err = rand.Read(identity); err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO system_metadata(key,value) VALUES(?,?)", installationIDKey, hex.EncodeToString(identity)); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	if _, err = db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		return err
	}
	previous := DB
	DB = db
	defer func() {
		if resultErr != nil {
			DB = previous
		}
	}()
	return nil
}

func baseConnectionString(path, mode string) string {
	u := url.URL{Scheme: "file", Path: path}
	params := url.Values{}
	params.Set("mode", mode)
	params.Add("_pragma", "foreign_keys(1)")
	params.Add("_pragma", "busy_timeout(5000)")
	u.RawQuery = params.Encode()
	return u.String()
}

func validateBaseDatabase(ctx context.Context, q SchemaQueryer) error {
	if err := ValidateCurrentSchema(ctx, q); err != nil {
		return err
	}
	var id string
	if err := q.QueryRowContext(ctx, "SELECT value FROM system_metadata WHERE key=?", installationIDKey).Scan(&id); err != nil || !validInstallationID(id) {
		return errors.New("original installation identity is missing or invalid")
	}
	var conflicts int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM (
		SELECT lower(trim(domain)) FROM (
			SELECT domain FROM sites UNION ALL SELECT domain FROM domain_aliases
		) WHERE trim(domain)!='' GROUP BY lower(trim(domain)) HAVING count(*)>1
	)`).Scan(&conflicts); err != nil {
		return err
	}
	if conflicts != 0 {
		return errors.New("duplicate domain ownership requires explicit recovery")
	}
	return nil
}

// ValidateBaseSecrets verifies the existing encrypted representation without
// rewriting ciphertext or importing the bridge's legacy settings PAT.
func ValidateBaseSecrets() error {
	tx, err := DB.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var legacyPATs int
	if err = tx.QueryRow("SELECT count(*) FROM users WHERE COALESCE(github_pat,'')!=''").Scan(&legacyPATs); err != nil {
		return err
	}
	if legacyPATs != 0 {
		return errors.New("legacy GitHub settings token requires the supported bridge")
	}
	for _, store := range []struct{ table, columns string }{
		{"users", "fluxo_mysql_password,fluxo_postgres_password,fluxo_sudo_password,fluxo_db_password,webhook_secret"},
		{"github_accounts", "token"}, {"databases", "password"}, {"backup_destinations", "access_key,secret_key"}, {"backup_plans", "encryption_password"},
	} {
		columns := strings.Split(store.columns, ",")
		rows, err := tx.Query("SELECT " + store.columns + " FROM " + store.table)
		if err != nil {
			return err
		}
		for rows.Next() {
			values := make([]sql.NullString, len(columns))
			dest := make([]any, len(columns))
			for i := range dest {
				dest[i] = &values[i]
			}
			if err = rows.Scan(dest...); err != nil {
				rows.Close()
				return err
			}
			for i, value := range values {
				if _, err = config.DecryptSecret(value.String); err != nil {
					rows.Close()
					return fmt.Errorf("stored secret in %s.%s requires bridge conversion or original-key recovery", store.table, columns[i])
				}
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
