package maintenancetask

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
)

func absent() (bool, error) {
	for _, p := range []string{stateRoot, stateRoot + ".initialized", stateRoot + ".lock"} {
		_, err := os.Lstat(p)
		if err == nil {
			return false, nil
		}
		if !os.IsNotExist(err) {
			return false, err
		}
	}
	return true, nil
}

// CompatibilityRequired includes partial initialization. Downgrading must not
// reintroduce raw cron triggers or discard an uncertain invocation journal.
func CompatibilityRequired() (bool, error) {
	a, err := absent()
	return !a, err
}

// Quiescent is called with the exclusive lifecycle lease before installer or
// runtime mutation. It performs no initialization, retirement or history edits.
func Quiescent(ctx context.Context) error {
	a, err := absent()
	if err != nil || a {
		return err
	}
	unlock, err := gate(ctx, false)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	if err = settled(ctx, &j); err != nil {
		return err
	}
	for _, r := range j.Entries {
		if r.Enabled {
			if err = ready(ctx, r); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("A removed scheduled job still has guarded registration evidence; run system-task-reconcile to revoke it without replaying work: %w", err)
				}
				return err
			}
		}
	}
	return nil
}

func RunNow(ctx context.Context, c Contract, database string) (string, error) {
	unlock, err := gate(ctx, false)
	if err != nil {
		return "", err
	}
	j, err := read()
	if err != nil {
		unlock()
		return "", err
	}
	r, ok := j.Entries[c.ID]
	unlock()
	if !ok || r.Contract != c || r.Database != database {
		return "", errors.New("System-task registration does not match the current panel row")
	}
	return Dispatch(ctx, c.ID, r.Identity)
}

// Registered is used only to prevent a managed identity falling back to a raw
// shell command after its row is edited. Missing or unreadable evidence refuses.
func Registered(ctx context.Context, id int) (bool, error) {
	a, err := absent()
	if err != nil || a {
		return false, err
	}
	unlock, err := gate(ctx, false)
	if err != nil {
		return false, err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return false, err
	}
	_, ok := j.Entries[id]
	return ok, nil
}

func ConfigurationState(ctx context.Context, c Contract, dbPath string) string {
	unlock, err := gate(ctx, false)
	if err != nil {
		return "unknown"
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return "unknown"
	}
	r, ok := j.Entries[c.ID]
	if !ok || !r.Enabled {
		return "missing"
	}
	if r.Contract != c || r.Database != dbPath {
		return "modified"
	}
	if err = verifyRegistration(ctx, r); err != nil {
		if os.IsNotExist(err) {
			return "missing"
		}
		return "modified"
	}
	return "configured"
}
