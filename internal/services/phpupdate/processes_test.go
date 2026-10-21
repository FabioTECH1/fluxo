package phpupdate

import (
	"errors"
	"os"
	"testing"
)

func TestDrainWaitsForChildrenAfterMasterExits(t *testing.T) {
	old := unitSnapshot{Active: true, MainPID: 40, PIDs: map[int]string{40: "boot:master", 41: "boot:job"}}
	gone := func(int) (string, error) { return "", os.ErrNotExist }
	current := unitSnapshot{State: "inactive", PIDs: map[int]string{41: "boot:job"}}
	if ok, err := fullyDrained(old, current, gone); err != nil || ok {
		t.Fatalf("master exit hid a running child: drained=%v err=%v", ok, err)
	}
	current.PIDs = map[int]string{}
	// A process that moved out of the cgroup must still block mutation.
	key := func(pid int) (string, error) {
		if pid == 41 {
			return "boot:job", nil
		}
		return gone(pid)
	}
	if ok, err := fullyDrained(old, current, key); err != nil || ok {
		t.Fatalf("escaped original worker was ignored: drained=%v err=%v", ok, err)
	}
	if ok, err := fullyDrained(old, current, gone); err != nil || !ok {
		t.Fatalf("completed workers did not drain: drained=%v err=%v", ok, err)
	}
}

func TestDrainDoesNotConfusePIDReuseOrInspectionFailureWithCompletion(t *testing.T) {
	old := unitSnapshot{PIDs: map[int]string{41: "previous-boot:10"}}
	current := unitSnapshot{State: "inactive", PIDs: map[int]string{}}
	if ok, err := fullyDrained(old, current, func(int) (string, error) { return "new-boot:10", nil }); err != nil || !ok {
		t.Fatalf("PID reuse across reboot prevented recovery: %v %v", ok, err)
	}
	denied := errors.New("permission denied")
	if ok, err := fullyDrained(old, current, func(int) (string, error) { return "", denied }); ok || !errors.Is(err, denied) {
		t.Fatalf("inability to inspect was treated as completion: %v %v", ok, err)
	}
	for _, state := range []string{"active", "activating", "deactivating", "reloading", ""} {
		current.State = state
		if ok, _ := fullyDrained(old, current, func(int) (string, error) { return "", os.ErrNotExist }); ok {
			t.Fatalf("nonterminal state %q admitted package mutation", state)
		}
	}
}
