package server

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/daemon"
	"fluxo/internal/services/deploy"
	sitepkg "fluxo/internal/services/site"
	"fluxo/internal/syscmd"
)

const (
	horizonDaemonName     = deploy.HorizonDaemonName
	horizonDaemonSelector = deploy.HorizonDaemonSelector
	horizonTerminateLine  = deploy.HorizonTerminateLine
)

func isHorizonEnabled(siteID int) bool {
	return deploy.IsHorizonEnabled(siteID)
}

func addHorizonTerminateToDeployScript(siteID int) error {
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
	current = withHorizonTerminate(current)
	_, err := database.DB.Exec("UPDATE sites SET deploy_script = ? WHERE id = ?", current, siteID)
	return err
}

func removeHorizonTerminateFromDeployScript(siteID int) error {
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

	_, err := database.DB.Exec("UPDATE sites SET deploy_script = ? WHERE id = ?", withoutHorizonTerminate(script.String), siteID)
	return err
}

func withHorizonTerminate(script string) string {
	return deploy.WithHorizonTerminate(script)
}

func withoutHorizonTerminate(script string) string {
	return deploy.WithoutHorizonTerminate(script)
}

func validateHorizonRedis(ctx context.Context, sitePath, phpVersion, strategy string) error {
	directory := sitepkg.ActiveSitePath(sitePath, strategy)
	php := "php" + phpVersion
	if _, err := syscmd.RunAsUserInDir(ctx, 30*time.Second, "fluxo", directory, php, "artisan", "config:clear"); err != nil {
		return err
	}
	const redisProbe = `require 'vendor/autoload.php'; $app = require 'bootstrap/app.php'; $app->make(\Illuminate\Contracts\Console\Kernel::class)->bootstrap(); $connection = $app['config']->get('horizon.use', 'default'); $app->make('redis')->connection($connection)->ping();`
	_, err := syscmd.RunAsUserInDir(ctx, 30*time.Second, "fluxo", directory, php, "-r", redisProbe)
	return err
}

func deleteHorizonDaemons(ctx context.Context, siteID int) error {
	rows, err := database.DB.Query("SELECT id FROM daemons WHERE site_id = ? AND "+horizonDaemonSelector, siteID, horizonDaemonName)
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
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, id := range daemonIDs {
		if err := daemon.Delete(ctx, id); err != nil {
			return err
		}
		if _, err := database.DB.Exec("DELETE FROM daemons WHERE id = ?", id); err != nil {
			return err
		}
	}
	return nil
}

func syncHorizonDaemonForSite(ctx context.Context, siteID int) error {
	if !isHorizonEnabled(siteID) {
		return nil
	}

	var sitePath, phpVersion, appType, strategy string
	if err := database.DB.QueryRow("SELECT path, php_version, app_type, deployment_strategy FROM sites WHERE id = ?", siteID).Scan(&sitePath, &phpVersion, &appType, &strategy); err != nil {
		return err
	}
	if appType != "laravel" && appType != "php" {
		if err := deleteHorizonDaemons(ctx, siteID); err != nil {
			return err
		}
		return removeHorizonTerminateFromDeployScript(siteID)
	}
	if phpVersion == "" {
		phpVersion = "8.4"
	}

	directory := sitepkg.ActiveSitePath(sitePath, strategy)
	command := fmt.Sprintf("php%s artisan horizon", phpVersion)
	rows, err := database.DB.Query("SELECT id FROM daemons WHERE site_id = ? AND "+horizonDaemonSelector, siteID, horizonDaemonName)
	if err != nil {
		return err
	}
	var ids []int
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, id := range ids {
		if _, err := database.DB.Exec("UPDATE daemons SET name = ?, managed_kind = 'laravel_horizon', command = ?, directory = ?, user = 'fluxo', instances = 1, start_seconds = 1, stop_seconds = 30, stop_signal = 'SIGTERM' WHERE id = ?", horizonDaemonName, command, directory, id); err != nil {
			return err
		}
		if err := daemon.GenerateServiceFile(id, command, directory, "fluxo", 1, 30, "SIGTERM"); err != nil {
			return err
		}
	}
	if len(ids) > 0 {
		if _, err := syscmd.Run(ctx, 10*time.Second, "systemctl", "daemon-reload"); err != nil {
			return err
		}
	}
	for _, id := range ids {
		if err := daemon.RestartAndWait(ctx, id); err != nil {
			return err
		}
		database.DB.Exec("UPDATE daemons SET status = 'active' WHERE id = ?", id)
	}
	return addHorizonTerminateToDeployScript(siteID)
}

// POST /api/v1/sites/{id}/features/horizon/enable
func (s *Server) handleEnableHorizon() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))
		if !requireHorizonPackage(w, siteID) {
			return
		}

		var sitePath, phpVersion, strategy string
		if err := database.DB.QueryRow("SELECT path, php_version, deployment_strategy FROM sites WHERE id = ?", siteID).Scan(&sitePath, &phpVersion, &strategy); err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		if phpVersion == "" {
			phpVersion = "8.4"
		}
		if err := horizonQueueConnectionReady(sitePath); err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		if err := validateHorizonRedis(r.Context(), sitePath, phpVersion, strategy); err != nil {
			log.Printf("Horizon Redis preflight failed for site %d: %v", siteID, err)
			http.Error(w, "Horizon could not connect to Redis using the site's current configuration", http.StatusUnprocessableEntity)
			return
		}

		restoreQueueOnFailure := false
		queueConfig := defaultQueueWorkerConfig()
		if deploy.IsQueueWorkerEnabled(siteID) {
			var loadErr error
			queueConfig, _, loadErr = loadQueueWorkerConfig(siteID)
			if loadErr != nil {
				http.Error(w, "Failed to load the queue worker configuration", http.StatusInternalServerError)
				return
			}
			if err := disableQueueWorkerRuntime(r.Context(), siteID); err != nil {
				http.Error(w, "Failed to stop the standard queue worker before enabling Horizon", http.StatusInternalServerError)
				return
			}
			restoreQueueOnFailure = true
			defer func() {
				if !restoreQueueOnFailure {
					return
				}
				if err := restoreQueueWorkerDetached(siteID, queueConfig, true); err != nil {
					log.Printf("Failed to restore Laravel queue worker for site %d after Horizon activation failed: %v", siteID, err)
				}
			}()
		}

		if isHorizonEnabled(siteID) {
			if err := syncHorizonDaemonForSite(r.Context(), siteID); err != nil {
				http.Error(w, "Failed to sync Horizon", http.StatusInternalServerError)
				return
			}
			restoreQueueOnFailure = false
			w.WriteHeader(http.StatusNoContent)
			return
		}

		directory := sitepkg.ActiveSitePath(sitePath, strategy)
		command := fmt.Sprintf("php%s artisan horizon", phpVersion)
		res, err := database.DB.Exec(
			"INSERT INTO daemons (site_id, name, managed_kind, command, directory, user, instances, start_seconds, stop_seconds, stop_signal) VALUES (?, ?, 'laravel_horizon', ?, ?, ?, ?, ?, ?, ?)",
			siteID, horizonDaemonName, command, directory, "fluxo", 1, 1, 30, "SIGTERM",
		)
		if err != nil {
			http.Error(w, "Failed to create Horizon daemon", http.StatusInternalServerError)
			return
		}

		id, err := res.LastInsertId()
		if err != nil {
			database.DB.Exec("DELETE FROM daemons WHERE site_id = ? AND name = ?", siteID, horizonDaemonName)
			http.Error(w, "Failed to identify Horizon daemon", http.StatusInternalServerError)
			return
		}
		cleanupDaemon := func() {
			cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), time.Minute)
			defer cancelCleanup()
			_ = daemon.Delete(cleanupCtx, int(id))
			database.DB.Exec("DELETE FROM daemons WHERE id = ?", id)
		}
		if err := daemon.GenerateServiceFile(int(id), command, directory, "fluxo", 1, 30, "SIGTERM"); err != nil {
			cleanupDaemon()
			http.Error(w, "Failed to configure Horizon daemon: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := daemon.EnableAndStart(r.Context(), int(id)); err != nil {
			cleanupDaemon()
			http.Error(w, "Failed to start Horizon daemon: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := daemon.WaitHealthy(r.Context(), int(id)); err != nil {
			cleanupDaemon()
			http.Error(w, "Horizon did not remain healthy after startup: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := database.DB.Exec("UPDATE daemons SET status = 'active' WHERE id = ?", id); err != nil {
			cleanupDaemon()
			http.Error(w, "Failed to activate Horizon daemon", http.StatusInternalServerError)
			return
		}
		if err := addHorizonTerminateToDeployScript(siteID); err != nil {
			cleanupDaemon()
			http.Error(w, "Failed to update deployment script", http.StatusInternalServerError)
			return
		}

		LogActivity(siteID, "feature", "Laravel Horizon enabled")
		restoreQueueOnFailure = false
		w.WriteHeader(http.StatusCreated)
	}
}

// POST /api/v1/sites/{id}/features/horizon/disable
func (s *Server) handleDisableHorizon() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var domain string
		if err := database.DB.QueryRow("SELECT domain FROM sites WHERE id = ?", siteID).Scan(&domain); err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		if err := deleteHorizonDaemons(r.Context(), siteID); err != nil {
			http.Error(w, "Failed to remove Horizon daemon", http.StatusInternalServerError)
			return
		}
		if err := removeHorizonTerminateFromDeployScript(siteID); err != nil {
			http.Error(w, "Failed to update deployment script", http.StatusInternalServerError)
			return
		}

		LogActivity(siteID, "feature", "Laravel Horizon disabled for "+domain)
		w.WriteHeader(http.StatusNoContent)
	}
}
