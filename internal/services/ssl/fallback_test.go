package ssl

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnsureNginxFallbackCertificateRepairsCorruptFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	certPath, keyPath, err := ensureNginxFallbackCertificate(dir, now)
	if err != nil {
		t.Fatalf("create fallback certificate: %v", err)
	}
	if err := os.WriteFile(certPath, []byte("corrupt certificate"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("corrupt key"), 0600); err != nil {
		t.Fatal(err)
	}

	certPath, keyPath, err = ensureNginxFallbackCertificate(dir, now)
	if err != nil {
		t.Fatalf("repair fallback certificate: %v", err)
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !validFallbackCertificate(certPEM, keyPEM, now) {
		t.Fatal("repaired fallback certificate is not valid")
	}
}

func TestEnsureNginxFallbackCertificateRepairsMismatchedPair(t *testing.T) {
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	otherDir := t.TempDir()
	certPath, keyPath, err := ensureNginxFallbackCertificate(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	_, otherKeyPath, err := ensureNginxFallbackCertificate(otherDir, now)
	if err != nil {
		t.Fatal(err)
	}
	otherKey, err := os.ReadFile(otherKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, otherKey, 0600); err != nil {
		t.Fatal(err)
	}

	certPath, keyPath, err = ensureNginxFallbackCertificate(dir, now)
	if err != nil {
		t.Fatalf("repair mismatched fallback pair: %v", err)
	}
	certPEM, _ := os.ReadFile(certPath)
	keyPEM, _ := os.ReadFile(keyPath)
	if !validFallbackCertificate(certPEM, keyPEM, now) {
		t.Fatal("mismatched fallback pair was not repaired")
	}
}

func TestEnsureNginxFallbackCertificateActivatesOnlyCompletePair(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	certPath, keyPath, err := ensureNginxFallbackCertificate(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(filepath.Dir(certPath)) != "current" || filepath.Base(filepath.Dir(keyPath)) != "current" {
		t.Fatalf("fallback paths are not stable current paths: %s %s", certPath, keyPath)
	}
	currentTarget, err := os.Readlink(filepath.Join(dir, "current"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "server-orphan.crt"), []byte("incomplete"), 0644); err != nil {
		t.Fatal(err)
	}

	activeCert, activeKey, err := ensureNginxFallbackCertificate(dir, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if activeCert != certPath || activeKey != keyPath {
		t.Fatalf("unreferenced files changed the active pair: %s %s", activeCert, activeKey)
	}
	if target, err := os.Readlink(filepath.Join(dir, "current")); err != nil || target != currentTarget {
		t.Fatalf("unreferenced files changed the active pair link: target=%q err=%v", target, err)
	}
}

func TestEnsureNginxFallbackCertificateKeepsValidPair(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	certPath, keyPath, err := ensureNginxFallbackCertificate(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	certBefore, _ := os.ReadFile(certPath)
	keyBefore, _ := os.ReadFile(keyPath)
	if err := os.Chmod(keyPath, 0644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := ensureNginxFallbackCertificate(dir, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	certAfter, _ := os.ReadFile(certPath)
	keyAfter, _ := os.ReadFile(keyPath)
	if !bytes.Equal(certBefore, certAfter) || !bytes.Equal(keyBefore, keyAfter) {
		t.Fatal("valid fallback pair was unexpectedly regenerated")
	}
	if mode := fileMode(t, keyPath); mode.Perm() != 0600 {
		t.Fatalf("fallback private key permissions are %o, want 600", mode.Perm())
	}
}

func TestEnsureNginxFallbackCertificateAcceptsLegacyPair(t *testing.T) {
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	sourceDir := t.TempDir()
	sourceCert, sourceKey, err := ensureNginxFallbackCertificate(sourceDir, now)
	if err != nil {
		t.Fatal(err)
	}
	certPEM, _ := os.ReadFile(sourceCert)
	keyPEM, _ := os.ReadFile(sourceKey)

	legacyDir := t.TempDir()
	legacyCert := filepath.Join(legacyDir, "server.crt")
	legacyKey := filepath.Join(legacyDir, "server.key")
	if err := os.WriteFile(legacyCert, certPEM, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyKey, keyPEM, 0600); err != nil {
		t.Fatal(err)
	}

	certPath, keyPath, err := ensureNginxFallbackCertificate(legacyDir, now)
	if err != nil {
		t.Fatal(err)
	}
	if certPath == legacyCert || keyPath == legacyKey {
		t.Fatalf("legacy pair was not migrated to stable current paths: %s %s", certPath, keyPath)
	}
	migratedCert, _ := os.ReadFile(certPath)
	migratedKey, _ := os.ReadFile(keyPath)
	if !bytes.Equal(migratedCert, certPEM) || !bytes.Equal(migratedKey, keyPEM) {
		t.Fatal("legacy pair contents changed during migration")
	}
}

func TestValidFallbackCertificateRejectsExpiringCertificate(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	certPath, keyPath, err := ensureNginxFallbackCertificate(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	certPEM, _ := os.ReadFile(certPath)
	keyPEM, _ := os.ReadFile(keyPath)
	if validFallbackCertificate(certPEM, keyPEM, now.Add(10*365*24*time.Hour)) {
		t.Fatal("expired fallback certificate was accepted")
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()
}
