package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/bridge"
)

func TestBaseBridgeCommandDoesNotInitializeProductionState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent-data")
	t.Setenv("FLUXO_ENV", "prod")
	t.Setenv("FLUXO_DATA_DIR", dir)
	previous := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = previous }()
	if !baseInstallCommand([]string{"base-install", "bridge-command"}) {
		t.Fatal("read-only bridge command was not handled")
	}
	w.Close()
	raw, err := io.ReadAll(r)
	r.Close()
	if err != nil || strings.TrimSpace(string(raw)) != bridge.BridgeInstallCommand {
		t.Fatal("default bridge diagnostic changed", string(raw), err)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatal("bridge diagnostic initialized data", err)
	}
}
