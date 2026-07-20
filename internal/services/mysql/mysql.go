package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"fluxo/internal/safeinput"

	_ "github.com/go-sql-driver/mysql"
)

func CheckConnection() error {
	db, err := sql.Open("mysql", "root@unix(/var/run/mysqld/mysqld.sock)/")
	if err != nil {
		return fmt.Errorf("failed to connect to mysql: %w", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("mysql is unavailable: %w", err)
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

	db, err := sql.Open("mysql", "root@unix(/var/run/mysqld/mysqld.sock)/")
	if err != nil {
		return fmt.Errorf("failed to connect to mysql: %w", err)
	}
	defer db.Close()

	if _, err := db.Exec(fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'localhost' IDENTIFIED BY '%s'", safeinput.EscapeSQLString(user), safeinput.EscapeSQLString(password))); err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	if _, err := db.Exec(fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost'", name, safeinput.EscapeSQLString(user))); err != nil {
		return fmt.Errorf("failed to grant privileges: %w", err)
	}

	if _, err := db.Exec("FLUSH PRIVILEGES"); err != nil {
		return fmt.Errorf("failed to flush privileges: %w", err)
	}

	return nil
}

// CreateDatabaseOnly creates a database without creating a user.
func CreateDatabaseOnly(name string) error {
	if !safeinput.ValidateDBIdent(name) {
		return fmt.Errorf("invalid database name")
	}
	db, err := sql.Open("mysql", "root@unix(/var/run/mysqld/mysqld.sock)/")
	if err != nil {
		return fmt.Errorf("failed to connect to mysql: %w", err)
	}
	defer db.Close()

	if _, err := db.Exec(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", name)); err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}

	return nil
}

// DeleteDatabase drops a database without deleting any database users.
func DeleteDatabase(name string) error {
	if !safeinput.ValidateDBIdent(name) {
		return fmt.Errorf("invalid database name")
	}
	db, err := sql.Open("mysql", "root@unix(/var/run/mysqld/mysqld.sock)/")
	if err != nil {
		return fmt.Errorf("failed to connect to mysql: %w", err)
	}
	defer db.Close()

	if _, err := db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", name)); err != nil {
		return fmt.Errorf("failed to drop database: %w", err)
	}

	return nil
}
