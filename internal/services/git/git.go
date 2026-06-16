package git

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	"fluxo/internal/syscmd"
)

// chownToFluxo sets ownership of path to the fluxo user.
func chownToFluxo(path string) error {
	u, err := user.Lookup("fluxo")
	if err != nil {
		return nil // Ignore if user not found (e.g. dev environment)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return os.Chown(path, uid, gid)
}

// GetSSHKeyPath returns the path to the site's private SSH deploy key.
func GetSSHKeyPath(siteID int) string {
	var sshDir string
	if os.Getenv("FLUXO_ENV") == "prod" {
		sshDir = "/home/fluxo/.ssh"
	} else {
		home, _ := os.UserHomeDir()
		sshDir = filepath.Join(home, ".ssh")
	}
	return filepath.Join(sshDir, fmt.Sprintf("fluxo_site_%d_ed25519", siteID))
}

// GenerateSSHKey creates an Ed25519 keypair and returns (privPath, pubKeyContent, error).
func GenerateSSHKey(ctx context.Context, siteID int) (string, string, error) {
	privPath := GetSSHKeyPath(siteID)
	pubPath := privPath + ".pub"
	sshDir := filepath.Dir(privPath)

	os.MkdirAll(sshDir, 0700)
	if os.Getenv("FLUXO_ENV") == "prod" {
		chownToFluxo(sshDir)
	}

	// If key doesn't exist, generate it
	if _, err := os.Stat(privPath); os.IsNotExist(err) {
		_, err := syscmd.Run(ctx, 10*time.Second, "ssh-keygen", "-t", "ed25519", "-N", "", "-f", privPath, "-C", fmt.Sprintf("fluxo-site-%d", siteID))
		if err != nil {
			return "", "", fmt.Errorf("failed to generate ssh key: %w", err)
		}
		if os.Getenv("FLUXO_ENV") == "prod" {
			chownToFluxo(privPath)
			chownToFluxo(pubPath)
		}
	}

	pubBytes, err := os.ReadFile(pubPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to read public key: %w", err)
	}

	return privPath, string(pubBytes), nil
}
