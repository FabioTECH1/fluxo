package ssl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IssueCustom writes a user-provided certificate and private key to the Nginx SSL directory.
func IssueCustom(domain, certContent, keyContent string) error {
	certContent = strings.TrimSpace(certContent)
	keyContent = strings.TrimSpace(keyContent)

	if certContent == "" || keyContent == "" {
		return fmt.Errorf("certificate and private key must not be empty")
	}

	if !strings.HasPrefix(certContent, "-----BEGIN CERTIFICATE-----") {
		return fmt.Errorf("invalid certificate format: missing PEM header")
	}

	if !strings.HasPrefix(keyContent, "-----BEGIN") || !strings.Contains(keyContent, "PRIVATE KEY-----") {
		return fmt.Errorf("invalid private key format: missing PEM header")
	}

	sslDir := filepath.Join("/etc/nginx/ssl", domain)
	if err := os.MkdirAll(sslDir, 0755); err != nil {
		return fmt.Errorf("failed to create ssl dir: %w", err)
	}

	certPath := filepath.Join(sslDir, "server.crt")
	if err := os.WriteFile(certPath, []byte(certContent+"\n"), 0644); err != nil {
		return fmt.Errorf("failed to write certificate: %w", err)
	}

	keyPath := filepath.Join(sslDir, "server.key")
	if err := os.WriteFile(keyPath, []byte(keyContent+"\n"), 0600); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	// Ensure the private key is readable by nginx (www-data group).
	_ = os.Chown(keyPath, 0, 33) // root:www-data (gid 33)
	_ = os.Chmod(keyPath, 0640)

	return nil
}
