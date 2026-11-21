package phpupdate

import (
	"testing"
	"time"
)

func TestBaseInstallerPreservesCronBudgetAndFreshAdmissionMinimum(t *testing.T) {
	for _, test := range []struct{ waited, want time.Duration }{
		{0, 90 * time.Second},
		{13 * time.Second, 77 * time.Second},
		{80 * time.Second, 10 * time.Second},
		{90 * time.Second, 10 * time.Second},
		{-time.Second, 90 * time.Second},
	} {
		got := installerAdmissionBudget(test.waited)
		if got != test.want {
			t.Fatalf("wait %s: admission budget %s, want %s", test.waited, got, test.want)
		}
		if test.waited >= 0 && test.waited+got > 100*time.Second {
			t.Fatal("helper readiness exceeds its bounded lock/admission window")
		}
	}
}
