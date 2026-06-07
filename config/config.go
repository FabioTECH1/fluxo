package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	Port    string
	DBPath  string
	Env     string // "dev" or "prod"
	DataDir string
}

func LoadConfig() *Config {
	env := os.Getenv("FLUXO_ENV")
	if env == "" {
		env = "dev"
	}

	port := os.Getenv("FLUXO_PORT")
	if port == "" {
		port = "8080"
	}

	dataDir := os.Getenv("FLUXO_DATA_DIR")
	if dataDir == "" {
		if env == "prod" {
			dataDir = "/var/lib/fluxo"
		} else {
			dataDir = "."
		}
	}

	// Ensure data directory exists
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
