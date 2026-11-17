package syscmd

import (
	"context"
	"testing"
	"time"
)

func TestMaintenanceStdoutSeparatesStreamsAndEnvironment(t *testing.T) {
	t.Setenv("FLUXO_TEST_PRIVILEGED_ENV", "test-only-parent-value")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := RunMaintenanceStdout(ctx, "/usr/bin/python3", "-I", "-S", "-c", "import os,sys; print(os.environ.get('FLUXO_TEST_PRIVILEGED_ENV','absent')); print('observation-warning',file=sys.stderr)")
	if err != nil || out != "absent\n" {
		t.Fatalf("stdout observation or clean environment changed: %q %v", out, err)
	}
}
