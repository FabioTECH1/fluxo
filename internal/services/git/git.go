package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	sshservice "fluxo/internal/services/ssh"
	"fluxo/internal/syscmd"
)

var deployKeysMu sync.Mutex

func fluxoIdentity() (int, int, error) {
	if os.Getenv("FLUXO_ENV") != "prod" {
		return -1, -1, nil
	}
	u, err := user.Lookup("fluxo")
	if err != nil {
		return 0, 0, err
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, err
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return 0, 0, err
	}
	return uid, gid, nil
}

func sshHomeDirectory() (string, error) {
	if os.Getenv("FLUXO_ENV") == "prod" {
		return "/home/fluxo", nil
	}
	return os.UserHomeDir()
}

func openDeployKeyDirectory(create bool) (*sshservice.ManagedSSHDirectory, error) {
	home, err := sshHomeDirectory()
	if err != nil {
		return nil, err
	}
	if create && os.Getenv("FLUXO_ENV") != "prod" {
		if err := os.MkdirAll(home, 0700); err != nil {
			return nil, err
		}
	}
	uid, gid, err := fluxoIdentity()
	if err != nil {
		return nil, err
	}
	return sshservice.OpenManagedSSHDirectory(home, create, uid, gid)
}

func deployKeyName(siteID int) string {
	return fmt.Sprintf("fluxo_site_%d_ed25519", siteID)
}

// GetSSHKeyPath returns the path to the site's private SSH deploy key.
func GetSSHKeyPath(siteID int) string {
	home, _ := sshHomeDirectory()
	return filepath.Join(home, ".ssh", deployKeyName(siteID))
}

func generateStagedSSHKey(ctx context.Context, siteID int) (string, string, func(), error) {
	directory, err := os.MkdirTemp("", fmt.Sprintf("fluxo-site-%d-key-", siteID))
	if err != nil {
		return "", "", nil, err
	}
	if err := os.Chmod(directory, 0700); err != nil {
		_ = os.RemoveAll(directory)
		return "", "", nil, err
	}
	privatePath := filepath.Join(directory, "deploy_key")
	cleanup := func() { _ = os.RemoveAll(directory) }
	if _, err := syscmd.Run(ctx, 10*time.Second, "ssh-keygen", "-t", "ed25519", "-N", "", "-f", privatePath, "-C", fmt.Sprintf("fluxo-site-%d", siteID)); err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("failed to generate ssh key: %w", err)
	}
	public, err := readStagedKeyFile(privatePath + ".pub")
	if err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("failed to read public key: %w", err)
	}
	return privatePath, string(public), cleanup, nil
}

func readStagedKeyFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("staged SSH key is not a regular file")
	}
	return os.ReadFile(path)
}

// GenerateSSHKey creates an Ed25519 keypair and returns (privPath, pubKeyContent, error).
func GenerateSSHKey(ctx context.Context, siteID int) (string, string, error) {
	deployKeysMu.Lock()
	defer deployKeysMu.Unlock()

	directory, err := openDeployKeyDirectory(true)
	if err != nil {
		return "", "", err
	}
	defer directory.Close()
	name := deployKeyName(siteID)
	private, _, err := directory.ReadFile(name)
	if err != nil {
		return "", "", err
	}
	if private == nil {
		stagedPath, _, cleanup, err := generateStagedSSHKey(ctx, siteID)
		if err != nil {
			return "", "", err
		}
		defer cleanup()
		if err := replaceSSHKeyPair(directory, name, stagedPath); err != nil {
			return "", "", err
		}
	}
	public, _, err := directory.ReadFile(name + ".pub")
	if err != nil || public == nil {
		if err == nil {
			err = errors.New("public deploy key is missing")
		}
		return "", "", fmt.Errorf("failed to read public key: %w", err)
	}
	path, err := directory.Path(name)
	return path, string(public), err
}

// GenerateTemporarySSHKey creates a staged keypair for rotating a site's deploy key.
func GenerateTemporarySSHKey(ctx context.Context, siteID int) (string, string, func(), error) {
	return generateStagedSSHKey(ctx, siteID)
}

// ReplaceSSHKeyPair safely swaps a staged keypair into the site's canonical key path.
func ReplaceSSHKeyPair(siteID int, stagedPrivPath string) error {
	deployKeysMu.Lock()
	defer deployKeysMu.Unlock()

	directory, err := openDeployKeyDirectory(true)
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := replaceSSHKeyPair(directory, deployKeyName(siteID), stagedPrivPath); err != nil {
		return err
	}
	_ = os.Remove(stagedPrivPath)
	_ = os.Remove(stagedPrivPath + ".pub")
	return nil
}

func replaceSSHKeyPair(directory *sshservice.ManagedSSHDirectory, name, stagedPrivPath string) error {
	stagedPrivate, err := readStagedKeyFile(stagedPrivPath)
	if err != nil {
		return err
	}
	stagedPublic, err := readStagedKeyFile(stagedPrivPath + ".pub")
	if err != nil {
		return err
	}
	previousPrivate, previousPrivateStat, err := directory.ReadFile(name)
	if err != nil {
		return err
	}
	previousPublic, previousPublicStat, err := directory.ReadFile(name + ".pub")
	if err != nil {
		return err
	}
	restore := func(filename string, previous []byte, existed bool, mode os.FileMode) error {
		if existed {
			return directory.WriteFileAtomic(filename, previous, mode)
		}
		return directory.RemoveFile(filename)
	}
	if err := directory.WriteFileAtomic(name, stagedPrivate, 0600); err != nil {
		return err
	}
	if err := directory.WriteFileAtomic(name+".pub", stagedPublic, 0644); err != nil {
		rollbackErr := errors.Join(
			restore(name, previousPrivate, previousPrivateStat != nil, 0600),
			restore(name+".pub", previousPublic, previousPublicStat != nil, 0644),
		)
		if rollbackErr != nil {
			return fmt.Errorf("install deploy key: %w (rollback failed: %v)", err, rollbackErr)
		}
		return fmt.Errorf("install deploy key: %w", err)
	}
	return nil
}

// RemoveSSHKeyPair removes a site's managed deploy key without following a
// replaced .ssh pathname.
func RemoveSSHKeyPair(siteID int) error {
	deployKeysMu.Lock()
	defer deployKeysMu.Unlock()

	directory, err := openDeployKeyDirectory(false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer directory.Close()
	name := deployKeyName(siteID)
	if err := directory.RemoveFile(name); err != nil {
		return err
	}
	return directory.RemoveFile(name + ".pub")
}
