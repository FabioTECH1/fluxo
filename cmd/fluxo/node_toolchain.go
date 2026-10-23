package main

import (
	"errors"
	"os"
	"path/filepath"
)

// This root-only maintenance command targets the installed panel even when
// invoked from an application user's home without the service's environment.
// Never infer an additional maintenance database from the working directory.
func nodeToolchainDataDirs() ([]string, error) {
	dirs := []string{"/var/lib/fluxo"}
	if configured := os.Getenv("FLUXO_DATA_DIR"); configured != "" {
		if !filepath.IsAbs(configured) {
			return nil, errors.New("Node.js toolchain installation requires an absolute FLUXO_DATA_DIR")
		}
		if configured = filepath.Clean(configured); configured != dirs[0] {
			dirs = append(dirs, configured)
		}
	}
	return dirs, nil
}
