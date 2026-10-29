package maintenancetask

import (
	"context"
	"database/sql"
	"fmt"

	"fluxo/internal/services/scheduler"
	sitepkg "fluxo/internal/services/site"
)

// InspectDatabase is read-only. Both PHP review and execution use this same
// inventory; an unavailable registration never becomes permission to run raw.
func InspectDatabase(ctx context.Context, db *sql.DB) ([]Receipt, []string, error) {
	var receipts []Receipt
	var reasons []string
	var dbPath string
	if e := Quiescent(ctx); e != nil {
		reasons = append(reasons, "Scheduled-job coordination: "+e.Error())
	}
	if err := db.QueryRowContext(ctx, "SELECT file FROM pragma_database_list WHERE name='main'").Scan(&dbPath); err != nil {
		return nil, nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT c.id,c.site_id,COALESCE(c.name,''),c.expression,c.command,c.user,COALESCE(s.path,''),COALESCE(s.domain,''),COALESCE(s.deployment_strategy,''),COALESCE(s.app_type,''),COALESCE(s.app_directory,'.') FROM crons c LEFT JOIN sites s ON s.id=c.site_id ORDER BY c.id`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, siteID int
		var name, expr, cmd, user, path, domain, strategy, appType, appDir string
		if err = rows.Scan(&id, &siteID, &name, &expr, &cmd, &user, &path, &domain, &strategy, &appType, &appDir); err != nil {
			return nil, nil, err
		}
		label := sitepkg.MaintenanceLabel(name, domain)
		if expected, ok := scheduler.QualifySite(id, siteID, path, strategy, appType, expr, cmd, user); ok {
			actual, e := scheduler.Inspect(ctx, id)
			if e != nil || !actual.Ready || actual.Identity == "" || actual.Directory != expected.Directory || actual.Command != expected.Command || actual.Version != expected.Version || actual.Database != dbPath {
				reasons = append(reasons, fmt.Sprintf("Scheduled task %s needs verified managed admission before runtime maintenance", label))
			} else if e = scheduler.NodeMaintenanceReady(); e != nil {
				reasons = append(reasons, fmt.Sprintf("Scheduled task %s: %v", label, e))
			}
			continue
		}
		c, ok := QualifySite(id, siteID, path, strategy, appType, appDir, expr, cmd, user)
		if !ok {
			reasons = append(reasons, fmt.Sprintf("Scheduled task %s requires manual runtime maintenance", label))
			continue
		}
		r, e := Inspect(ctx, c, dbPath)
		if e != nil {
			reasons = append(reasons, fmt.Sprintf("Scheduled task %s: %v", label, e))
			continue
		}
		r.Name, r.Domain = name, domain
		receipts = append(receipts, r)
	}
	return receipts, reasons, rows.Err()
}
