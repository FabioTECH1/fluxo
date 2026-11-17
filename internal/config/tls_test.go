package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPartialPanelTLSNeverReplacesExistingFile(t *testing.T) {
	for _, name := range []string{"cert.pem", "key.pem"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "tls"), 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "tls", name)
			if err := os.WriteFile(path, []byte("preserve original"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadOrGenerateTLS(dir); err == nil {
				t.Fatal("partial pair admitted")
			}
			b, err := os.ReadFile(path)
			if err != nil || string(b) != "preserve original" {
				t.Fatal("original TLS file changed", err)
			}
			entries, err := os.ReadDir(filepath.Join(dir, "tls"))
			if err != nil || len(entries) != 1 {
				t.Fatal("replacement TLS file created", err)
			}
		})
	}
}
