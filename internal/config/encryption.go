package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var secretKey []byte

// InitEncryption loads or generates the AES-256 encryption key from dataDir/encryption.key.
func InitEncryption(dataDir string) error {
	keyPath := filepath.Join(dataDir, "encryption.key")

	info, statErr := os.Lstat(keyPath)
	if statErr == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("encryption key must be a regular file")
		}
		if info.Mode().Perm()&0077 != 0 {
			return fmt.Errorf("encryption key permissions are too broad; expected owner-only access")
		}
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("failed to inspect encryption key: %w", statErr)
	}

	key, err := os.ReadFile(keyPath)
	if err == nil {
		if len(key) != 32 {
			return fmt.Errorf("encryption key has invalid length: expected 32 bytes")
		}
		secretKey = key
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read encryption key: %w", err)
	}

	newKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, newKey); err != nil {
		return fmt.Errorf("failed to generate encryption key: %w", err)
	}

	file, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("failed to create encryption key: %w", err)
	}
	if _, err := file.Write(newKey); err != nil {
		file.Close()
		return fmt.Errorf("failed to write encryption key: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("failed to sync encryption key: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to write encryption key: %w", err)
	}
	secretKey = newKey
	return nil
}

// Encrypt returns a base64-encoded AES-GCM ciphertext prefixed with "enc:", or the input unchanged if empty/already encrypted.
func Encrypt(plaintext string) string {
	if plaintext == "" || secretKey == nil || strings.HasPrefix(plaintext, "enc:") {
		return plaintext
	}

	ciphertext, err := EncryptSecret(plaintext)
	if err != nil {
		return plaintext
	}
	return ciphertext
}

// EncryptSecret encrypts a plaintext secret with AES-GCM and reports failures.
// Security-sensitive callers should use this instead of the backwards-compatible Encrypt helper.
func EncryptSecret(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if secretKey == nil {
		return "", fmt.Errorf("encryption is not initialized")
	}

	block, err := aes.NewCipher(secretKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return "enc:" + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt reverses Encrypt, returning the plaintext from an "enc:"-prefixed ciphertext, or the input unchanged.
func Decrypt(ciphertext string) string {
	if ciphertext == "" || secretKey == nil || !strings.HasPrefix(ciphertext, "enc:") {
		return ciphertext
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ciphertext, "enc:"))
	if err != nil {
		return ciphertext
	}

	block, err := aes.NewCipher(secretKey)
	if err != nil {
		return ciphertext
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return ciphertext
	}

	nonceSize := gcm.NonceSize()
	if len(decoded) < nonceSize {
		return ciphertext
	}

	nonce, ciphertextBytes := decoded[:nonceSize], decoded[nonceSize:]
	plaintextBytes, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return ciphertext
	}

	return string(plaintextBytes)
}
