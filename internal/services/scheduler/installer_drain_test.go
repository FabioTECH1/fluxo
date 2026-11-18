package scheduler

import (
	"errors"
	"fmt"
	"testing"
)

func TestInstallerCronDrainRequiresTheTypedSingleObservation(t *testing.T) {
	busy := diagnostic("waiting_for_legacy_jobs", "observed extra cron threads")
	for _, tc := range []struct {
		name    string
		err     error
		pending bool
	}{
		{"observed", busy, true},
		{"wrapped-observation", fmt.Errorf("current provider: %w", busy), true},
		{"idle", nil, false},
		{"typed-nil", (*diagnosticError)(nil), false},
		{"same-text-is-not-proof", errors.New(busy.Error()), false},
		{"identity-changed", errors.New("Scheduler cron execution domain changed"), false},
		{"reload", diagnostic("waiting_for_reload", "receipt pending"), false},
		{"panel", diagnostic("panel_proof_required", "panel boundary missing"), false},
		{"mixed-failures", errors.Join(busy, errors.New("changed provider")), false},
		{"wrapped-mixed-failures", fmt.Errorf("admission: %w", errors.Join(busy, errors.New("changed provider"))), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := InstallerCronDrainPending(tc.err); got != tc.pending {
				t.Fatal("incorrect installer drain classification", got, tc.err)
			}
		})
	}
}
