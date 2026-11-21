package bridge

import (
	"context"
	"fmt"
	"log"
	"time"

	"fluxo/internal/services/scheduler"
)

// The ordinary installer can observe a real cron child between qualification
// and the empty-provider check. Retry only that typed observation, after the
// entire attempt releases its lifecycle/package/native/journal/database gates.
// A retained preparing/pending operation is re-read and resumed with its exact
// image, floor and archived plan; it is never discarded or replaced for a retry.
func installSchedulerPolicy(ctx context.Context, dataDir, version string, complete bool) error {
	waitCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	return retryInstallerSchedulerDrain(waitCtx,
		func() error { return installSchedulerPolicyOnce(ctx, dataDir, version, complete) },
		scheduler.InstallerAdmissionPending,
		func(waitCtx context.Context) error {
			select {
			case <-waitCtx.Done():
				return waitCtx.Err()
			case <-time.After(time.Second):
				return nil
			}
		})
}

// The waiting window controls whether another attempt may start. An admitted
// exact operation keeps the caller's original context, so expiry of this
// retry window cannot cancel publication, acknowledgement or panel restoration
// midway through a successful attempt. The CLI's existing overall deadline
// still bounds that work. Other errors remain immediate refusals.
func retryInstallerSchedulerDrain(waitCtx context.Context, attempt func() error, pending func(error) bool, pause func(context.Context) error) error {
	announced := false
	for {
		if err := waitCtx.Err(); err != nil {
			return fmt.Errorf("waiting for cron activity before Scheduler setup: %w", err)
		}
		err := attempt()
		if !pending(err) {
			return err
		}
		if err := waitCtx.Err(); err != nil {
			return fmt.Errorf("waiting for cron activity before Scheduler setup: %w", err)
		}
		if !announced {
			log.Print("Waiting for current cron work to finish before Scheduler setup; no jobs will be stopped...")
			announced = true
		}
		if err := pause(waitCtx); err != nil {
			return fmt.Errorf("waiting for cron activity before Scheduler setup: %w", err)
		}
	}
}
