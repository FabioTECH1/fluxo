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

// InitEncryption loads or generates the encryption key from FLUXO_DATA_DIR/encryption.key.
func InitEncryption(dataDir string) error {
	keyPath := filepath.Join(dataDir, "encryption.key")

	key, err := os.ReadFile(keyPath)
	if err == nil && len(key) == 32 {
		secretKey = key
		return nil
	}

	secretKey = make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, secretKey); err != nil {
		return fmt.Errorf("failed to generate encryption key: %w", err)
	}

	if err := os.WriteFile(keyPath, secretKey, 0600); err != nil {
		return fmt.Errorf("failed to write encryption key: %w", err)
	}

	return nil
}

// Encrypt encrypts a plaintext string and returns a base64-encoded ciphertext prefixed with "enc:".
// If the input is already encrypted, it returns it unchanged.
// If the input is empty or the key is not initialized, it returns the input unchanged.
func Encrypt(plaintext string) string {
	if plaintext == "" || secretKey == nil || strings.HasPrefix(plaintext, "enc:") {
		return plaintext
	}

	block, err := aes.NewCipher(secretKey)
	if err != nil {
		return plaintext
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return plaintext
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return plaintext
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return "enc:" + base64.StdEncoding.EncodeToString(ciphertext)
}

// Decrypt takes a base64-encoded ciphertext prefixed with "enc:" and returns the plaintext.
// If the input is not encrypted, it returns it unchanged.
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
