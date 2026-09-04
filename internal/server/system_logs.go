package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/site"
	"fluxo/internal/syscmd"
)

// isAllowedLogPath checks whether a log file path is in the whitelist.
func isAllowedLogPath(path string) bool {
	allowedPrefixes := []string{
		"/var/log/nginx/",
		"/var/log/php",
		"/var/log/mysql/",
		"/var/log/mariadb/",
		"/var/log/postgresql/",
		"/var/log/redis/",
		"/var/log/fluxo/",
	}
	cleaned := filepath.Clean(path)
	if strings.Contains(cleaned, "..") {
		return false
	}
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		resolved = cleaned
	}
	if strings.HasPrefix(resolved, "/home/fluxo/") && strings.HasSuffix(resolved, ".log") {
		return true
	}
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(resolved, prefix) {
			return true
		}
	}
	return false
}

// handleGetLogs tails a log file and returns the last N lines.
func (s *Server) handleGetLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if path == "" {
			path = "/var/log/nginx/error.log"
		}
		if !isAllowedLogPath(path) {
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}
		linesStr := r.URL.Query().Get("lines")
		lines := 50
		if n, err := strconv.Atoi(linesStr); err == nil && n > 0 && n <= 500 {
			lines = n
		}

		out, err := syscmd.Run(r.Context(), 5*time.Second, "tail", "-n", strconv.Itoa(lines), path)
		if err != nil {
			http.Error(w, "Failed to read log: "+err.Error(), http.StatusInternalServerError)
			return
		}

		content := strings.TrimSpace(out)
		logLines := strings.Split(content, "\n")
		if len(logLines) == 1 && logLines[0] == "" {
			logLines = []string{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"path":  path,
			"lines": logLines,
			"total": len(logLines),
		})
	}
}

// handleGetLogList discovers available log files on the system.
func (s *Server) handleGetLogList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		candidates := []site.LogSource{
			{ID: "nginx-error", Label: "Nginx Error Log", Path: "/var/log/nginx/error.log"},
			{ID: "nginx-access", Label: "Nginx Access Log", Path: "/var/log/nginx/access.log"},
		}

		// Detect PHP-FPM versions
		for _, v := range []string{"8.5", "8.4", "8.3", "8.2", "8.1", "8.0", "7.4"} {
			p := fmt.Sprintf("/var/log/php%v-fpm.log", v)
			if _, err := os.Stat(p); err == nil {
				candidates = append(candidates, site.LogSource{ID: "php" + v, Label: "PHP " + v + " FPM Log", Path: p})
			}
		}

		// MySQL / MariaDB
		if _, err := os.Stat("/var/log/mysql/error.log"); err == nil {
			candidates = append(candidates, site.LogSource{ID: "mysql", Label: "MySQL Error Log", Path: "/var/log/mysql/error.log"})
		}
		if _, err := os.Stat("/var/log/mariadb/mariadb.log"); err == nil {
			candidates = append(candidates, site.LogSource{ID: "mariadb", Label: "MariaDB Log", Path: "/var/log/mariadb/mariadb.log"})
		}

		// PostgreSQL
		pgDir := "/var/log/postgresql"
		if entries, err := os.ReadDir(pgDir); err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
					candidates = append(candidates, site.LogSource{
						ID: "postgres", Label: "PostgreSQL Log (" + e.Name() + ")",
						Path: filepath.Join(pgDir, e.Name()),
					})
				}
			}
		}

		// Redis
		if _, err := os.Stat("/var/log/redis/redis-server.log"); err == nil {
			candidates = append(candidates, site.LogSource{ID: "redis", Label: "Redis Log", Path: "/var/log/redis/redis-server.log"})
		}

		// Verify each log is actually readable
		result := make([]site.LogSource, 0, len(candidates))
		for _, c := range candidates {
			f, err := os.Open(c.Path)
			if err == nil {
				f.Close()
				c.Exists = true
				result = append(result, c)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

// handleClearLog truncates a log file to zero bytes.
func (s *Server) handleClearLog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if path == "" {
			http.Error(w, "Missing path", http.StatusBadRequest)
			return
		}

		if !isAllowedLogPath(path) {
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}

		if err := os.Truncate(path, 0); err != nil {
			http.Error(w, "Failed to clear log: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}
}

// handleDownloadLog returns a log file as an attachment download.
func (s *Server) handleDownloadLog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if path == "" {
			http.Error(w, "Missing path", http.StatusBadRequest)
			return
		}

		if !isAllowedLogPath(path) {
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}

		data, err := os.ReadFile(path)
		if err != nil {
			http.Error(w, "Failed to read log: "+err.Error(), http.StatusInternalServerError)
			return
		}

		filename := filepath.Base(path)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		w.Write(data)
	}
}

// handleSiteLogSources returns available log files for a specific site.
func (s *Server) handleSiteLogSources() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteIDStr := r.PathValue("id")
		siteID, err := strconv.Atoi(siteIDStr)
		if err != nil {
			http.Error(w, "Invalid ID", http.StatusBadRequest)
			return
		}

		var domain, sitePath, appType, phpVer string
		err = database.DB.QueryRow("SELECT domain, path, app_type, php_version FROM sites WHERE id = ?", siteID).Scan(&domain, &sitePath, &appType, &phpVer)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		prov := site.Resolve(appType)
		candidates := prov.LogSources(domain, sitePath, phpVer)
		if appType == "node" || appType == "python" {
			rows, err := database.DB.Query("SELECT id, COALESCE(name, '') FROM daemons WHERE site_id = ? ORDER BY id ASC", siteID)
			if err == nil {
				for rows.Next() {
					var daemonID int
					var name string
					if rows.Scan(&daemonID, &name) == nil {
						label := name
						if label == "" {
							label = "Daemon"
						}
						candidates = append(candidates, site.LogSource{
							ID:    fmt.Sprintf("daemon-%d", daemonID),
							Label: label + " Log",
							Path:  fmt.Sprintf("/var/log/fluxo/fluxo-daemon-%d.log", daemonID),
						})
					}
				}
				rows.Close()
			}
		}

		candidates = append(candidates, site.LogSource{ID: "nginx-error", Label: "Nginx Error Log", Path: "/var/log/nginx/error.log"})
		candidates = append(candidates, site.LogSource{ID: "nginx-access", Label: "Nginx Access Log", Path: "/var/log/nginx/access.log"})

		for _, v := range []string{"8.5", "8.4", "8.3", "8.2", "8.1", "8.0", "7.4"} {
			p := fmt.Sprintf("/var/log/php%v-fpm.log", v)
			if _, err := os.Stat(p); err == nil {
				candidates = append(candidates, site.LogSource{ID: "php" + v, Label: "PHP " + v + " FPM Log", Path: p})
			}
		}

		if _, err := os.Stat("/var/log/mysql/error.log"); err == nil {
			candidates = append(candidates, site.LogSource{ID: "mysql", Label: "MySQL Error Log", Path: "/var/log/mysql/error.log"})
		}

		result := make([]site.LogSource, 0, len(candidates))
		for _, c := range candidates {
			f, err := os.Open(c.Path)
			if err == nil {
				f.Close()
				c.Exists = true
				result = append(result, c)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}
