package phpupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fluxo/internal/services/scheduler"
	"os"
	"strings"
	"testing"

	"fluxo/internal/config"
	"fluxo/internal/syscmd"
)

func TestWorkerEnvironmentSurvivesCleanSubprocess(t *testing.T) {
	// A real subprocess must receive the intended production defaults/custom
	// directory despite RunMaintenance discarding the inherited environment.
	for _, dir := range []string{"/var/lib", "/var/lib/../lib"} {
		m := NewManager(dir)
		env, err := m.workerEnvironment()
		if err != nil {
			t.Fatal(err)
		}
		var out strings.Builder
		if err := syscmd.RunMaintenance(context.Background(), "", &out, env, "/usr/bin/env"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "FLUXO_ENV=prod\n") || !strings.Contains(out.String(), "FLUXO_DATA_DIR=/var/lib\n") {
			t.Fatal(out.String())
		}
	}
	t.Setenv("FLUXO_ENV", "prod")
	t.Setenv("FLUXO_DATA_DIR", "")
	if config.LoadConfig().DataDir != "/var/lib/fluxo" {
		t.Fatal("unexpected default")
	}
}
func TestRecoveryChecksConsumerMeaningButAllowsIniRepair(t *testing.T) {
	original := Impact{Workers: []Consumer{{ID: 1, Kind: "laravel_queue", Command: "php8.4 artisan queue:work"}}, UnitHash: "effective unit", ConfigHash: "old ini"}
	repaired := original
	repaired.ConfigHash = "fixed ini"
	if err := recoveryCompatible(original, repaired); err != nil {
		t.Fatal(err)
	}
	for _, changed := range []Impact{
		{Workers: original.Workers, UnitHash: "changed drop-in"},
		{Workers: []Consumer{{ID: 1, Command: "php8.3 artisan queue:work"}}, UnitHash: original.UnitHash},
		{Workers: append(append([]Consumer{}, original.Workers...), Consumer{ID: 2}), UnitHash: original.UnitHash},
		{Workers: original.Workers, UnitHash: original.UnitHash, BlockedReasons: []string{"unsafe worker"}},
	} {
		if recoveryCompatible(original, changed) == nil {
			t.Fatalf("accepted conflicting recovery: %+v", changed)
		}
	}
}
func TestAdoptionNeverRestartsHealthyReplacementOrDrainingGeneration(t *testing.T) {
	old := unitSnapshot{Name: "worker", Active: true, MainPID: 10, PIDs: map[int]string{10: "old"}}
	current := unitSnapshot{Name: "worker", Active: true, MainPID: 20, PIDs: map[int]string{20: "new"}}
	healthy := func(int) error { return nil }
	gone := func(int) (string, error) { return "", os.ErrNotExist }
	if !runtimeAdopted(old, current, false, healthy, gone) {
		t.Fatal("healthy replacement was not adopted")
	}
	if sameOriginalMain(old, current) {
		t.Fatal("replacement could be signalled")
	}
	if runtimeAdopted(old, old, false, healthy, gone) {
		t.Fatal("old worker considered refreshed")
	}
	draining := current
	draining.PIDs = map[int]string{20: "new", 10: "old"}
	if runtimeAdopted(old, draining, false, healthy, gone) {
		t.Fatal("old child ignored")
	}
	if sameOriginalMain(old, draining) {
		t.Fatal("draining replacement could be signalled")
	}
	stale := func(int) error { return errors.New("deleted library") }
	if runtimeAdopted(old, current, true, stale, gone) {
		t.Fatal("stale FPM accepted")
	}
}
func TestStoppedOctaneDoesNotRequirePortReadiness(t *testing.T) {
	c := Consumer{ID: 4, Instances: 2}
	before := runtimeSnapshot{Units: []unitSnapshot{{Name: unitName(4, 1)}, {Name: unitName(4, 2)}}}
	if consumerWasActive(c, before) {
		t.Fatal("stopped Octane required readiness")
	}
	before.Units[1].Active = true
	if !consumerWasActive(c, before) {
		t.Fatal("running instance was ignored")
	}
}

func TestAdoptionDistinguishesOPcacheMemoryFromReplacedCode(t *testing.T) {
	for _, mapping := range []string{
		"7f0000-7f1000 rw-s 00000000 00:01 5 /dev/zero (deleted)",
		"7f0000-7f1000 rwxs 00000000 00:01 5 /memfd:opcache (deleted)",
		"7f0000-7f1000 rw-s 00000000 00:01 5 /SYSV00000000 (deleted)",
		"7f0000-7f1000 rw-p 00000000 08:01 5 /tmp/app-data (deleted)",
	} {
		if hasReplacedCodeMapping(mapping) {
			t.Fatalf("anonymous/data mapping rejected: %s", mapping)
		}
	}
	for _, mapping := range []string{
		"7f0000-7f1000 r--p 00000000 08:01 5 /usr/lib/php/20240924/opcache.so (deleted)",
		"7f0000-7f1000 r-xp 00000000 08:01 5 /usr/bin/php8.4 (deleted)",
		"7f0000-7f1000 r--p 00000000 08:01 5 /usr/lib/x86_64-linux-gnu/libssl.so.3 (deleted)",
	} {
		if !hasReplacedCodeMapping(mapping) {
			t.Fatalf("replaced code accepted: %s", mapping)
		}
	}
}

func TestRecoveryRequiresConfiguredPackagesIndependentlyOfRepositoryAvailability(t *testing.T) {
	expected := Inventory{Packages: []Package{{Name: "php8.4-fpm", Architecture: "amd64", Installed: "8.4.1-1"}}, Changes: []Package{{Name: "php8.4-fpm", Architecture: "amd64", Candidate: "8.4.1-2"}}}
	actual := Inventory{Packages: []Package{{Name: "php8.4-fpm", Architecture: "amd64", Installed: "8.4.1-2"}}, BlockedReasons: []string{"repository unavailable"}}
	if verifyInstalledInventory(expected, actual) == nil {
		t.Fatal("matching unpacked revision passed verification")
	}
	actual.Packages[0].Configured = true
	if err := verifyInstalledInventory(expected, actual); err != nil {
		t.Fatalf("candidate availability prevented installed runtime verification: %v", err)
	}
}

func TestRecoveryAcceptsSerializedEmptySchedulersAndRejectsAddedScheduler(t *testing.T) {
	original := Impact{Sites: []Site{}, Workers: []Consumer{}, UnitHash: "same"}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var loaded Impact
	if err = json.Unmarshal(raw, &loaded); err != nil {
		t.Fatal(err)
	}
	current := original
	current.Schedulers = []scheduler.Entry{}
	if err = recoveryCompatible(loaded, current); err != nil {
		t.Fatal("serialized empty Scheduler collection rejected", err)
	}
	current.Schedulers = []scheduler.Entry{{ID: 4, Version: "8.4", Identity: "new"}}
	if recoveryCompatible(loaded, current) == nil {
		t.Fatal("new Scheduler did not invalidate the recovery review")
	}
}
