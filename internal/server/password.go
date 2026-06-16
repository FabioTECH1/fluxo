package server

import (
	"crypto/sha256"
	"encoding/hex"

	"golang.org/x/crypto/bcrypt"
)

// hashPassword hashes a password using bcrypt.
func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// verifyPassword checks a password against a stored hash, supporting both legacy SHA-256 and bcrypt.
func verifyPassword(password, storedHash string) bool {
	if len(storedHash) == 64 && isHex(storedHash) {
		shaHash := sha256.Sum256([]byte(password))
		shaStr := hex.EncodeToString(shaHash[:])
		return shaStr == storedHash
	}
	err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password))
	return err == nil
}

// isLegacySHA256 returns true if the hash is a 64-char hex string (legacy format).
func isLegacySHA256(hash string) bool {
	return len(hash) == 64 && isHex(hash)
}

// isHex returns true if all characters are lowercase hex digits.
func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
