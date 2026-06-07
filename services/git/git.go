package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"fluxo/syscmd"
)

// GenerateSSHKey creates an Ed25519 SSH keypair for a site.
// It returns the path to the private key and the string contents of the public key.
func GenerateSSHKey(ctx context.Context, siteID int) (string, string, error) {
	home, _ := os.UserHomeDir()
	sshDir := filepath.Join(home, ".ssh")
	os.MkdirAll(sshDir, 0700)

	privPath := filepath.Join(sshDir, fmt.Sprintf("fluxo_site_%d_ed25519", siteID))
	pubPath := privPath + ".pub"

	// If key doesn't exist, generate it
	if _, err := os.Stat(privPath); os.IsNotExist(err) {
		_, err := syscmd.Run(ctx, 10*time.Second, "ssh-keygen", "-t", "ed25519", "-N", "", "-f", privPath, "-C", fmt.Sprintf("fluxo-site-%d", siteID))
		if err != nil {
			return "", "", fmt.Errorf("failed to generate ssh key: %w", err)
		}
	}

	pubBytes, err := os.ReadFile(pubPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to read public key: %w", err)
	}

	return privPath, string(pubBytes), nil
}
