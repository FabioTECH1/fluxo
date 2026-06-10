// Runtime management handlers: PHP settings, PHP version install/remove,
// Nginx/Node/MySQL info and restart, database engine installation.
// These endpoints control the server environment at the OS level.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"fluxo/services/nginx"
	"fluxo/services/php"
	"fluxo/syscmd"
)

func phpIniPath(version string) string {
	return fmt.Sprintf("/etc/php/%s/fpm/php.ini", version)
}

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

func (s *Server) handleGetPHPSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		version := r.URL.Query().Get("version")
		if version == "" {
			version = "8.4"
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

func (s *Server) handleInstallPHPVersion() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Version string `json:"version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Version == "" {
			http.Error(w, "Version is required", http.StatusBadRequest)
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

func (s *Server) handleRemovePHPVersion() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Version string `json:"version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Version == "" {
			http.Error(w, "Version is required", http.StatusBadRequest)
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

func (s *Server) handleSetDefaultPHP() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Version string `json:"version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Version == "" {
			http.Error(w, "Version is required", http.StatusBadRequest)
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

func (s *Server) handleRestartNginx() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := nginx.Reload(r.Context()); err != nil {
			http.Error(w, "Failed to restart Nginx: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

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
		if err := php.ReloadFPM(r.Context(), version); err != nil {
			http.Error(w, "Failed to restart PHP-FPM: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

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

		ctx := r.Context()
		_, err := syscmd.Run(ctx, 10*time.Second, "systemctl", "start", fmt.Sprintf("php%s-fpm", version))
		if err != nil {
			http.Error(w, "Failed to start PHP-FPM: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

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

		ctx := r.Context()
		_, err := syscmd.Run(ctx, 10*time.Second, "systemctl", "stop", fmt.Sprintf("php%s-fpm", version))
		if err != nil {
			http.Error(w, "Failed to stop PHP-FPM: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleGetNginxInfo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info := map[string]interface{}{}

		if out, err := exec.LookPath("nginx"); err == nil {
			info["binary"] = out
		} else {
			info["binary"] = ""
		}

		// nginx -v outputs to stderr, capture both streams
		if out, err := exec.CommandContext(r.Context(), "nginx", "-v").CombinedOutput(); err == nil {
			info["version"] = strings.TrimSpace(string(out))
		}

		info["config_dir"] = "/etc/nginx"
		info["sites_available"] = filepath.Join("/etc/nginx", "sites-available")
		info["sites_enabled"] = filepath.Join("/etc/nginx", "sites-enabled")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	}
}

func (s *Server) handleRestartNode() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Kill all node processes; they'll be restarted by supervisor/daemon
		syscmd.Run(r.Context(), 10*time.Second, "pkill", "-x", "node")
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleInstallNode() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := exec.LookPath("node"); err == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Node.js already installed"})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()

		if _, err := syscmd.Run(ctx, 5*time.Minute, "apt-get", "update"); err != nil {
			http.Error(w, "apt-get update failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := syscmd.Run(ctx, 5*time.Minute, "apt-get", "install", "-y", "nodejs", "npm"); err != nil {
			http.Error(w, "Node.js installation failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Node.js installed successfully"})
	}
}

func (s *Server) handleRemoveNode() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()

		if _, err := syscmd.Run(ctx, 3*time.Minute, "apt-get", "purge", "-y", "nodejs", "npm"); err != nil {
			http.Error(w, "Node.js removal failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Node.js removed successfully"})
	}
}

func (s *Server) handleGetNodeInfo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info := map[string]interface{}{}

		if out, err := exec.LookPath("node"); err == nil {
			info["binary"] = out
		} else {
			info["binary"] = ""
		}

		if out, err := syscmd.Run(r.Context(), 5*time.Second, "node", "--version"); err == nil {
			info["version"] = strings.TrimSpace(out)
		} else {
			info["version"] = ""
		}

		if out, err := syscmd.Run(r.Context(), 5*time.Second, "npm", "--version"); err == nil {
			info["npm"] = strings.TrimSpace(out)
		} else {
			info["npm"] = ""
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	}
}

func (s *Server) handleGetMySQLInfo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info := map[string]interface{}{}

		if out, err := exec.LookPath("mysql"); err == nil {
			info["binary"] = out
		} else {
			info["binary"] = ""
		}

		if out, err := syscmd.Run(r.Context(), 5*time.Second, "mysql", "--version"); err == nil {
			raw := strings.TrimSpace(out)
			if idx := strings.Index(raw, "Distrib "); idx != -1 {
				rest := raw[idx+len("Distrib "):]
				if comma := strings.Index(rest, ","); comma != -1 {
					rest = rest[:comma]
				}
				info["version"] = rest
			} else {
				info["version"] = raw
			}
		} else {
			info["version"] = ""
		}

		info["socket"] = "/var/run/mysqld/mysqld.sock"

		if _, err := os.Stat("/var/run/mysqld/mysqld.sock"); err == nil {
			info["status"] = "running"
		} else {
			info["status"] = "stopped"
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	}
}

func (s *Server) handleGetRedisInfo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info := map[string]interface{}{}

		if out, err := exec.LookPath("redis-server"); err == nil {
			info["binary"] = out
		} else {
			info["binary"] = ""
		}

		if out, err := syscmd.Run(r.Context(), 5*time.Second, "redis-server", "--version"); err == nil {
			raw := strings.TrimSpace(out)
			if idx := strings.Index(raw, "v="); idx != -1 {
				rest := raw[idx+len("v="):]
				if space := strings.Index(rest, " "); space != -1 {
					rest = rest[:space]
				}
				info["version"] = rest
			} else {
				info["version"] = raw
			}
		} else {
			info["version"] = ""
		}

		running := false
		if conn, err := net.DialTimeout("tcp", "127.0.0.1:6379", 1*time.Second); err == nil {
			conn.Close()
			running = true
		} else if _, err := os.Stat("/run/redis/redis-server.sock"); err == nil {
			running = true
		}

		if running {
			info["status"] = "running"
		} else {
			info["status"] = "stopped"
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	}
}

func (s *Server) handleRestartMySQL() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := syscmd.Run(r.Context(), 10*time.Second, "systemctl", "restart", "mariadb"); err != nil {
			http.Error(w, "Failed to restart MySQL: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleStartMySQL() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := syscmd.Run(r.Context(), 10*time.Second, "systemctl", "start", "mariadb"); err != nil {
			http.Error(w, "Failed to start MySQL: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleStopMySQL() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := syscmd.Run(r.Context(), 10*time.Second, "systemctl", "stop", "mariadb"); err != nil {
			http.Error(w, "Failed to stop MySQL: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleRestartRedis() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := syscmd.Run(r.Context(), 10*time.Second, "systemctl", "restart", "redis-server"); err != nil {
			http.Error(w, "Failed to restart Redis: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleStartRedis() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := syscmd.Run(r.Context(), 10*time.Second, "systemctl", "start", "redis-server"); err != nil {
			http.Error(w, "Failed to start Redis: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleStopRedis() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := syscmd.Run(r.Context(), 10*time.Second, "systemctl", "stop", "redis-server"); err != nil {
			http.Error(w, "Failed to stop Redis: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleRestartPostgres() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := syscmd.Run(r.Context(), 10*time.Second, "systemctl", "restart", "postgresql"); err != nil {
			http.Error(w, "Failed to restart PostgreSQL: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleStartPostgres() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := syscmd.Run(r.Context(), 10*time.Second, "systemctl", "start", "postgresql"); err != nil {
			http.Error(w, "Failed to start PostgreSQL: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleStopPostgres() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := syscmd.Run(r.Context(), 10*time.Second, "systemctl", "stop", "postgresql"); err != nil {
			http.Error(w, "Failed to stop PostgreSQL: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleGetPostgresInfo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info := map[string]interface{}{}

		if out, err := exec.LookPath("psql"); err == nil {
			info["binary"] = out
		} else {
			info["binary"] = ""
		}

		if out, err := syscmd.Run(r.Context(), 5*time.Second, "psql", "--version"); err == nil {
			raw := strings.TrimSpace(out)
			if idx := strings.Index(raw, "psql (PostgreSQL) "); idx != -1 {
				info["version"] = raw[idx+len("psql (PostgreSQL) "):]
			} else {
				info["version"] = raw
			}
		} else {
			info["version"] = ""
		}

		running := false
		if conn, err := net.DialTimeout("tcp", "127.0.0.1:5432", 1*time.Second); err == nil {
			conn.Close()
			running = true
		} else if _, err := os.Stat("/var/run/postgresql/.s.PGSQL.5432"); err == nil {
			running = true
		}

		if running {
			info["status"] = "running"
		} else {
			info["status"] = "stopped"
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	}
}

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
