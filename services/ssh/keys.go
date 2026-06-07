package ssh

import (
	"os"
	"path/filepath"
	"strings"
)

func getAuthorizedKeysPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(sshDir, "authorized_keys"), nil
}

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

	return nil
}

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

	return os.WriteFile(path, []byte(newContent), 0600)
}
