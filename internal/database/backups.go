package database

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func ListBackupDestinations() ([]BackupDestination, error) {
	rows, err := DB.Query(`SELECT id, name, provider, bucket, region, account_id, jurisdiction, prefix, server_id,
		access_key, secret_key, use_instance_role, is_default, created_at, updated_at
		FROM backup_destinations ORDER BY is_default DESC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	destinations := make([]BackupDestination, 0)
	for rows.Next() {
		var destination BackupDestination
		var useRole, isDefault int
		if err := rows.Scan(
			&destination.ID, &destination.Name, &destination.Provider, &destination.Bucket,
			&destination.Region, &destination.AccountID, &destination.Jurisdiction, &destination.Prefix, &destination.ServerID,
			&destination.AccessKey, &destination.SecretKey, &useRole, &isDefault,
			&destination.CreatedAt, &destination.UpdatedAt,
		); err != nil {
			return nil, err
		}
		destination.UseInstanceRole = useRole != 0
		destination.IsDefault = isDefault != 0
		destinations = append(destinations, destination)
	}
	return destinations, rows.Err()
}

func GetBackupDestination(id int) (BackupDestination, error) {
	var destination BackupDestination
	var useRole, isDefault int
	err := DB.QueryRow(`SELECT id, name, provider, bucket, region, account_id, jurisdiction, prefix, server_id,
		access_key, secret_key, use_instance_role, is_default, created_at, updated_at
		FROM backup_destinations WHERE id = ?`, id).Scan(
		&destination.ID, &destination.Name, &destination.Provider, &destination.Bucket,
		&destination.Region, &destination.AccountID, &destination.Jurisdiction, &destination.Prefix, &destination.ServerID,
		&destination.AccessKey, &destination.SecretKey, &useRole, &isDefault,
		&destination.CreatedAt, &destination.UpdatedAt,
	)
	destination.UseInstanceRole = useRole != 0
	destination.IsDefault = isDefault != 0
	return destination, err
}

func CreateBackupDestination(destination *BackupDestination) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if destination.IsDefault {
		if _, err := tx.Exec("UPDATE backup_destinations SET is_default = 0"); err != nil {
			return err
		}
	}
	result, err := tx.Exec(`INSERT INTO backup_destinations
		(name, provider, bucket, region, account_id, jurisdiction, prefix, server_id, access_key, secret_key, use_instance_role, is_default)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		destination.Name, destination.Provider, destination.Bucket, destination.Region,
		destination.AccountID, destination.Jurisdiction, destination.Prefix, destination.ServerID, destination.AccessKey,
		destination.SecretKey, destination.UseInstanceRole, destination.IsDefault)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	destination.ID = int(id)
	return tx.Commit()
}

func UpdateBackupDestination(destination BackupDestination) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if destination.IsDefault {
		if _, err := tx.Exec("UPDATE backup_destinations SET is_default = 0"); err != nil {
			return err
		}
	}
	result, err := tx.Exec(`UPDATE backup_destinations SET name = ?, access_key = ?, secret_key = ?,
		use_instance_role = ?, is_default = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		destination.Name, destination.AccessKey, destination.SecretKey, destination.UseInstanceRole,
		destination.IsDefault, destination.ID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func DeleteBackupDestination(id int) error {
	var references int
	if err := DB.QueryRow(`SELECT
		(SELECT COUNT(*) FROM backup_plans WHERE destination_id = ?) +
		(SELECT COUNT(*) FROM backup_runs WHERE destination_id = ?)`, id, id).Scan(&references); err != nil {
		return err
	}
	if references > 0 {
		return errors.New("destination is still used by backup plans or history")
	}
	result, err := DB.Exec("DELETE FROM backup_destinations WHERE id = ?", id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func ListBackupPlans() ([]BackupPlan, error) {
	rows, err := DB.Query(`SELECT p.id, p.name, p.site_id, COALESCE(s.domain, ''), p.destination_id,
		COALESCE(d.name, ''), p.include_files, p.schedule, p.backup_hour, p.retention_profile,
		p.enabled, p.next_run_at, p.last_run_at, p.created_at, p.updated_at
		FROM backup_plans p
		LEFT JOIN sites s ON s.id = p.site_id
		LEFT JOIN backup_destinations d ON d.id = p.destination_id
		ORDER BY p.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	plans := make([]BackupPlan, 0)
	for rows.Next() {
		plan, err := scanBackupPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanBackupPlan(row rowScanner) (BackupPlan, error) {
	var plan BackupPlan
	var includeFiles, enabled int
	var nextRun, lastRun sql.NullTime
	err := row.Scan(
		&plan.ID, &plan.Name, &plan.SiteID, &plan.SiteDomain, &plan.DestinationID,
		&plan.DestinationName, &includeFiles, &plan.Schedule, &plan.BackupHour,
		&plan.RetentionProfile, &enabled, &nextRun, &lastRun, &plan.CreatedAt, &plan.UpdatedAt,
	)
	if err != nil {
		return plan, err
	}
	plan.IncludeFiles = includeFiles != 0
	plan.Enabled = enabled != 0
	if nextRun.Valid {
		value := nextRun.Time
		plan.NextRunAt = &value
	}
	if lastRun.Valid {
		value := lastRun.Time
		plan.LastRunAt = &value
	}
	plan.DatabaseIDs, err = BackupPlanDatabaseIDs(plan.ID)
	return plan, err
}

func GetBackupPlan(id int) (BackupPlan, error) {
	row := DB.QueryRow(`SELECT p.id, p.name, p.site_id, COALESCE(s.domain, ''), p.destination_id,
		COALESCE(d.name, ''), p.include_files, p.schedule, p.backup_hour, p.retention_profile,
		p.enabled, p.next_run_at, p.last_run_at, p.created_at, p.updated_at
		FROM backup_plans p
		LEFT JOIN sites s ON s.id = p.site_id
		LEFT JOIN backup_destinations d ON d.id = p.destination_id
		WHERE p.id = ?`, id)
	return scanBackupPlan(row)
}

func BackupPlanDatabaseIDs(planID int) ([]int, error) {
	rows, err := DB.Query("SELECT database_id FROM backup_plan_databases WHERE plan_id = ? ORDER BY database_id", planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int, 0)
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func CreateBackupPlan(plan *BackupPlan) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`INSERT INTO backup_plans
		(name, site_id, destination_id, include_files, schedule, backup_hour, retention_profile, enabled, next_run_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, plan.Name, plan.SiteID, plan.DestinationID,
		plan.IncludeFiles, plan.Schedule, plan.BackupHour, plan.RetentionProfile, plan.Enabled, plan.NextRunAt)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	plan.ID = int(id)
	if err := replaceBackupPlanDatabases(tx, plan.ID, plan.DatabaseIDs); err != nil {
		return err
	}
	return tx.Commit()
}

func UpdateBackupPlan(plan BackupPlan) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE backup_plans SET name = ?, site_id = ?, destination_id = ?,
		include_files = ?, schedule = ?, backup_hour = ?, retention_profile = ?, enabled = ?,
		next_run_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		plan.Name, plan.SiteID, plan.DestinationID, plan.IncludeFiles, plan.Schedule,
		plan.BackupHour, plan.RetentionProfile, plan.Enabled, plan.NextRunAt, plan.ID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	if err := replaceBackupPlanDatabases(tx, plan.ID, plan.DatabaseIDs); err != nil {
		return err
	}
	return tx.Commit()
}

func replaceBackupPlanDatabases(tx *sql.Tx, planID int, databaseIDs []int) error {
	if _, err := tx.Exec("DELETE FROM backup_plan_databases WHERE plan_id = ?", planID); err != nil {
		return err
	}
	for _, databaseID := range databaseIDs {
		if _, err := tx.Exec("INSERT INTO backup_plan_databases (plan_id, database_id) VALUES (?, ?)", planID, databaseID); err != nil {
			return err
		}
	}
	return nil
}

func DeleteBackupPlan(id int) error {
	var active int
	if err := DB.QueryRow("SELECT COUNT(*) FROM backup_runs WHERE plan_id = ? AND status IN ('queued', 'running')", id).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return errors.New("plan has a queued or running backup")
	}
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM backup_plan_databases WHERE plan_id = ?", id); err != nil {
		return err
	}
	result, err := tx.Exec("DELETE FROM backup_plans WHERE id = ?", id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func DeleteBackupPlansForSite(siteID int) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM backup_plan_databases WHERE plan_id IN (SELECT id FROM backup_plans WHERE site_id = ?)", siteID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM backup_plans WHERE site_id = ?", siteID); err != nil {
		return err
	}
	return tx.Commit()
}

func CreateBackupRun(run BackupRun) error {
	_, err := DB.Exec(`INSERT INTO backup_runs
		(id, plan_id, plan_name, destination_id, destination_name, site_id, site_domain, trigger, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'queued')`, run.ID, run.PlanID, run.PlanName,
		run.DestinationID, run.DestinationName, run.SiteID, run.SiteDomain, run.Trigger)
	return err
}

func CreateScheduledBackupRun(run BackupRun, nextRunAt time.Time) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO backup_runs
		(id, plan_id, plan_name, destination_id, destination_name, site_id, site_domain, trigger, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'scheduled', 'queued')`, run.ID, run.PlanID, run.PlanName,
		run.DestinationID, run.DestinationName, run.SiteID, run.SiteDomain); err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE backup_plans SET next_run_at = ?
		WHERE id = ? AND enabled = 1 AND schedule != 'manual'`, nextRunAt, run.PlanID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return errors.New("backup plan is no longer scheduled")
	}
	return tx.Commit()
}

func ListBackupRuns(limit int) ([]BackupRun, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := DB.Query(`SELECT id, plan_id, plan_name, destination_id, destination_name,
		site_id, site_domain, trigger, status, total_size_bytes, manifest_key, manifest_version_id, error,
		started_at, completed_at, created_at FROM backup_runs ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make([]BackupRun, 0)
	for rows.Next() {
		run, err := scanBackupRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func ListCompletedBackupRunsForPlan(planID int) ([]BackupRun, error) {
	rows, err := DB.Query(`SELECT id, plan_id, plan_name, destination_id, destination_name,
		site_id, site_domain, trigger, status, total_size_bytes, manifest_key, manifest_version_id, error,
		started_at, completed_at, created_at FROM backup_runs
		WHERE plan_id = ? AND status = 'completed' ORDER BY completed_at DESC`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make([]BackupRun, 0)
	for rows.Next() {
		run, err := scanBackupRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func GetBackupRun(id string) (BackupRun, error) {
	return scanBackupRun(DB.QueryRow(`SELECT id, plan_id, plan_name, destination_id, destination_name,
		site_id, site_domain, trigger, status, total_size_bytes, manifest_key, manifest_version_id, error,
		started_at, completed_at, created_at FROM backup_runs WHERE id = ?`, id))
}

func scanBackupRun(row rowScanner) (BackupRun, error) {
	var run BackupRun
	var startedAt, completedAt sql.NullTime
	err := row.Scan(&run.ID, &run.PlanID, &run.PlanName, &run.DestinationID,
		&run.DestinationName, &run.SiteID, &run.SiteDomain, &run.Trigger, &run.Status,
		&run.TotalSizeBytes, &run.ManifestKey, &run.ManifestVersionID, &run.Error, &startedAt, &completedAt, &run.CreatedAt)
	if err != nil {
		return run, err
	}
	if startedAt.Valid {
		value := startedAt.Time
		run.StartedAt = &value
	}
	if completedAt.Valid {
		value := completedAt.Time
		run.CompletedAt = &value
	}
	run.Artifacts, err = ListBackupArtifacts(run.ID)
	return run, err
}

func ListBackupArtifacts(runID string) ([]BackupArtifact, error) {
	rows, err := DB.Query(`SELECT id, run_id, kind, database_id, database_name, engine,
		object_key, object_version_id, filename, size_bytes, sha256, created_at
		FROM backup_artifacts WHERE run_id = ? ORDER BY id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	artifacts := make([]BackupArtifact, 0)
	for rows.Next() {
		var artifact BackupArtifact
		if err := rows.Scan(&artifact.ID, &artifact.RunID, &artifact.Kind, &artifact.DatabaseID,
			&artifact.DatabaseName, &artifact.Engine, &artifact.ObjectKey, &artifact.ObjectVersionID, &artifact.Filename,
			&artifact.SizeBytes, &artifact.SHA256, &artifact.CreatedAt); err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}

func InsertBackupArtifact(artifact *BackupArtifact) error {
	result, err := DB.Exec(`INSERT INTO backup_artifacts
		(run_id, kind, database_id, database_name, engine, object_key, object_version_id, filename, size_bytes, sha256)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, artifact.RunID, artifact.Kind, artifact.DatabaseID,
		artifact.DatabaseName, artifact.Engine, artifact.ObjectKey, artifact.ObjectVersionID, artifact.Filename,
		artifact.SizeBytes, artifact.SHA256)
	if err != nil {
		return err
	}
	id, _ := result.LastInsertId()
	artifact.ID = int(id)
	return nil
}

func PrepareBackupRunObjects(runID, manifestKey string, artifacts []BackupArtifact) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM backup_artifacts WHERE run_id = ?", runID); err != nil {
		return err
	}
	for index := range artifacts {
		result, err := tx.Exec(`INSERT INTO backup_artifacts
			(run_id, kind, database_id, database_name, engine, object_key, object_version_id, filename, size_bytes, sha256)
			VALUES (?, ?, ?, ?, ?, ?, '', ?, ?, ?)`, artifacts[index].RunID, artifacts[index].Kind,
			artifacts[index].DatabaseID, artifacts[index].DatabaseName, artifacts[index].Engine,
			artifacts[index].ObjectKey, artifacts[index].Filename, artifacts[index].SizeBytes, artifacts[index].SHA256)
		if err != nil {
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		artifacts[index].ID = int(id)
	}
	result, err := tx.Exec("UPDATE backup_runs SET manifest_key = ?, manifest_version_id = '' WHERE id = ? AND status = 'running'", manifestKey, runID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func SetBackupArtifactVersion(id int, versionID string) error {
	_, err := DB.Exec("UPDATE backup_artifacts SET object_version_id = ? WHERE id = ?", versionID, id)
	return err
}

func SetBackupManifestVersion(id, versionID string) error {
	_, err := DB.Exec("UPDATE backup_runs SET manifest_version_id = ? WHERE id = ?", versionID, id)
	return err
}

func ClearBackupRunObjects(id string) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM backup_artifacts WHERE run_id = ?", id); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE backup_runs SET manifest_key = '', manifest_version_id = '' WHERE id = ?", id); err != nil {
		return err
	}
	return tx.Commit()
}

func MarkBackupRunRunning(id string) error {
	_, err := DB.Exec("UPDATE backup_runs SET status = 'running', started_at = CURRENT_TIMESTAMP, error = '' WHERE id = ? AND status = 'queued'", id)
	return err
}

func CompleteBackupRun(id string, planID int, totalSize int64, nextRunAt *time.Time) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE backup_runs SET status = 'completed',
		total_size_bytes = ?, completed_at = CURRENT_TIMESTAMP, error = '' WHERE id = ? AND status = 'running'`, totalSize, id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return errors.New("backup run is no longer running")
	}
	if _, err := tx.Exec(`UPDATE backup_plans SET next_run_at = ?, last_run_at = CURRENT_TIMESTAMP
		WHERE id = ?`, nextRunAt, planID); err != nil {
		return err
	}
	return tx.Commit()
}

func MarkBackupRunFailed(id, message string) error {
	_, err := DB.Exec(`UPDATE backup_runs SET status = 'failed', error = ?, completed_at = CURRENT_TIMESTAMP
		WHERE id = ?`, message, id)
	return err
}

func MarkInterruptedBackupRunsFailed() error {
	_, err := DB.Exec(`UPDATE backup_runs SET status = 'failed', error = 'Backup was interrupted by a server restart.',
		completed_at = CURRENT_TIMESTAMP WHERE status = 'running'`)
	return err
}

func QueuedBackupRunIDs() ([]string, error) {
	rows, err := DB.Query("SELECT id FROM backup_runs WHERE status = 'queued' ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func NextQueuedBackupRunID() (string, error) {
	var id string
	err := DB.QueryRow("SELECT id FROM backup_runs WHERE status = 'queued' ORDER BY created_at LIMIT 1").Scan(&id)
	return id, err
}

func DueBackupPlanIDs(now time.Time) ([]int, error) {
	rows, err := DB.Query(`SELECT id FROM backup_plans WHERE enabled = 1 AND schedule != 'manual'
		AND next_run_at IS NOT NULL AND next_run_at <= ? ORDER BY next_run_at`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int, 0)
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func SetBackupPlanScheduleState(planID int, nextRun time.Time, lastRun bool) error {
	if lastRun {
		_, err := DB.Exec("UPDATE backup_plans SET next_run_at = ?, last_run_at = CURRENT_TIMESTAMP WHERE id = ?", nextRun, planID)
		return err
	}
	_, err := DB.Exec("UPDATE backup_plans SET next_run_at = ? WHERE id = ?", nextRun, planID)
	return err
}

func BackupPlanHasActiveRun(planID int) (bool, error) {
	var active bool
	err := DB.QueryRow("SELECT EXISTS(SELECT 1 FROM backup_runs WHERE plan_id = ? AND status IN ('queued', 'running'))", planID).Scan(&active)
	return active, err
}

func DeleteBackupRunRecord(id string) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM backup_artifacts WHERE run_id = ?", id); err != nil {
		return err
	}
	result, err := tx.Exec("DELETE FROM backup_runs WHERE id = ?", id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("backup run not found")
	}
	return tx.Commit()
}
