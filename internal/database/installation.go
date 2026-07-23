package database

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const installationIDKey = "installation_id"

// InstallationID returns the persistent identity shared by this Fluxo installation.
func InstallationID() (string, error) {
	var id string
	err := DB.QueryRow("SELECT value FROM system_metadata WHERE key = ?", installationIDKey).Scan(&id)
	if err == nil {
		id = strings.TrimSpace(id)
		if !validInstallationID(id) {
			return "", errors.New("installation ID is invalid")
		}
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate installation ID: %w", err)
	}
	candidate := hex.EncodeToString(random)
	if _, err := DB.Exec(
		"INSERT OR IGNORE INTO system_metadata (key, value) VALUES (?, ?)", installationIDKey, candidate,
	); err != nil {
		return "", err
	}
	if err := DB.QueryRow("SELECT value FROM system_metadata WHERE key = ?", installationIDKey).Scan(&id); err != nil {
		return "", err
	}
	id = strings.TrimSpace(id)
	if !validInstallationID(id) {
		return "", errors.New("installation ID is invalid")
	}
	return id, nil
}

func validInstallationID(id string) bool {
	decoded, err := hex.DecodeString(id)
	return err == nil && len(decoded) == 16 && id == strings.ToLower(id)
}
