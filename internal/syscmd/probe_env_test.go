package syscmd

import (
	"context"
	"os"
	"os/user"
	"strings"
	"testing"
	"time"
)

func TestApplicationProbeEnvRefusesRoot(t *testing.T) {
	if _, err := RunApplicationProbeEnv(context.Background(), time.Second, "root", "/", nil, "/usr/bin/true"); err == nil {
		t.Fatal("a runtime probe was allowed to run as root")
	}
}

func TestApplicationProbeEnvKeepsDaemonSecretsOut(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("credential-drop execution needs root; covered by guarded VM acceptance")
	}
	if _, err := user.Lookup("nobody"); err != nil {
		t.Skip("no unprivileged nobody fixture user")
	}
	t.Setenv("FLUXO_PROBE_PARENT_SECRET", "fixture-private-value")
	out, err := RunApplicationProbeEnv(context.Background(), 5*time.Second, "nobody", "/", []string{"PATH=/usr/bin:/bin", "COREPACK_ENABLE_NETWORK=0"}, "/usr/bin/env")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "FLUXO_PROBE_PARENT_SECRET") || strings.Contains(out, "fixture-private-value") {
		t.Fatal("probe inherited privileged parent environment")
	}
	if !strings.Contains(out, "COREPACK_ENABLE_NETWORK=0\n") || !strings.Contains(out, "PATH=/usr/bin:/bin\n") {
		t.Fatal("explicit offline probe settings were lost")
	}
}
