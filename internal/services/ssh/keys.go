package ssh

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

// chownToFluxo sets ownership of the given path to the fluxo user.
func chownToFluxo(path string) error {
	u, err := user.Lookup("fluxo")
	if err != nil {
		return nil // Ignore if user not found (e.g. dev environment)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return os.Chown(path, uid, gid)
}

// getAuthorizedKeysPath returns the path to the authorized_keys file, creating the .ssh directory if needed.
func getAuthorizedKeysPath() (string, error) {
	var sshDir string
	if os.Getenv("FLUXO_ENV") == "prod" {
		sshDir = "/home/fluxo/.ssh"
	} else {
		dataDir := os.Getenv("FLUXO_DATA_DIR")
		if dataDir == "" {
			dataDir = "."
		}
		sshDir = filepath.Join(dataDir, ".ssh")
	}

	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return "", err
	}
	if os.Getenv("FLUXO_ENV") == "prod" {
		chownToFluxo(sshDir)
	}
	return filepath.Join(sshDir, "authorized_keys"), nil
}

// AddKey appends a public key to the authorized_keys file.
func AddKey(publicKey string) error {
	path, err := getAuthorizedKeysPath()
	if err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	if !strings.HasSuffix(publicKey, "\n") {
		publicKey += "\n"
	}
	if _, err := f.WriteString(publicKey); err != nil {
		return err
	}

	if os.Getenv("FLUXO_ENV") == "prod" {
		chownToFluxo(path)
	}
	return nil
}

// RemoveKey removes a public key from the authorized_keys file.
func RemoveKey(publicKey string) error {
	path, err := getAuthorizedKeysPath()
	if err != nil {
		return err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string
	cleanPublicKey := strings.TrimSpace(publicKey)

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine == "" {
			continue
		}
		if trimmedLine != cleanPublicKey {
			newLines = append(newLines, line)
		}
	}

	newContent := strings.Join(newLines, "\n")
	if len(newLines) > 0 {
		newContent += "\n"
	}

	err = os.WriteFile(path, []byte(newContent), 0600)
	if err == nil && os.Getenv("FLUXO_ENV") == "prod" {
		chownToFluxo(path)
	}
	return err
}
