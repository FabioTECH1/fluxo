package bridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestInstallerSchedulerDrainRetryReleasesAdmissionBeforeWaiting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lifecycle.lock")
	owner, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	worker, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	busy := errors.New("typed test drain observation")
	calls, pauses := 0, 0
	err = retryInstallerSchedulerDrain(t.Context(), func() error {
		calls++
		if err := unix.Flock(int(owner.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
			return err
		}
		defer unix.Flock(int(owner.Fd()), unix.LOCK_UN)
		if calls == 1 {
			return busy
		}
		return nil
	}, func(err error) bool { return err == busy }, func(context.Context) error {
		pauses++
		if err := unix.Flock(int(worker.Fd()), unix.LOCK_SH|unix.LOCK_NB); err != nil {
			t.Fatalf("cron worker could not obtain released admission: %v", err)
		}
		return unix.Flock(int(worker.Fd()), unix.LOCK_UN)
	})
	if err != nil || calls != 2 || pauses != 1 {
		t.Fatal(err, calls, pauses)
	}
}

func TestInstallerSchedulerDrainRetryKeepsHardRefusals(t *testing.T) {
	busy := errors.New("single typed test observation")
	for _, hard := range []error{errors.New("same words are not proof"), errors.New("image/floor changed"), errors.Join(busy, errors.New("unsafe state"))} {
		calls := 0
		got := retryInstallerSchedulerDrain(t.Context(), func() error { calls++; return hard }, func(err error) bool { return err == busy }, func(context.Context) error { t.Fatal("hard refusal retried"); return nil })
		if !errors.Is(got, hard) || calls != 1 {
			t.Fatal(got, calls)
		}
	}
}

func TestInstallerSchedulerDrainRetryCancellationAndSuccessfulCompletion(t *testing.T) {
	busy := errors.New("single typed test observation")
	pending := func(err error) bool { return err == busy }
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got := retryInstallerSchedulerDrain(ctx, func() error { t.Fatal("expired window started work"); return nil }, pending, func(context.Context) error { t.Fatal("expired window paused"); return nil })
	if !errors.Is(got, context.Canceled) {
		t.Fatal(got)
	}
	ctx, cancel = context.WithCancel(t.Context())
	calls := 0
	got = retryInstallerSchedulerDrain(ctx, func() error { calls++; cancel(); return busy }, pending, func(context.Context) error { t.Fatal("expired window paused"); return nil })
	if !errors.Is(got, context.Canceled) || calls != 1 {
		t.Fatal(got, calls)
	}
	ctx, cancel = context.WithCancel(t.Context())
	// A valid operation may finish after its retry window expires. Do not turn
	// completed publication into a failure or launch another attempt.
	got = retryInstallerSchedulerDrain(ctx, func() error { cancel(); return nil }, pending, func(context.Context) error { t.Fatal("successful operation retried"); return nil })
	if got != nil {
		t.Fatal(got)
	}
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	calls = 0
	got = retryInstallerSchedulerDrain(ctx, func() error { calls++; return busy }, pending, func(context.Context) error { cancel(); return ctx.Err() })
	if !errors.Is(got, context.Canceled) || calls != 1 {
		t.Fatal(got, calls)
	}
}
