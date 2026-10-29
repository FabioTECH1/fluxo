package maintenancetask

import (
	"context"
	"database/sql"
	"errors"
	"fluxo/internal/services/scheduler"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
)

func registrationRowAbsent(ctx context.Context, r registration) (bool, error) {
	if err := trustedDatabase(r.Database); err != nil {
		return false, err
	}
	u := url.URL{Scheme: "file", Path: r.Database}
	db, err := sql.Open("sqlite", u.String()+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return false, err
	}
	defer db.Close()
	var present bool
	if err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM crons WHERE id=?)", r.Contract.ID).Scan(&present); err != nil {
		return false, err
	}
	return !present, nil
}

// Called only by explicit reconciliation under exclusive lifecycle/package/
// native leases and the journal gate. API creation can fail after durable
// publication intent but before its row survives. Finish only the exact saved
// guarded publication, establish the real reload/drain proof, then revoke it.
// No application code runs and no missing user record is recreated.
func reconcileAbsentRegistrations(ctx context.Context, j *journal) error {
	for id, r := range j.Entries {
		if !r.Enabled {
			continue
		}
		absent, err := registrationRowAbsent(ctx, r)
		if err != nil {
			return err
		}
		if !absent {
			continue
		}
		if err = verifyRetainedHelper(r); err != nil {
			return err
		}
		raw, readErr := readRegular(cronPath(id), 1<<20)
		if readErr != nil && !os.IsNotExist(readErr) {
			return readErr
		}
		if !r.Published || os.IsNotExist(readErr) {
			if !r.Published {
				if err = publish(j, r); err != nil {
					return err
				}
			} else {
				// An absent orphan's replacement is revocation scaffolding only: its
				// missing DB row makes every delayed dispatcher reject before fork.
				if err = publishAbsentRetirement(r); err != nil {
					return err
				}
			}
			return errors.New("Orphaned scheduled-job authority is guarded; wait for cron acknowledgement and natural drain, then run reconciliation again")
		}
		if string(raw) != cronContent(r) {
			return errors.New("Orphaned scheduled-job trigger changed; retain its evidence for operator repair")
		}
		if err = readyBackend(ctx, r); err != nil {
			return err
		}
		r.Enabled = false
		j.Entries[id] = r
		if err = save(*j); err != nil {
			return err
		}
		if err = os.Remove(cronPath(id)); err != nil {
			return err
		}
		if err = syncDir(filepath.Dir(cronPath(id))); err != nil {
			return err
		}
	}
	return nil
}

func publishAbsentRetirement(r registration) error {
	return scheduler.PublishSystemTaskCron("fluxo-cron-"+strconv.Itoa(r.Contract.ID), [][]byte{nil}, []byte(cronContent(r)))
}
