package postgres

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

func CreateDatabase(name, user, password string) error {
	db, err := sql.Open("postgres", "host=/var/run/postgresql user=postgres dbname=postgres sslmode=disable")
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}
	defer db.Close()

	if _, err := db.Exec(fmt.Sprintf("CREATE ROLE \"%s\" WITH LOGIN PASSWORD '%s'", user, password)); err != nil {
		return fmt.Errorf("failed to create role: %w", err)
	}

	if _, err := db.Exec(fmt.Sprintf("CREATE DATABASE \"%s\" OWNER \"%s\"", name, user)); err != nil {
		db.Exec(fmt.Sprintf("DROP ROLE \"%s\"", user))
		return fmt.Errorf("failed to create database: %w", err)
	}

	return nil
}

func DeleteDatabase(name, user string) error {
	db, err := sql.Open("postgres", "host=/var/run/postgresql user=postgres dbname=postgres sslmode=disable")
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}
	defer db.Close()

	if _, err := db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS \"%s\"", name)); err != nil {
		return fmt.Errorf("failed to drop database: %w", err)
	}

	if _, err := db.Exec(fmt.Sprintf("DROP ROLE IF EXISTS \"%s\"", user)); err != nil {
		return fmt.Errorf("failed to drop role: %w", err)
	}

	return nil
}
