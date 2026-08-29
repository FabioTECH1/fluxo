package ssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	xssh "golang.org/x/crypto/ssh"
	"golang.org/x/sys/unix"
)

var supportedPublicKeyTypes = map[string]bool{
	xssh.KeyAlgoRSA:      true,
	xssh.KeyAlgoED25519:  true,
	xssh.KeyAlgoECDSA256: true,
	xssh.KeyAlgoECDSA384: true,
	xssh.KeyAlgoECDSA521: true,
}

var (
	keysMu                  sync.Mutex
	ErrFinalAuthorizedKey   = errors.New("cannot remove the final SSH key while password login is disabled")
	ErrSSHPolicyUnavailable = errors.New("unable to verify the effective SSH access policy")
)

func fluxoIdentity() (int, int, error) {
	if os.Getenv("FLUXO_ENV") != "prod" {
		return -1, -1, nil
	}
	u, err := user.Lookup("fluxo")
	if err != nil {
		return 0, 0, fmt.Errorf("look up fluxo system user: %w", err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse fluxo user ID: %w", err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse fluxo group ID: %w", err)
	}
	return uid, gid, nil
}

func authorizedKeysPath() string {
	if os.Getenv("FLUXO_ENV") == "prod" {
		return "/home/fluxo/.ssh/authorized_keys"
	}
	dataDir := os.Getenv("FLUXO_DATA_DIR")
	if dataDir == "" {
		dataDir = "."
	}
	return filepath.Join(dataDir, ".ssh", "authorized_keys")
}

type authorizedKeysStore struct {
	*ManagedSSHDirectory
	path string
}

func openAuthorizedKeysStore(create bool) (*authorizedKeysStore, error) {
	path := authorizedKeysPath()
	homeDir := filepath.Dir(filepath.Dir(path))
	if create && os.Getenv("FLUXO_ENV") != "prod" {
		if err := os.MkdirAll(homeDir, 0700); err != nil {
			return nil, err
		}
	}
	uid, gid, err := fluxoIdentity()
	if err != nil {
		return nil, err
	}
	directory, err := OpenManagedSSHDirectory(homeDir, create, uid, gid)
	if err != nil {
		return nil, err
	}
	return &authorizedKeysStore{ManagedSSHDirectory: directory, path: path}, nil
}

func (s *authorizedKeysStore) close() {
	if s != nil {
		s.Close()
	}
}

func (s *authorizedKeysStore) read() ([]byte, *unix.Stat_t, error) {
	return s.ReadFile(filepath.Base(s.path))
}

func (s *authorizedKeysStore) write(content []byte) error {
	return s.WriteFileAtomic(filepath.Base(s.path), content, 0600)
}

// ValidatePublicKey parses one public key and rejects options, unsupported key
// algorithms, multiple keys, and trailing malformed data.
func ValidatePublicKey(value string) error {
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return errors.New("SSH public key must contain exactly one line")
	}
	key, _, options, rest, err := xssh.ParseAuthorizedKey([]byte(strings.TrimSpace(value)))
	if err != nil || len(bytes.TrimSpace(rest)) != 0 {
		return errors.New("invalid SSH public key")
	}
	if len(options) != 0 {
		return errors.New("SSH key options are not supported in the Fluxo key form")
	}
	if !supportedPublicKeyTypes[key.Type()] {
		return fmt.Errorf("unsupported SSH public key type %q", key.Type())
	}
	return nil
}

func parseAuthorizedKeys(content []byte) ([]xssh.PublicKey, error) {
	remaining := bytes.TrimSpace(content)
	keys := make([]xssh.PublicKey, 0)
	for len(remaining) > 0 {
		key, _, _, rest, err := xssh.ParseAuthorizedKey(remaining)
		if err != nil {
			return nil, fmt.Errorf("authorized_keys contains an invalid entry: %w", err)
		}
		keys = append(keys, key)
		remaining = bytes.TrimSpace(rest)
	}
	return keys, nil
}

func publicKeyIdentity(value string) ([]byte, error) {
	key, _, _, rest, err := xssh.ParseAuthorizedKey([]byte(strings.TrimSpace(value)))
	if err != nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("invalid SSH public key")
	}
	return key.Marshal(), nil
}

// AddKey atomically adds a validated, non-duplicate public key.
func AddKey(publicKey string) error {
	keysMu.Lock()
	defer keysMu.Unlock()
	return addKey(publicKey)
}

func addKey(publicKey string) error {
	if err := ValidatePublicKey(publicKey); err != nil {
		return err
	}
	store, err := openAuthorizedKeysStore(true)
	if err != nil {
		return err
	}
	defer store.close()
	content, _, err := store.read()
	if err != nil {
		return err
	}
	existing, err := parseAuthorizedKeys(content)
	if err != nil {
		return err
	}
	identity, err := publicKeyIdentity(publicKey)
	if err != nil {
		return err
	}
	for _, key := range existing {
		if bytes.Equal(key.Marshal(), identity) {
			return errors.New("SSH public key is already installed")
		}
	}
	trimmed := bytes.TrimSpace(content)
	content = make([]byte, 0, len(trimmed)+len(publicKey)+2)
	if len(trimmed) > 0 {
		content = append(content, trimmed...)
		content = append(content, '\n')
	}
	content = append(content, []byte(strings.TrimSpace(publicKey)+"\n")...)
	return store.write(content)
}

func contentWithoutKey(content []byte, publicKey string) ([]byte, error) {
	identity, err := publicKeyIdentity(publicKey)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(content), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		key, _, _, rest, parseErr := xssh.ParseAuthorizedKey([]byte(trimmed))
		if parseErr != nil || len(bytes.TrimSpace(rest)) != 0 {
			return nil, errors.New("authorized_keys contains an invalid entry")
		}
		if !bytes.Equal(key.Marshal(), identity) {
			kept = append(kept, line)
		}
	}
	if len(kept) == 0 {
		return nil, nil
	}
	return []byte(strings.Join(kept, "\n") + "\n"), nil
}

// RemainingKeyCount returns the number of valid public keys that would remain
// after removing publicKey.
func RemainingKeyCount(publicKey string) (int, error) {
	keysMu.Lock()
	defer keysMu.Unlock()
	return remainingKeyCount(publicKey)
}

func remainingKeyCount(publicKey string) (int, error) {
	store, err := openAuthorizedKeysStore(false)
	if err != nil {
		return 0, err
	}
	defer store.close()
	content, _, err := store.read()
	if err != nil {
		return 0, err
	}
	remaining, err := contentWithoutKey(content, publicKey)
	if err != nil {
		return 0, err
	}
	return usableAuthorizedKeyCount(remaining)
}

// AuthorizedKeyCount validates the Fluxo authorized_keys file and returns its
// key count. It does not create the file as a side effect.
func AuthorizedKeyCount() (int, error) {
	keysMu.Lock()
	defer keysMu.Unlock()
	return authorizedKeyCount()
}

func authorizedKeyCount() (int, error) {
	store, err := openAuthorizedKeysStore(false)
	if err != nil {
		return 0, err
	}
	defer store.close()
	content, stat, err := store.read()
	if err != nil {
		return 0, err
	}
	if stat == nil {
		return 0, os.ErrNotExist
	}
	if err := validateAuthorizedKeysSecurity(store, stat); err != nil {
		return 0, err
	}
	return usableAuthorizedKeyCount(content)
}

func usableAuthorizedKeyCount(content []byte) (int, error) {
	remaining := bytes.TrimSpace(content)
	count := 0
	for len(remaining) > 0 {
		key, _, options, rest, err := xssh.ParseAuthorizedKey(remaining)
		if err != nil {
			return 0, fmt.Errorf("authorized_keys contains an invalid entry: %w", err)
		}
		if len(options) == 0 && supportedPublicKeyTypes[key.Type()] {
			count++
		}
		remaining = bytes.TrimSpace(rest)
	}
	return count, nil
}

func validateAuthorizedKeysSecurity(store *authorizedKeysStore, fileStat *unix.Stat_t) error {
	if os.Getenv("FLUXO_ENV") != "prod" {
		return nil
	}
	uid, _, err := fluxoIdentity()
	if err != nil {
		return err
	}
	var directoryStat unix.Stat_t
	if err := unix.Fstat(int(store.ManagedSSHDirectory.directory.Fd()), &directoryStat); err != nil {
		return err
	}
	for _, item := range []struct {
		path string
		stat *unix.Stat_t
	}{
		{path: filepath.Dir(store.path), stat: &directoryStat},
		{path: store.path, stat: fileStat},
	} {
		if item.stat.Mode&0022 != 0 {
			return fmt.Errorf("%s is writable by group or other users", item.path)
		}
		if item.stat.Uid != uint32(uid) && item.stat.Uid != 0 {
			return fmt.Errorf("%s is not owned by fluxo or root", item.path)
		}
	}
	return nil
}

// RemoveKey atomically removes a public key from authorized_keys.
func RemoveKey(publicKey string) error {
	keysMu.Lock()
	defer keysMu.Unlock()
	return removeKey(publicKey)
}

func removeKey(publicKey string) error {
	store, err := openAuthorizedKeysStore(true)
	if err != nil {
		return err
	}
	defer store.close()
	content, _, err := store.read()
	if err != nil {
		return err
	}
	content, err = contentWithoutKey(content, publicKey)
	if err != nil {
		return err
	}
	return store.write(content)
}

// RemoveKeySafely makes the effective-policy check and key removal one atomic
// operation relative to hardening activation and other key mutations.
func RemoveKeySafely(ctx context.Context, publicKey string) error {
	keysMu.Lock()
	defer keysMu.Unlock()
	status := inspectSecurityStatus(ctx, defaultHardeningEnvironment())
	if !status.Available {
		return ErrSSHPolicyUnavailable
	}
	if !status.PasswordLoginEnabled {
		remaining, err := remainingKeyCount(publicKey)
		if err != nil {
			return err
		}
		if remaining < 1 {
			return ErrFinalAuthorizedKey
		}
	}
	return removeKey(publicKey)
}
