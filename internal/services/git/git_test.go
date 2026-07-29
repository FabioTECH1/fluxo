package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceSSHKeyPair(t *testing.T) {
	t.Setenv("FLUXO_ENV", "")
	t.Setenv("HOME", t.TempDir())

	target := GetSSHKeyPath(42)
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old-private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target+".pub", []byte("old-public"), 0644); err != nil {
		t.Fatal(err)
	}

	staged := filepath.Join(filepath.Dir(target), "staged_key")
	if err := os.WriteFile(staged, []byte("new-private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged+".pub", []byte("new-public"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ReplaceSSHKeyPair(42, staged); err != nil {
		t.Fatal(err)
	}

	if got := readTestFile(t, target); got != "new-private" {
		t.Fatalf("private key = %q, want new-private", got)
	}
	if got := readTestFile(t, target+".pub"); got != "new-public" {
		t.Fatalf("public key = %q, want new-public", got)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("staged private key still exists or stat failed: %v", err)
	}
}

func TestReplaceSSHKeyPairRestoresExistingKeyOnFailure(t *testing.T) {
	t.Setenv("FLUXO_ENV", "")
	t.Setenv("HOME", t.TempDir())

	target := GetSSHKeyPath(43)
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old-private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target+".pub", []byte("old-public"), 0644); err != nil {
		t.Fatal(err)
	}

	staged := filepath.Join(filepath.Dir(target), "staged_key")
	if err := os.WriteFile(staged, []byte("new-private"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := ReplaceSSHKeyPair(43, staged); err == nil {
		t.Fatal("expected missing staged public key to fail")
	}

	if got := readTestFile(t, target); got != "old-private" {
		t.Fatalf("private key = %q, want old-private", got)
	}
	if got := readTestFile(t, target+".pub"); got != "old-public" {
		t.Fatalf("public key = %q, want old-public", got)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}
