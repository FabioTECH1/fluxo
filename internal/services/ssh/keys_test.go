package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	xssh "golang.org/x/crypto/ssh"
)

func testPublicKey(t *testing.T, comment string) string {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := xssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(xssh.MarshalAuthorizedKey(key))) + " " + comment
}

func TestValidatePublicKeyRequiresOneSupportedRealKey(t *testing.T) {
	valid := testPublicKey(t, "laptop")
	if err := ValidatePublicKey(valid); err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
	for _, invalid := range []string{
		"ssh-ed25519 AAAAC3... fake",
		valid + "\n" + valid,
		"from=\"192.0.2.1\" " + valid,
	} {
		if err := ValidatePublicKey(invalid); err == nil {
			t.Fatalf("invalid key accepted: %q", invalid)
		}
	}
}

func TestAuthorizedKeyLifecycleIsAtomicAndPreventsDuplicates(t *testing.T) {
	t.Setenv("FLUXO_ENV", "")
	t.Setenv("FLUXO_DATA_DIR", t.TempDir())
	key := testPublicKey(t, "workstation")
	if err := AddKey(key); err != nil {
		t.Fatal(err)
	}
	if count, err := AuthorizedKeyCount(); err != nil || count != 1 {
		t.Fatalf("key count = %d, err = %v", count, err)
	}
	if err := AddKey(key); err == nil || !strings.Contains(err.Error(), "already installed") {
		t.Fatalf("duplicate key error = %v", err)
	}
	if count, err := RemainingKeyCount(key); err != nil || count != 0 {
		t.Fatalf("remaining key count = %d, err = %v", count, err)
	}
	if err := RemoveKey(key); err != nil {
		t.Fatal(err)
	}
	if count, err := AuthorizedKeyCount(); err != nil || count != 0 {
		t.Fatalf("key count after removal = %d, err = %v", count, err)
	}
}

func TestAuthorizedKeysStorePinsDirectoryAcrossSymlinkSwap(t *testing.T) {
	t.Setenv("FLUXO_ENV", "")
	dataDir := t.TempDir()
	t.Setenv("FLUXO_DATA_DIR", dataDir)
	store, err := openAuthorizedKeysStore(true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()

	sshDir := filepath.Join(dataDir, ".ssh")
	pinnedDir := filepath.Join(dataDir, ".ssh-pinned")
	outside := t.TempDir()
	if err := os.Rename(sshDir, pinnedDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, sshDir); err != nil {
		t.Fatal(err)
	}
	content := []byte(testPublicKey(t, "pinned") + "\n")
	if err := store.write(content); !errors.Is(err, ErrManagedSSHDirectoryChanged) {
		t.Fatalf("write after directory swap = %v", err)
	}

	if _, err := os.Stat(filepath.Join(pinnedDir, "authorized_keys")); !os.IsNotExist(err) {
		t.Fatalf("detached directory was modified after the swap: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "authorized_keys")); !os.IsNotExist(err) {
		t.Fatalf("write escaped through replacement symlink: %v", err)
	}
}

func TestAuthorizedKeysStoreRejectsSymlinkBeforeOpen(t *testing.T) {
	t.Setenv("FLUXO_ENV", "")
	dataDir := t.TempDir()
	t.Setenv("FLUXO_DATA_DIR", dataDir)
	if err := os.Symlink(t.TempDir(), filepath.Join(dataDir, ".ssh")); err != nil {
		t.Fatal(err)
	}
	if _, err := openAuthorizedKeysStore(true); err == nil {
		t.Fatal("expected the SSH directory symlink to be rejected")
	}
}

func TestRemainingKeyCountExcludesRestrictedLegacyKeys(t *testing.T) {
	t.Setenv("FLUXO_ENV", "")
	t.Setenv("FLUXO_DATA_DIR", t.TempDir())
	removing := testPublicKey(t, "removing")
	restricted := `from="192.0.2.1" ` + testPublicKey(t, "restricted")
	store, err := openAuthorizedKeysStore(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.write([]byte(removing + "\n" + restricted + "\n")); err != nil {
		store.close()
		t.Fatal(err)
	}
	store.close()
	count, err := RemainingKeyCount(removing)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("restricted legacy key counted as usable: %d", count)
	}
}
