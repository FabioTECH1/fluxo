package database

// CreatePendingOctaneDaemon claims enablement in the same SQLite write that
// checks deployment ownership. ClaimPendingDeployment performs the reciprocal
// check, so neither operation can slip between another owner's read and write.
func CreatePendingOctaneDaemon(siteID int, name, command, directory string, port ...int) (id int64, claimed bool, err error) {
	tx, err := DB.Begin()
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO daemons
		(site_id,name,managed_kind,status,command,directory,user,instances,start_seconds,stop_seconds,stop_signal)
		SELECT ?,?,'laravel_octane','enable_pending',?,?,'fluxo',1,1,15,'SIGTERM'
		WHERE NOT EXISTS (SELECT 1 FROM deployments WHERE site_id=? AND status='running')
		AND NOT EXISTS (SELECT 1 FROM daemons WHERE site_id=? AND managed_kind='laravel_octane' AND status IN ('enable_pending','enable_publish_pending','enable_fallback_pending','enable_cleanup_pending'))`, siteID, name, command, directory, siteID, siteID)
	if err != nil {
		return 0, false, err
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return 0, false, err
	}
	id, err = res.LastInsertId()
	if err != nil {
		return 0, false, err
	}
	if len(port) > 0 {
		res, err = tx.Exec("UPDATE sites SET app_port=? WHERE id=? AND NOT EXISTS (SELECT 1 FROM sites WHERE id!=? AND app_port=?)", port[0], siteID, siteID, port[0])
		if err != nil {
			return 0, false, err
		}
		n, err = res.RowsAffected()
		if err != nil || n != 1 {
			return 0, false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, false, err
	}
	return id, true, nil
}
