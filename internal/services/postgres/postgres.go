package postgres

import (
	"context"
	"fmt"
	"time"

	"fluxo/internal/safeinput"
	"fluxo/internal/syscmd"
)

// CreateDatabase creates a PostgreSQL role and database, rolling back the role on failure.
// Revokes PUBLIC CONNECT on the new database so only the owner and explicitly granted users can connect.
func CreateDatabase(name, user, password string) error {
	ctx := context.Background()
	if !safeinput.ValidateDBIdent(name) || !safeinput.ValidateDBIdent(user) {
		return fmt.Errorf("invalid database or user name")
	}

	_, err := syscmd.RunStdin(ctx, 10*time.Second,
		fmt.Sprintf("CREATE ROLE \"%s\" WITH LOGIN PASSWORD '%s';", safeinput.EscapeSQLString(user), safeinput.EscapeSQLString(password)),
		"sudo", "-u", "postgres", "psql")
	if err != nil {
		return fmt.Errorf("failed to create role: %w", err)
	}

	if err := createDB(name, user); err != nil {
		syscmd.RunStdin(ctx, 5*time.Second,
			fmt.Sprintf("DROP ROLE \"%s\";", safeinput.EscapeSQLString(user)),
			"sudo", "-u", "postgres", "psql")
		return err
	}

	return nil
}

// CreateDatabaseOnly creates a PostgreSQL database owned by the postgres user.
// Revokes PUBLIC CONNECT so only postgres (superuser) can connect.
func CreateDatabaseOnly(name string) error {
	if !safeinput.ValidateDBIdent(name) {
		return fmt.Errorf("invalid database name")
	}
	if err := createDB(name, "postgres"); err != nil {
		return err
	}

	ctx := context.Background()
	_, err := syscmd.Run(ctx, 5*time.Second, "sudo", "-u", "postgres", "psql", "-c",
		fmt.Sprintf("REVOKE CONNECT ON DATABASE \"%s\" FROM PUBLIC", safeinput.EscapeSQLString(name)))
	return err
}

func createDB(name, owner string) error {
	ctx := context.Background()
	if !safeinput.ValidateDBIdent(name) || !safeinput.ValidateDBIdent(owner) {
		return fmt.Errorf("invalid database or owner name")
	}
	_, err := syscmd.RunStdin(ctx, 10*time.Second,
		fmt.Sprintf("CREATE DATABASE \"%s\" OWNER \"%s\";\nREVOKE CONNECT ON DATABASE \"%s\" FROM PUBLIC;", safeinput.EscapeSQLString(name), safeinput.EscapeSQLString(owner), safeinput.EscapeSQLString(name)),
		"sudo", "-u", "postgres", "psql")
	if err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}
	return nil
}

// DeleteDatabase drops a PostgreSQL database. Users must be deleted separately.
func DeleteDatabase(name, user string) error {
	ctx := context.Background()
	if !safeinput.ValidateDBIdent(name) || !safeinput.ValidateDBIdent(user) {
		return fmt.Errorf("invalid database or user name")
	}

	_, err := syscmd.RunStdin(ctx, 10*time.Second,
		fmt.Sprintf("DROP DATABASE IF EXISTS \"%s\";", safeinput.EscapeSQLString(name)),
		"sudo", "-u", "postgres", "psql")
	if err != nil {
		return fmt.Errorf("failed to drop database: %w", err)
	}

	return nil
}
