package cron

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"fluxo/internal/database"
	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/packages"
	"fluxo/internal/services/scheduler"
)

func PanelDatabasePath() (string, error) {
	var path string
	err := database.DB.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path)
	return path, err
}

func taskContract(id, siteID int, dir, expression, command, user string) (maintenancetask.Contract, bool) {
	if c, ok := maintenancetask.Qualify(id, siteID, expression, command, user); ok {
		return c, true
	}
	return maintenancetask.QualifyApplication(id, siteID, dir, expression, command, user)
}

// CreateManagedTask returns false only when no prior managed identity exists
// and the host/profile cannot be adopted. An adopted job never falls back to sh.
func CreateManagedTask(ctx context.Context, id, siteID int, dir, expression, command, user string) (bool, error) {
	c, ok := taskContract(id, siteID, dir, expression, command, user)
	registered, err := maintenancetask.Registered(ctx, id)
	if err != nil {
		return true, err
	}
	if !ok {
		if registered {
			return true, errors.New("Retire the guarded scheduled job before changing its execution contract")
		}
		return false, nil
	}
	dbPath, err := PanelDatabasePath()
	if err != nil {
		return true, err
	}
	err = maintenancetask.Materialize(ctx, c, dbPath, true)
	if errors.Is(err, maintenancetask.ErrUnsupportedProfile) && !registered {
		return false, nil
	}
	return true, err
}

// ReconcileSystemTasks leaves missing triggers and independent duplicate rows
// alone. Qualification reads code/configuration; it never runs a scheduled job.
func ReconcileSystemTasks(ctx context.Context) error {
	release, err := packages.Lifecycle(ctx, true)
	if err != nil {
		return err
	}
	defer release()
	// Registration itself never launches work. Recovery can adopt guarded
	// replacements while the durable runtime gate continues to reject every tick.
	dbPath, err := PanelDatabasePath()
	if err != nil {
		return err
	}
	rows, err := database.DB.QueryContext(ctx, `SELECT c.id,c.site_id,c.expression,c.command,c.user,COALESCE(s.path,''),COALESCE(s.deployment_strategy,''),COALESCE(s.app_type,''),COALESCE(s.app_directory,'.') FROM crons c LEFT JOIN sites s ON s.id=c.site_id ORDER BY c.id`)
	if err != nil {
		return err
	}
	type record struct {
		id, site                                         int
		expr, cmd, user, path, strategy, appType, appDir string
	}
	var records []record
	for rows.Next() {
		var r record
		if err = rows.Scan(&r.id, &r.site, &r.expr, &r.cmd, &r.user, &r.path, &r.strategy, &r.appType, &r.appDir); err != nil {
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
		if _, ok := scheduler.QualifySite(r.id, r.site, r.path, r.strategy, r.appType, r.expr, r.cmd, r.user); ok {
			continue
		}
		c, ok := maintenancetask.QualifySite(r.id, r.site, r.path, r.strategy, r.appType, r.appDir, r.expr, r.cmd, r.user)
		if !ok {
			continue
		}
		if err = maintenancetask.Materialize(ctx, c, dbPath, false); err != nil {
			failures = append(failures, fmt.Sprintf("scheduled job %d: %v", r.id, err))
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}
