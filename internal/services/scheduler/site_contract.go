package scheduler

import (
	"context"
	"database/sql"
	sitepkg "fluxo/internal/services/site"
)

// QualifySite binds the exact generated command to the site's current deployment
// layout. The interpreter comes from the command, not a possibly newer site default.
func QualifySite(id, siteID int, path, strategy, appType, expression, command, user string) (Entry, bool) {
	if appType != "laravel" {
		return Entry{}, false
	}
	return Qualify(id, siteID, sitepkg.ActiveSitePath(path, strategy), expression, command, user)
}
func MatchesSite(entry Entry, siteID int, path, strategy, appType, expression, command, user string) bool {
	current, ok := QualifySite(entry.ID, siteID, path, strategy, appType, expression, command, user)
	return ok && entry.SiteID == current.SiteID && entry.Directory == current.Directory && entry.Version == current.Version && entry.Command == current.Command
}

func validateCommittedSite(ctx context.Context, db *sql.DB, e Entry) error {
	var path, strategy, appType, expression, command, user string
	err := db.QueryRowContext(ctx, `SELECT s.path,COALESCE(s.deployment_strategy,''),s.app_type,c.expression,c.command,c.user FROM crons c JOIN sites s ON s.id=c.site_id WHERE c.id=? AND c.site_id=?`, e.ID, e.SiteID).Scan(&path, &strategy, &appType, &expression, &command, &user)
	if err != nil {
		return err
	}
	if !MatchesSite(e, e.SiteID, path, strategy, appType, expression, command, user) {
		return sql.ErrNoRows
	}
	return nil
}
