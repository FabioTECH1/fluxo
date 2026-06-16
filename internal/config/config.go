// Package config reads Fluxo's runtime configuration from environment variables with sensible defaults.
package config

import (
	"os"
	"path/filepath"
)

// Config holds the resolved runtime parameters for the Fluxo daemon.
type Config struct {
	Port    string // HTTP listen port (e.g. "8080")
	DBPath  string // Absolute path to the SQLite database file
	Env     string // "dev" or "prod" — controls data directory defaults
	DataDir string // Root directory for persistent state (DB, keys, etc.)
}

// LoadConfig reads FLUXO_ENV, FLUXO_PORT, and FLUXO_DATA_DIR and resolves the full configuration with defaults.
func LoadConfig() *Config {
	env := os.Getenv("FLUXO_ENV")
	if env == "" {
		env = "dev"
	}

	port := os.Getenv("FLUXO_PORT")
	if port == "" {
		port = "9595"
	}

	dataDir := os.Getenv("FLUXO_DATA_DIR")
	if dataDir == "" {
		if env == "prod" {
			dataDir = "/var/lib/fluxo"
		} else {
			dataDir = "."
		}
	}

	// Ensure data directory exists in production.
	if env == "prod" {
		os.MkdirAll(dataDir, 0755)
	}

	dbPath := filepath.Join(dataDir, "fluxo.db")

	return &Config{
		Port:    port,
		DBPath:  dbPath,
		Env:     env,
		DataDir: dataDir,
	}
}
