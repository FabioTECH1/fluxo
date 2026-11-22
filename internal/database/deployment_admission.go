package database

import "context"

// ClaimPendingDeployment serializes deployment admission with the reciprocal
// Octane enable claim. SQLite evaluates the pending-runtime check in the same
// write statement that changes the deployment state.
func ClaimPendingDeployment(deployID int64, siteID int) (bool, error) {
	networkClause := ""
	absent, err := absentSiteNetworkSchema(context.Background(), DB)
	if err != nil {
		return false, err
	}
	if !absent {
		networkClause = " AND NOT EXISTS(SELECT 1 FROM site_network_operations WHERE site_id = ? AND phase = 'pending')"
	}
	args := []any{deployID, siteID, siteID, siteID}
	if !absent {
		args = append(args, siteID)
	}
	result, err := DB.Exec(`UPDATE deployments
		SET status = 'running', started_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND site_id = ? AND status = 'pending'
		AND EXISTS (SELECT 1 FROM sites WHERE id = ? AND COALESCE(deletion_status, '') = '')
		AND NOT EXISTS (SELECT 1 FROM daemons WHERE site_id = ?
			AND managed_kind = 'laravel_octane'
			AND status IN ('enable_pending','enable_publish_pending','enable_fallback_pending','enable_cleanup_pending'))`+networkClause, args...)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

// InterruptRunningDeployments records restart detection without inventing an
// execution finish. The previous process's actual terminal time is unknown.
func InterruptRunningDeployments() error {
	_, err := DB.Exec(`UPDATE deployments
		SET status = 'failed', output = 'Deployment was interrupted by a server restart.',
			failure_reason = 'Deployment was interrupted by a server restart.',
			finished_at = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE status = 'running'`)
	return err
}
