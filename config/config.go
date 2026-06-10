// Package config reads Fluxo's runtime configuration from environment variables.
// All values have sensible defaults so a zero-config dev startup works.
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

// LoadConfig reads FLUXO_ENV, FLUXO_PORT, and FLUXO_DATA_DIR from the
// environment and resolves the full configuration with defaults.
// In prod mode it ensures the data directory exists.
func LoadConfig() *Config {
	// Environment: defaults to "dev" for local development.
	env := os.Getenv("FLUXO_ENV")
	if env == "" {
		env = "dev"
	}

	// HTTP port: defaults to 9595.
	port := os.Getenv("FLUXO_PORT")
	if port == "" {
		port = "9595"
	}

	// Data directory: dev uses the current working directory,
	// prod uses /var/lib/fluxo (standard FHS location).
	dataDir := os.Getenv("FLUXO_DATA_DIR")
	if dataDir == "" {
		if env == "prod" {
			dataDir = "/var/lib/fluxo"
		} else {
			dataDir = "."
		}
	}

	// In production, guarantee the data directory exists so the DB
	// can be created on first run.
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
