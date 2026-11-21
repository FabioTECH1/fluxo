package phpupdate

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

// The waiting budget includes lifecycle acquisition and every retry pause.
// Base admission keeps the remaining ownership/brief-cron budget, with a fresh
// 10s minimum. Worst-case readiness stays within 100s, below the shell's 120s.
func retryInstallerLeaseAdmission(waitCtx context.Context, parentAlive func() bool,
	acquire func(context.Context) (func(), error), prepare func(context.Context) (string, error),
	pending func(error) bool, pause func(context.Context) error,
) (func(), string, error) {
	announced := false
	for {
		if err := waitCtx.Err(); err != nil {
			return nil, "", fmt.Errorf("waiting for Scheduler launch admission: %w", err)
		}
		if !parentAlive() {
			return nil, "", errors.New("Installer exited while waiting for maintenance access; no upgrade lease was created")
		}
		release, err := acquire(waitCtx)
		if err != nil {
			return nil, "", err
		}
		if err := waitCtx.Err(); err != nil {
			release()
			return nil, "", fmt.Errorf("waiting for Scheduler launch admission: %w", err)
		}
		if !parentAlive() {
			release()
			return nil, "", errors.New("Installer exited while waiting for maintenance access; no upgrade lease was created")
		}
		attemptCtx, cancel := context.WithTimeout(context.Background(), installerLeaseAdmissionBudget(waitCtx))
		id, err := prepare(attemptCtx)
		if err != nil && attemptCtx.Err() != nil {
			err = attemptCtx.Err()
		}
		cancel()
		if err == nil {
			// READY is owned by the caller. Do not cancel an already admitted
			// durable lease when the retry-admission window has just expired.
			return release, id, nil
		}
		release()
		if !pending(err) {
			return nil, "", err
		}
		if !announced {
			log.Print("Waiting for Scheduler work to settle before the upgrade...")
			announced = true
		}
		// No lifecycle, package, journal or database gate crosses this pause.
		if err := pause(waitCtx); err != nil {
			return nil, "", fmt.Errorf("waiting for Scheduler launch admission: %w", err)
		}
	}
}

// Recompute after every acquisition: a pending launch must not reset the base's
// existing 90s budget. A late successful acquisition retains its fresh 10s.
func installerLeaseAdmissionBudget(waitCtx context.Context) time.Duration {
	deadline, bounded := waitCtx.Deadline()
	if !bounded {
		return 10 * time.Second
	}
	return installerAdmissionBudget(90*time.Second - time.Until(deadline))
}
