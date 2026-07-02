package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"fluxo/internal/services/php"
	"fluxo/internal/syscmd"
)

var phpVersionRegex = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)

// phpIniPath returns the php.ini path for a given PHP version.
func phpIniPath(version string) string {
	return fmt.Sprintf("/etc/php/%s/fpm/php.ini", version)
}

// readPhpIni reads a single key from the php.ini file for the given version.
func readPhpIni(version, key string) string {
	path := phpIniPath(version)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, key) && strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

// writePhpIni sets a key in the php.ini file, creating it if absent.
func writePhpIni(version, key, value string) error {
	path := phpIniPath(version)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	found := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		cleanLine := trimmed
		if strings.HasPrefix(trimmed, ";") {
			cleanLine = strings.TrimSpace(strings.TrimPrefix(trimmed, ";"))
		}

		if strings.HasPrefix(cleanLine, key) && strings.Contains(cleanLine, "=") {
			parts := strings.SplitN(cleanLine, "=", 2)
			if strings.TrimSpace(parts[0]) == key {
				lines[i] = fmt.Sprintf("%s = %s", key, value)
				found = true
				break
			}
		}
	}

	if !found {
		lines = append(lines, fmt.Sprintf("%s = %s", key, value))
	}

	newContent := strings.Join(lines, "\n")
	return os.WriteFile(path, []byte(newContent), 0644)
}

// handleGetPHPSettings returns current php.ini settings for a PHP version.
func (s *Server) handleGetPHPSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		version := r.URL.Query().Get("version")
		if version == "" {
			version = "8.4"
		}
		if !phpVersionRegex.MatchString(version) {
			http.Error(w, "Invalid PHP version format", http.StatusBadRequest)
			return
		}

		settings := map[string]string{
			"upload_max_filesize": readPhpIni(version, "upload_max_filesize"),
			"max_execution_time":  readPhpIni(version, "max_execution_time"),
			"opcache_enable":      readPhpIni(version, "opcache.enable"),
			"memory_limit":        readPhpIni(version, "memory_limit"),
			"post_max_size":       readPhpIni(version, "post_max_size"),
			"max_input_time":      readPhpIni(version, "max_input_time"),
		}

		if settings["upload_max_filesize"] == "" {
			settings["upload_max_filesize"] = "8M"
		}
		if settings["max_execution_time"] == "" {
			settings["max_execution_time"] = "30"
		}
		if settings["opcache_enable"] == "" {
			settings["opcache_enable"] = "1"
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(settings)
	}
}

// handleUpdatePHPSettings updates php.ini settings and reloads PHP-FPM.
func (s *Server) handleUpdatePHPSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Version       string `json:"version"`
			UploadMaxSize string `json:"upload_max_filesize"`
			MaxExecTime   string `json:"max_execution_time"`
			OpcacheEnable string `json:"opcache_enable"`
			MemoryLimit   string `json:"memory_limit"`
			PostMaxSize   string `json:"post_max_size"`
			MaxInputTime  string `json:"max_input_time"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		if req.Version == "" {
			req.Version = "8.4"
		}
		if !phpVersionRegex.MatchString(req.Version) {
			http.Error(w, "Invalid PHP version format", http.StatusBadRequest)
			return
		}

		ctx := context.Background()

		updates := map[string]string{
			"upload_max_filesize": req.UploadMaxSize,
			"max_execution_time":  req.MaxExecTime,
			"opcache.enable":      req.OpcacheEnable,
			"memory_limit":        req.MemoryLimit,
			"post_max_size":       req.PostMaxSize,
			"max_input_time":      req.MaxInputTime,
		}

		for key, val := range updates {
			if val == "" {
				continue
			}
			if err := writePhpIni(req.Version, key, val); err != nil {
				log.Printf("Failed to write php.ini setting %s=%s: %v", key, val, err)
			}
		}

		syscmd.Run(ctx, 10*time.Second, "systemctl", "reload", fmt.Sprintf("php%s-fpm", req.Version))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}
}

// handleInstallPHPVersion installs a PHP version with common extensions via apt-get.
func (s *Server) handleInstallPHPVersion() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Version string `json:"version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Version == "" {
			http.Error(w, "Version is required", http.StatusBadRequest)
			return
		}
		if !phpVersionRegex.MatchString(req.Version) {
			http.Error(w, "Invalid PHP version format", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()

		_, err := syscmd.Run(ctx, 5*time.Minute, "apt-get", "install", "-y",
			fmt.Sprintf("php%s-fpm", req.Version),
			fmt.Sprintf("php%s-cli", req.Version),
			fmt.Sprintf("php%s-mysql", req.Version),
			fmt.Sprintf("php%s-pgsql", req.Version),
			fmt.Sprintf("php%s-sqlite3", req.Version),
			fmt.Sprintf("php%s-curl", req.Version),
			fmt.Sprintf("php%s-mbstring", req.Version),
			fmt.Sprintf("php%s-xml", req.Version),
			fmt.Sprintf("php%s-gd", req.Version),
			fmt.Sprintf("php%s-zip", req.Version),
			fmt.Sprintf("php%s-bcmath", req.Version),
			fmt.Sprintf("php%s-intl", req.Version),
			fmt.Sprintf("php%s-redis", req.Version),
		)
		if err != nil {
			http.Error(w, "Installation failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}
}

// handleRemovePHPVersion removes a PHP version via apt-get.
func (s *Server) handleRemovePHPVersion() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Version string `json:"version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Version == "" {
			http.Error(w, "Version is required", http.StatusBadRequest)
			return
		}
		if !phpVersionRegex.MatchString(req.Version) {
			http.Error(w, "Invalid PHP version format", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()

		_, err := syscmd.Run(ctx, 2*time.Minute, "apt-get", "remove", "-y",
			fmt.Sprintf("php%s-fpm", req.Version),
			fmt.Sprintf("php%s-cli", req.Version),
		)
		if err != nil {
			http.Error(w, "Remove failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}
}

// handleSetDefaultPHP sets the system-wide default PHP version via update-alternatives.
func (s *Server) handleSetDefaultPHP() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Version string `json:"version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Version == "" {
			http.Error(w, "Version is required", http.StatusBadRequest)
			return
		}
		if !phpVersionRegex.MatchString(req.Version) {
			http.Error(w, "Invalid PHP version format", http.StatusBadRequest)
			return
		}

		ctx := context.Background()
		_, err := syscmd.Run(ctx, 10*time.Second, "update-alternatives", "--set", "php", fmt.Sprintf("/usr/bin/php%s", req.Version))
		if err != nil {
			http.Error(w, "Failed to set default: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}
}

// handleGetPHPVersions lists installed PHP versions with binaries.
func (s *Server) handleGetPHPVersions() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		versions := []string{}

		entries, err := os.ReadDir("/etc/php")
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					// Verify the PHP binary actually exists before listing
					if _, err := exec.LookPath("php" + entry.Name()); err == nil {
						versions = append(versions, entry.Name())
					}
				}
			}
		}

		if len(versions) == 0 {
			versions = append(versions, "8.4")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(versions)
	}
}

// handleGetPHPAvailableVersions returns all supported PHP versions with install status.
func (s *Server) handleGetPHPAvailableVersions() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		available := []string{"8.4", "8.3", "8.2", "8.1", "8.0", "7.4"}

		installed := map[string]bool{}
		entries, err := os.ReadDir("/etc/php")
		if err == nil {
			for _, e := range entries {
				if e.IsDir() {
					installed[e.Name()] = true
				}
			}
		}

		result := make([]map[string]interface{}, 0)
		for _, v := range available {
			status := "not_installed"
			if installed[v] {
				_, err := syscmd.Run(r.Context(), 2*time.Second, "systemctl", "is-active", "--quiet", fmt.Sprintf("php%s-fpm", v))
				if err == nil {
					status = "running"
				} else {
					status = "stopped"
				}
			}
			result = append(result, map[string]interface{}{
				"version":   v,
				"installed": installed[v],
				"status":    status,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

// handleRestartPHP reloads the PHP-FPM service for a specific version.
func (s *Server) handleRestartPHP() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		version := r.PathValue("version")
		if version == "" {
			version = r.URL.Query().Get("version")
		}
		if version == "" {
			http.Error(w, "Version is required", http.StatusBadRequest)
			return
		}
		if !phpVersionRegex.MatchString(version) {
			http.Error(w, "Invalid PHP version format", http.StatusBadRequest)
			return
		}
		if err := php.ReloadFPM(r.Context(), version); err != nil {
			http.Error(w, "Failed to restart PHP-FPM: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleStartPHP starts the PHP-FPM service for a specific version.
func (s *Server) handleStartPHP() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		version := r.PathValue("version")
		if version == "" {
			version = r.URL.Query().Get("version")
		}
		if version == "" {
			http.Error(w, "Version is required", http.StatusBadRequest)
			return
		}
		if !phpVersionRegex.MatchString(version) {
			http.Error(w, "Invalid PHP version format", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		_, err := syscmd.Run(ctx, 10*time.Second, "systemctl", "start", fmt.Sprintf("php%s-fpm", version))
		if err != nil {
			http.Error(w, "Failed to start PHP-FPM: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleStopPHP stops the PHP-FPM service for a specific version.
func (s *Server) handleStopPHP() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		version := r.PathValue("version")
		if version == "" {
			version = r.URL.Query().Get("version")
		}
		if version == "" {
			http.Error(w, "Version is required", http.StatusBadRequest)
			return
		}
		if !phpVersionRegex.MatchString(version) {
			http.Error(w, "Invalid PHP version format", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		_, err := syscmd.Run(ctx, 10*time.Second, "systemctl", "stop", fmt.Sprintf("php%s-fpm", version))
		if err != nil {
			http.Error(w, "Failed to stop PHP-FPM: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleGetCLIDefaultPHP returns the system default PHP CLI version.
func (s *Server) handleGetCLIDefaultPHP() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		version := ""
		path, err := filepath.EvalSymlinks("/usr/bin/php")
		if err == nil {
			base := filepath.Base(path)
			if strings.HasPrefix(base, "php") {
				version = strings.TrimPrefix(base, "php")
			}
		}
		if version == "" {
			version = "8.4" // fallback
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"version": version})
	}
}
