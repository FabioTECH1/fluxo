package cron

import (
	"context"
	"fmt"
	"strings"

	"fluxo/internal/database"
	"fluxo/internal/services/scheduler"
	sitepkg "fluxo/internal/services/site"
)

func ManagedScheduler(ctx context.Context, id int, dir, expression, command, user string) (bool, error) {
	legacy := fmt.Sprintf("# Fluxo Cron ID: %d\n%s %s cd %s && %s >> /var/log/fluxo/cron-%d.log 2>&1\n", id, expression, user, dir, command, id)
	return scheduler.Managed(ctx, id, legacy)
}

// ReconcileSchedulers adopts only exact Fluxo-generated existing entries.
// Missing triggers stay missing. Migration uses verified cron receipts and a
// natural legacy-domain drain; normal scheduling continues during the transition.
func ReconcileSchedulers(ctx context.Context) error {
	if err := scheduler.ReconcileRegistrations(ctx); err != nil {
		return err
	}
	rows, err := database.DB.Query(`SELECT c.id,c.site_id,c.expression,c.command,c.user,s.path,COALESCE(s.deployment_strategy,'standard') FROM crons c JOIN sites s ON s.id=c.site_id WHERE s.app_type='laravel' AND (c.name='Laravel Scheduler' OR c.command LIKE '%artisan schedule:run%') ORDER BY c.id`)
	if err != nil {
		return err
	}
	type record struct {
		id, site                        int
		expr, cmd, user, path, strategy string
	}
	var records []record
	for rows.Next() {
		var r record
		if err = rows.Scan(&r.id, &r.site, &r.expr, &r.cmd, &r.user, &r.path, &r.strategy); err != nil {
			rows.Close()
			return err
		}
		records = append(records, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var failures []string
	for _, r := range records {
		dir := sitepkg.ActiveSitePath(r.path, r.strategy)
		e, ok := scheduler.Qualify(r.id, r.site, dir, r.expr, r.cmd, r.user)
		if !ok {
			continue
		}
		legacy := fmt.Sprintf("# Fluxo Cron ID: %d\n%s %s cd %s && %s >> /var/log/fluxo/cron-%d.log 2>&1\n", r.id, r.expr, r.user, dir, r.cmd, r.id)
		if err = scheduler.Materialize(ctx, e, legacy, false); err != nil {
			failures = append(failures, fmt.Sprintf("cron #%d: %v", r.id, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("Scheduler migration: %s", strings.Join(failures, "; "))
	}
	return nil
}
