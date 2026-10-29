package maintenancetask

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"

	"fluxo/internal/services/scheduler"
	sitepkg "fluxo/internal/services/site"
	_ "modernc.org/sqlite"
)

var ErrMaintenance = errors.New("Scheduled system task paused while runtime maintenance or recovery is pending")

// queryAdmission is shared by dispatch and execution. Retained barriers remain
// authoritative even after their operation becomes terminal or a worker dies.
func queryAdmission(ctx context.Context, db *sql.DB, contract Contract) error {
	var id, site int
	var expression, command, user string
	if err := db.QueryRowContext(ctx, "SELECT id,site_id,expression,command,user FROM crons WHERE id=?", contract.ID).Scan(&id, &site, &expression, &command, &user); err != nil {
		return err
	}
	if err := matchDatabaseContract(ctx, db, contract, id, site, expression, command, user); err != nil {
		return errors.New("Scheduled system-task database contract changed")
	}
	for table, predicate := range map[string]string{
		"node_update_operations":    "kind!='check' AND status IN ('queued','running')",
		"node_update_generations":   "state NOT IN ('succeeded','rolled_back')",
		"node_update_barriers":      "1",
		"node_update_php_barriers":  "1",
		"php_update_operations":     "kind IN ('apply','verify','repair','rollback','repository_review','repository_apply') AND status IN ('queued','running')",
		"php_update_generations":    "state NOT IN ('succeeded','rolled_back')",
		"php_update_barriers":       "1",
		"php_repository_migrations": "state='pending'",
	} {
		var exists int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&exists); err != nil {
			return err
		}
		// A registered helper is only installed by a version with all these
		// tables. A missing table is lost admission evidence, not an old schema.
		if exists != 1 {
			return errors.New("Runtime maintenance admission schema is incomplete")
		}
		var pending bool
		if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+" WHERE "+predicate+")").Scan(&pending); err != nil {
			return err
		}
		if pending {
			return ErrMaintenance
		}
	}
	return nil
}

func withDatabase(ctx context.Context, r registration, checkPending bool) error {
	if !filepath.IsAbs(r.Database) || filepath.Clean(r.Database) != r.Database {
		return errors.New("System task requires an absolute panel database")
	}
	// Inspect ownership without reading SQLite bytes concurrently with writers.
	if err := trustedDatabase(r.Database); err != nil {
		return err
	}
	u := url.URL{Scheme: "file", Path: r.Database}
	db, err := sql.Open("sqlite", u.String()+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return err
	}
	defer db.Close()
	if checkPending {
		if err = queryAdmission(ctx, db, r.Contract); err != nil {
			return err
		}
		pending, err := scheduler.Pending()
		if err != nil {
			return err
		}
		if pending {
			return ErrMaintenance
		}
		busy, err := scheduler.UpgradeBusy()
		if err != nil {
			return err
		}
		if busy {
			return ErrMaintenance
		}
		return nil
	}
	var id, site int
	var expr, cmd, user string
	if err = db.QueryRowContext(ctx, "SELECT id,site_id,expression,command,user FROM crons WHERE id=?", r.Contract.ID).Scan(&id, &site, &expr, &cmd, &user); err != nil {
		return err
	}
	return matchDatabaseContract(ctx, db, r.Contract, id, site, expr, cmd, user)
}

func matchDatabaseContract(ctx context.Context, db *sql.DB, c Contract, id, site int, expression, command, username string) error {
	var current Contract
	var ok bool
	if c.Kind == ApplicationJob {
		dir := ""
		if site > 0 {
			var path, strategy, appType, appDirectory string
			if err := db.QueryRowContext(ctx, "SELECT path,COALESCE(deployment_strategy,'standard'),COALESCE(app_type,'php'),COALESCE(app_directory,'.') FROM sites WHERE id=?", site).Scan(&path, &strategy, &appType, &appDirectory); err != nil {
				return err
			}
			dir = sitepkg.ActiveSitePath(path, strategy)
			if appType == "python" {
				normalized, err := sitepkg.NormalizeAppDirectory(appDirectory)
				if err != nil {
					return err
				}
				dir = filepath.Join(dir, normalized)
			}
		}
		current, ok = QualifyApplication(id, site, dir, expression, command, username)
	} else {
		current, ok = Qualify(id, site, expression, command, username)
	}
	if !ok || current != c {
		return errors.New("Scheduled-job row or active site layout changed")
	}
	return nil
}
