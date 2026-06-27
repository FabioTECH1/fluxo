package postgres

import (
	"context"
	"fmt"
	"time"

	"fluxo/internal/syscmd"
)

// CreateDatabase creates a PostgreSQL role and database, rolling back the role on failure.
// Revokes PUBLIC CONNECT on the new database so only the owner and explicitly granted users can connect.
func CreateDatabase(name, user, password string) error {
	ctx := context.Background()

	_, err := syscmd.RunStdin(ctx, 10*time.Second,
		fmt.Sprintf("CREATE ROLE \"%s\" WITH LOGIN PASSWORD '%s';", user, password),
		"sudo", "-u", "postgres", "psql")
	if err != nil {
		return fmt.Errorf("failed to create role: %w", err)
	}

	if err := createDB(name, user); err != nil {
		syscmd.RunStdin(ctx, 5*time.Second,
			fmt.Sprintf("DROP ROLE \"%s\";", user),
			"sudo", "-u", "postgres", "psql")
		return err
	}

	return nil
}

// CreateDatabaseOnly creates a PostgreSQL database owned by the postgres user.
// Revokes PUBLIC CONNECT so only postgres (superuser) can connect.
func CreateDatabaseOnly(name string) error {
	if err := createDB(name, "postgres"); err != nil {
		return err
	}

	ctx := context.Background()
	_, err := syscmd.Run(ctx, 5*time.Second, "sudo", "-u", "postgres", "psql", "-c",
		fmt.Sprintf("REVOKE CONNECT ON DATABASE \"%s\" FROM PUBLIC", name))
	return err
}

func createDB(name, owner string) error {
	ctx := context.Background()
	_, err := syscmd.RunStdin(ctx, 10*time.Second,
		fmt.Sprintf("CREATE DATABASE \"%s\" OWNER \"%s\";\nREVOKE CONNECT ON DATABASE \"%s\" FROM PUBLIC;", name, owner, name),
		"sudo", "-u", "postgres", "psql")
	if err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}
	return nil
}

// DeleteDatabase drops a PostgreSQL database and its associated role.
func DeleteDatabase(name, user string) error {
	ctx := context.Background()

	_, err := syscmd.RunStdin(ctx, 10*time.Second,
		fmt.Sprintf("DROP DATABASE IF EXISTS \"%s\";", name),
		"sudo", "-u", "postgres", "psql")
	if err != nil {
		return fmt.Errorf("failed to drop database: %w", err)
	}

	_, err = syscmd.RunStdin(ctx, 10*time.Second,
		fmt.Sprintf("DROP ROLE IF EXISTS \"%s\";", user),
		"sudo", "-u", "postgres", "psql")
	if err != nil {
		return fmt.Errorf("failed to drop role: %w", err)
	}

	return nil
}
