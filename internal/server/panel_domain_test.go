package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizePanelDomain(t *testing.T) {
	domain, err := normalizePanelDomain("  PANEL.Example.COM. ")
	if err != nil {
		t.Fatalf("normalize panel domain: %v", err)
	}
	if domain != "panel.example.com" {
		t.Fatalf("normalized domain = %q", domain)
	}

	for _, invalid := range []string{"localhost", "https://panel.example.com", "panel.example.com:443", "-panel.example.com", "192.0.2.10"} {
		if _, err := normalizePanelDomain(invalid); err == nil {
			t.Fatalf("invalid panel domain %q was accepted", invalid)
		}
	}
}

func TestPanelChallengeRootIsOutsidePrivateProductionData(t *testing.T) {
	root, err := panelChallengeRootPath("prod", "/var/lib/fluxo")
	if err != nil {
		t.Fatalf("resolve production challenge root: %v", err)
	}
	if root != productionPanelChallengeRoot {
		t.Fatalf("production challenge root = %q", root)
	}
	if root == "/var/lib/fluxo" || filepath.Dir(root) == "/var/lib/fluxo" {
		t.Fatalf("ACME webroot must not be nested under private Fluxo data: %q", root)
	}
}

func TestEnsurePanelChallengeRootIsTraversable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "acme")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := ensurePanelChallengeRoot(root); err != nil {
		t.Fatalf("ensure panel challenge root: %v", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0755 {
		t.Fatalf("panel challenge root mode = %o, want 755", mode)
	}
}
