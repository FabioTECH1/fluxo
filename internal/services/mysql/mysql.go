package mysql

import (
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
)

// CreateDatabase creates a database, user, and grants all privileges.
func CreateDatabase(name, user, password string) error {
	if err := CreateDatabaseOnly(name); err != nil {
		return err
	}

	db, err := sql.Open("mysql", "root@unix(/var/run/mysqld/mysqld.sock)/")
	if err != nil {
		return fmt.Errorf("failed to connect to mysql: %w", err)
	}
	defer db.Close()

	if _, err := db.Exec(fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'localhost' IDENTIFIED BY '%s'", user, password)); err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	if _, err := db.Exec(fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost'", name, user)); err != nil {
		return fmt.Errorf("failed to grant privileges: %w", err)
	}

	if _, err := db.Exec("FLUSH PRIVILEGES"); err != nil {
		return fmt.Errorf("failed to flush privileges: %w", err)
	}

	return nil
}

// CreateDatabaseOnly creates a database without creating a user.
func CreateDatabaseOnly(name string) error {
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

// DeleteDatabase drops the database and its associated user.
func DeleteDatabase(name, user string) error {
	db, err := sql.Open("mysql", "root@unix(/var/run/mysqld/mysqld.sock)/")
	if err != nil {
		return fmt.Errorf("failed to connect to mysql: %w", err)
	}
	defer db.Close()

	if _, err := db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", name)); err != nil {
		return fmt.Errorf("failed to drop database: %w", err)
	}

	if _, err := db.Exec(fmt.Sprintf("DROP USER IF EXISTS '%s'@'localhost'", user)); err != nil {
		return fmt.Errorf("failed to drop user: %w", err)
	}

	return nil
}
