package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteServiceFileAtomicallyReplacesContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fluxo-daemon.service")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeServiceFile(path, []byte("new")); err != nil {
		t.Fatalf("writeServiceFile() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("service content = %q, want new", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0644 {
		t.Fatalf("service mode = %o, want 0644", info.Mode().Perm())
	}
}
