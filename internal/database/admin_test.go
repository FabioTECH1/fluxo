package database

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadAdminUsername(t *testing.T) {
	previousDB := DB
	dbPath := filepath.Join(t.TempDir(), "fluxo.db")
	if err := InitDB(dbPath); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	t.Cleanup(func() {
		if DB != nil {
			DB.Close()
		}
		DB = previousDB
	})

	username, err := ReadAdminUsername(dbPath)
	if err != nil {
		t.Fatalf("ReadAdminUsername() empty database error = %v", err)
	}
	if username != "" {
		t.Fatalf("ReadAdminUsername() empty database = %q, want empty", username)
	}

	if _, err := DB.Exec("INSERT INTO users (username, token_hash) VALUES (?, ?)", "admin", "hash"); err != nil {
		t.Fatalf("insert admin user: %v", err)
	}
	username, err = ReadAdminUsername(dbPath)
	if err != nil {
		t.Fatalf("ReadAdminUsername() error = %v", err)
	}
	if username != "admin" {
		t.Fatalf("ReadAdminUsername() = %q, want %q", username, "admin")
	}

	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	relativePath, err := filepath.Rel(workingDir, dbPath)
	if err != nil {
		t.Fatalf("build relative database path: %v", err)
	}
	username, err = ReadAdminUsername(relativePath)
	if err != nil {
		t.Fatalf("ReadAdminUsername() relative path error = %v", err)
	}
	if username != "admin" {
		t.Fatalf("ReadAdminUsername() relative path = %q, want %q", username, "admin")
	}
}

func TestReadAdminUsernameDoesNotCreateMissingDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "missing.db")
	if _, err := ReadAdminUsername(dbPath); err == nil {
		t.Fatal("ReadAdminUsername() should fail for a missing database")
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("ReadAdminUsername() created missing database, stat error = %v", err)
	}
}
