package server

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/daemon"
	"fluxo/internal/services/site"
	"fluxo/internal/syscmd"
)

func syncNodeDaemonForSite(ctx context.Context, siteID int) error {
	var sitePath, appType, strategy, nodePreset, nodeMode, packageManager, startCommand string
	var appPort int
	err := database.DB.QueryRow("SELECT path, app_type, deployment_strategy, COALESCE(app_port, 0), node_preset, node_mode, package_manager, start_command FROM sites WHERE id = ?", siteID).Scan(&sitePath, &appType, &strategy, &appPort, &nodePreset, &nodeMode, &packageManager, &startCommand)
	if err != nil {
		return err
	}

	if appType != "node" || site.NormalizeNodeMode(nodeMode) != "server" {
		return deleteNodeDaemon(ctx, siteID)
	}

	nodePreset = site.NormalizeNodePreset(nodePreset)
	packageManager = site.NormalizePackageManager(packageManager)
	if startCommand == "" {
		startCommand = site.DefaultNodeStartCommand(nodePreset, packageManager)
	}
	command := site.RenderNodeStartCommand(startCommand, appPort)
	if command == "" {
		return fmt.Errorf("Node.js start command is empty")
	}

	directory := sitePath
	if strategy == "zero-downtime" {
		directory = filepath.Join(directory, "current")
	}

	var daemonID int
	err = database.DB.QueryRow("SELECT id FROM daemons WHERE site_id = ? AND name = 'Node.js' ORDER BY id ASC LIMIT 1", siteID).Scan(&daemonID)
	if err == sql.ErrNoRows {
		res, err := database.DB.Exec(
			"INSERT INTO daemons (site_id, name, managed_kind, command, directory, user, instances, start_seconds, stop_seconds, stop_signal) VALUES (?, ?, 'node_app', ?, ?, ?, ?, ?, ?, ?)",
			siteID, "Node.js", command, directory, "fluxo", 1, 1, 15, "SIGTERM",
		)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		daemonID = int(id)
		if err := daemon.GenerateServiceFile(daemonID, command, directory, "fluxo", 1, 15, "SIGTERM"); err != nil {
			_ = daemon.Delete(ctx, daemonID)
			database.DB.Exec("DELETE FROM daemons WHERE id = ?", daemonID)
			return err
		}
		if err := daemon.EnableAndStart(ctx, daemonID); err != nil {
			_ = daemon.Delete(ctx, daemonID)
			database.DB.Exec("DELETE FROM daemons WHERE id = ?", daemonID)
			return err
		}
		database.DB.Exec("UPDATE daemons SET status = 'active' WHERE id = ?", daemonID)
		LogActivity(siteID, "daemon_created", "Daemon \"Node.js\" was created")
		return nil
	}
	if err != nil {
		return err
	}

	if _, err := database.DB.Exec("UPDATE daemons SET managed_kind = 'node_app', command = ?, directory = ?, user = 'fluxo', instances = 1, start_seconds = 1, stop_seconds = 15, stop_signal = 'SIGTERM' WHERE id = ?", command, directory, daemonID); err != nil {
		return err
	}
	if err := daemon.GenerateServiceFile(daemonID, command, directory, "fluxo", 1, 15, "SIGTERM"); err != nil {
		return err
	}
	if _, err := syscmd.Run(ctx, 10*time.Second, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := daemon.Restart(ctx, daemonID); err != nil {
		return err
	}
	database.DB.Exec("UPDATE daemons SET status = 'active' WHERE id = ?", daemonID)
	return nil
}

func deleteNodeDaemon(ctx context.Context, siteID int) error {
	rows, err := database.DB.Query("SELECT id FROM daemons WHERE site_id = ? AND name = 'Node.js'", siteID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		daemon.Delete(ctx, id)
		database.DB.Exec("DELETE FROM daemons WHERE id = ?", id)
	}
	return nil
}
