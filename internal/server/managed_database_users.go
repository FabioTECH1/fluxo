package server

import (
	"errors"
	"fmt"

	"fluxo/internal/database"
	"fluxo/internal/services/mysql"
)

var errManagedDatabaseUserExists = errors.New("database user is already reserved or managed by Fluxo")

func reserveManagedMySQLUser(username string) error {
	created, err := database.BeginManagedDatabaseUser("mysql", username, mysql.LocalTCPHost)
	if err != nil {
		return err
	}
	if !created {
		return errManagedDatabaseUserExists
	}
	return nil
}

func activateManagedMySQLUser(username string) error {
	return database.ActivateManagedDatabaseUser("mysql", username, mysql.LocalTCPHost)
}

func releaseManagedMySQLUser(username string) error {
	return database.DeleteManagedDatabaseUser("mysql", username, mysql.LocalTCPHost)
}

func requireManagedMySQLUser(username string) error {
	state, err := database.ManagedDatabaseUserState("mysql", username, mysql.LocalTCPHost)
	if err != nil {
		return err
	}
	if state != database.ManagedDatabaseUserActive {
		return fmt.Errorf("this MySQL account is not managed by Fluxo")
	}
	return nil
}
