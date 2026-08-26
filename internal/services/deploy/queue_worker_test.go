package deploy

import (
	"strings"
	"testing"
)

func TestApplyQueueWorkerDeploymentHook(t *testing.T) {
	original := "#!/bin/bash\nset -e\necho done\n"
	withHook := ApplyQueueWorkerDeploymentHook(original, true)
	if !strings.Contains(withHook, QueueRestartLine) {
		t.Fatalf("queue restart hook was not added: %q", withHook)
	}
	if !strings.Contains(withHook, QueueRestartMarker) {
		t.Fatalf("queue restart ownership marker was not added: %q", withHook)
	}
	if got := ApplyQueueWorkerDeploymentHook(withHook, true); got != withHook {
		t.Fatal("queue restart hook was not idempotent")
	}
	withoutHook := ApplyQueueWorkerDeploymentHook(withHook, false)
	if strings.Contains(withoutHook, "artisan queue:restart") {
		t.Fatalf("queue restart hook was not removed: %q", withoutHook)
	}
	if !strings.Contains(withoutHook, "echo done") {
		t.Fatalf("unrelated deployment commands were removed: %q", withoutHook)
	}
}

func TestWithoutQueueRestartPreservesUserOwnedHook(t *testing.T) {
	original := "#!/bin/bash\n" + QueueRestartLine + "\necho user-owned\n"
	if got := WithoutQueueRestart(original); got != original {
		t.Fatalf("user-owned queue restart hook changed:\n%s", got)
	}
}

func TestWithoutQueueRestartPreservesEditedManagedHook(t *testing.T) {
	original := QueueRestartMarker + "\nphp artisan queue:restart --custom\n"
	got := WithoutQueueRestart(original)
	if strings.Contains(got, QueueRestartMarker) {
		t.Fatalf("ownership marker was not removed: %q", got)
	}
	if !strings.Contains(got, "php artisan queue:restart --custom") {
		t.Fatalf("edited user command was removed: %q", got)
	}
}
