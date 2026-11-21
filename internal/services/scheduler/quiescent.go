package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fluxo/internal/services/executiondomain"
)

// Quiescent checks durable execution obligations independently of the helper's
// lifecycle lease. The caller must hold exclusive lifecycle admission through
// its upgrade or snapshot: a crashed helper can release its lease while its
// detached descendants still occupy the recorded execution domain.
// This check does not retire launch authority, rewrite history or stop work.
func Quiescent(ctx context.Context) error {
	if absent() {
		return nil
	}
	unlock, err := lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	for _, v := range j.Invocations {
		if err = invocationQuiescent(ctx, v); err != nil {
			return err
		}
	}
	return nil
}

func invocationQuiescent(ctx context.Context, v invocation) error {
	if !v.Started {
		// A current bootstrap record is still one-use launch authority, never
		// evidence of absence. Only installer admission may wait and re-read it;
		// ordinary runtime/upgrade checks continue to refuse the same error.
		if v.ExecutionContract == executiondomain.SchedulerBootstrapContract && validateBootstrapInvocation(v) == nil {
			return &installerLaunchPendingError{}
		}
		return errors.New("reconcile outstanding Scheduler launch authority before continuing")
	}
	p, err := inspectInvocationUnit(ctx, v.Unit, v)
	if err != nil {
		return err
	}
	// Check the expected hierarchy even if PID 1 no longer reports a unit.
	// Inactive/failed state and MainPID=0 alone do not prove descendant drain.
	raw, err := os.ReadFile(filepath.Join("/sys/fs/cgroup/system.slice", v.Unit, "pids.current"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		n, parseErr := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
		if parseErr != nil {
			return errors.New("invalid Scheduler hierarchical process count")
		}
		if n != 0 {
			return fmt.Errorf("wait for Scheduler execution domain %s to finish naturally", v.Unit)
		}
	}
	if !executiondomain.UnitQuiescent(v.Adapter, p) {
		return errors.New("wait for Scheduler execution domains to finish")
	}
	return nil
}
