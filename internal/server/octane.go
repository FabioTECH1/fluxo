package server

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/safeinput"
	"fluxo/internal/services/daemon"
	"fluxo/internal/services/deploy"
	sitepkg "fluxo/internal/services/site"
	"fluxo/internal/syscmd"
)

const (
	octaneDaemonName = "Laravel Octane"
	octaneReloadLine = "$FLUXO_PHP artisan octane:reload"
)

func isOctaneEnabled(siteID int) bool {
	var count int
	_ = database.DB.QueryRow("SELECT COUNT(*) FROM daemons WHERE site_id = ? AND (name = ? OR command LIKE '%artisan octane:start%')", siteID, octaneDaemonName).Scan(&count)
	return count > 0
}

func nextOctanePort(siteID int) (int, error) {
	used := map[int]bool{}
	rows, err := database.DB.Query("SELECT id, COALESCE(app_port, 0) FROM sites WHERE app_port > 0")
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, port int
		if rows.Scan(&id, &port) == nil && id != siteID {
			used[port] = true
		}
	}

	for port := 8000; port <= 65535; port++ {
		if !used[port] && safeinput.ValidatePortNumber(port) {
			return port, nil
		}
	}

	return 0, fmt.Errorf("no available application ports")
}

func addOctaneReloadToDeployScript(siteID int) error {
	var script sql.NullString
	var strategy, appType, scriptMode string
	if err := database.DB.QueryRow("SELECT deploy_script, deployment_strategy, app_type, COALESCE(deploy_script_mode, 'legacy') FROM sites WHERE id = ?", siteID).Scan(&script, &strategy, &appType, &scriptMode); err != nil {
		return err
	}
	if scriptMode == deploy.ScriptModeManaged {
		return nil
	}

	current := script.String
	if current == "" {
		current = deploy.GenerateDeployScript(strategy, appType)
	}
	if strings.Contains(current, "artisan octane:reload") {
		return nil
	}

	current = strings.TrimRight(current, "\r\n")
	current += "\n\n" + octaneReloadLine + "\n"
	_, err := database.DB.Exec("UPDATE sites SET deploy_script = ? WHERE id = ?", current, siteID)
	return err
}

func removeOctaneReloadFromDeployScript(siteID int) error {
	var script sql.NullString
	var scriptMode string
	if err := database.DB.QueryRow("SELECT deploy_script, COALESCE(deploy_script_mode, 'legacy') FROM sites WHERE id = ?", siteID).Scan(&script, &scriptMode); err != nil {
		return err
	}
	if scriptMode == deploy.ScriptModeManaged {
		return nil
	}
	if !script.Valid || script.String == "" {
		return nil
	}

	lines := strings.Split(strings.ReplaceAll(script.String, "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Contains(line, "artisan octane:reload") {
			continue
		}
		kept = append(kept, line)
	}

	_, err := database.DB.Exec("UPDATE sites SET deploy_script = ? WHERE id = ?", strings.Join(kept, "\n"), siteID)
	return err
}

func deleteOctaneDaemons(ctx context.Context, siteID int) error {
	rows, err := database.DB.Query("SELECT id FROM daemons WHERE site_id = ? AND (name = ? OR command LIKE '%artisan octane:start%')", siteID, octaneDaemonName)
	if err != nil {
		return err
	}
	defer rows.Close()

	var daemonIDs []int
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil {
			daemonIDs = append(daemonIDs, id)
		}
	}

	for _, id := range daemonIDs {
		daemon.Delete(ctx, id)
		database.DB.Exec("DELETE FROM daemons WHERE id = ?", id)
	}
	return nil
}

func syncOctaneDaemonForSite(ctx context.Context, siteID int) error {
	var sitePath, phpVersion, appType, strategy string
	var appPort int
	if err := database.DB.QueryRow("SELECT path, php_version, app_type, deployment_strategy, COALESCE(app_port, 0) FROM sites WHERE id = ?", siteID).Scan(&sitePath, &phpVersion, &appType, &strategy, &appPort); err != nil {
		return err
	}
	if (appType != "laravel" && appType != "php") || strategy == "zero-downtime" || !safeinput.ValidatePortNumber(appPort) {
		if err := deleteOctaneDaemons(ctx, siteID); err != nil {
			return err
		}
		if err := removeOctaneReloadFromDeployScript(siteID); err != nil {
			return err
		}
		if appType != "node" {
			database.DB.Exec("UPDATE sites SET app_port = 0 WHERE id = ?", siteID)
		}
		return nil
	}
	if phpVersion == "" {
		phpVersion = "8.4"
	}

	directory := sitePath
	command := fmt.Sprintf("php%s artisan octane:start --host=127.0.0.1 --port=%d", phpVersion, appPort)

	rows, err := database.DB.Query("SELECT id FROM daemons WHERE site_id = ? AND (name = ? OR command LIKE '%artisan octane:start%')", siteID, octaneDaemonName)
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
		if _, err := database.DB.Exec("UPDATE daemons SET managed_kind = 'laravel_octane', command = ?, directory = ?, user = 'fluxo', instances = 1, start_seconds = 1, stop_seconds = 15, stop_signal = 'SIGTERM' WHERE id = ?", command, directory, id); err != nil {
			return err
		}
		if err := daemon.GenerateServiceFile(id, command, directory, "fluxo", 1, 15, "SIGTERM"); err != nil {
			return err
		}
		if _, err := syscmd.Run(ctx, 10*time.Second, "systemctl", "daemon-reload"); err != nil {
			return err
		}
		if err := daemon.Restart(ctx, id); err != nil {
			return err
		}
		database.DB.Exec("UPDATE daemons SET status = 'active' WHERE id = ?", id)
	}

	return nil
}

// POST /api/v1/sites/{id}/features/octane/enable
func (s *Server) handleEnableOctane() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var sitePath, phpVersion, strategy string
		var appPort int
		err := database.DB.QueryRow("SELECT path, php_version, deployment_strategy, COALESCE(app_port, 0) FROM sites WHERE id = ?", siteID).Scan(&sitePath, &phpVersion, &strategy, &appPort)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		capabilities, err := composerCapabilitiesForSite(siteID)
		if err != nil {
			http.Error(w, "Unable to inspect the active composer.lock", http.StatusUnprocessableEntity)
			return
		}
		if !capabilities.Octane {
			http.Error(w, "laravel/octane 1.0 or later was not found in the active composer.lock", http.StatusUnprocessableEntity)
			return
		}
		if strategy == "zero-downtime" {
			http.Error(w, "Laravel Octane cannot be enabled while zero-downtime deployment is active", http.StatusBadRequest)
			return
		}
		if phpVersion == "" {
			phpVersion = "8.4"
		}

		if safeinput.ValidatePortNumber(appPort) {
			var ownerCount int
			if err := database.DB.QueryRow("SELECT COUNT(*) FROM sites WHERE id != ? AND app_port = ?", siteID, appPort).Scan(&ownerCount); err != nil {
				http.Error(w, "Failed to validate Octane port", http.StatusInternalServerError)
				return
			}
			if ownerCount > 0 {
				appPort = 0
			}
		}

		if !safeinput.ValidatePortNumber(appPort) {
			appPort, err = nextOctanePort(siteID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if _, err := database.DB.Exec("UPDATE sites SET app_port = ? WHERE id = ?", appPort, siteID); err != nil {
				http.Error(w, "Failed to reserve Octane port", http.StatusInternalServerError)
				return
			}
		}

		dir := sitepkg.ActiveSitePath(sitePath, strategy)
		cmd := fmt.Sprintf("php%s artisan octane:start --host=127.0.0.1 --port=%d", phpVersion, appPort)

		if isOctaneEnabled(siteID) {
			if err := addOctaneReloadToDeployScript(siteID); err != nil {
				http.Error(w, "Failed to update deployment script", http.StatusInternalServerError)
				return
			}
			if err := syncOctaneDaemonForSite(r.Context(), siteID); err != nil {
				http.Error(w, "Failed to sync Octane daemon", http.StatusInternalServerError)
				return
			}
			if err := regenerateNginxForSiteWithError(siteID); err != nil {
				http.Error(w, "Failed to update Nginx config: "+err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		res, err := database.DB.Exec(
			"INSERT INTO daemons (site_id, name, managed_kind, command, directory, user, instances, start_seconds, stop_seconds, stop_signal) VALUES (?, ?, 'laravel_octane', ?, ?, ?, ?, ?, ?, ?)",
			siteID, octaneDaemonName, cmd, dir, "fluxo", 1, 1, 15, "SIGTERM",
		)
		if err != nil {
			http.Error(w, "Failed to create Octane daemon", http.StatusInternalServerError)
			return
		}

		id, _ := res.LastInsertId()
		if err := daemon.GenerateServiceFile(int(id), cmd, dir, "fluxo", 1, 15, "SIGTERM"); err != nil {
			database.DB.Exec("DELETE FROM daemons WHERE id = ?", id)
			database.DB.Exec("UPDATE sites SET app_port = 0 WHERE id = ?", siteID)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := daemon.EnableAndStart(r.Context(), int(id)); err != nil {
			daemon.Delete(r.Context(), int(id))
			database.DB.Exec("DELETE FROM daemons WHERE id = ?", id)
			database.DB.Exec("UPDATE sites SET app_port = 0 WHERE id = ?", siteID)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		database.DB.Exec("UPDATE daemons SET status = 'active' WHERE id = ?", id)

		if err := addOctaneReloadToDeployScript(siteID); err != nil {
			daemon.Delete(r.Context(), int(id))
			database.DB.Exec("DELETE FROM daemons WHERE id = ?", id)
			database.DB.Exec("UPDATE sites SET app_port = 0 WHERE id = ?", siteID)
			http.Error(w, "Failed to update deployment script", http.StatusInternalServerError)
			return
		}
		if err := regenerateNginxForSiteWithError(siteID); err != nil {
			daemon.Delete(r.Context(), int(id))
			database.DB.Exec("DELETE FROM daemons WHERE id = ?", id)
			database.DB.Exec("UPDATE sites SET app_port = 0 WHERE id = ?", siteID)
			removeOctaneReloadFromDeployScript(siteID)
			regenerateNginxForSite(siteID)
			http.Error(w, "Failed to update Nginx config: "+err.Error(), http.StatusInternalServerError)
			return
		}
		LogActivity(siteID, "feature", "Laravel Octane enabled")

		w.WriteHeader(http.StatusCreated)
	}
}

// POST /api/v1/sites/{id}/features/octane/disable
func (s *Server) handleDisableOctane() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var domain string
		if err := database.DB.QueryRow("SELECT domain FROM sites WHERE id = ?", siteID).Scan(&domain); err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		if err := deleteOctaneDaemons(r.Context(), siteID); err != nil {
			http.Error(w, "Failed to remove Octane daemon", http.StatusInternalServerError)
			return
		}

		if err := removeOctaneReloadFromDeployScript(siteID); err != nil {
			http.Error(w, "Failed to update deployment script", http.StatusInternalServerError)
			return
		}
		database.DB.Exec("UPDATE sites SET app_port = 0 WHERE id = ? AND app_type != 'node'", siteID)
		if err := regenerateNginxForSiteWithError(siteID); err != nil {
			http.Error(w, "Failed to update Nginx config: "+err.Error(), http.StatusInternalServerError)
			return
		}
		LogActivity(siteID, "feature", "Laravel Octane disabled for "+domain)

		w.WriteHeader(http.StatusNoContent)
	}
}
