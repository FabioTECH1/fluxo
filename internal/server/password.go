package server

import (
	"crypto/sha256"
	"encoding/hex"

	"golang.org/x/crypto/bcrypt"
)

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func verifyPassword(password, storedHash string) bool {
	if len(storedHash) == 64 && isHex(storedHash) {
		shaHash := sha256.Sum256([]byte(password))
		shaStr := hex.EncodeToString(shaHash[:])
		return shaStr == storedHash
	}
	err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password))
	return err == nil
}

func isLegacySHA256(hash string) bool {
	return len(hash) == 64 && isHex(hash)
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
