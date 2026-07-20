package ssl

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxCertificateFileSize = 1 << 20

type CertificateInspection struct {
	Certificate    *x509.Certificate
	CertificatePEM []byte
	PrivateKeyPEM  []byte
	Fingerprint    string
}

// InspectCertificateFiles verifies a managed certificate, its validity period, and its private key.
func InspectCertificateFiles(certPath, keyPath string) (*CertificateInspection, error) {
	if !isManagedCertificatePath(certPath) || !isManagedCertificatePath(keyPath) {
		return nil, fmt.Errorf("certificate files are outside Fluxo-managed SSL directories")
	}

	certPEM, err := readCertificateFile(certPath)
	if err != nil {
		return nil, err
	}
	keyPEM, err := readCertificateFile(keyPath)
	if err != nil {
		return nil, err
	}
	return InspectCertificatePEM(certPEM, keyPEM)
}

// InspectCertificatePEM verifies a PEM certificate bundle and matching private key.
func InspectCertificatePEM(certPEM, keyPEM []byte) (*CertificateInspection, error) {
	if len(certPEM) == 0 || len(keyPEM) == 0 || len(certPEM) > maxCertificateFileSize || len(keyPEM) > maxCertificateFileSize {
		return nil, fmt.Errorf("certificate or private key has an invalid size")
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("certificate and private key do not match: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return nil, fmt.Errorf("certificate bundle has no leaf certificate")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse leaf certificate: %w", err)
	}

	now := time.Now()
	if now.Before(leaf.NotBefore) {
		return nil, fmt.Errorf("certificate is not valid until %s", leaf.NotBefore.Format(time.RFC3339))
	}
	if !now.Before(leaf.NotAfter) {
		return nil, fmt.Errorf("certificate expired at %s", leaf.NotAfter.Format(time.RFC3339))
	}

	fingerprint := sha256.Sum256(leaf.Raw)
	return &CertificateInspection{
		Certificate:    leaf,
		CertificatePEM: certPEM,
		PrivateKeyPEM:  keyPEM,
		Fingerprint:    hex.EncodeToString(fingerprint[:]),
	}, nil
}

// VerifyCertificateDomains requires the certificate to cover every configured hostname.
func VerifyCertificateDomains(cert *x509.Certificate, domains []string) error {
	if cert == nil {
		return fmt.Errorf("certificate is missing")
	}
	for _, domain := range domains {
		domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
		if domain == "" {
			continue
		}
		if err := cert.VerifyHostname(domain); err != nil {
			return fmt.Errorf("certificate does not cover %s", domain)
		}
	}
	return nil
}

// CloneCertificateFiles writes an independent copy for the target site.
func CloneCertificateFiles(targetDomain string, sourceCertificateID int, inspection *CertificateInspection) (string, string, error) {
	if inspection == nil || inspection.Certificate == nil {
		return "", "", fmt.Errorf("certificate inspection is required")
	}

	targetRoot := filepath.Join("/etc/nginx/ssl", targetDomain)
	if err := os.MkdirAll(targetRoot, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create target SSL directory: %w", err)
	}
	cloneDir, err := os.MkdirTemp(targetRoot, fmt.Sprintf("cloned-%d-", sourceCertificateID))
	if err != nil {
		return "", "", fmt.Errorf("failed to create cloned certificate directory: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(cloneDir)
		}
	}()
	if err := os.Chmod(cloneDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to secure cloned certificate directory: %w", err)
	}

	certPath := filepath.Join(cloneDir, "server.crt")
	keyPath := filepath.Join(cloneDir, "server.key")
	if err := os.WriteFile(certPath, inspection.CertificatePEM, 0644); err != nil {
		return "", "", fmt.Errorf("failed to write cloned certificate: %w", err)
	}
	if err := os.WriteFile(keyPath, inspection.PrivateKeyPEM, 0600); err != nil {
		return "", "", fmt.Errorf("failed to write cloned private key: %w", err)
	}
	_ = os.Chown(keyPath, 0, 33)
	if err := os.Chmod(keyPath, 0640); err != nil {
		return "", "", fmt.Errorf("failed to secure cloned private key: %w", err)
	}

	cleanup = false
	return certPath, keyPath, nil
}

func RemoveClonedCertificateFiles(certPath string) {
	dir := filepath.Dir(certPath)
	if filepath.Base(dir) == "." || !strings.HasPrefix(filepath.Base(dir), "cloned-") {
		return
	}
	if isPathWithin("/etc/nginx/ssl", dir) {
		_ = os.RemoveAll(dir)
	}
}

// RemoveManagedCertificateFiles removes an unreferenced custom or cloned copy, but never Certbot-managed files.
func RemoveManagedCertificateFiles(certPath, keyPath string) error {
	if !isPathWithin("/etc/nginx/ssl", certPath) || !isPathWithin("/etc/nginx/ssl", keyPath) {
		return fmt.Errorf("certificate files are outside Fluxo-managed custom SSL storage")
	}
	certDir := filepath.Dir(certPath)
	keyDir := filepath.Dir(keyPath)
	if certDir == keyDir {
		base := filepath.Base(certDir)
		if strings.HasPrefix(base, "cloned-") || strings.HasPrefix(base, "custom-") {
			if err := os.RemoveAll(certDir); err != nil {
				return fmt.Errorf("failed to remove certificate directory: %w", err)
			}
			return nil
		}
	}

	var cleanupErr error
	if err := os.Remove(certPath); err != nil && !os.IsNotExist(err) {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove certificate: %w", err))
	}
	if err := os.Remove(keyPath); err != nil && !os.IsNotExist(err) {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove private key: %w", err))
	}
	return cleanupErr
}

func readCertificateFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxCertificateFileSize {
		return nil, fmt.Errorf("certificate file has an invalid size")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate file: %w", err)
	}
	return data, nil
}

func isManagedCertificatePath(path string) bool {
	return isPathWithin("/etc/nginx/ssl", path) || isPathWithin("/etc/letsencrypt/live", path)
}

func isPathWithin(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
