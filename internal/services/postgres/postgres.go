package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"fluxo/internal/safeinput"
	"fluxo/internal/syscmd"
)

func CheckConnection() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := syscmd.Run(ctx, 5*time.Second, "sudo", "-u", "postgres", "psql", "-tAc", "SELECT 1"); err != nil {
		return fmt.Errorf("postgres is unavailable: %w", err)
	}
	return nil
}

func runPSQL(ctx context.Context, timeout time.Duration, databaseName, sql string) (string, error) {
	args := []string{"-u", "postgres", "psql", "-v", "ON_ERROR_STOP=1"}
	if databaseName != "" {
		args = append(args, "-d", databaseName)
	}
	return syscmd.RunStdin(ctx, timeout, sql, "sudo", args...)
}

// SyncAdminRole creates the Fluxo PostgreSQL administrator when missing and
// updates it in place otherwise. Updating in place preserves owned objects and grants.
func SyncAdminRole(password string) error {
	ctx := context.Background()
	if password == "" {
		return fmt.Errorf("postgres administrator password is empty")
	}

	out, err := syscmd.Run(ctx, 5*time.Second, "sudo", "-u", "postgres", "psql", "-tAc", "SELECT 1 FROM pg_roles WHERE rolname = 'fluxo'")
	if err != nil {
		return fmt.Errorf("failed to inspect fluxo role: %w", err)
	}

	statement := fmt.Sprintf("CREATE ROLE fluxo WITH LOGIN SUPERUSER PASSWORD '%s';", safeinput.EscapeSQLString(password))
	if out != "" {
		statement = fmt.Sprintf("ALTER ROLE fluxo WITH LOGIN SUPERUSER PASSWORD '%s';", safeinput.EscapeSQLString(password))
	}
	if _, err := runPSQL(ctx, 10*time.Second, "", statement); err != nil {
		return fmt.Errorf("failed to sync fluxo role: %w", err)
	}
	return nil
}

// CreateRole creates a login role and fails if it already exists.
func CreateRole(user, password string) error {
	if !safeinput.ValidateDBIdent(user) || password == "" {
		return fmt.Errorf("invalid database user or password")
	}
	_, err := runPSQL(context.Background(), 10*time.Second, "", fmt.Sprintf(
		"CREATE ROLE \"%s\" WITH LOGIN PASSWORD '%s';",
		safeinput.EscapeSQLString(user), safeinput.EscapeSQLString(password)))
	if err != nil {
		return fmt.Errorf("failed to create role: %w", err)
	}
	return nil
}

// UpdateRolePassword updates a PostgreSQL login role without replacing it.
func UpdateRolePassword(user, password string) error {
	if !safeinput.ValidateDBIdent(user) || password == "" {
		return fmt.Errorf("invalid database user or password")
	}
	_, err := runPSQL(context.Background(), 10*time.Second, "", fmt.Sprintf(
		"ALTER ROLE \"%s\" WITH LOGIN PASSWORD '%s';",
		safeinput.EscapeSQLString(user), safeinput.EscapeSQLString(password)))
	if err != nil {
		return fmt.Errorf("failed to update role password: %w", err)
	}
	return nil
}

// ListDatabaseGrantees returns non-system login roles explicitly represented
// in a database ACL. It is used to repair grants created by older Fluxo releases.
func ListDatabaseGrantees(name string) ([]string, error) {
	if !safeinput.ValidateDBIdent(name) {
		return nil, fmt.Errorf("invalid database name")
	}
	sql := fmt.Sprintf(`SELECT DISTINCT r.rolname
FROM pg_database d
CROSS JOIN LATERAL aclexplode(COALESCE(d.datacl, acldefault('d', d.datdba))) acl
JOIN pg_roles r ON r.oid = acl.grantee
WHERE d.datname = '%s'
  AND acl.privilege_type = 'CONNECT'
  AND NOT has_database_privilege('public', d.datname, 'CONNECT')
  AND r.rolcanlogin
  AND NOT r.rolsuper
  AND r.rolname NOT LIKE 'pg_%%'
ORDER BY r.rolname;`, safeinput.EscapeSQLString(name))
	out, err := runPSQL(context.Background(), 10*time.Second, "", sql)
	if err != nil {
		return nil, fmt.Errorf("failed to list database grantees: %w", err)
	}
	roles := make([]string, 0)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		role := strings.TrimSpace(line)
		if role != "" && safeinput.ValidateDBIdent(role) {
			roles = append(roles, role)
		}
	}
	return roles, nil
}

func databaseOwner(name string) (string, error) {
	out, err := runPSQL(context.Background(), 10*time.Second, "", fmt.Sprintf(
		"SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = '%s';",
		safeinput.EscapeSQLString(name)))
	if err != nil {
		return "", fmt.Errorf("failed to inspect database owner: %w", err)
	}
	owner := strings.TrimSpace(out)
	if !safeinput.ValidateDBIdent(owner) {
		return "", fmt.Errorf("database owner is missing or invalid")
	}
	return owner, nil
}

// GrantDatabaseAccess grants both database-level access and the schema/object
// privileges required to run migrations in PostgreSQL.
func GrantDatabaseAccess(name, user string) error {
	if !safeinput.ValidateDBIdent(name) || !safeinput.ValidateDBIdent(user) {
		return fmt.Errorf("invalid database or user name")
	}
	ctx := context.Background()
	databaseName := name
	name = safeinput.EscapeSQLString(name)
	user = safeinput.EscapeSQLString(user)

	if _, err := runPSQL(ctx, 10*time.Second, "", fmt.Sprintf(
		"REVOKE CONNECT ON DATABASE \"%s\" FROM PUBLIC;\nGRANT ALL PRIVILEGES ON DATABASE \"%s\" TO \"%s\";",
		name, name, user)); err != nil {
		return fmt.Errorf("failed to grant database access: %w", err)
	}

	owner, err := databaseOwner(databaseName)
	if err != nil {
		return err
	}
	grantees, err := ListDatabaseGrantees(databaseName)
	if err != nil {
		return err
	}

	creators := map[string]struct{}{"postgres": {}, owner: {}}
	for _, role := range grantees {
		creators[role] = struct{}{}
	}
	defaultGrants := make([]string, 0, len(creators)*2)
	for creator := range creators {
		if !safeinput.ValidateDBIdent(creator) {
			continue
		}
		creator = safeinput.EscapeSQLString(creator)
		defaultGrants = append(defaultGrants,
			fmt.Sprintf(`ALTER DEFAULT PRIVILEGES FOR ROLE "%s" IN SCHEMA public GRANT ALL PRIVILEGES ON TABLES TO "%s";`, creator, user),
			fmt.Sprintf(`ALTER DEFAULT PRIVILEGES FOR ROLE "%s" IN SCHEMA public GRANT ALL PRIVILEGES ON SEQUENCES TO "%s";`, creator, user),
		)
	}
	for _, grantee := range grantees {
		if grantee == user || !safeinput.ValidateDBIdent(grantee) {
			continue
		}
		grantee = safeinput.EscapeSQLString(grantee)
		defaultGrants = append(defaultGrants,
			fmt.Sprintf(`ALTER DEFAULT PRIVILEGES FOR ROLE "%s" IN SCHEMA public GRANT ALL PRIVILEGES ON TABLES TO "%s";`, user, grantee),
			fmt.Sprintf(`ALTER DEFAULT PRIVILEGES FOR ROLE "%s" IN SCHEMA public GRANT ALL PRIVILEGES ON SEQUENCES TO "%s";`, user, grantee),
		)
	}

	schemaSQL := fmt.Sprintf(`GRANT USAGE, CREATE ON SCHEMA public TO "%[1]s";
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO "%[1]s";
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO "%[1]s";`, user)
	schemaSQL += "\n" + strings.Join(defaultGrants, "\n")
	if _, err := runPSQL(ctx, 10*time.Second, name, schemaSQL); err != nil {
		return fmt.Errorf("failed to grant schema access: %w", err)
	}
	return nil
}

// DropRole removes a login role's ownership and privileges from every
// connectable database before dropping the cluster-wide role.
func DropRole(user string) error {
	if !safeinput.ValidateDBIdent(user) {
		return fmt.Errorf("invalid database user")
	}
	ctx := context.Background()
	escapedUser := safeinput.EscapeSQLString(user)
	out, err := runPSQL(ctx, 10*time.Second, "", fmt.Sprintf(
		"SELECT 1 FROM pg_roles WHERE rolname = '%s';", escapedUser))
	if err != nil {
		return fmt.Errorf("failed to inspect role: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return nil
	}

	out, err = runPSQL(ctx, 10*time.Second, "", "SELECT datname FROM pg_database WHERE datallowconn AND NOT datistemplate ORDER BY datname;")
	if err != nil {
		return fmt.Errorf("failed to list databases: %w", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		db := strings.TrimSpace(line)
		if db == "" {
			continue
		}
		cleanupSQL := fmt.Sprintf("REASSIGN OWNED BY \"%s\" TO postgres;\nDROP OWNED BY \"%s\";", escapedUser, escapedUser)
		if _, err := runPSQL(ctx, 20*time.Second, db, cleanupSQL); err != nil {
			return fmt.Errorf("failed to clean role from database %s: %w", db, err)
		}
	}
	if _, err := runPSQL(ctx, 10*time.Second, "", fmt.Sprintf("DROP ROLE \"%s\";", escapedUser)); err != nil {
		return fmt.Errorf("failed to drop role: %w", err)
	}
	return nil
}

// RevokeDatabaseAccess removes schema/object privileges before revoking access
// to the database itself.
func RevokeDatabaseAccess(name, user string) error {
	if !safeinput.ValidateDBIdent(name) || !safeinput.ValidateDBIdent(user) {
		return fmt.Errorf("invalid database or user name")
	}
	ctx := context.Background()
	databaseName := name
	name = safeinput.EscapeSQLString(name)
	user = safeinput.EscapeSQLString(user)

	owner, err := databaseOwner(databaseName)
	if err != nil {
		return err
	}
	grantees, err := ListDatabaseGrantees(databaseName)
	if err != nil {
		return err
	}
	creators := map[string]struct{}{"postgres": {}, owner: {}}
	for _, role := range grantees {
		creators[role] = struct{}{}
	}
	defaultRevokes := make([]string, 0, len(creators)*2)
	for creator := range creators {
		if !safeinput.ValidateDBIdent(creator) {
			continue
		}
		creator = safeinput.EscapeSQLString(creator)
		defaultRevokes = append(defaultRevokes,
			fmt.Sprintf(`ALTER DEFAULT PRIVILEGES FOR ROLE "%s" IN SCHEMA public REVOKE ALL PRIVILEGES ON TABLES FROM "%s";`, creator, user),
			fmt.Sprintf(`ALTER DEFAULT PRIVILEGES FOR ROLE "%s" IN SCHEMA public REVOKE ALL PRIVILEGES ON SEQUENCES FROM "%s";`, creator, user),
		)
	}
	for _, grantee := range grantees {
		if grantee == user || !safeinput.ValidateDBIdent(grantee) {
			continue
		}
		grantee = safeinput.EscapeSQLString(grantee)
		defaultRevokes = append(defaultRevokes,
			fmt.Sprintf(`ALTER DEFAULT PRIVILEGES FOR ROLE "%s" IN SCHEMA public REVOKE ALL PRIVILEGES ON TABLES FROM "%s";`, user, grantee),
			fmt.Sprintf(`ALTER DEFAULT PRIVILEGES FOR ROLE "%s" IN SCHEMA public REVOKE ALL PRIVILEGES ON SEQUENCES FROM "%s";`, user, grantee),
		)
	}

	schemaSQL := strings.Join(defaultRevokes, "\n") + "\n" + fmt.Sprintf(`REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM "%[1]s";
REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public FROM "%[1]s";
REVOKE USAGE, CREATE ON SCHEMA public FROM "%[1]s";`, user)
	if _, err := runPSQL(ctx, 10*time.Second, name, schemaSQL); err != nil {
		return fmt.Errorf("failed to revoke schema access: %w", err)
	}
	if _, err := runPSQL(ctx, 10*time.Second, "", fmt.Sprintf(
		"REVOKE ALL PRIVILEGES ON DATABASE \"%s\" FROM \"%s\";\nREVOKE CONNECT ON DATABASE \"%s\" FROM \"%s\";",
		name, user, name, user)); err != nil {
		return fmt.Errorf("failed to revoke database access: %w", err)
	}
	return nil
}

// CreateDatabase creates a PostgreSQL role and database, rolling back the role on failure.
// Revokes PUBLIC CONNECT on the new database so only the owner and explicitly granted users can connect.
func CreateDatabase(name, user, password string) error {
	if !safeinput.ValidateDBIdent(name) || !safeinput.ValidateDBIdent(user) {
		return fmt.Errorf("invalid database or user name")
	}

	if err := CreateRole(user, password); err != nil {
		return err
	}

	if err := createDB(name, user); err != nil {
		if cleanupErr := DropRole(user); cleanupErr != nil {
			return fmt.Errorf("%w; additionally failed to remove role: %v", err, cleanupErr)
		}
		return err
	}
	if err := GrantDatabaseAccess(name, user); err != nil {
		if cleanupErr := DeleteDatabase(name); cleanupErr != nil {
			return fmt.Errorf("%w; additionally failed to remove database: %v", err, cleanupErr)
		}
		if cleanupErr := DropRole(user); cleanupErr != nil {
			return fmt.Errorf("%w; additionally failed to remove role: %v", err, cleanupErr)
		}
		return err
	}

	return nil
}

// CreateDatabaseOnly creates a PostgreSQL database owned by the Fluxo administrator.
func CreateDatabaseOnly(name string) error {
	if !safeinput.ValidateDBIdent(name) {
		return fmt.Errorf("invalid database name")
	}
	if err := createDB(name, "fluxo"); err != nil {
		return err
	}
	if err := GrantDatabaseAccess(name, "fluxo"); err != nil {
		if cleanupErr := DeleteDatabase(name); cleanupErr != nil {
			return fmt.Errorf("%w; additionally failed to remove database: %v", err, cleanupErr)
		}
		return err
	}
	return nil
}

func createDB(name, owner string) error {
	ctx := context.Background()
	if !safeinput.ValidateDBIdent(name) || !safeinput.ValidateDBIdent(owner) {
		return fmt.Errorf("invalid database or owner name")
	}
	_, err := runPSQL(ctx, 10*time.Second, "",
		fmt.Sprintf("CREATE DATABASE \"%s\" OWNER \"%s\";\nREVOKE CONNECT ON DATABASE \"%s\" FROM PUBLIC;", safeinput.EscapeSQLString(name), safeinput.EscapeSQLString(owner), safeinput.EscapeSQLString(name)),
	)
	if err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}
	return nil
}

// DeleteDatabase drops a PostgreSQL database without deleting any roles.
func DeleteDatabase(name string) error {
	ctx := context.Background()
	if !safeinput.ValidateDBIdent(name) {
		return fmt.Errorf("invalid database name")
	}

	_, err := runPSQL(ctx, 10*time.Second, "",
		fmt.Sprintf("DROP DATABASE IF EXISTS \"%s\";", safeinput.EscapeSQLString(name)),
	)
	if err != nil {
		return fmt.Errorf("failed to drop database: %w", err)
	}

	return nil
}
