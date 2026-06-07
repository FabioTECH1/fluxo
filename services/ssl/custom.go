package ssl

import (
	"fmt"
	"os"
	"path/filepath"
)

func IssueCustom(domain, certContent, keyContent string) error {
	sslDir := filepath.Join("/etc/nginx/ssl", domain)
	if err := os.MkdirAll(sslDir, 0755); err != nil {
		return fmt.Errorf("failed to create ssl dir: %w", err)
	}

	certPath := filepath.Join(sslDir, "server.crt")
	if err := os.WriteFile(certPath, []byte(certContent), 0644); err != nil {
		return fmt.Errorf("failed to write certificate: %w", err)
	}

	keyPath := filepath.Join(sslDir, "server.key")
	if err := os.WriteFile(keyPath, []byte(keyContent), 0600); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	return nil
}
