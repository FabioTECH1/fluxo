package backup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/syscmd"
)

func TestEncryptArtifactsProducesDecryptableOpenPGPFile(t *testing.T) {
	if _, err := os.Stat("/usr/bin/gpg"); err != nil {
		t.Skip("gpg is not installed")
	}
	gnupgHome := t.TempDir()
	if err := os.Chmod(gnupgHome, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GNUPGHOME", gnupgHome)

	workDir := t.TempDir()
	plaintext := []byte("Fluxo encrypted backup fixture\n")
	source := filepath.Join(workDir, "site-files.tar.gz")
	if err := os.WriteFile(source, plaintext, 0600); err != nil {
		t.Fatal(err)
	}
	artifacts := []localArtifact{{BackupArtifact: database.BackupArtifact{
		RunID: "run-1", Kind: "files", Filename: "site-files.tar.gz",
	}, Path: source}}

	encrypted, err := encryptArtifacts(context.Background(), artifacts, "correct horse battery staple", workDir)
	if err != nil {
		t.Fatalf("encrypt artifact: %v", err)
	}
	if len(encrypted) != 1 || encrypted[0].Filename != "site-files.tar.gz.gpg" {
		t.Fatalf("encrypted artifacts = %+v", encrypted)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("plaintext artifact still exists: %v", err)
	}
	ciphertext, err := os.ReadFile(encrypted[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ciphertext), string(plaintext)) {
		t.Fatal("encrypted artifact contains plaintext")
	}

	restored := filepath.Join(workDir, "restored.tar.gz")
	if _, err := syscmd.RunStdin(context.Background(), time.Minute, "correct horse battery staple\n", "gpg",
		"--batch", "--yes", "--no-tty", "--pinentry-mode", "loopback", "--passphrase-fd", "0",
		"--output", restored, "--decrypt", encrypted[0].Path); err != nil {
		t.Fatalf("decrypt artifact: %v", err)
	}
	got, err := os.ReadFile(restored)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("decrypted artifact = %q, want %q", got, plaintext)
	}
}
