package ssl

import (
	"context"
	"fmt"
	"time"

	"fluxo/internal/syscmd"
)

// IssueLetsEncrypt requests a Let's Encrypt certificate via certbot webroot challenge.
func IssueLetsEncrypt(ctx context.Context, domain, webRoot, email string) error {
	cmd := []string{
		"certbot", "certonly", "--webroot",
		"-w", webRoot,
		"-d", domain,
		"--non-interactive",
		"--agree-tos",
		"-m", email,
		"--deploy-hook", "systemctl reload nginx",
	}

	_, err := syscmd.Run(ctx, 5*time.Minute, cmd[0], cmd[1:]...)
	if err != nil {
		return fmt.Errorf("certbot execution failed: %w", err)
	}

	return nil
}
