package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"fluxo/internal/safeinput"

	driver "github.com/go-sql-driver/mysql"
)

const LocalTCPHost = "127.0.0.1"

var ErrUserExists = errors.New("database user already exists")
var ErrUnmanagedUser = errors.New("database user is not managed by Fluxo")

func adminConfig() *driver.Config {
	config := driver.NewConfig()
	config.User = "root"
	config.Net = "unix"
	config.Addr = "/var/run/mysqld/mysqld.sock"
	// Account-management statements cannot use server-side placeholders on
	// supported MariaDB releases. Driver interpolation still binds values and
	// escapes them according to the negotiated NO_BACKSLASH_ESCAPES status.
	config.InterpolateParams = true
	return config
}

func openAdmin() (*sql.DB, error) {
	db, err := sql.Open("mysql", adminConfig().FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("failed to connect to mysql: %w", err)
	}
	return db, nil
}

func CheckConnection() error {
	db, err := openAdmin()
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("mysql is unavailable: %w", err)
	}
	return nil
}

func LocalAccountExists(user, host string) (bool, error) {
	if !safeinput.ValidateDBIdent(user) || (host != LocalTCPHost && host != "localhost") {
		return false, fmt.Errorf("invalid local database account")
	}
	db, err := openAdmin()
	if err != nil {
		return false, err
	}
	defer db.Close()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM mysql.user WHERE User = ? AND Host = ?", user, host).Scan(&count); err != nil {
		return false, fmt.Errorf("inspect local database account: %w", err)
	}
	return count > 0, nil
}

// SyncAdminUser keeps socket access for local administration while adding the
// exact loopback TCP account used by generated application and phpMyAdmin
// configuration. No wildcard host account is created.
func SyncAdminUser(password string) error {
	if password == "" || safeinput.HasControlChars(password) {
		return fmt.Errorf("invalid MySQL administrator password")
	}
	db, err := openAdmin()
	if err != nil {
		return err
	}
	defer db.Close()

	for _, host := range []string{"localhost", LocalTCPHost} {
		account := fmt.Sprintf("'fluxo'@'%s'", host)
		if _, err := db.Exec("CREATE USER IF NOT EXISTS "+account+" IDENTIFIED BY ?", password); err != nil {
			return fmt.Errorf("failed to create fluxo MySQL account for %s: %w", host, err)
		}
		if _, err := db.Exec("ALTER USER "+account+" IDENTIFIED BY ?", password); err != nil {
			return fmt.Errorf("failed to update fluxo MySQL account for %s: %w", host, err)
		}
		if _, err := db.Exec("GRANT ALL PRIVILEGES ON *.* TO " + account + " WITH GRANT OPTION"); err != nil {
			return fmt.Errorf("failed to grant fluxo MySQL privileges for %s: %w", host, err)
		}
	}
	return nil
}

func isUserExistsError(err error) bool {
	var mysqlErr *driver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1396
}

// CreateUser creates a local-only TCP account. It deliberately fails when the
// account already exists so a caller-provided password is never accepted while
// MySQL silently keeps a different existing password.
func CreateUser(user, password string) error {
	if !safeinput.ValidateDBIdent(user) || password == "" || safeinput.HasControlChars(password) {
		return fmt.Errorf("invalid database user or password")
	}
	db, err := openAdmin()
	if err != nil {
		return err
	}
	defer db.Close()
	var localAccounts int
	if err := db.QueryRow("SELECT COUNT(*) FROM mysql.user WHERE User = ? AND Host IN (?, 'localhost')", user, LocalTCPHost).Scan(&localAccounts); err != nil {
		return fmt.Errorf("failed to inspect existing local database accounts: %w", err)
	}
	if localAccounts > 0 {
		return ErrUserExists
	}

	statement := fmt.Sprintf("CREATE USER '%s'@'%s' IDENTIFIED BY ?",
		safeinput.EscapeSQLString(user), LocalTCPHost)
	if _, err := db.Exec(statement, password); err != nil {
		if isUserExistsError(err) {
			return ErrUserExists
		}
		return fmt.Errorf("failed to create user: %w", err)
	}
	return nil
}

func DropUser(user string) error {
	if !safeinput.ValidateDBIdent(user) {
		return fmt.Errorf("invalid database user")
	}
	db, err := openAdmin()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(fmt.Sprintf("DROP USER IF EXISTS '%s'@'%s'", safeinput.EscapeSQLString(user), LocalTCPHost))
	if err != nil {
		return fmt.Errorf("failed to drop user: %w", err)
	}
	return nil
}

// DropManagedLocalUser removes only the exact TCP account Fluxo creates or
// repairs. Socket and remote accounts with the same username are preserved.
func DropManagedLocalUser(user string) error {
	if !safeinput.ValidateDBIdent(user) {
		return fmt.Errorf("invalid database user")
	}
	db, err := openAdmin()
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(fmt.Sprintf("DROP USER IF EXISTS '%s'@'%s'", safeinput.EscapeSQLString(user), LocalTCPHost)); err != nil {
		return fmt.Errorf("failed to drop managed TCP user account: %w", err)
	}
	return nil
}

// UpdateLocalUserPassword rotates only the exact TCP account Fluxo manages.
// Password data is bound through the driver's mode-aware interpolation.
func UpdateLocalUserPassword(user, password string) error {
	if !safeinput.ValidateDBIdent(user) || password == "" || safeinput.HasControlChars(password) {
		return fmt.Errorf("invalid database user or password")
	}
	db, err := openAdmin()
	if err != nil {
		return err
	}
	defer db.Close()

	var exists int
	if err := db.QueryRow("SELECT COUNT(*) FROM mysql.user WHERE User = ? AND Host = ?", user, LocalTCPHost).Scan(&exists); err != nil {
		return fmt.Errorf("failed to inspect managed TCP database account: %w", err)
	}
	if exists == 0 {
		return fmt.Errorf("database user not found")
	}
	account := fmt.Sprintf("'%s'@'%s'", safeinput.EscapeSQLString(user), LocalTCPHost)
	if _, err := db.Exec("ALTER USER "+account+" IDENTIFIED BY ?", password); err != nil {
		return fmt.Errorf("failed to rotate managed TCP database account: %w", err)
	}
	return nil
}

func GrantDatabaseAccess(name, user string) error {
	if !safeinput.ValidateDBIdent(name) || !safeinput.ValidateDBIdent(user) {
		return fmt.Errorf("invalid database or user name")
	}
	db, err := openAdmin()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%s'",
		name, safeinput.EscapeSQLString(user), LocalTCPHost))
	if err != nil {
		return fmt.Errorf("failed to grant privileges: %w", err)
	}
	return nil
}

func databaseGrants(statements []string) map[string]struct{} {
	grants := make(map[string]struct{})
	const prefix = "GRANT ALL PRIVILEGES ON `"
	for _, statement := range statements {
		start := strings.Index(statement, prefix)
		if start < 0 {
			continue
		}
		remainder := statement[start+len(prefix):]
		end := strings.Index(remainder, "`.*")
		if end > 0 {
			grants[remainder[:end]] = struct{}{}
		}
	}
	return grants
}

// ReplaceDatabaseAccess updates only database-scoped grants for Fluxo's exact
// TCP account. Additions happen before removals so a transient failure cannot
// first take an application offline; the operation is safe to retry.
func ReplaceDatabaseAccess(user string, databaseNames []string) error {
	if !safeinput.ValidateDBIdent(user) {
		return fmt.Errorf("invalid database user")
	}
	desired := make(map[string]struct{}, len(databaseNames))
	for _, name := range databaseNames {
		if !safeinput.ValidateDBIdent(name) {
			return fmt.Errorf("invalid database name")
		}
		desired[name] = struct{}{}
	}
	db, err := openAdmin()
	if err != nil {
		return err
	}
	defer db.Close()

	escapedUser := safeinput.EscapeSQLString(user)
	rows, err := db.Query(fmt.Sprintf("SHOW GRANTS FOR '%s'@'%s'", escapedUser, LocalTCPHost))
	if err != nil {
		return fmt.Errorf("read current database grants: %w", err)
	}
	statements, readErr := readStatementRows(rows)
	closeErr := rows.Close()
	if readErr != nil {
		return fmt.Errorf("read current database grants: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close current database grants: %w", closeErr)
	}
	current := databaseGrants(statements)
	for name := range desired {
		if _, exists := current[name]; exists {
			continue
		}
		if _, err := db.Exec(fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%s'", name, escapedUser, LocalTCPHost)); err != nil {
			return fmt.Errorf("grant database %s: %w", name, err)
		}
	}
	for name := range current {
		if _, keep := desired[name]; keep {
			continue
		}
		if _, err := db.Exec(fmt.Sprintf("REVOKE ALL PRIVILEGES ON `%s`.* FROM '%s'@'%s'", name, escapedUser, LocalTCPHost)); err != nil {
			return fmt.Errorf("revoke database %s: %w", name, err)
		}
	}
	return nil
}

// CreateDatabase creates a database, user, and grants all privileges.
func CreateDatabase(name, user, password string) error {
	if !safeinput.ValidateDBIdent(name) || !safeinput.ValidateDBIdent(user) {
		return fmt.Errorf("invalid database or user name")
	}
	if err := CreateDatabaseOnly(name); err != nil {
		return err
	}
	if err := CreateUser(user, password); err != nil {
		if cleanupErr := DeleteDatabase(name); cleanupErr != nil {
			return errors.Join(err, fmt.Errorf("failed to remove database during rollback: %w", cleanupErr))
		}
		return err
	}
	if err := GrantDatabaseAccess(name, user); err != nil {
		cleanupErr := errors.Join(DropUser(user), DeleteDatabase(name))
		if cleanupErr != nil {
			return errors.Join(err, fmt.Errorf("database rollback was incomplete: %w", cleanupErr))
		}
		return err
	}
	return nil
}

// CreateDatabaseOnly creates a database without creating a user.
func CreateDatabaseOnly(name string) error {
	if !safeinput.ValidateDBIdent(name) {
		return fmt.Errorf("invalid database name")
	}
	db, err := openAdmin()
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.Exec(fmt.Sprintf("CREATE DATABASE `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", name)); err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}

	return nil
}

// DeleteDatabase drops a database without deleting any database users.
func DeleteDatabase(name string) error {
	if !safeinput.ValidateDBIdent(name) {
		return fmt.Errorf("invalid database name")
	}
	db, err := openAdmin()
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", name)); err != nil {
		return fmt.Errorf("failed to drop database: %w", err)
	}

	return nil
}

// VerifyDatabaseAccess checks the same TCP path written into generated site
// configuration. This prevents provisioning an application with credentials
// that authenticate successfully only over a different MySQL account host.
func VerifyDatabaseAccess(name, user, password string) error {
	if !safeinput.ValidateDBIdent(name) || !safeinput.ValidateDBIdent(user) || password == "" || safeinput.HasControlChars(password) {
		return fmt.Errorf("invalid database credentials")
	}
	config := driver.NewConfig()
	config.User = user
	config.Passwd = password
	config.Net = "tcp"
	config.Addr = LocalTCPHost + ":3306"
	config.DBName = name
	config.Timeout = 5 * time.Second
	config.ReadTimeout = 5 * time.Second
	config.WriteTimeout = 5 * time.Second

	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		return fmt.Errorf("database credentials are unavailable: %w", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("database credentials cannot access the selected database: %w", err)
	}
	return nil
}

func readStatementRows(rows *sql.Rows) ([]string, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	statements := make([]string, 0)
	for rows.Next() {
		values := make([]sql.RawBytes, len(columns))
		destinations := make([]any, len(columns))
		for i := range values {
			destinations[i] = &values[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, err
		}
		if len(values) > 0 {
			statements = append(statements, string(values[len(values)-1]))
		}
	}
	return statements, rows.Err()
}

func readSingleStatement(db *sql.DB, query string) (string, error) {
	rows, err := db.Query(query)
	if err != nil {
		return "", err
	}
	statements, readErr := readStatementRows(rows)
	closeErr := rows.Close()
	if readErr != nil {
		return "", readErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if len(statements) != 1 {
		return "", fmt.Errorf("unexpected statement response")
	}
	return statements[0], nil
}

func rewriteAccountHost(statement, user, fromHost, toHost string) (string, bool) {
	patterns := [][2]string{
		{fmt.Sprintf("'%s'@'%s'", user, fromHost), fmt.Sprintf("'%s'@'%s'", user, toHost)},
		{fmt.Sprintf("`%s`@`%s`", user, fromHost), fmt.Sprintf("`%s`@`%s`", user, toHost)},
	}
	for _, pattern := range patterns {
		if strings.Contains(statement, pattern[0]) {
			return strings.ReplaceAll(statement, pattern[0], pattern[1]), true
		}
	}
	return statement, false
}

// EnsureTCPAccountFromLocalhost clones an existing socket-only account's
// authentication definition and grants only the database names supplied by
// Fluxo. The original socket account and any unrelated grants remain untouched.
// existingTargetOwned must be true before an existing TCP identity is mutated.
func EnsureTCPAccountFromLocalhost(user string, databaseNames []string, existingTargetOwned bool) error {
	if !safeinput.ValidateDBIdent(user) || user == "root" {
		return fmt.Errorf("invalid managed database user")
	}
	seenDatabases := make(map[string]struct{}, len(databaseNames))
	for _, name := range databaseNames {
		if !safeinput.ValidateDBIdent(name) {
			return fmt.Errorf("invalid managed database name")
		}
		seenDatabases[name] = struct{}{}
	}
	db, err := openAdmin()
	if err != nil {
		return err
	}
	defer db.Close()

	var sourceExists int
	if err := db.QueryRow("SELECT COUNT(*) FROM mysql.user WHERE User = ? AND Host = 'localhost'", user).Scan(&sourceExists); err != nil {
		return fmt.Errorf("inspect socket database account: %w", err)
	}
	if sourceExists == 0 {
		return fmt.Errorf("socket database account not found")
	}

	escapedUser := safeinput.EscapeSQLString(user)
	sourceCreate, err := readSingleStatement(db, fmt.Sprintf("SHOW CREATE USER '%s'@'localhost'", escapedUser))
	if err != nil {
		return fmt.Errorf("read socket database account: %w", err)
	}
	createStatement, ok := rewriteAccountHost(sourceCreate, user, "localhost", LocalTCPHost)
	if !ok {
		return fmt.Errorf("read socket database account: unrecognized account format")
	}

	var targetExists int
	if err := db.QueryRow("SELECT COUNT(*) FROM mysql.user WHERE User = ? AND Host = ?", user, LocalTCPHost).Scan(&targetExists); err != nil {
		return fmt.Errorf("inspect TCP database account: %w", err)
	}
	createdTarget := false
	if targetExists > 0 {
		if !existingTargetOwned {
			return ErrUnmanagedUser
		}
		targetCreate, err := readSingleStatement(db, fmt.Sprintf("SHOW CREATE USER '%s'@'%s'", escapedUser, LocalTCPHost))
		if err != nil {
			return fmt.Errorf("read TCP database account: %w", err)
		}
		if targetCreate != createStatement {
			return fmt.Errorf("managed TCP account authentication differs from its socket source")
		}
	} else if _, err := db.Exec(createStatement); err != nil {
		if !isUserExistsError(err) {
			return fmt.Errorf("create TCP database account: %w", err)
		}
		targetCreate, inspectErr := readSingleStatement(db, fmt.Sprintf("SHOW CREATE USER '%s'@'%s'", escapedUser, LocalTCPHost))
		if inspectErr != nil || targetCreate != createStatement {
			return fmt.Errorf("create TCP database account raced with a different account definition: %w", err)
		}
	} else {
		createdTarget = true
	}

	for name := range seenDatabases {
		statement := fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%s'", name, escapedUser, LocalTCPHost)
		if _, err := db.Exec(statement); err != nil {
			operationErr := fmt.Errorf("grant registered database access: %w", err)
			if createdTarget {
				operationErr = errors.Join(operationErr, dropAccount(db, user, LocalTCPHost))
			}
			return operationErr
		}
	}
	return nil
}

func dropAccount(db *sql.DB, user, host string) error {
	_, err := db.Exec(fmt.Sprintf("DROP USER IF EXISTS '%s'@'%s'", safeinput.EscapeSQLString(user), safeinput.EscapeSQLString(host)))
	return err
}
