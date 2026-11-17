package database

import (
	"fmt"
	"strings"
)

// Existing columns are the only expected no-op. A failed ALTER or backfill is
// never evidence that an installation has completed bridge preparation.
func applyLegacyStatement(statement string) error {
	fields := strings.Fields(statement)
	if len(fields) >= 6 && fields[0] == "ALTER" && fields[1] == "TABLE" && fields[3] == "ADD" && fields[4] == "COLUMN" {
		rows, err := DB.Query("PRAGMA table_info(" + fields[2] + ")")
		if err != nil {
			return err
		}
		found := false
		for rows.Next() {
			var cid, notNull, pk int
			var name, kind string
			var defaultValue any
			if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
				rows.Close()
				return err
			}
			found = found || name == fields[5]
		}
		err = rows.Err()
		closeErr := rows.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if found {
			return nil
		}
	}
	if _, err := DB.Exec(statement); err != nil {
		return fmt.Errorf("bridge preparation SQL failed (%s): %w", strings.Join(fields[:min(6, len(fields))], " "), err)
	}
	return nil
}

// HasEncryptedSecrets prevents startup from inventing a replacement key for
// an installation whose encrypted stores still depend on the missing key.
func HasEncryptedSecrets() (bool, error) {
	for _, s := range []struct{ table, columns string }{
		{"users", "github_pat,fluxo_mysql_password,fluxo_postgres_password,fluxo_sudo_password,fluxo_db_password,webhook_secret"},
		{"github_accounts", "token"}, {"databases", "password"}, {"backup_destinations", "access_key,secret_key"}, {"backup_plans", "encryption_password"},
	} {
		var predicates []string
		for _, c := range strings.Split(s.columns, ",") {
			predicates = append(predicates, c+" LIKE 'enc:%'")
		}
		var found bool
		if err := DB.QueryRow("SELECT EXISTS(SELECT 1 FROM " + s.table + " WHERE " + strings.Join(predicates, " OR ") + ")").Scan(&found); err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}
