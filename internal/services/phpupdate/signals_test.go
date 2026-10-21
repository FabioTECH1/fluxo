package phpupdate

import (
	"errors"
	"testing"
)

func TestReviewedFPMRetryRequiresUnconfirmedDeliveryAndUnrefreshedOriginalMaster(t *testing.T) {
	old := unitSnapshot{MainPID: 41, PIDs: map[int]string{41: "boot:old"}}
	current := unitSnapshot{Active: true, MainPID: 41, PIDs: map[int]string{41: "boot:old"}}
	stale := func(int) error { return errors.New("replaced executable") }
	for _, delivery := range []string{"", "intent", "failed"} {
		if !retryUnconfirmedFPM(true, old, current, delivery, "previous-attempt", "reviewed-repair", stale) {
			t.Fatalf("unconfirmed %q signal could never be retried", delivery)
		}
	}
	if retryUnconfirmedFPM(false, old, current, "intent", "old", "new", stale) {
		t.Fatal("unreviewed signal replay")
	}
	if retryUnconfirmedFPM(true, old, current, "sent", "old", "new", stale) {
		t.Fatal("confirmed delivery replayed during draining")
	}
	if retryUnconfirmedFPM(true, old, current, "intent", "same", "same", stale) {
		t.Fatal("same attempt delivered repeated signals")
	}
	if retryUnconfirmedFPM(true, old, current, "intent", "old", "new", func(int) error { return nil }) {
		t.Fatal("reexecuted FPM with unchanged PID was signalled again")
	}
	current.PIDs[41] = "boot:replacement"
	if retryUnconfirmedFPM(true, old, current, "intent", "old", "new", stale) {
		t.Fatal("replacement master was signalled")
	}
}
