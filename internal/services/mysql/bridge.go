package mysql

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"fluxo/internal/safeinput"
)

// BridgeDatabaseReady uses socket administration to inspect the account and
// grants. New database creation intentionally returns its password once instead
// of retaining it in the panel, so an absent stored password is not corruption.
// This function never changes an account, password, database or grant.
func BridgeDatabaseReady(ctx context.Context, name, user string, databaseOnly bool) error {
	if !safeinput.ValidateDBIdent(name) || !safeinput.ValidateDBIdent(user) {
		return errors.New("invalid database identity")
	}
	if systemDatabase(name) {
		return errors.New("system schemas are not application database records")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	db, err := openAdmin()
	if err != nil {
		return err
	}
	defer db.Close()
	var exists int
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=?", name).Scan(&exists); err != nil || exists != 1 {
		return errors.New("verify the existing MySQL database")
	}
	if databaseOnly && user == "fluxo" {
		return nil
	}
	if strings.EqualFold(user, "fluxo") || strings.EqualFold(user, "root") || strings.EqualFold(user, "postgres") {
		return errors.New("hosted database needs a dedicated application account")
	}
	// The exact loopback account is Fluxo's current managed endpoint. Legacy
	// localhost accounts remain admissible when their own catalog is valid.
	host := ""
	for _, candidate := range []string{LocalTCPHost, "localhost"} {
		if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM mysql.user WHERE User=? AND Host=?", user, candidate).Scan(&exists); err != nil {
			return errors.New("cannot inspect MySQL application account")
		}
		if exists == 1 {
			host = candidate
			break
		}
	}
	if host == "" {
		return errors.New("recover the local application account")
	}
	var plugin, auth, expired string
	if err = db.QueryRowContext(ctx, "SELECT plugin,authentication_string,password_expired FROM mysql.user WHERE User=? AND Host=?", user, host).Scan(&plugin, &auth, &expired); err != nil {
		return errors.New("cannot inspect MySQL authentication policy")
	}
	if expired != "N" || !bridgeAuthentication(plugin, auth) {
		return errors.New("verify application password authentication")
	}
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM information_schema.PLUGINS WHERE PLUGIN_NAME=? AND PLUGIN_STATUS='ACTIVE'", plugin).Scan(&exists); err != nil || exists != 1 {
		return errors.New("application authentication plugin is unavailable")
	}
	account := fmt.Sprintf("'%s'@'%s'", safeinput.EscapeSQLString(user), host)
	rows, err := db.QueryContext(ctx, "SHOW CREATE USER "+account)
	if err != nil {
		return errors.New("cannot verify application account locking policy")
	}
	cols, err := rows.Columns()
	if err != nil || len(cols) < 1 || len(cols) > 2 {
		rows.Close()
		return errors.New("unsupported MySQL account policy")
	}
	values := make([]string, len(cols))
	dest := make([]any, len(cols))
	for i := range values {
		dest[i] = &values[i]
	}
	if !rows.Next() {
		rows.Close()
		return errors.New("application account is missing")
	}
	err = rows.Scan(dest...)
	rows.Close()
	if err != nil {
		return errors.New("cannot inspect account policy")
	}
	policy := values[len(values)-1]
	if strings.Contains(policy, " OR ") || (strings.Contains(policy, "REQUIRE ") && !strings.Contains(policy, "REQUIRE NONE")) {
		return errors.New("review custom application authentication requirements")
	}
	if strings.Contains(policy, " ACCOUNT LOCK") || (strings.Contains(policy, "PASSWORD EXPIRE") && !strings.Contains(policy, "PASSWORD EXPIRE NEVER") && !strings.Contains(policy, "PASSWORD EXPIRE DEFAULT")) {
		return errors.New("application password is locked or expires under an unsupported profile")
	}
	if strings.Contains(policy, "PASSWORD EXPIRE DEFAULT") {
		var lifetime int
		if err = db.QueryRowContext(ctx, "SELECT @@default_password_lifetime").Scan(&lifetime); err != nil || lifetime != 0 {
			return errors.New("review application password expiry policy")
		}
	}
	rows, err = db.QueryContext(ctx, "SHOW GRANTS FOR "+account)
	if err != nil {
		return errors.New("cannot inspect application grants")
	}
	statements, err := readStatementRows(rows)
	rows.Close()
	if err != nil {
		return errors.New("cannot read application grants")
	}
	literal, err := literalGrantModeContext(ctx, db)
	if err != nil {
		return err
	}
	return bridgeGrants(name, statements, literal)
}

var nativeAuth = regexp.MustCompile(`^\*[0-9A-Fa-f]{40}$`)

func bridgeAuthentication(plugin, hash string) bool {
	switch plugin {
	case "mysql_native_password":
		return nativeAuth.MatchString(hash)
	case "caching_sha2_password":
		return strings.HasPrefix(hash, "$A$") && len(hash) >= 70 && len(hash) <= 255
	case "sha256_password":
		return strings.HasPrefix(hash, "$5$") && len(hash) >= 40 && len(hash) <= 255
	}
	return false
}
func bridgeGrants(name string, statements []string, literal bool) error {
	access := false
	for _, statement := range statements {
		if strings.Contains(statement, " WITH GRANT OPTION") {
			return errors.New("application grant delegation requires review")
		}
		if strings.HasPrefix(statement, "GRANT USAGE ON *.* TO ") {
			continue
		}
		if !strings.HasPrefix(statement, "GRANT ") || strings.Contains(statement, " WITH GRANT OPTION") {
			return errors.New("review non-database application authority")
		}
		_, tail, ok := strings.Cut(statement, " ON ")
		if !ok {
			return errors.New("review role-based application authority")
		}
		scope, _, ok := strings.Cut(tail, " TO ")
		if !ok || !strings.HasSuffix(scope, "`.*") || !strings.HasPrefix(scope, "`") {
			return errors.New("review global or custom application grants")
		}
		pattern := strings.TrimSuffix(strings.TrimPrefix(scope, "`"), "`.*")
		schema := pattern
		if !literal {
			schema = strings.NewReplacer("\\_", "_", "\\%", "%").Replace(pattern)
		}
		if pattern != grantDatabasePatternForMode(schema, literal) {
			return errors.New("review historical wildcard grants through explicit grant replacement")
		}
		if systemDatabase(schema) {
			return errors.New("application account can access a system schema")
		}
		if !safeinput.ValidateDBIdent(schema) {
			return errors.New("review wildcard application grants")
		}
		if schema == name && strings.HasPrefix(statement, "GRANT ALL PRIVILEGES ON ") {
			access = true
		}
	}
	if !access {
		return errors.New("verify application database-scoped access")
	}
	return nil
}

func systemDatabase(name string) bool {
	switch strings.ToLower(name) {
	case "mysql", "sys", "performance_schema", "information_schema":
		return true
	}
	return false
}
