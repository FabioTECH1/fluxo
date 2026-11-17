package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"fluxo/internal/safeinput"
	"fluxo/internal/syscmd"
)

// BridgeDatabaseReady inspects native catalogs without requiring a second copy
// of one-time application credentials. It never alters roles, ACLs or passwords.
func BridgeDatabaseReady(ctx context.Context, name, user string, databaseOnly bool) error {
	if !safeinput.ValidateDBIdent(name) || !safeinput.ValidateDBIdent(user) {
		return errors.New("invalid database identity")
	}
	switch strings.ToLower(name) {
	case "postgres", "template0", "template1":
		return errors.New("system databases are not application records")
	}
	name, user = safeinput.EscapeSQLString(name), safeinput.EscapeSQLString(user)
	statement := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname='%s' AND datallowconn AND datconnlimit!=0);", name)
	if !(databaseOnly && user == "fluxo") {
		if strings.EqualFold(user, "fluxo") || strings.EqualFold(user, "postgres") || strings.EqualFold(user, "root") {
			return errors.New("hosted database needs a dedicated application account")
		}
		statement = fmt.Sprintf(`SELECT rolcanlogin AND rolconnlimit!=0 AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolreplication AND NOT rolbypassrls
 AND (rolpassword ~ '^md5[0-9a-f]{32}$' OR rolpassword ~ '^SCRAM-SHA-256\$[1-9][0-9]*:[A-Za-z0-9+/]+={0,2}\$[A-Za-z0-9+/]+={0,2}:[A-Za-z0-9+/]+={0,2}$')
 AND (rolvaliduntil IS NULL OR rolvaliduntil>now())
 AND has_database_privilege(rolname,'%s','CONNECT')
 AND EXISTS(SELECT 1 FROM pg_database WHERE datname=current_database() AND datallowconn AND datconnlimit!=0)
 AND has_schema_privilege(rolname,'public','USAGE') AND has_schema_privilege(rolname,'public','CREATE')
 AND NOT EXISTS(SELECT 1 FROM pg_roles elevated WHERE elevated.oid!=account.oid
  AND (elevated.rolsuper OR elevated.rolcreatedb OR elevated.rolcreaterole OR elevated.rolreplication OR elevated.rolbypassrls
   OR (left(elevated.rolname,3)='pg_' AND elevated.rolname!='pg_database_owner'))
  AND pg_has_role(account.oid,elevated.oid,'MEMBER'))
 FROM pg_authid account WHERE rolname='%s';`, name, user)
	}
	// These are applied after sudo's environment reset. Database owners can set
	// per-database search_path; never resolve catalog functions through public.
	out, err := syscmd.Run(ctx, 5*time.Second,
		"/usr/bin/sudo", "-u", "postgres", "/usr/bin/env", "--ignore-environment", "--chdir=/",
		"PATH=/usr/bin:/bin", "LC_ALL=C",
		"PGOPTIONS=-c search_path=pg_catalog -c default_transaction_read_only=on",
		"PGPASSFILE=/dev/null", "PGCONNECT_TIMEOUT=5",
		"/usr/bin/psql", "-X", "--no-password", "-h", "/var/run/postgresql", "-p", "5432", "-U", "postgres", "-d", name, "-v", "ON_ERROR_STOP=1", "-tAc", statement)
	if err != nil {
		return fmt.Errorf("inspect PostgreSQL catalog: %w", err)
	}
	if strings.TrimSpace(out) != "t" {
		return errors.New("verify PostgreSQL database, login and nonprivileged role authority")
	}
	return nil
}
