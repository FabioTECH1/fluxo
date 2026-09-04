package server

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"

	"fluxo/internal/database"
	"fluxo/internal/services/daemon"
	"fluxo/internal/services/site"
)

var pythonDaemonSyncMu sync.Mutex

func syncPythonDaemonForSite(ctx context.Context, siteID int) error {
	pythonDaemonSyncMu.Lock()
	defer pythonDaemonSyncMu.Unlock()

	var sitePath, appType, strategy, preset, entrypoint, appDirectory, startCommand string
	var appPort int
	err := database.DB.QueryRow(`SELECT path, app_type, deployment_strategy, COALESCE(app_port, 0),
		COALESCE(python_preset, ''), COALESCE(python_entrypoint, ''), COALESCE(app_directory, '.'), COALESCE(start_command, '')
		FROM sites WHERE id = ?`, siteID).Scan(&sitePath, &appType, &strategy, &appPort, &preset, &entrypoint, &appDirectory, &startCommand)
	if err != nil {
		return err
	}
	if appType != "python" {
		return deletePythonDaemon(ctx, siteID)
	}
	if startCommand == "" {
		startCommand = site.DefaultPythonStartCommand(preset, entrypoint)
	}
	command := site.RenderPythonStartCommand(startCommand, appPort)
	if command == "" {
		return fmt.Errorf("Python start command is empty")
	}
	command = fmt.Sprintf("/usr/bin/env PYTHONUNBUFFERED=1 PORT=%d HOST=127.0.0.1 %s", appPort, command)
	appDirectory, err = site.NormalizeAppDirectory(appDirectory)
	if err != nil {
		return err
	}
	directory := sitePath
	if strategy == "zero-downtime" {
		directory = filepath.Join(directory, "current")
	}
	directory = filepath.Join(directory, appDirectory)
	environmentFile := filepath.Join(sitePath, ".env")

	var daemonID int
	err = database.DB.QueryRow("SELECT id FROM daemons WHERE site_id = ? AND managed_kind = 'python_app' ORDER BY id ASC LIMIT 1", siteID).Scan(&daemonID)
	if err == sql.ErrNoRows {
		result, err := database.DB.Exec(`INSERT INTO daemons
			(site_id, name, managed_kind, command, directory, user, instances, start_seconds, stop_seconds, stop_signal)
			VALUES (?, 'Python', 'python_app', ?, ?, 'fluxo', 1, 1, 15, 'SIGTERM')`, siteID, command, directory)
		if err != nil {
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		daemonID = int(id)
		if err := daemon.GenerateServiceFileWithEnvironmentFile(daemonID, command, directory, "fluxo", 1, 15, "SIGTERM", environmentFile); err != nil {
			_ = daemon.Delete(ctx, daemonID)
			_, _ = database.DB.Exec("DELETE FROM daemons WHERE id = ?", daemonID)
			return err
		}
		if err := daemon.EnableAndStart(ctx, daemonID); err != nil {
			_ = daemon.Delete(ctx, daemonID)
			_, _ = database.DB.Exec("DELETE FROM daemons WHERE id = ?", daemonID)
			return err
		}
		if err := daemon.WaitHealthy(ctx, daemonID); err != nil {
			_ = daemon.Delete(ctx, daemonID)
			_, _ = database.DB.Exec("DELETE FROM daemons WHERE id = ?", daemonID)
			return fmt.Errorf("Python application process did not become healthy: %w", err)
		}
		_, _ = database.DB.Exec("UPDATE daemons SET status = 'active' WHERE id = ?", daemonID)
		LogActivity(siteID, "daemon_created", "Managed Python application process was created")
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := database.DB.Exec(`UPDATE daemons SET managed_kind = 'python_app', command = ?, directory = ?,
		user = 'fluxo', instances = 1, start_seconds = 1, stop_seconds = 15, stop_signal = 'SIGTERM' WHERE id = ?`, command, directory, daemonID); err != nil {
		return err
	}
	if err := daemon.GenerateServiceFileWithEnvironmentFile(daemonID, command, directory, "fluxo", 1, 15, "SIGTERM", environmentFile); err != nil {
		return err
	}
	if err := daemon.Reload(ctx); err != nil {
		return err
	}
	if err := daemon.RestartAndWait(ctx, daemonID); err != nil {
		return fmt.Errorf("Python application process did not become healthy: %w", err)
	}
	_, _ = database.DB.Exec("UPDATE daemons SET status = 'active' WHERE id = ?", daemonID)
	return nil
}

// syncPythonCustomDaemonDirectories keeps custom site processes aligned with
// the app directory selected for Python commands, crons, and deployments.
func syncPythonCustomDaemonDirectories(ctx context.Context, siteID int) error {
	pythonDaemonSyncMu.Lock()
	defer pythonDaemonSyncMu.Unlock()

	var sitePath, appType, strategy, appDirectory string
	if err := database.DB.QueryRow(`SELECT path, app_type, deployment_strategy, COALESCE(app_directory, '.')
		FROM sites WHERE id = ?`, siteID).Scan(&sitePath, &appType, &strategy, &appDirectory); err != nil {
		return err
	}
	if appType != "python" {
		return nil
	}
	normalized, err := site.NormalizeAppDirectory(appDirectory)
	if err != nil {
		return err
	}
	directory := filepath.Join(site.ActiveSitePath(sitePath, strategy), normalized)
	environmentFile := filepath.Join(sitePath, ".env")

	type daemonRecord struct {
		id, startSeconds, stopSeconds        int
		command, directory, user, stopSignal string
		active                               bool
	}
	rows, err := database.DB.Query(`SELECT id, command, directory, user, start_seconds, stop_seconds, stop_signal
		FROM daemons WHERE site_id = ? AND COALESCE(managed_kind, '') = ''`, siteID)
	if err != nil {
		return err
	}
	var records []daemonRecord
	for rows.Next() {
		var record daemonRecord
		if err := rows.Scan(&record.id, &record.command, &record.directory, &record.user, &record.startSeconds, &record.stopSeconds, &record.stopSignal); err != nil {
			rows.Close()
			return err
		}
		if filepath.Clean(record.directory) != filepath.Clean(directory) {
			records = append(records, record)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}
	for index := range records {
		records[index].active = daemon.IsActive(ctx, records[index].id)
	}

	for _, record := range records {
		if err := daemon.GenerateServiceFileWithEnvironmentFile(record.id, record.command, directory, record.user,
			record.startSeconds, record.stopSeconds, record.stopSignal, environmentFile); err != nil {
			return fmt.Errorf("update custom daemon %d: %w", record.id, err)
		}
		if _, err := database.DB.Exec("UPDATE daemons SET directory = ? WHERE id = ?", directory, record.id); err != nil {
			_ = daemon.GenerateServiceFileWithEnvironmentFile(record.id, record.command, record.directory, record.user,
				record.startSeconds, record.stopSeconds, record.stopSignal, environmentFile)
			return fmt.Errorf("save custom daemon %d directory: %w", record.id, err)
		}
	}
	if err := daemon.Reload(ctx); err != nil {
		return err
	}
	for _, record := range records {
		if record.active {
			if err := daemon.RestartAndWait(ctx, record.id); err != nil {
				return fmt.Errorf("restart custom daemon %d: %w", record.id, err)
			}
		}
	}
	return nil
}

func deletePythonDaemon(ctx context.Context, siteID int) error {
	rows, err := database.DB.Query("SELECT id FROM daemons WHERE site_id = ? AND managed_kind = 'python_app'", siteID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := daemon.Delete(ctx, id); err != nil {
			return err
		}
		if _, err := database.DB.Exec("DELETE FROM daemons WHERE id = ?", id); err != nil {
			return err
		}
	}
	return nil
}
