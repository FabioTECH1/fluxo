package phpupdate

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

// The waiting budget includes lifecycle acquisition and every retry pause. Each
// admitted attempt retains the existing separate 10s check budget, keeping the
// worst-case readiness bound below the shell's 120s deadline (90s + 10s).
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
		attemptCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
			log.Print("Waiting for a recorded Scheduler launch to settle before the upgrade...")
			announced = true
		}
		// No lifecycle, package, journal or database gate crosses this pause.
		if err := pause(waitCtx); err != nil {
			return nil, "", fmt.Errorf("waiting for Scheduler launch admission: %w", err)
		}
	}
}
