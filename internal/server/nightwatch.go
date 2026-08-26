package server

import (
	"context"
	"encoding/json"
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
	"fluxo/internal/services/daemon"
	sitepkg "fluxo/internal/services/site"
	"fluxo/internal/syscmd"
)

const (
	nightwatchDaemonName   = "Nightwatch"
	nightwatchDaemonFilter = "(name = 'Nightwatch' OR command LIKE '%nightwatch:agent%')"
	nightwatchStartingPort = 2407
	nightwatchListenPrefix = "--listen-on=127.0.0.1:"
)

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

func isNightwatchEnabled(siteID int) bool {
	var count int
	_ = database.DB.QueryRow("SELECT COUNT(*) FROM daemons WHERE site_id = ? AND "+nightwatchDaemonFilter, siteID).Scan(&count)
	return count > 0
}

func nextNightwatchPort() (int, error) {
	rows, err := database.DB.Query("SELECT command FROM daemons WHERE " + nightwatchDaemonFilter)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	usedPorts := map[int]bool{}
	for rows.Next() {
		var command string
		if err := rows.Scan(&command); err != nil {
			return 0, err
		}
		if port, ok := nightwatchPortFromCommand(command); ok {
			usedPorts[port] = true
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	port := nightwatchStartingPort
	for usedPorts[port] {
		port++
	}
	return port, nil
}

func nightwatchPortFromCommand(command string) (int, bool) {
	idx := strings.Index(command, nightwatchListenPrefix)
	if idx == -1 {
		return 0, false
	}
	portText := command[idx+len(nightwatchListenPrefix):]
	portText = strings.Split(portText, " ")[0]
	port, err := strconv.Atoi(portText)
	return port, err == nil
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

		port, err := nextNightwatchPort()
		if err != nil {
			http.Error(w, "Failed to reserve Nightwatch port", http.StatusInternalServerError)
			return
		}

		uri := "127.0.0.1:" + strconv.Itoa(port)
		cmd := "php" + phpVersion + " artisan nightwatch:agent --listen-on=" + uri
		dir := sitepkg.ActiveSitePath(sitePath, deploymentStrategy)
		envPath := filepath.Join(sitePath, ".env")
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
			"INSERT INTO daemons (site_id, name, managed_kind, command, directory, user, instances, start_seconds, stop_seconds, stop_signal) VALUES (?, ?, 'laravel_nightwatch', ?, ?, ?, ?, ?, ?, ?)",
			siteID, nightwatchDaemonName, cmd, dir, "fluxo", 1, 1, 15, "SIGTERM",
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
			_ = daemon.Delete(r.Context(), int(id))
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
		err := database.DB.QueryRow("SELECT id FROM daemons WHERE site_id = ? AND "+nightwatchDaemonFilter+" LIMIT 1", siteID).Scan(&id)
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
