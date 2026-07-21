package server

import (
	"strings"
	"testing"
)

func TestHorizonTerminateDeployHook(t *testing.T) {
	original := "#!/bin/bash\nset -e\necho done\n"
	withHook := withHorizonTerminate(original)
	if !strings.Contains(withHook, horizonTerminateLine) {
		t.Fatalf("deployment hook was not added: %q", withHook)
	}
	if got := withHorizonTerminate(withHook); got != withHook {
		t.Fatal("adding the Horizon hook was not idempotent")
	}

	withoutHook := withoutHorizonTerminate(withHook)
	if strings.Contains(withoutHook, "artisan horizon:terminate") {
		t.Fatalf("deployment hook was not removed: %q", withoutHook)
	}
	if !strings.Contains(withoutHook, "echo done") {
		t.Fatalf("unrelated deployment commands were removed: %q", withoutHook)
	}
}
