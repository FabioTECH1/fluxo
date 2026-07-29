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

// GenerateTemporarySSHKey creates a staged keypair for rotating a site's deploy key.
func GenerateTemporarySSHKey(ctx context.Context, siteID int) (string, string, func(), error) {
	targetPath := GetSSHKeyPath(siteID)
	sshDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return "", "", nil, err
	}
	if os.Getenv("FLUXO_ENV") == "prod" {
		chownToFluxo(sshDir)
	}

	tmp, err := os.CreateTemp(sshDir, fmt.Sprintf(".fluxo_site_%d_*.ed25519", siteID))
	if err != nil {
		return "", "", nil, err
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", "", nil, err
	}
	_ = os.Remove(tmpPath)

	cleanup := func() {
		_ = os.Remove(tmpPath)
		_ = os.Remove(tmpPath + ".pub")
	}

	if _, err := syscmd.Run(ctx, 10*time.Second, "ssh-keygen", "-t", "ed25519", "-N", "", "-f", tmpPath, "-C", fmt.Sprintf("fluxo-site-%d", siteID)); err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("failed to generate temporary ssh key: %w", err)
	}
	if os.Getenv("FLUXO_ENV") == "prod" {
		chownToFluxo(tmpPath)
		chownToFluxo(tmpPath + ".pub")
	}

	pubBytes, err := os.ReadFile(tmpPath + ".pub")
	if err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("failed to read temporary public key: %w", err)
	}

	return tmpPath, string(pubBytes), cleanup, nil
}

// ReplaceSSHKeyPair safely swaps a staged keypair into the site's canonical key path.
func ReplaceSSHKeyPair(siteID int, stagedPrivPath string) error {
	targetPrivPath := GetSSHKeyPath(siteID)
	targetPubPath := targetPrivPath + ".pub"
	stagedPubPath := stagedPrivPath + ".pub"
	backupSuffix := fmt.Sprintf(".backup-%d", time.Now().UnixNano())
	backupPrivPath := targetPrivPath + backupSuffix
	backupPubPath := targetPubPath + backupSuffix
	hadPriv := false
	hadPub := false

	restore := func() {
		_ = os.Remove(targetPrivPath)
		_ = os.Remove(targetPubPath)
		if hadPriv {
			_ = os.Rename(backupPrivPath, targetPrivPath)
		}
		if hadPub {
			_ = os.Rename(backupPubPath, targetPubPath)
		}
	}

	if _, err := os.Stat(targetPrivPath); err == nil {
		if err := os.Rename(targetPrivPath, backupPrivPath); err != nil {
			return err
		}
		hadPriv = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(targetPubPath); err == nil {
		if err := os.Rename(targetPubPath, backupPubPath); err != nil {
			restore()
			return err
		}
		hadPub = true
	} else if !os.IsNotExist(err) {
		restore()
		return err
	}

	if err := os.Rename(stagedPrivPath, targetPrivPath); err != nil {
		restore()
		return err
	}
	if err := os.Rename(stagedPubPath, targetPubPath); err != nil {
		restore()
		return err
	}
	if os.Getenv("FLUXO_ENV") == "prod" {
		chownToFluxo(targetPrivPath)
		chownToFluxo(targetPubPath)
	}

	_ = os.Remove(backupPrivPath)
	_ = os.Remove(backupPubPath)
	return nil
}
