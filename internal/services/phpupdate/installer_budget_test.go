package phpupdate

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBaseInstallerAdmissionUsesRemainingWaitDeadline(t *testing.T) {
	for _, test := range []struct{ remaining, want time.Duration }{
		{90 * time.Second, 90 * time.Second},
		{77 * time.Second, 77 * time.Second},
		{10 * time.Second, 10 * time.Second},
		{3 * time.Second, 10 * time.Second},
		{-time.Second, 10 * time.Second},
		{120 * time.Second, 90 * time.Second},
	} {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(test.remaining))
		got := installerLeaseAdmissionBudget(ctx)
		cancel()
		if got < test.want-250*time.Millisecond || got > test.want {
			t.Fatalf("remaining %s: budget %s, want approximately %s", test.remaining, got, test.want)
		}
	}
	if got := installerLeaseAdmissionBudget(context.Background()); got != 10*time.Second {
		t.Fatal("unbounded test context lost the fresh admission minimum", got)
	}
}

type installerBudgetDeadline struct {
	context.Context
	deadline time.Time
}

func (c *installerBudgetDeadline) Deadline() (time.Time, bool) { return c.deadline, true }

func TestBaseInstallerLaunchRetryDoesNotResetCronDrainBudget(t *testing.T) {
	ctx := &installerBudgetDeadline{Context: context.Background(), deadline: time.Now().Add(60 * time.Second)}
	pending := errors.New("single pending launch fixture")
	attempts, released := 0, 0
	release, id, err := retryInstallerLeaseAdmission(ctx,
		func() bool { return true },
		func(context.Context) (func(), error) { return func() { released++ }, nil },
		func(attempt context.Context) (string, error) {
			attempts++
			deadline, ok := attempt.Deadline()
			if !ok {
				t.Fatal("admission attempt has no deadline")
			}
			want := 60 * time.Second
			if attempts == 2 {
				want = 10 * time.Second
			}
			if got := time.Until(deadline); got < want-250*time.Millisecond || got > want {
				t.Fatalf("attempt %d: budget %s, want approximately %s", attempts, got, want)
			}
			if attempts == 1 {
				return "", pending
			}
			return "retained-owner", nil
		}, func(err error) bool { return err == pending },
		func(context.Context) error {
			if released != 1 {
				t.Fatal("retry retained the previous lifecycle lock", released)
			}
			ctx.deadline = time.Now().Add(3 * time.Second)
			return nil
		})
	if err != nil || id != "retained-owner" || release == nil || attempts != 2 || released != 1 {
		t.Fatal("retry failed or lost durable ownership", id, err, attempts, released)
	}
	release()
	if released != 2 {
		t.Fatal("successful admission was not retained for its caller")
	}
}

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
