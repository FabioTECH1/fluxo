package syscmd

import (
	"context"
	"strings"
	"testing"
	"time"
)

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

func TestRunEnvTimeoutKillsCommandGroupAndKeepsDiagnostics(t *testing.T) {
	started := time.Now()
	_, err := RunEnv(context.Background(), 50*time.Millisecond, nil, "sh", "-c", "echo child-started; sleep 30")
	if err == nil || !strings.Contains(err.Error(), "command timed out") {
		t.Fatalf("RunEnv() error = %v, want timeout", err)
	}
	if !strings.Contains(err.Error(), "child-started") {
		t.Fatalf("RunEnv() timeout omitted captured diagnostics: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("RunEnv() took %s after its timeout; a child process may have retained the output pipe", elapsed)
	}
}
