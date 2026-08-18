package database

import (
	"database/sql"
	"fmt"
)

const (
	ManagedDatabaseUserPending = "pending"
	ManagedDatabaseUserActive  = "active"
)

type ManagedDatabaseUser struct {
	Engine   string
	Username string
	Host     string
	State    string
}

// BeginManagedDatabaseUser reserves an exact engine/user/host identity before
// the external database account is created. The returned boolean is false when
// another Fluxo operation already owns that identity.
func BeginManagedDatabaseUser(engine, username, host string) (bool, error) {
	result, err := DB.Exec(`
		INSERT OR IGNORE INTO managed_database_users (engine, username, host, state)
		VALUES (?, ?, ?, ?)`, engine, username, host, ManagedDatabaseUserPending)
	if err != nil {
		return false, fmt.Errorf("reserve managed database user: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("inspect managed database user reservation: %w", err)
	}
	return rows == 1, nil
}

func ActivateManagedDatabaseUser(engine, username, host string) error {
	result, err := DB.Exec(`
		UPDATE managed_database_users
		SET state = ?, updated_at = CURRENT_TIMESTAMP
		WHERE engine = ? AND username = ? AND host = ?`,
		ManagedDatabaseUserActive, engine, username, host)
	if err != nil {
		return fmt.Errorf("activate managed database user: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect managed database user activation: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("managed database user reservation not found")
	}
	return nil
}

func ManagedDatabaseUserState(engine, username, host string) (string, error) {
	var state string
	err := DB.QueryRow(`
		SELECT state FROM managed_database_users
		WHERE engine = ? AND username = ? AND host = ?`, engine, username, host).Scan(&state)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read managed database user: %w", err)
	}
	return state, nil
}

func DeleteManagedDatabaseUser(engine, username, host string) error {
	if _, err := DB.Exec(`
		DELETE FROM managed_database_users
		WHERE engine = ? AND username = ? AND host = ?`, engine, username, host); err != nil {
		return fmt.Errorf("remove managed database user: %w", err)
	}
	return nil
}

func ListManagedDatabaseUsers(engine, state string) ([]ManagedDatabaseUser, error) {
	rows, err := DB.Query(`
		SELECT engine, username, host, state
		FROM managed_database_users
		WHERE engine = ? AND state = ?
		ORDER BY username, host`, engine, state)
	if err != nil {
		return nil, fmt.Errorf("list managed database users: %w", err)
	}
	defer rows.Close()
	users := make([]ManagedDatabaseUser, 0)
	for rows.Next() {
		var user ManagedDatabaseUser
		if err := rows.Scan(&user.Engine, &user.Username, &user.Host, &user.State); err != nil {
			return nil, fmt.Errorf("read managed database user: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read managed database users: %w", err)
	}
	return users, nil
}
