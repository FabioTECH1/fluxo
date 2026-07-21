package syscmd

import "testing"

func TestResolveCredentialFailsClosed(t *testing.T) {
	if _, err := ResolveCredential("fluxo-user-that-must-not-exist"); err == nil {
		t.Fatal("missing command user unexpectedly resolved")
	}
	root, err := ResolveCredential("root")
	if err != nil {
		t.Fatalf("explicit root command user did not resolve: %v", err)
	}
	if root.Uid != 0 {
		t.Fatalf("root UID = %d, want 0", root.Uid)
	}
}
