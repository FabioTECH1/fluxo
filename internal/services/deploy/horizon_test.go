package deploy

import (
	"strings"
	"testing"
)

func TestApplyHorizonDeploymentHookToRollback(t *testing.T) {
	rollback := "#!/bin/bash\nset -e\necho rollback\n"

	withoutHorizon := ApplyHorizonDeploymentHook(rollback, false)
	if withoutHorizon != rollback {
		t.Fatal("disabled Horizon changed the rollback script")
	}

	withHorizon := ApplyHorizonDeploymentHook(rollback, true)
	if !strings.Contains(withHorizon, HorizonTerminateLine) {
		t.Fatalf("rollback script is missing Horizon termination: %q", withHorizon)
	}
	if got := ApplyHorizonDeploymentHook(withHorizon, true); got != withHorizon {
		t.Fatal("Horizon rollback hook was not idempotent")
	}
}
