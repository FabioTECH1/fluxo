package postgres

import (
	"fmt"
	"os/exec"
	"strings"
)

func CreateDatabase(name, user, password string) error {
	// Create role
	createRoleCmd := exec.Command("sudo", "-u", "postgres", "psql")
	createRoleCmd.Stdin = strings.NewReader(fmt.Sprintf("CREATE ROLE \"%s\" WITH LOGIN PASSWORD '%s';\n", user, password))
	if out, err := createRoleCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create role: %v (%s)", err, string(out))
	}

	// Create database owned by user
	if err := createDB(name, user); err != nil {
		// Clean up role
		dropRoleCmd := exec.Command("sudo", "-u", "postgres", "psql")
		dropRoleCmd.Stdin = strings.NewReader(fmt.Sprintf("DROP ROLE \"%s\";\n", user))
		dropRoleCmd.Run()
		return err
	}

	return nil
}

func CreateDatabaseOnly(name string) error {
	return createDB(name, "postgres")
}

func createDB(name, owner string) error {
	createDbCmd := exec.Command("sudo", "-u", "postgres", "psql")
	createDbCmd.Stdin = strings.NewReader(fmt.Sprintf("CREATE DATABASE \"%s\" OWNER \"%s\";\n", name, owner))
	if out, err := createDbCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create database: %v (%s)", err, string(out))
	}
	return nil
}

func DeleteDatabase(name, user string) error {
	// Drop database
	dropDbCmd := exec.Command("sudo", "-u", "postgres", "psql")
	dropDbCmd.Stdin = strings.NewReader(fmt.Sprintf("DROP DATABASE IF EXISTS \"%s\";\n", name))
	if out, err := dropDbCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to drop database: %v (%s)", err, string(out))
	}

	// Drop role
	dropRoleCmd := exec.Command("sudo", "-u", "postgres", "psql")
	dropRoleCmd.Stdin = strings.NewReader(fmt.Sprintf("DROP ROLE IF EXISTS \"%s\";\n", user))
	if out, err := dropRoleCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to drop role: %v (%s)", err, string(out))
	}

	return nil
}
