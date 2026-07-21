package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/cron"
	sitepkg "fluxo/internal/services/site"
	"fluxo/internal/syscmd"
)

func composerCapabilitiesForSite(siteID int) (sitepkg.ComposerCapabilities, error) {
	var sitePath, appType, deploymentStrategy string
	if err := database.DB.QueryRow("SELECT path, app_type, deployment_strategy FROM sites WHERE id = ?", siteID).Scan(&sitePath, &appType, &deploymentStrategy); err != nil {
		return sitepkg.ComposerCapabilities{}, err
	}
	if appType != "laravel" && appType != "php" {
		return sitepkg.ComposerCapabilities{}, nil
	}
	return sitepkg.DetectComposerCapabilities(sitePath, deploymentStrategy)
}

func requireLaravelSite(w http.ResponseWriter, siteID int) bool {
	capabilities, err := composerCapabilitiesForSite(siteID)
	if err != nil {
		writeComposerInspectionError(w, siteID, err)
		return false
	}
	if !capabilities.Laravel {
		http.Error(w, "Laravel 5.0 or later was not found in the active composer.lock", http.StatusUnprocessableEntity)
		return false
	}
	return true
}

func requireHorizonPackage(w http.ResponseWriter, siteID int) bool {
	capabilities, err := composerCapabilitiesForSite(siteID)
	if err != nil {
		writeComposerInspectionError(w, siteID, err)
		return false
	}
	if !capabilities.Horizon {
		http.Error(w, "laravel/horizon was not found in the active composer.lock", http.StatusUnprocessableEntity)
		return false
	}
	return true
}

func writeComposerInspectionError(w http.ResponseWriter, siteID int, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "Site not found", http.StatusNotFound)
		return
	}
	log.Printf("Failed to inspect composer.lock for site %d: %v", siteID, err)
	http.Error(w, "Unable to inspect the active composer.lock", http.StatusUnprocessableEntity)
}

// GET /api/v1/sites/{id}/features
func (s *Server) handleGetFeatures() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var sitePath, appType, deploymentStrategy string
		err := database.DB.QueryRow("SELECT path, app_type, deployment_strategy FROM sites WHERE id = ?", siteID).Scan(&sitePath, &appType, &deploymentStrategy)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		capabilities := sitepkg.ComposerCapabilities{}
		if appType == "laravel" || appType == "php" {
			capabilities, err = sitepkg.DetectComposerCapabilities(sitePath, deploymentStrategy)
			if err != nil {
				log.Printf("Failed to inspect composer.lock for site %d: %v", siteID, err)
				capabilities = sitepkg.ComposerCapabilities{}
			}
		}

		// Check if scheduler cron exists
		var schedulerCount int
		database.DB.QueryRow("SELECT COUNT(*) FROM crons WHERE site_id = ? AND (name = 'Laravel Scheduler' OR command LIKE '%artisan schedule:run%')", siteID).Scan(&schedulerCount)

		// Check if Horizon daemon exists
		var horizonCount int
		database.DB.QueryRow("SELECT COUNT(*) FROM daemons WHERE site_id = ? AND "+horizonDaemonSelector, siteID, horizonDaemonName).Scan(&horizonCount)

		// Check if Octane daemon exists
		var octaneCount int
		database.DB.QueryRow("SELECT COUNT(*) FROM daemons WHERE site_id = ? AND (name = 'Laravel Octane' OR command LIKE '%artisan octane:start%')", siteID).Scan(&octaneCount)

		nextPort := nightwatchStartingPort
		if availablePort, portErr := nextNightwatchPort(); portErr == nil {
			nextPort = availablePort
		}

		// Check if in maintenance mode
		activePath := sitepkg.ActiveSitePath(sitePath, deploymentStrategy)
		downFile := filepath.Join(activePath, "storage/framework/down")
		inMaintenance := false
		if appType == "laravel" || appType == "php" {
			_, downErr := os.Stat(downFile)
			inMaintenance = downErr == nil
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"composer_lock_found":   capabilities.LockFound,
			"laravel_detected":      capabilities.Laravel,
			"laravel_version":       capabilities.LaravelVersion,
			"scheduler_enabled":     schedulerCount > 0,
			"scheduler_available":   capabilities.Laravel,
			"nightwatch_enabled":    isNightwatchEnabled(siteID),
			"nightwatch_installed":  capabilities.Nightwatch,
			"nightwatch_version":    capabilities.NightwatchVersion,
			"nightwatch_available":  capabilities.Nightwatch,
			"horizon_enabled":       horizonCount > 0,
			"horizon_installed":     capabilities.Horizon,
			"horizon_version":       capabilities.HorizonVersion,
			"horizon_available":     capabilities.Horizon,
			"octane_enabled":        octaneCount > 0,
			"octane_installed":      capabilities.Octane,
			"octane_version":        capabilities.OctaneVersion,
			"octane_available":      capabilities.Octane && deploymentStrategy != "zero-downtime",
			"maintenance_available": capabilities.Laravel,
			"deployment_strategy":   deploymentStrategy,
			"in_maintenance":        inMaintenance,
			"app_type":              appType,
			"next_nightwatch_port":  nextPort,
		})
	}
}

// POST /api/v1/sites/{id}/features/scheduler/enable
func (s *Server) handleEnableScheduler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))
		if !requireLaravelSite(w, siteID) {
			return
		}

		var sitePath, phpVersion, deploymentStrategy string
		err := database.DB.QueryRow("SELECT path, php_version, deployment_strategy FROM sites WHERE id = ?", siteID).Scan(&sitePath, &phpVersion, &deploymentStrategy)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		if phpVersion == "" {
			phpVersion = "8.4"
		}

		activePath := sitepkg.ActiveSitePath(sitePath, deploymentStrategy)
		cmd := "php" + phpVersion + " " + filepath.Join(activePath, "artisan") + " schedule:run"

		tx, err := database.DB.Begin()
		if err != nil {
			http.Error(w, "Failed to create scheduler", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		res, err := tx.Exec(
			"INSERT INTO crons (site_id, name, expression, command, user) VALUES (?, ?, ?, ?, ?)",
			siteID, "Laravel Scheduler", "* * * * *", cmd, "fluxo",
		)
		if err != nil {
			http.Error(w, "Failed to create scheduler", http.StatusInternalServerError)
			return
		}

		id, err := res.LastInsertId()
		if err != nil {
			http.Error(w, "Failed to create scheduler", http.StatusInternalServerError)
			return
		}
		if err := cron.Create(int(id), activePath, "* * * * *", cmd, "fluxo"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := tx.Commit(); err != nil {
			if cleanupErr := cron.Delete(int(id)); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
				log.Printf("Failed to remove scheduler file %d after database commit failed: %v", id, cleanupErr)
			}
			http.Error(w, "Failed to create scheduler", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
	}
}

// POST /api/v1/sites/{id}/features/scheduler/disable
func (s *Server) handleDisableScheduler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))
		var id int
		err := database.DB.QueryRow("SELECT id FROM crons WHERE site_id = ? AND (name = 'Laravel Scheduler' OR command LIKE '%artisan schedule:run%') LIMIT 1", siteID).Scan(&id)
		if err != nil {
			http.Error(w, "Scheduler not found", http.StatusNotFound)
			return
		}

		if err := cron.Delete(id); err != nil && !os.IsNotExist(err) {
			http.Error(w, "Failed to remove scheduler: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := database.DB.Exec("DELETE FROM crons WHERE id = ?", id); err != nil {
			http.Error(w, "Failed to remove scheduler record", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// POST /api/v1/sites/{id}/features/maintenance/enable
func (s *Server) handleEnableMaintenance() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))
		if !requireLaravelSite(w, siteID) {
			return
		}

		var sitePath, phpVersion, deploymentStrategy string
		err := database.DB.QueryRow("SELECT path, php_version, deployment_strategy FROM sites WHERE id = ?", siteID).Scan(&sitePath, &phpVersion, &deploymentStrategy)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		if phpVersion == "" {
			phpVersion = "8.4"
		}

		dir := sitepkg.ActiveSitePath(sitePath, deploymentStrategy)
		out, err := syscmd.RunAsUserInDir(r.Context(), 30*time.Second, "fluxo", dir, "php"+phpVersion, "artisan", "down")
		if err != nil {
			http.Error(w, "Failed: "+err.Error()+out, http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// POST /api/v1/sites/{id}/features/maintenance/disable
func (s *Server) handleDisableMaintenance() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var sitePath, phpVersion, deploymentStrategy string
		err := database.DB.QueryRow("SELECT path, php_version, deployment_strategy FROM sites WHERE id = ?", siteID).Scan(&sitePath, &phpVersion, &deploymentStrategy)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		if phpVersion == "" {
			phpVersion = "8.4"
		}

		dir := sitepkg.ActiveSitePath(sitePath, deploymentStrategy)
		artisanPath := filepath.Join(dir, "artisan")
		if _, err := os.Stat(artisanPath); err == nil {
			out, runErr := syscmd.RunAsUserInDir(r.Context(), 30*time.Second, "fluxo", dir, "php"+phpVersion, "artisan", "up")
			if runErr == nil {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			log.Printf("Laravel maintenance command failed for site %d; removing the down file directly: %v%s", siteID, runErr, out)
		}

		downFile := filepath.Join(dir, "storage/framework/down")
		if err := os.Remove(downFile); err != nil && !os.IsNotExist(err) {
			http.Error(w, "Failed to disable maintenance mode: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// resolveArtisanCommand prefixes artisan commands with the site's PHP version.
func resolveArtisanCommand(cmd string, siteID int) string {
	if !strings.HasPrefix(cmd, "artisan") {
		return cmd
	}

	var appType, phpVersion string
	database.DB.QueryRow("SELECT app_type, php_version FROM sites WHERE id = ?", siteID).Scan(&appType, &phpVersion)
	if appType != "laravel" && appType != "php" {
		return cmd
	}
	if phpVersion == "" {
		phpVersion = "8.4"
	}

	return "php" + phpVersion + " " + cmd
}

// resolveArtisanCronCommand prefixes artisan commands for cron with full path and PHP version.
func resolveArtisanCronCommand(cmd string, siteID int) string {
	if !strings.HasPrefix(cmd, "artisan") {
		return cmd
	}

	var appType, phpVersion, sitePath, deploymentStrategy string
	database.DB.QueryRow("SELECT app_type, php_version, path, deployment_strategy FROM sites WHERE id = ?", siteID).Scan(&appType, &phpVersion, &sitePath, &deploymentStrategy)
	if appType != "laravel" && appType != "php" {
		return cmd
	}
	if phpVersion == "" {
		phpVersion = "8.4"
	}

	return "php" + phpVersion + " " + filepath.Join(sitepkg.ActiveSitePath(sitePath, deploymentStrategy), cmd)
}
