package ssl

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"
)

// GetCertExpiry reads a PEM certificate file and returns the NotAfter date.
// Returns time.Time{} and an error if the file cannot be read or parsed.
func GetCertExpiry(certPath string) (time.Time, error) {
	pemBytes, err := os.ReadFile(certPath)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to read cert file %s: %w", certPath, err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return time.Time{}, fmt.Errorf("no PEM block found in %s", certPath)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse certificate %s: %w", certPath, err)
	}
	return cert.NotAfter, nil
}

// ParseCertExpiryFromPEM parses a PEM-encoded certificate string and returns the NotAfter date.
func ParseCertExpiryFromPEM(pemContent string) (time.Time, error) {
	block, _ := pem.Decode([]byte(pemContent))
	if block == nil {
		return time.Time{}, fmt.Errorf("no PEM block found in provided content")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse certificate: %w", err)
	}
	return cert.NotAfter, nil
}
