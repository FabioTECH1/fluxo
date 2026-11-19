package main

import (
	"errors"
	"os"
	"path/filepath"
)

func parseNodeToolchainArgs(args []string) (preserve bool, lease string, err error) {
	usage := errors.New("Usage: fluxo node-toolchain install [--preserve] [--installer-lease UUID]")
	if len(args) == 0 || args[0] != "install" {
		return false, "", usage
	}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--preserve":
			if preserve {
				return false, "", usage
			}
			preserve = true
		case "--installer-lease":
			if lease != "" || i+1 >= len(args) || args[i+1] == "" {
				return false, "", usage
			}
			i++
			lease = args[i]
		default:
			return false, "", usage
		}
	}
	return preserve, lease, nil
}

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
