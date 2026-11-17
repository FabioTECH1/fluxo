package bridge

import (
	"context"
	"errors"

	"fluxo/internal/services/executiondomain"
)

// Both source cores are already fenced current formats. Before touching helper
// references, publish the prepared floor so retained old binaries also refuse
// receipts/dispatch after a crash. Exact recovery reuses the saved nonces.
func applyTaskHelperRefreshTransitions(ctx context.Context, files []executiondomain.ActivationFile, write func(executiondomain.ActivationFile) error, prepare func() error, seal ...func() error) error {
	core := -1
	for i, f := range files {
		if f.Directory || f.Lock || len(f.Fence) != 0 || f.Path == "/var/lib/fluxo-scheduler/state.json" {
			return errors.New("task helper refresh cannot initialize or change Scheduler infrastructure")
		}
		if f.Path == "/var/lib/fluxo-system-tasks/state.json" {
			if core >= 0 || f.Before == "absent" {
				return errors.New("task helper refresh requires one existing core")
			}
			core = i
		}
	}
	if core < 0 {
		return errors.New("task helper refresh lost its existing core")
	}
	for _, check := range seal {
		if check != nil {
			if err := check(); err != nil {
				return err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := prepare(); err != nil {
		return err
	}
	apply := func(f executiondomain.ActivationFile) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return write(f)
	}
	for _, f := range files {
		if f.Image {
			if err := apply(f); err != nil {
				return err
			}
		}
	}
	if err := apply(files[core]); err != nil {
		return err
	}
	for _, f := range files {
		if f.Cron {
			if err := apply(f); err != nil {
				return err
			}
		}
	}
	return nil
}

// The exact intent exists before creation. Operation-bound invalid anchors
// fence historical initializers before mkdir/gate publication. Lock files are
// created/acquired in place, never renamed. After gates, recheck original or
// exact intended target bytes before staging helpers and the first current
// cores. Both current cores are durable BEFORE prepared floor, so historical
// readers cannot write new identity semantics during a process-crash window.
func applyExecutionTransitions(ctx context.Context, files []executiondomain.ActivationFile, write func(executiondomain.ActivationFile) error, prepare func() error, seal ...func() error) error {

	schedulerCore, taskCore := -1, -1
	for i, f := range files {
		if f.Path == "/var/lib/fluxo-scheduler/state.json" {
			if schedulerCore >= 0 {
				return errors.New("duplicate Scheduler core fence")
			}
			schedulerCore = i
		}
		if f.Path == "/var/lib/fluxo-system-tasks/state.json" {
			if taskCore >= 0 {
				return errors.New("duplicate task core fence")
			}
			taskCore = i
		}
	}
	if schedulerCore < 0 || taskCore < 0 {
		return errors.New("activation requires both explicit current core fences before any prepared floor")
	}
	apply := func(f executiondomain.ActivationFile) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return write(f)
	}
	for _, f := range files {
		if len(f.Fence) > 0 {
			f.PublishFence = true
			if err := apply(f); err != nil {
				return err
			}
		}
	}
	// The parent task gate can be locked before creating either subsystem dir.
	for _, f := range files {
		if f.Lock && f.Path == "/var/lib/fluxo-system-tasks.lock" {
			if err := apply(f); err != nil {
				return err
			}
		}
	}
	for _, f := range files {
		if f.Directory {
			if err := apply(f); err != nil {
				return err
			}
		}
	}
	for _, f := range files {
		if f.Lock && f.Path != "/var/lib/fluxo-system-tasks.lock" {
			if err := apply(f); err != nil {
				return err
			}
		}
	}
	for _, check := range seal {
		if check != nil {
			if err := check(); err != nil {
				return err
			}
		}
	}
	for _, f := range files {
		if f.Image {
			if err := apply(f); err != nil {
				return err
			}
		}
	}

	// Scheduler first also closes historical task Pending admission. Both exact
	// current cores are durable before publishing any prepared-floor metadata.
	if err := apply(files[schedulerCore]); err != nil {
		return err
	}
	if err := apply(files[taskCore]); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := prepare(); err != nil {
		return err
	}

	for i, f := range files {
		if i == schedulerCore || i == taskCore || f.Directory || f.Lock || f.Image || f.Path == "/var/lib/fluxo-scheduler/state.json" || f.Path == "/var/lib/fluxo-system-tasks/state.json" {
			continue
		}
		if err := apply(f); err != nil {
			return err
		}
	}
	return nil
}

// A same-schema reader6 -> reader7 transition must fence prior readers before
// any core/helper write. Both current source cores and their archive already
// exist. Legacy absent-core activation retains its separate original ordering.
func applyReader7UpgradeTransitions(ctx context.Context, files []executiondomain.ActivationFile, write func(executiondomain.ActivationFile) error, prepare func() error, seal ...func() error) error {
	for _, f := range files {
		if (f.Path == "/var/lib/fluxo-scheduler/state.json" || f.Path == "/var/lib/fluxo-system-tasks/state.json") && f.Before == "absent" {
			return errors.New("reader7 upgrade requires existing current source cores")
		}
		if f.Lock || len(f.Fence) != 0 {
			return errors.New("reader7 upgrade cannot initialize execution anchors")
		}
	}
	for _, check := range seal {
		if check != nil {
			if err := check(); err != nil {
				return err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := prepare(); err != nil {
		return err
	}
	return applyExecutionTransitions(ctx, files, write, func() error { return nil }, seal...)
}
