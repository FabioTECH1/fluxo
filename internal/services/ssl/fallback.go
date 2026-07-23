package ssl

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const nginxFallbackDir = "/etc/nginx/ssl/fluxo-fallback"

type fallbackManifest struct {
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"private_key"`
}

var nginxFallbackMu sync.Mutex

// EnsureNginxFallbackCertificate provides stable paths backed by an atomically
// selected certificate directory. Every Nginx config can safely share them.
func EnsureNginxFallbackCertificate() (string, string, error) {
	nginxFallbackMu.Lock()
	defer nginxFallbackMu.Unlock()
	return ensureNginxFallbackCertificate(nginxFallbackDir, time.Now())
}

func ensureNginxFallbackCertificate(dir string, now time.Time) (string, string, error) {
	certPath, keyPath := stableFallbackPaths(dir)
	if secureFallbackCertificate(certPath, keyPath, now) {
		return certPath, keyPath, nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create Nginx fallback certificate directory: %w", err)
	}

	certPEM, keyPEM, ok := legacyFallbackCertificate(dir, now)
	if !ok {
		var err error
		certPEM, keyPEM, err = generateFallbackCertificate(now)
		if err != nil {
			return "", "", err
		}
	}
	if err := activateFallbackCertificate(dir, certPEM, keyPEM, now); err != nil {
		return "", "", err
	}
	return certPath, keyPath, nil
}

func stableFallbackPaths(dir string) (string, string) {
	current := filepath.Join(dir, "current")
	return filepath.Join(current, "server.crt"), filepath.Join(current, "server.key")
}

func legacyFallbackCertificate(dir string, now time.Time) ([]byte, []byte, bool) {
	manifestData, err := os.ReadFile(filepath.Join(dir, "current.json"))
	if err == nil {
		var manifest fallbackManifest
		if json.Unmarshal(manifestData, &manifest) == nil && validFallbackFilename(manifest.Certificate) && validFallbackFilename(manifest.PrivateKey) {
			if certPEM, keyPEM, ok := readValidFallbackPair(
				filepath.Join(dir, manifest.Certificate),
				filepath.Join(dir, manifest.PrivateKey),
				now,
			); ok {
				return certPEM, keyPEM, true
			}
		}
	}

	certPEM, keyPEM, ok := readValidFallbackPair(
		filepath.Join(dir, "server.crt"),
		filepath.Join(dir, "server.key"),
		now,
	)
	return certPEM, keyPEM, ok
}

func validFallbackFilename(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name
}

func readValidFallbackPair(certPath, keyPath string, now time.Time) ([]byte, []byte, bool) {
	certPEM, certErr := os.ReadFile(certPath)
	keyPEM, keyErr := os.ReadFile(keyPath)
	if certErr != nil || keyErr != nil || !validFallbackCertificate(certPEM, keyPEM, now) {
		return nil, nil, false
	}
	return certPEM, keyPEM, true
}

func secureFallbackCertificate(certPath, keyPath string, now time.Time) bool {
	if _, _, ok := readValidFallbackPair(certPath, keyPath, now); !ok {
		return false
	}
	return os.Chmod(certPath, 0644) == nil && os.Chmod(keyPath, 0600) == nil
}

func generateFallbackCertificate(now time.Time) ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate Nginx fallback private key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate Nginx fallback certificate serial: %w", err)
	}
	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "HTTPS not configured",
			Organization: []string{"Fluxo"},
		},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"invalid.fluxo.local"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Nginx fallback certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to encode Nginx fallback private key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), nil
}

func activateFallbackCertificate(dir string, certPEM, keyPEM []byte, now time.Time) error {
	digest := sha256.Sum256(certPEM)
	pairID := hex.EncodeToString(digest[:16])
	pairDir := filepath.Join(dir, "pairs", pairID)
	if err := os.MkdirAll(pairDir, 0755); err != nil {
		return fmt.Errorf("failed to create Nginx fallback pair directory: %w", err)
	}
	if err := writeFallbackFile(filepath.Join(pairDir, "server.crt"), certPEM, 0644); err != nil {
		return fmt.Errorf("failed to install Nginx fallback certificate: %w", err)
	}
	if err := writeFallbackFile(filepath.Join(pairDir, "server.key"), keyPEM, 0600); err != nil {
		return fmt.Errorf("failed to install Nginx fallback private key: %w", err)
	}
	if !secureFallbackCertificate(filepath.Join(pairDir, "server.crt"), filepath.Join(pairDir, "server.key"), now) {
		return fmt.Errorf("installed Nginx fallback certificate pair is invalid")
	}

	currentPath := filepath.Join(dir, "current")
	temporaryLink := currentPath + ".tmp"
	_ = os.Remove(temporaryLink)
	if err := os.Symlink(filepath.Join("pairs", pairID), temporaryLink); err != nil {
		return fmt.Errorf("failed to stage Nginx fallback certificate activation: %w", err)
	}
	defer os.Remove(temporaryLink)
	if err := os.Rename(temporaryLink, currentPath); err != nil {
		return fmt.Errorf("failed to activate Nginx fallback certificate pair: %w", err)
	}
	return nil
}

func validFallbackCertificate(certPEM, keyPEM []byte, now time.Time) bool {
	certBlock, _ := pem.Decode(certPEM)
	keyBlock, _ := pem.Decode(keyPEM)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" || keyBlock == nil || keyBlock.Type != "EC PRIVATE KEY" {
		return false
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil || now.Before(cert.NotBefore) || !now.Add(24*time.Hour).Before(cert.NotAfter) {
		return false
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return false
	}
	publicKey, ok := cert.PublicKey.(*ecdsa.PublicKey)
	return ok && publicKey.Curve == key.Curve && publicKey.X.Cmp(key.X) == 0 && publicKey.Y.Cmp(key.Y) == 0
}

func writeFallbackFile(path string, contents []byte, mode os.FileMode) error {
	temporaryPath := path + ".tmp"
	defer os.Remove(temporaryPath)
	if err := os.WriteFile(temporaryPath, contents, mode); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
