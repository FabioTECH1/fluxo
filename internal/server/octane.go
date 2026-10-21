package server

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/safeinput"
	"fluxo/internal/services/daemon"
	"fluxo/internal/services/deploy"
	"fluxo/internal/services/octaneruntime"
	"fluxo/internal/services/phpupdate"
	sitepkg "fluxo/internal/services/site"
	"fluxo/internal/syscmd"
)

const (
	octaneDaemonName = "Laravel Octane"
	octaneReloadLine = "$FLUXO_PHP artisan octane:reload"
)

var octaneMutationMu sync.Mutex
var cleanupOctaneRuntime = phpupdate.CleanupManagedOctane

// Cleanup must outlive cancellation/lost replies from the enable request.
// Keep a failed record and reserved port if systemd cleanup cannot be confirmed.
func cleanupFailedOctane(siteID, daemonID, originalPort int) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var branch, directory string
	var port int
	if err := database.DB.QueryRow("SELECT COALESCE(NULLIF(s.php_version,''),'8.4'),d.directory,COALESCE(s.app_port,0) FROM daemons d JOIN sites s ON s.id=d.site_id WHERE d.id=? AND s.id=?", daemonID, siteID).Scan(&branch, &directory, &port); err != nil {
		return err
	}
	var status string
	if err := database.DB.QueryRow("SELECT status FROM daemons WHERE id=?", daemonID).Scan(&status); err != nil {
		return err
	}
	if status == "enable_publish_pending" || status == "enable_fallback_pending" {
		if err := restorePendingOctaneRouting(siteID, daemonID, status); err != nil {
			return err
		}
	}
	if _, err := database.DB.Exec("UPDATE daemons SET status='enable_cleanup_pending' WHERE id=?", daemonID); err != nil {
		return err
	}
	if err := cleanupOctaneRuntime(ctx, branch, daemonID, directory, port); err != nil {
		return err
	}
	if _, err := database.DB.Exec("DELETE FROM daemons WHERE id=?", daemonID); err != nil {
		return err
	}
	_, err := database.DB.Exec("UPDATE sites SET app_port=? WHERE id=?", originalPort, siteID)
	return err
}

func isOctaneEnabled(siteID int) bool {
	var count int
	_ = database.DB.QueryRow("SELECT COUNT(*) FROM daemons WHERE site_id = ? AND status NOT IN ('enable_pending','enable_publish_pending','enable_fallback_pending','enable_cleanup_pending') AND (name = ? OR command LIKE '%artisan octane:start%')", siteID, octaneDaemonName).Scan(&count)
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
		var status string
		if err := database.DB.QueryRow("SELECT status FROM daemons WHERE id=?", id).Scan(&status); err != nil {
			return err
		}
		if status == "enable_pending" || status == "enable_publish_pending" || status == "enable_fallback_pending" || status == "enable_cleanup_pending" {
			var port int
			if err := database.DB.QueryRow("SELECT app_port FROM sites WHERE id=?", siteID).Scan(&port); err != nil {
				return err
			}
			if err := cleanupFailedOctane(siteID, id, port); err != nil {
				return err
			}
			continue
		}
		if err := daemon.Delete(ctx, id); err != nil {
			return err
		}
		if _, err := database.DB.Exec("DELETE FROM daemons WHERE id = ?", id); err != nil {
			return err
		}
	}
	return nil
}

func syncOctaneDaemonForSite(ctx context.Context, siteID int) error {
	var pending int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM daemons WHERE site_id=? AND status IN ('enable_pending','enable_publish_pending','enable_fallback_pending','enable_cleanup_pending') AND (name=? OR managed_kind='laravel_octane')", siteID, octaneDaemonName).Scan(&pending); err != nil {
		return err
	}
	if pending != 0 {
		return fmt.Errorf("Octane enablement cleanup is pending; retry enablement to safely clean up first")
	}
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
		if appType != "node" && appType != "python" {
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
		var existingCommand string
		if err := database.DB.QueryRow("SELECT command FROM daemons WHERE id=?", id).Scan(&existingCommand); err != nil {
			return err
		}
		nextCommand := command
		if strings.Contains(existingCommand, octaneruntime.Launcher) {
			if err := octaneruntime.Install(); err != nil {
				return err
			}
			nextCommand = octaneruntime.Command(phpVersion, appPort)
		}
		if _, err := database.DB.Exec("UPDATE daemons SET managed_kind = 'laravel_octane', command = ?, directory = ?, user = 'fluxo', instances = 1, start_seconds = 1, stop_seconds = 15, stop_signal = 'SIGTERM' WHERE id = ?", nextCommand, directory, id); err != nil {
			return err
		}
		if err := daemon.GenerateServiceFile(id, nextCommand, directory, "fluxo", 1, 15, "SIGTERM"); err != nil {
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
		octaneMutationMu.Lock()
		defer octaneMutationMu.Unlock()
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var sitePath, phpVersion, strategy string
		var appPort int
		err := database.DB.QueryRow("SELECT path, php_version, deployment_strategy, COALESCE(app_port, 0) FROM sites WHERE id = ?", siteID).Scan(&sitePath, &phpVersion, &strategy, &appPort)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		originalPort := appPort
		rows, err := database.DB.Query("SELECT id FROM daemons WHERE site_id=? AND status IN ('enable_pending','enable_publish_pending','enable_fallback_pending','enable_cleanup_pending') AND (name=? OR managed_kind='laravel_octane')", siteID, octaneDaemonName)
		if err != nil {
			http.Error(w, "Unable to inspect pending Octane cleanup", http.StatusInternalServerError)
			return
		}
		var pendingIDs []int
		for rows.Next() {
			var id int
			if err = rows.Scan(&id); err != nil {
				break
			}
			pendingIDs = append(pendingIDs, id)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			http.Error(w, "Unable to inspect pending Octane cleanup", http.StatusInternalServerError)
			return
		}
		for _, pendingID := range pendingIDs {
			if err := cleanupFailedOctane(siteID, pendingID, originalPort); err != nil {
				http.Error(w, "Octane enablement cleanup remains pending; routing was not changed: "+err.Error(), http.StatusConflict)
				return
			}
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
		if !isOctaneEnabled(siteID) {
			override, err := database.GetSiteVhostOverride(siteID)
			if err != nil {
				http.Error(w, "Unable to inspect Nginx configuration", http.StatusInternalServerError)
				return
			}
			if override != nil {
				http.Error(w, "Custom Nginx configuration must be reconciled before enabling Octane", http.StatusUnprocessableEntity)
				return
			}
			if err := octaneruntime.Install(); err != nil {
				http.Error(w, "Unable to install the managed Octane integration: "+err.Error(), http.StatusInternalServerError)
				return
			}
			if err := phpupdate.ValidateManagedOctane(r.Context(), phpVersion, sitepkg.ActiveSitePath(sitePath, strategy)); err != nil {
				http.Error(w, "Octane compatibility preflight failed: "+err.Error(), http.StatusUnprocessableEntity)
				return
			}
		}

		appPortMutationMu.Lock()
		if safeinput.ValidatePortNumber(appPort) {
			var ownerCount int
			if err := database.DB.QueryRow("SELECT COUNT(*) FROM sites WHERE id != ? AND app_port = ?", siteID, appPort).Scan(&ownerCount); err != nil {
				appPortMutationMu.Unlock()
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
				appPortMutationMu.Unlock()
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

		}
		appPortMutationMu.Unlock()

		dir := sitepkg.ActiveSitePath(sitePath, strategy)
		cmd := octaneruntime.Command(phpVersion, appPort)

		if isOctaneEnabled(siteID) {
			if appPort != originalPort {
				if _, err := database.DB.Exec("UPDATE sites SET app_port=? WHERE id=?", appPort, siteID); err != nil {
					http.Error(w, "Failed to reserve Octane port", http.StatusInternalServerError)
					return
				}
			}
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

		if err := octaneruntime.Install(); err != nil {
			http.Error(w, "Unable to install the managed Octane runtime integration: "+err.Error(), http.StatusInternalServerError)
			return
		}
		id, claimed, err := database.CreatePendingOctaneDaemon(siteID, octaneDaemonName, cmd, dir, appPort)
		if err != nil {
			http.Error(w, "Failed to create Octane daemon", http.StatusInternalServerError)
			return
		}
		if !claimed {
			http.Error(w, "Wait for the site's running deployment or pending Octane enablement to finish, or retry port allocation", http.StatusConflict)
			return
		}

		failEnable := func(cause error, status int) {
			if cleanupErr := cleanupFailedOctane(siteID, int(id), originalPort); cleanupErr != nil {
				http.Error(w, cause.Error()+"; failed to clean up the new Octane service: "+cleanupErr.Error(), http.StatusInternalServerError)
				return
			}
			http.Error(w, cause.Error(), status)
		}
		if err := daemon.GenerateServiceFile(int(id), cmd, dir, "fluxo", 1, 15, "SIGTERM"); err != nil {
			failEnable(err, http.StatusInternalServerError)
			return
		}
		if err := daemon.EnableAndStart(r.Context(), int(id)); err != nil {
			failEnable(err, http.StatusInternalServerError)
			return
		}
		if err := phpupdate.VerifyManagedOctaneStartup(r.Context(), phpVersion, int(id), dir, appPort); err != nil {
			failEnable(err, http.StatusUnprocessableEntity)
			return
		}
		if _, err := database.DB.Exec("UPDATE daemons SET status='enable_publish_pending' WHERE id=? AND status='enable_pending'", id); err != nil {
			failEnable(fmt.Errorf("Failed to record Octane routing intent: %w", err), http.StatusInternalServerError)
			return
		}
		if err := addOctaneReloadToDeployScript(siteID); err != nil {
			failEnable(fmt.Errorf("Failed to update deployment script: %w", err), http.StatusInternalServerError)
			return
		}
		if err := regenerateNginxForOctaneEnable(siteID); err != nil {
			failEnable(fmt.Errorf("Failed to update Nginx config: %w", err), http.StatusInternalServerError)
			return
		}
		if _, err := database.DB.Exec("UPDATE daemons SET status='active' WHERE id=? AND status='enable_publish_pending'", id); err != nil {
			failEnable(fmt.Errorf("Failed to finalize Octane enablement: %w", err), http.StatusInternalServerError)
			return
		}
		LogActivity(siteID, "feature", "Laravel Octane enabled")

		w.WriteHeader(http.StatusCreated)
	}
}

// POST /api/v1/sites/{id}/features/octane/disable
func (s *Server) handleDisableOctane() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		octaneMutationMu.Lock()
		defer octaneMutationMu.Unlock()
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
		database.DB.Exec("UPDATE sites SET app_port = 0 WHERE id = ? AND app_type NOT IN ('node', 'python')", siteID)
		if err := regenerateNginxForSiteWithError(siteID); err != nil {
			http.Error(w, "Failed to update Nginx config: "+err.Error(), http.StatusInternalServerError)
			return
		}
		LogActivity(siteID, "feature", "Laravel Octane disabled for "+domain)

		w.WriteHeader(http.StatusNoContent)
	}
}
