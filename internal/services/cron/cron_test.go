package cron

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteCronConfigReplacesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fluxo-cron-1")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	content := "# Fluxo Cron ID: 1\n0 0 * * * root true\n"
	if err := writeCronConfig(path, content); err != nil {
		t.Fatalf("writeCronConfig() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Fatalf("cron config = %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0644 {
		t.Fatalf("cron config mode = %o, want 0644", info.Mode().Perm())
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".fluxo-cron-") {
			t.Fatalf("temporary cron file was not removed: %s", entry.Name())
		}
	}
}
