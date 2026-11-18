package bridge

import (
	"context"
	"errors"
	"testing"

	"fluxo/internal/services/executiondomain"
)

func TestInstallerRenewalQualificationFailureLeavesPreparedFloorBeforeTaskWrites(t *testing.T) {
	floor := "source"
	busy := errors.New("backend busy")
	failFence := errors.New("floor write failed")
	files := []executiondomain.ActivationFile{{Path: "/var/lib/fluxo-system-tasks/state.json", Before: "existing"}}
	run := func(prepare, qualify func() error) error {
		return applyTaskHelperRefreshTransitions(context.Background(), files, func(executiondomain.ActivationFile) error {
			t.Fatal("attempted task write after failed fence or qualification")
			return nil
		}, func() error {
			return prepareTaskRequalificationFloor(activationInstallerTaskBackendRenewal, prepare, qualify)
		})
	}
	err := run(func() error { return failFence }, func() error {
		t.Fatal("qualified without a durable floor")
		return nil
	})
	if !errors.Is(err, failFence) {
		t.Fatal(err)
	}
	err = run(func() error { floor = "prepared"; return nil }, func() error {
		if floor != "prepared" {
			t.Fatal("old helpers can still run during qualification")
		}
		return busy
	})
	if !errors.Is(err, busy) || floor != "prepared" {
		t.Fatalf("qualification error %v left floor %q", err, floor)
	}
}
