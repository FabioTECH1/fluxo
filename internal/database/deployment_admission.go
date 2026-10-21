package database

// ClaimPendingDeployment serializes deployment admission with the reciprocal
// Octane enable claim. SQLite evaluates the pending-runtime check in the same
// write statement that changes the deployment state.
func ClaimPendingDeployment(deployID int64, siteID int) (bool, error) {
	result, err := DB.Exec(`UPDATE deployments
		SET status = 'running', updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND site_id = ? AND status = 'pending'
		AND EXISTS (SELECT 1 FROM sites WHERE id = ? AND COALESCE(deletion_status, '') = '')
		AND NOT EXISTS (SELECT 1 FROM daemons WHERE site_id = ?
			AND managed_kind = 'laravel_octane'
			AND status IN ('enable_pending','enable_publish_pending','enable_fallback_pending','enable_cleanup_pending'))`, deployID, siteID, siteID, siteID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
