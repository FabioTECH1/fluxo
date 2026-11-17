package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	sitepkg "fluxo/internal/services/site"
)

// ValidateActivationSchedulerRows reads the bridge's PRIVATE SQLite snapshot.
// It never opens a live SQLite database, initializes a subsystem, adopts a cron
// or writes a marker. Journal absence cannot erase existing plain authority.
func ValidateActivationSchedulerRows(ctx context.Context, db *sql.DB) error {
	registrations := map[int]Entry{}
	if !absent() {
		j, e := read()
		if e != nil {
			return e
		}
		registrations = j.Entries
	}
	rows, e := db.QueryContext(ctx, `SELECT c.id,c.site_id,c.expression,c.command,c.user,s.path,COALESCE(s.deployment_strategy,'standard'),s.app_type FROM crons c JOIN sites s ON s.id=c.site_id ORDER BY c.id`)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var id, site int
		var expression, command, user, path, strategy, kind string
		if e = rows.Scan(&id, &site, &expression, &command, &user, &path, &strategy, &kind); e != nil {
			return e
		}
		expected, qualified := Qualify(id, site, sitepkg.ActiveSitePath(path, strategy), expression, command, user)
		if kind != "laravel" || !qualified {
			continue
		}
		raw, err := os.ReadFile(CronPath(id))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err = owned(CronPath(id), false); err != nil {
			return err
		}
		registered, known := registrations[id]
		if err = validateActivationSchedulerRow(expected, registered, known, raw); err != nil {
			return fmt.Errorf("Scheduler row %d: %w", id, err)
		}
	}
	return rows.Err()
}
func validateActivationSchedulerRow(expected, registered Entry, known bool, trigger []byte) error {
	if !known || identity(expected) != identity(registered) || string(trigger) != CronContent(registered) {
		return errors.New("complete the patched-bridge Scheduler registration and historical retirement before execution-format activation")
	}
	return nil
}
