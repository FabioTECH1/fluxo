package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/cron"
	"fluxo/internal/services/daemon"
	"fluxo/internal/syscmd"
)

// GET /api/v1/sites/{id}/features
func (s *Server) handleGetFeatures() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var domain, phpVersion, appType string
		err := database.DB.QueryRow("SELECT domain, php_version, app_type FROM sites WHERE id = ?", siteID).Scan(&domain, &phpVersion, &appType)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		// Check if scheduler cron exists
		var schedulerCount int
		database.DB.QueryRow("SELECT COUNT(*) FROM crons WHERE site_id = ? AND (name = 'Laravel Scheduler' OR command LIKE '%artisan schedule:run%')", siteID).Scan(&schedulerCount)

		// Check if nightwatch daemon exists
		var nightwatchCount int
		database.DB.QueryRow("SELECT COUNT(*) FROM daemons WHERE site_id = ? AND (name = 'Nightwatch' OR command LIKE '%nightwatch:agent%')", siteID).Scan(&nightwatchCount)

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
		downFile := filepath.Join("/home/fluxo", domain, "storage/framework/down")
		_, downErr := os.Stat(downFile)
		inMaintenance := downErr == nil

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"scheduler_enabled":    schedulerCount > 0,
			"nightwatch_enabled":   nightwatchCount > 0,
			"in_maintenance":       inMaintenance,
			"app_type":             appType,
			"next_nightwatch_port": nextPort,
		})
	}
}

// POST /api/v1/sites/{id}/features/scheduler/enable
func (s *Server) handleEnableScheduler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var domain, phpVersion string
		err := database.DB.QueryRow("SELECT domain, php_version FROM sites WHERE id = ?", siteID).Scan(&domain, &phpVersion)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		if phpVersion == "" {
			phpVersion = "8.4"
		}

		cmd := "php" + phpVersion + " /home/fluxo/" + domain + "/artisan schedule:run"

		res, err := database.DB.Exec(
			"INSERT INTO crons (site_id, name, expression, command, user) VALUES (?, ?, ?, ?, ?)",
			siteID, "Laravel Scheduler", "* * * * *", cmd, "fluxo",
		)
		if err != nil {
			http.Error(w, "Failed to create scheduler", http.StatusInternalServerError)
			return
		}

		id, _ := res.LastInsertId()
		if err := cron.Create(int(id), domain, "* * * * *", cmd, "fluxo"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		LogActivity(siteID, "scheduler_enabled", "Laravel Scheduler enabled for "+domain)
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

		cron.Delete(id)
		database.DB.Exec("DELETE FROM crons WHERE id = ?", id)

		LogActivity(siteID, "scheduler_disabled", "Laravel Scheduler disabled")
		w.WriteHeader(http.StatusNoContent)
	}
}

// POST /api/v1/sites/{id}/features/nightwatch/enable
func (s *Server) handleEnableNightwatch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var req struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
			http.Error(w, "Missing token", http.StatusBadRequest)
			return
		}

		req.Token = strings.ReplaceAll(req.Token, "\n", "")
		req.Token = strings.ReplaceAll(req.Token, "\r", "")

		var domain, phpVersion string
		err := database.DB.QueryRow("SELECT domain, php_version FROM sites WHERE id = ?", siteID).Scan(&domain, &phpVersion)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		if phpVersion == "" {
			phpVersion = "8.4"
		}

		// Find next available port
		port := 2407
		rows, err := database.DB.Query("SELECT command FROM daemons WHERE name = 'Nightwatch' OR command LIKE '%nightwatch:agent%'")
		if err == nil {
			var used []int
			for rows.Next() {
				var cmd string
				if rows.Scan(&cmd) == nil {
					idx := strings.Index(cmd, "--listen-on=127.0.0.1:")
					if idx != -1 {
						portStr := cmd[idx+len("--listen-on=127.0.0.1:"):]
						portStr = strings.Split(portStr, " ")[0]
						if p, err := strconv.Atoi(portStr); err == nil {
							used = append(used, p)
						}
					}
				}
			}
			for {
				taken := false
				for _, u := range used {
					if u == port {
						taken = true
						break
					}
				}
				if !taken {
					break
				}
				port++
			}
		}
		rows.Close()

		uri := "127.0.0.1:" + strconv.Itoa(port)
		cmd := "php" + phpVersion + " artisan nightwatch:agent --listen-on=" + uri
		dir := "/home/fluxo/" + domain

		res, err := database.DB.Exec(
			"INSERT INTO daemons (site_id, name, command, directory, user, instances, start_seconds, stop_seconds, stop_signal) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			siteID, "Nightwatch", cmd, dir, "fluxo", 1, 1, 15, "SIGTERM",
		)
		if err != nil {
			http.Error(w, "Failed to create daemon", http.StatusInternalServerError)
			return
		}

		id, _ := res.LastInsertId()
		daemon.GenerateServiceFile(int(id), cmd, dir, "fluxo", 1, 15, "SIGTERM")
		daemon.EnableAndStart(r.Context(), int(id))
		database.DB.Exec("UPDATE daemons SET status = 'active' WHERE id = ?", id)

		// Merge Nightwatch env vars into .env
		envPath := filepath.Join("/home/fluxo", domain, ".env")
		mergeEnvLine(envPath, "NIGHTWATCH_TOKEN", req.Token)
		mergeEnvLine(envPath, "NIGHTWATCH_INGEST_URI", uri)
		mergeEnvLine(envPath, "LOG_CHANNEL", "stack")
		mergeEnvLine(envPath, "LOG_STACK", "single,nightwatch")

		LogActivity(siteID, "nightwatch_enabled", "Nightwatch enabled for "+domain+" on "+uri)
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

		daemon.Delete(r.Context(), id)
		database.DB.Exec("DELETE FROM daemons WHERE id = ?", id)

		LogActivity(siteID, "nightwatch_disabled", "Nightwatch disabled")
		w.WriteHeader(http.StatusNoContent)
	}
}

// POST /api/v1/sites/{id}/features/maintenance/enable
func (s *Server) handleEnableMaintenance() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var domain, phpVersion string
		err := database.DB.QueryRow("SELECT domain, php_version FROM sites WHERE id = ?", siteID).Scan(&domain, &phpVersion)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		if phpVersion == "" {
			phpVersion = "8.4"
		}

		dir := "/home/fluxo/" + domain
		out, err := syscmd.RunAsUserInDir(r.Context(), 30*time.Second, "fluxo", dir, "php"+phpVersion, "artisan", "down")
		if err != nil {
			http.Error(w, "Failed: "+err.Error()+out, http.StatusInternalServerError)
			return
		}

		LogActivity(siteID, "maintenance_enabled", "Maintenance mode enabled for "+domain)
		w.WriteHeader(http.StatusNoContent)
	}
}

// POST /api/v1/sites/{id}/features/maintenance/disable
func (s *Server) handleDisableMaintenance() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var domain, phpVersion string
		err := database.DB.QueryRow("SELECT domain, php_version FROM sites WHERE id = ?", siteID).Scan(&domain, &phpVersion)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		if phpVersion == "" {
			phpVersion = "8.4"
		}

		dir := "/home/fluxo/" + domain
		out, err := syscmd.RunAsUserInDir(r.Context(), 30*time.Second, "fluxo", dir, "php"+phpVersion, "artisan", "up")
		if err != nil {
			http.Error(w, "Failed: "+err.Error()+out, http.StatusInternalServerError)
			return
		}

		LogActivity(siteID, "maintenance_disabled", "Maintenance mode disabled for "+domain)
		w.WriteHeader(http.StatusNoContent)
	}
}

// mergeEnvLine adds or replaces a KEY=VALUE line in the .env file at envPath.
func mergeEnvLine(envPath, key, value string) {
	data, err := os.ReadFile(envPath)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(line, key+"=") {
			lines[i] = key + "=" + value
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, key+"="+value)
	}
	os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0640)
}

// resolveArtisanCommand prefixes artisan commands with the site's PHP version
// and (for cron) the full path. Returns the command unchanged if it doesn't start
// with "artisan" or the site is not a PHP/Laravel app.
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

// resolveArtisanCronCommand prefixes artisan commands for cron with full PHP path and site directory.
func resolveArtisanCronCommand(cmd string, siteID int) string {
	if !strings.HasPrefix(cmd, "artisan") {
		return cmd
	}

	var appType, phpVersion, domain string
	database.DB.QueryRow("SELECT app_type, php_version, domain FROM sites WHERE id = ?", siteID).Scan(&appType, &phpVersion, &domain)
	if appType != "laravel" && appType != "php" {
		return cmd
	}
	if phpVersion == "" {
		phpVersion = "8.4"
	}

	return "php" + phpVersion + " /home/fluxo/" + domain + "/" + cmd
}
