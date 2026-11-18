package scheduler

import (
	"context"
	"testing"
)

func TestQualifyPinsInterpreterAndExactCommand(t *testing.T) {
	dir := "/home/fluxo/app/current"
	good := "php8.4 " + dir + "/artisan schedule:run"
	if e, ok := Qualify(1, 2, dir, "* * * * *", good, "fluxo"); !ok || e.Version != "8.4" {
		t.Fatal("generated Scheduler rejected")
	}
	for _, command := range []string{"php " + dir + "/artisan schedule:run", good + " && id", good + " --verbose", "/usr/local/bin/" + good, "php8.4 artisan schedule:run"} {
		if _, ok := Qualify(1, 2, dir, "* * * * *", command, "fluxo"); ok {
			t.Fatalf("custom command accepted: %q", command)
		}
	}
	if _, ok := Qualify(1, 0, dir, "* * * * *", good, "fluxo"); ok {
		t.Fatal("global Scheduler adopted")
	}
	if _, ok := Qualify(1, 2, dir, "* * * * *", good, "root"); ok {
		t.Fatal("root application accepted")
	}
	if _, ok := Qualify(1, 2, dir, "*/2 * * * *", good, "fluxo"); ok {
		t.Fatal("custom schedule adopted")
	}
}

func TestReconcileDispatcherLocked(t *testing.T) {
	ctx := context.Background()

	// 1. When dispatcher does not exist, it cleanly returns nil
	j := journal{}
	if err := ReconcileDispatcherLocked(ctx, j); err != nil {
		t.Fatalf("expected nil when dispatcher absent, got: %v", err)
	}

	// 2. When holds are active, it defers without error
	jHold := journal{
		Holds: map[string]string{"8.4": "maintenance-1"},
	}
	if err := ReconcileDispatcherLocked(ctx, jHold); err != nil {
		t.Fatalf("expected nil when holds active, got: %v", err)
	}

	// 3. When upgrade is active, it defers without error
	jUpgrade := journal{
		Upgrade: &upgradeLease{},
	}
	if err := ReconcileDispatcherLocked(ctx, jUpgrade); err != nil {
		t.Fatalf("expected nil when upgrade active, got: %v", err)
	}
}
