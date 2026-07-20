package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/safeinput"
	"fluxo/internal/services/cron"
	"fluxo/internal/services/daemon"
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

func requireNightwatchPackage(w http.ResponseWriter, siteID int) bool {
	capabilities, err := composerCapabilitiesForSite(siteID)
	if err != nil {
		writeComposerInspectionError(w, siteID, err)
		return false
	}
	if !capabilities.Nightwatch {
		http.Error(w, "laravel/nightwatch was not found in the active composer.lock", http.StatusUnprocessableEntity)
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

		// Check if nightwatch daemon exists
		var nightwatchCount int
		database.DB.QueryRow("SELECT COUNT(*) FROM daemons WHERE site_id = ? AND (name = 'Nightwatch' OR command LIKE '%nightwatch:agent%')", siteID).Scan(&nightwatchCount)

		// Check if Octane daemon exists
		var octaneCount int
		database.DB.QueryRow("SELECT COUNT(*) FROM daemons WHERE site_id = ? AND (name = 'Laravel Octane' OR command LIKE '%artisan octane:start%')", siteID).Scan(&octaneCount)

		// Find next available nightwatch port
		var usedPorts []int
		rows, err := database.DB.Query("SELECT command FROM daemons WHERE name = 'Nightwatch' OR command LIKE '%nightwatch:agent%'")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var cmd string
				if rows.Scan(&cmd) == nil {
					idx := strings.Index(cmd, "--listen-on=127.0.0.1:")
					if idx != -1 {
						portStr := cmd[idx+len("--listen-on=127.0.0.1:"):]
						portStr = strings.Split(portStr, " ")[0]
						if p, err := strconv.Atoi(portStr); err == nil {
							usedPorts = append(usedPorts, p)
						}
					}
				}
			}
		}
		nextPort := 2407
		for {
			found := false
			for _, p := range usedPorts {
				if p == nextPort {
					found = true
					break
				}
			}
			if !found {
				break
			}
			nextPort++
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
			"nightwatch_enabled":    nightwatchCount > 0,
			"nightwatch_installed":  capabilities.Nightwatch,
			"nightwatch_version":    capabilities.NightwatchVersion,
			"nightwatch_available":  capabilities.Nightwatch,
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

// POST /api/v1/sites/{id}/features/nightwatch/enable
func (s *Server) handleEnableNightwatch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))
		if !requireNightwatchPackage(w, siteID) {
			return
		}

		var req struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
			http.Error(w, "Missing token", http.StatusBadRequest)
			return
		}

		if safeinput.HasControlChars(req.Token) {
			http.Error(w, "Invalid token", http.StatusBadRequest)
			return
		}

		var domain, sitePath, phpVersion, deploymentStrategy string
		err := database.DB.QueryRow("SELECT domain, path, php_version, deployment_strategy FROM sites WHERE id = ?", siteID).Scan(&domain, &sitePath, &phpVersion, &deploymentStrategy)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		if phpVersion == "" {
			phpVersion = "8.4"
		}

		// Find next available port.
		port := 2407
		rows, err := database.DB.Query("SELECT command FROM daemons WHERE name = 'Nightwatch' OR command LIKE '%nightwatch:agent%'")
		if err != nil {
			http.Error(w, "Failed to reserve Nightwatch port", http.StatusInternalServerError)
			return
		}
		var used []int
		for rows.Next() {
			var command string
			if err := rows.Scan(&command); err != nil {
				rows.Close()
				http.Error(w, "Failed to reserve Nightwatch port", http.StatusInternalServerError)
				return
			}
			idx := strings.Index(command, "--listen-on=127.0.0.1:")
			if idx != -1 {
				portStr := command[idx+len("--listen-on=127.0.0.1:"):]
				portStr = strings.Split(portStr, " ")[0]
				if parsedPort, err := strconv.Atoi(portStr); err == nil {
					used = append(used, parsedPort)
				}
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			http.Error(w, "Failed to reserve Nightwatch port", http.StatusInternalServerError)
			return
		}
		rows.Close()
		for {
			taken := false
			for _, usedPort := range used {
				if usedPort == port {
					taken = true
					break
				}
			}
			if !taken {
				break
			}
			port++
		}

		uri := "127.0.0.1:" + strconv.Itoa(port)
		cmd := "php" + phpVersion + " artisan nightwatch:agent --listen-on=" + uri
		dir := sitepkg.ActiveSitePath(sitePath, deploymentStrategy)
		envPath := filepath.Join("/home/fluxo", domain, ".env")
		envSnapshot, err := mergeEnvSettings(r.Context(), envPath, []envSetting{
			{key: "NIGHTWATCH_TOKEN", value: req.Token},
			{key: "NIGHTWATCH_INGEST_URI", value: uri},
			{key: "LOG_CHANNEL", value: "stack"},
			{key: "LOG_STACK", value: "single,nightwatch"},
		})
		if err != nil {
			http.Error(w, "Failed to update Nightwatch environment", http.StatusInternalServerError)
			return
		}
		restoreEnv := func() {
			if err := restoreEnvFile(r.Context(), envPath, envSnapshot); err != nil {
				log.Printf("Failed to restore Nightwatch environment for site %d: %v", siteID, err)
			}
		}

		res, err := database.DB.Exec(
			"INSERT INTO daemons (site_id, name, command, directory, user, instances, start_seconds, stop_seconds, stop_signal) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			siteID, "Nightwatch", cmd, dir, "fluxo", 1, 1, 15, "SIGTERM",
		)
		if err != nil {
			restoreEnv()
			http.Error(w, "Failed to create daemon", http.StatusInternalServerError)
			return
		}

		id, err := res.LastInsertId()
		if err != nil {
			restoreEnv()
			http.Error(w, "Failed to create daemon", http.StatusInternalServerError)
			return
		}
		cleanupDaemon := func() {
			daemon.Delete(r.Context(), int(id))
			if _, cleanupErr := database.DB.Exec("DELETE FROM daemons WHERE id = ?", id); cleanupErr != nil {
				log.Printf("Failed to roll back Nightwatch daemon %d: %v", id, cleanupErr)
			}
			restoreEnv()
		}
		if err := daemon.GenerateServiceFile(int(id), cmd, dir, "fluxo", 1, 15, "SIGTERM"); err != nil {
			cleanupDaemon()
			http.Error(w, "Failed to configure Nightwatch daemon: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := daemon.EnableAndStart(r.Context(), int(id)); err != nil {
			cleanupDaemon()
			http.Error(w, "Failed to start Nightwatch daemon: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := database.DB.Exec("UPDATE daemons SET status = 'active' WHERE id = ?", id); err != nil {
			cleanupDaemon()
			http.Error(w, "Failed to activate Nightwatch daemon", http.StatusInternalServerError)
			return
		}

		LogActivity(siteID, "feature", "Laravel Nightwatch enabled")
		w.WriteHeader(http.StatusCreated)
	}
}

// POST /api/v1/sites/{id}/features/nightwatch/disable
func (s *Server) handleDisableNightwatch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))
		var id int
		err := database.DB.QueryRow("SELECT id FROM daemons WHERE site_id = ? AND (name = 'Nightwatch' OR command LIKE '%nightwatch:agent%') LIMIT 1", siteID).Scan(&id)
		if err != nil {
			http.Error(w, "Nightwatch not found", http.StatusNotFound)
			return
		}

		if err := daemon.Delete(r.Context(), id); err != nil {
			http.Error(w, "Failed to remove Nightwatch daemon", http.StatusInternalServerError)
			return
		}
		if _, err := database.DB.Exec("DELETE FROM daemons WHERE id = ?", id); err != nil {
			http.Error(w, "Failed to remove Nightwatch daemon record", http.StatusInternalServerError)
			return
		}

		LogActivity(siteID, "feature", "Laravel Nightwatch disabled")
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

type envSetting struct {
	key   string
	value string
}

type envFileSnapshot struct {
	content []byte
	exists  bool
}

func mergeEnvSettings(ctx context.Context, envPath string, settings []envSetting) (envFileSnapshot, error) {
	data, err := os.ReadFile(envPath)
	snapshot := envFileSnapshot{content: data, exists: err == nil}
	if err != nil && !os.IsNotExist(err) {
		return envFileSnapshot{}, err
	}

	trimmed := strings.TrimSuffix(string(data), "\n")
	var lines []string
	if trimmed != "" {
		lines = strings.Split(trimmed, "\n")
	}
	for _, setting := range settings {
		found := false
		for i, line := range lines {
			if strings.HasPrefix(line, setting.key+"=") {
				lines[i] = setting.key + "=" + setting.value
				found = true
				break
			}
		}
		if !found {
			lines = append(lines, setting.key+"="+setting.value)
		}
	}

	content := []byte(strings.Join(lines, "\n") + "\n")
	if err := writeEnvFileAtomic(ctx, envPath, content); err != nil {
		return envFileSnapshot{}, err
	}
	return snapshot, nil
}

func restoreEnvFile(ctx context.Context, envPath string, snapshot envFileSnapshot) error {
	if !snapshot.exists {
		if err := os.Remove(envPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return writeEnvFileAtomic(ctx, envPath, snapshot.content)
}

func writeEnvFileAtomic(ctx context.Context, envPath string, content []byte) error {
	tmpFile, err := os.CreateTemp(filepath.Dir(envPath), ".env.tmp.*")
	if err != nil {
		return fmt.Errorf("create temporary env file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write temporary env file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return fmt.Errorf("sync temporary env file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temporary env file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0640); err != nil {
		return fmt.Errorf("set env permissions: %w", err)
	}
	if _, err := syscmd.Run(ctx, 5*time.Second, "chown", "fluxo:www-data", tmpPath); err != nil {
		return fmt.Errorf("set env ownership: %w", err)
	}
	if err := os.Rename(tmpPath, envPath); err != nil {
		return fmt.Errorf("activate env file: %w", err)
	}
	return nil
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
