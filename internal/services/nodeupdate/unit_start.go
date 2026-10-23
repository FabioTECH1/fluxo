package nodeupdate

import (
	"context"
	"errors"
	"time"

	"fluxo/internal/syscmd"
)

// A stopped disabled service may have been garbage-collected by systemd. A
// normal start loads it from its unchanged unit file; reset-failed on an
// unloaded inactive unit instead fails. Reset only retained failure/rate-limit
// state, and propagate both inspection and reset errors without enabling it.
func resetFailedForStart(ctx context.Context, name string) error {
	out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", name, "--property=LoadState,ActiveState,StartLimitHit")
	if err != nil {
		return err
	}
	p := properties(out)
	if p["LoadState"] != "loaded" {
		return errors.New("Application service cannot be loaded before restart")
	}
	if p["ActiveState"] == "failed" || p["StartLimitHit"] == "yes" {
		_, err = syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "reset-failed", name)
		return err
	}
	return nil
}
