package cron

import (
	"context"
	"errors"
	"fmt"
	"os"

	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/packages"
	"fluxo/internal/services/processlog"
)

// ErrNoGuardedRegistration proves refusal before launch authority or a trigger
// existed, and the caller's database-only setup was discarded under the same
// mutation reservation. No reconciler can publish a stale row in between.
var ErrNoGuardedRegistration = errors.New("guarded application job was not published")

// CreateGuardedApplication publishes only through the existing application-job
// protocol. Feature-owned jobs must never fall back to an unguarded cron file.
func CreateGuardedApplication(ctx context.Context, id, siteID int, dir, expression, command string, discard func() error) error {
	release, err := packages.Lifecycle(ctx, true)
	if err != nil {
		return err
	}
	defer release()
	unlock, err := reserveMutation(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	c, ok := maintenancetask.QualifyApplication(id, siteID, dir, expression, command, "fluxo")
	if !ok {
		return errors.New("WordPress Cron requires a supported application directory")
	}
	if err = processlog.Prepare(fmt.Sprintf("/var/log/fluxo/cron-%d.log", id), "fluxo"); err != nil {
		return err
	}
	dbPath, err := PanelDatabasePath()
	if err != nil {
		return err
	}
	err = maintenancetask.Materialize(ctx, c, dbPath, true)
	if err != nil {
		registered, inspectErr := maintenancetask.Registered(ctx, id)
		_, fileErr := os.Lstat(fmt.Sprintf("/etc/cron.d/fluxo-cron-%d", id))
		if discard != nil && inspectErr == nil && !registered && os.IsNotExist(fileErr) {
			if cleanupErr := discard(); cleanupErr != nil {
				return errors.Join(err, cleanupErr)
			}
			return fmt.Errorf("%w: %w", ErrNoGuardedRegistration, err)
		}
	}
	return err
}

// RetireGuardedApplication never removes an independently modified plain cron.
// A failed first publication may have no registration or trigger at all.
func RetireGuardedApplication(ctx context.Context, id int) error {
	release, err := packages.Lifecycle(ctx, true)
	if err != nil {
		return err
	}
	defer release()
	unlock, err := reserveMutation(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	registered, err := maintenancetask.Registered(ctx, id)
	if err != nil {
		return err
	}
	if registered {
		return maintenancetask.Retire(ctx, id)
	}
	if _, err := os.Lstat(fmt.Sprintf("/etc/cron.d/fluxo-cron-%d", id)); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("The WordPress cron trigger has no matching guarded registration; it was preserved for review")
}
