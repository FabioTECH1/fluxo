package mysql

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
)

func TestAdminConfigUsesModeAwareParameterInterpolation(t *testing.T) {
	config := adminConfig()
	if !config.InterpolateParams {
		t.Fatal("account-management passwords must use driver interpolation")
	}
	if config.Net != "unix" || config.Addr != "/var/run/mysqld/mysqld.sock" || config.User != "root" {
		t.Fatalf("unexpected administrator connection config: %#v", config)
	}
}

func TestCreateUserPreservesQuoteAndBackslashPassword(t *testing.T) {
	if os.Getenv("FLUXO_MYSQL_INTEGRATION") != "1" {
		t.Skip("set FLUXO_MYSQL_INTEGRATION=1 on a disposable MariaDB host")
	}
	const user = "fx_codex_bind_audit"
	const password = `abc\' REQUIRE SSL #still-password`
	_ = DropManagedLocalUser(user)
	t.Cleanup(func() { _ = DropManagedLocalUser(user) })

	if err := CreateUser(user, password); err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	config := driver.NewConfig()
	config.User = user
	config.Passwd = password
	config.Net = "tcp"
	config.Addr = LocalTCPHost + ":3306"
	config.Timeout = 5 * time.Second
	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatalf("open created user: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("created password did not authenticate exactly: %v", err)
	}

	admin, err := openAdmin()
	if err != nil {
		t.Fatalf("open administrator connection: %v", err)
	}
	defer admin.Close()
	statement, err := readSingleStatement(admin, "SHOW CREATE USER '"+user+"'@'"+LocalTCPHost+"'")
	if err != nil {
		t.Fatalf("SHOW CREATE USER: %v", err)
	}
	if strings.Contains(strings.ToUpper(statement), "REQUIRE SSL") {
		t.Fatalf("password escaped into account syntax: %s", statement)
	}

	const rotatedPassword = `rotated\' REQUIRE X509 #still-password`
	if err := UpdateLocalUserPassword(user, rotatedPassword); err != nil {
		t.Fatalf("UpdateLocalUserPassword() error: %v", err)
	}
	config.Passwd = rotatedPassword
	rotatedDB, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatalf("open rotated user: %v", err)
	}
	defer rotatedDB.Close()
	if err := rotatedDB.PingContext(ctx); err != nil {
		t.Fatalf("rotated password did not authenticate exactly: %v", err)
	}
}

func TestEnsureTCPAccountReconcilesInterruptedGrantCopy(t *testing.T) {
	if os.Getenv("FLUXO_MYSQL_INTEGRATION") != "1" {
		t.Skip("set FLUXO_MYSQL_INTEGRATION=1 on a disposable MariaDB host")
	}
	const user = "fx_codex_repair_audit"
	const databaseName = "fx_codex_repair_db"
	const password = "repair-secret"
	admin, err := openAdmin()
	if err != nil {
		t.Fatalf("open administrator connection: %v", err)
	}
	cleanup := func() {
		_, _ = admin.Exec("DROP USER IF EXISTS '" + user + "'@'" + LocalTCPHost + "'")
		_, _ = admin.Exec("DROP USER IF EXISTS '" + user + "'@'localhost'")
		_, _ = admin.Exec("DROP DATABASE IF EXISTS `" + databaseName + "`")
	}
	cleanup()
	t.Cleanup(func() {
		cleanup()
		_ = admin.Close()
	})

	if _, err := admin.Exec("CREATE DATABASE `" + databaseName + "`"); err != nil {
		t.Fatalf("create database: %v", err)
	}
	if _, err := admin.Exec("CREATE USER '"+user+"'@'localhost' IDENTIFIED BY ?", password); err != nil {
		t.Fatalf("create source account: %v", err)
	}
	if _, err := admin.Exec("GRANT ALL PRIVILEGES ON `" + databaseName + "`.* TO '" + user + "'@'localhost'"); err != nil {
		t.Fatalf("grant source account: %v", err)
	}
	if err := EnsureTCPAccountFromLocalhost(user, []string{databaseName}, false); err != nil {
		t.Fatalf("initial repair: %v", err)
	}
	if _, err := admin.Exec("REVOKE ALL PRIVILEGES, GRANT OPTION FROM '" + user + "'@'" + LocalTCPHost + "'"); err != nil {
		t.Fatalf("simulate interrupted grant copy: %v", err)
	}
	if err := EnsureTCPAccountFromLocalhost(user, []string{databaseName}, true); err != nil {
		t.Fatalf("reconciliation repair: %v", err)
	}
	rows, err := admin.Query("SHOW GRANTS FOR '" + user + "'@'" + LocalTCPHost + "'")
	if err != nil {
		t.Fatalf("show repaired grants: %v", err)
	}
	grants, err := readStatementRows(rows)
	rows.Close()
	if err != nil {
		t.Fatalf("read repaired grants: %v", err)
	}
	want := "`" + databaseName + "`.*"
	found := false
	for _, grant := range grants {
		if strings.Contains(grant, want) {
			found = true
		}
	}
	if !found {
		t.Fatalf("reconciled account is missing %s grant: %v", want, grants)
	}
}

func TestDropManagedLocalUserPreservesRemoteAccountVariants(t *testing.T) {
	if os.Getenv("FLUXO_MYSQL_INTEGRATION") != "1" {
		t.Skip("set FLUXO_MYSQL_INTEGRATION=1 on a disposable MariaDB host")
	}
	const user = "fx_codex_drop_scope"
	admin, err := openAdmin()
	if err != nil {
		t.Fatalf("open administrator connection: %v", err)
	}
	cleanup := func() {
		_, _ = admin.Exec("DROP USER IF EXISTS '" + user + "'@'%'")
		_, _ = admin.Exec("DROP USER IF EXISTS '" + user + "'@'" + LocalTCPHost + "'")
		_, _ = admin.Exec("DROP USER IF EXISTS '" + user + "'@'localhost'")
	}
	cleanup()
	t.Cleanup(func() {
		cleanup()
		_ = admin.Close()
	})

	if _, err := admin.Exec("CREATE USER '"+user+"'@'%' IDENTIFIED BY ?", "remote-secret"); err != nil {
		t.Fatalf("create remote account: %v", err)
	}
	if _, err := admin.Exec("CREATE USER '"+user+"'@'"+LocalTCPHost+"' IDENTIFIED BY ?", "local-secret"); err != nil {
		t.Fatalf("create local account: %v", err)
	}
	if _, err := admin.Exec("CREATE USER '"+user+"'@'localhost' IDENTIFIED BY ?", "socket-secret"); err != nil {
		t.Fatalf("create socket account: %v", err)
	}
	if err := DropManagedLocalUser(user); err != nil {
		t.Fatalf("DropManagedLocalUser() error: %v", err)
	}
	var remoteCount, localCount int
	if err := admin.QueryRow("SELECT COUNT(*) FROM mysql.user WHERE User = ? AND Host = '%'", user).Scan(&remoteCount); err != nil {
		t.Fatalf("inspect remote account: %v", err)
	}
	if err := admin.QueryRow("SELECT COUNT(*) FROM mysql.user WHERE User = ? AND Host IN (?, 'localhost')", user, LocalTCPHost).Scan(&localCount); err != nil {
		t.Fatalf("inspect local accounts: %v", err)
	}
	if remoteCount != 1 || localCount != 1 {
		t.Fatalf("account counts after managed TCP delete = remote:%d local:%d, want remote:1 local:1", remoteCount, localCount)
	}
}

func TestRewriteAccountHost(t *testing.T) {
	tests := []struct {
		name      string
		statement string
		want      string
		wantOK    bool
	}{
		{
			name:      "MariaDB create user",
			statement: "CREATE USER 'app_user'@'localhost' IDENTIFIED VIA mysql_native_password USING '*HASH'",
			want:      "CREATE USER 'app_user'@'127.0.0.1' IDENTIFIED VIA mysql_native_password USING '*HASH'",
			wantOK:    true,
		},
		{
			name:      "MySQL grant with backticks",
			statement: "GRANT ALL PRIVILEGES ON `app_db`.* TO `app_user`@`localhost`",
			want:      "GRANT ALL PRIVILEGES ON `app_db`.* TO `app_user`@`127.0.0.1`",
			wantOK:    true,
		},
		{
			name:      "does not rewrite another account",
			statement: "GRANT ALL PRIVILEGES ON `app_db`.* TO 'other_user'@'localhost'",
			want:      "GRANT ALL PRIVILEGES ON `app_db`.* TO 'other_user'@'localhost'",
			wantOK:    false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := rewriteAccountHost(test.statement, "app_user", "localhost", LocalTCPHost)
			if got != test.want || ok != test.wantOK {
				t.Fatalf("rewriteAccountHost() = (%q, %v), want (%q, %v)", got, ok, test.want, test.wantOK)
			}
		})
	}
}

func TestDatabaseGrants(t *testing.T) {
	got := databaseGrants([]string{
		"GRANT USAGE ON *.* TO `app_user`@`127.0.0.1`",
		"GRANT ALL PRIVILEGES ON `app_db`.* TO `app_user`@`127.0.0.1`",
		"GRANT SELECT ON `read_only`.* TO `app_user`@`127.0.0.1`",
	})
	if _, ok := got["app_db"]; !ok || len(got) != 1 {
		t.Fatalf("databaseGrants() = %#v, want only app_db", got)
	}
}

func TestIsUserExistsError(t *testing.T) {
	if !isUserExistsError(&driver.MySQLError{Number: 1396, Message: "Operation CREATE USER failed"}) {
		t.Fatal("expected MySQL error 1396 to be classified as an existing user")
	}
	if isUserExistsError(errors.New("some other failure")) {
		t.Fatal("ordinary errors must not be classified as an existing user")
	}
}

func TestVerifyDatabaseAccessRejectsInvalidCredentialsBeforeConnecting(t *testing.T) {
	for _, test := range []struct {
		name, database, user, password string
	}{
		{name: "invalid database", database: "bad database", user: "app_user", password: "secret123"},
		{name: "invalid user", database: "app_db", user: "bad user", password: "secret123"},
		{name: "empty password", database: "app_db", user: "app_user", password: ""},
		{name: "control character", database: "app_db", user: "app_user", password: "bad\npassword"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := VerifyDatabaseAccess(test.database, test.user, test.password); err == nil {
				t.Fatal("expected invalid credentials to be rejected")
			}
		})
	}
}
