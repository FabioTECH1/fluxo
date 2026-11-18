package scheduler

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/services/executiondomain/testfixture"
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
	if !testfixture.InRoot(t, "") {
		return
	}
	ctx := context.Background()
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		t.Fatal(err)
	}
	dispatcher := filepath.Join(stateDir, "dispatcher")
	if err := ReconcileDispatcherLocked(ctx, journal{}); err != nil {
		t.Fatal(err)
	}
	retained := []byte("previous dispatcher")
	reset := func() {
		t.Helper()
		if err := os.WriteFile(dispatcher, retained, 0700); err != nil {
			t.Fatal(err)
		}
	}
	requireBytes := func(want []byte) {
		t.Helper()
		got, err := os.ReadFile(dispatcher)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("unexpected retained dispatcher", err)
		}
	}
	reset()
	for _, j := range []journal{
		{Holds: map[string]string{"8.4": "maintenance-1"}},
		{Upgrade: &upgradeLease{}},
		{Invocations: map[string]invocation{"pending": {Started: false}}},
	} {
		if err := ReconcileDispatcherLocked(ctx, j); err != nil {
			t.Fatal(err)
		}
		requireBytes(retained)
	}
	if err := ReconcileDispatcherLocked(ctx, journal{}); err != nil {
		t.Fatal(err)
	}
	running, err := os.ReadFile("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}
	requireBytes(running)
	reset()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe+".new", []byte("replacement panel"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(exe+".new", exe); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileDispatcherLocked(ctx, journal{}); err == nil || !strings.Contains(err.Error(), "installed Scheduler panel image changed") {
		t.Fatalf("expected running/install identity mismatch, got %v", err)
	}
	requireBytes(retained)
}
