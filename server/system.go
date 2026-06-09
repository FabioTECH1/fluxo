package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fluxo/database"
	"fluxo/services/system"
	"fluxo/syscmd"
)

func (s *Server) handleGetMetrics() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m := system.GetMetrics(r.Context())
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(m)
	}
}

func (s *Server) handleGetEngines() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		engines := []string{"mysql"} // Always assume mariadb-server is installed via baseline

		if _, err := exec.LookPath("psql"); err == nil {
			engines = append(engines, "postgres")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(engines)
	}
}

func (s *Server) handleInstallPostgres() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := exec.LookPath("psql"); err == nil {
			w.WriteHeader(http.StatusOK)
			return
		}

		w.WriteHeader(http.StatusAccepted)

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()

			syscmd.Run(ctx, 10*time.Minute, "apt-get", "update")
			syscmd.Run(ctx, 10*time.Minute, "apt-get", "install", "-y", "postgresql")
		}()
	}
}

func (s *Server) handleGetLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if path == "" {
			path = "/var/log/nginx/error.log"
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

func (s *Server) handleGetActivity() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type ActivityItem struct {
			Type      string `json:"type"`
			Summary   string `json:"summary"`
			CreatedAt string `json:"created_at"`
		}

		items := make([]ActivityItem, 0)

		rows, err := database.DB.Query(`
			SELECT 'deployment', 'Deployment #' || id || ' for site ' || (SELECT domain FROM sites WHERE id = site_id), created_at
			FROM deployments ORDER BY created_at DESC LIMIT 20
		`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var item ActivityItem
				if err := rows.Scan(&item.Type, &item.Summary, &item.CreatedAt); err == nil {
					items = append(items, item)
				}
			}
		}

		if len(items) == 0 {
			items = []ActivityItem{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(items)
	}
}

type LogSource struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

func (s *Server) handleGetLogList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		candidates := []LogSource{
			{ID: "nginx-error", Label: "Nginx Error Log", Path: "/var/log/nginx/error.log"},
			{ID: "nginx-access", Label: "Nginx Access Log", Path: "/var/log/nginx/access.log"},
		}

		// Detect PHP-FPM versions
		for _, v := range []string{"8.4", "8.3", "8.2", "8.1", "8.0", "7.4"} {
			p := fmt.Sprintf("/var/log/php%v-fpm.log", v)
			if _, err := os.Stat(p); err == nil {
				candidates = append(candidates, LogSource{ID: "php" + v, Label: "PHP " + v + " FPM Log", Path: p})
			}
		}

		// MySQL / MariaDB
		if _, err := os.Stat("/var/log/mysql/error.log"); err == nil {
			candidates = append(candidates, LogSource{ID: "mysql", Label: "MySQL Error Log", Path: "/var/log/mysql/error.log"})
		}
		if _, err := os.Stat("/var/log/mariadb/mariadb.log"); err == nil {
			candidates = append(candidates, LogSource{ID: "mariadb", Label: "MariaDB Log", Path: "/var/log/mariadb/mariadb.log"})
		}

		// PostgreSQL
		pgDir := "/var/log/postgresql"
		if entries, err := os.ReadDir(pgDir); err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
					candidates = append(candidates, LogSource{
						ID: "postgres", Label: "PostgreSQL Log (" + e.Name() + ")",
						Path: filepath.Join(pgDir, e.Name()),
					})
				}
			}
		}

		// Redis
		if _, err := os.Stat("/var/log/redis/redis-server.log"); err == nil {
			candidates = append(candidates, LogSource{ID: "redis", Label: "Redis Log", Path: "/var/log/redis/redis-server.log"})
		}

		// Verify each log is actually readable
		result := make([]LogSource, 0, len(candidates))
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

func (s *Server) handleClearLog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if path == "" {
			http.Error(w, "Missing path", http.StatusBadRequest)
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

func (s *Server) handleDownloadLog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if path == "" {
			http.Error(w, "Missing path", http.StatusBadRequest)
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

func (s *Server) handleSiteLogSources() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteIDStr := r.PathValue("id")
		siteID, err := strconv.Atoi(siteIDStr)
		if err != nil {
			http.Error(w, "Invalid ID", http.StatusBadRequest)
			return
		}

		var domain string
		err = database.DB.QueryRow("SELECT domain FROM sites WHERE id = ?", siteID).Scan(&domain)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		candidates := []LogSource{}

		if domain != "" {
			accessLog := fmt.Sprintf("/var/log/nginx/%s.access.log", domain)
			if _, err := os.Stat(accessLog); err == nil {
				candidates = append(candidates, LogSource{ID: "site-nginx-access", Label: "Nginx Access (" + domain + ")", Path: accessLog})
			}
			errLog := fmt.Sprintf("/var/log/nginx/%s.error.log", domain)
			if _, err := os.Stat(errLog); err == nil {
				candidates = append(candidates, LogSource{ID: "site-nginx-error", Label: "Nginx Error (" + domain + ")", Path: errLog})
			}
		}

		candidates = append(candidates, LogSource{ID: "nginx-error", Label: "Nginx Error Log", Path: "/var/log/nginx/error.log"})
		candidates = append(candidates, LogSource{ID: "nginx-access", Label: "Nginx Access Log", Path: "/var/log/nginx/access.log"})

		for _, v := range []string{"8.4", "8.3", "8.2", "8.1", "8.0", "7.4"} {
			p := fmt.Sprintf("/var/log/php%v-fpm.log", v)
			if _, err := os.Stat(p); err == nil {
				candidates = append(candidates, LogSource{ID: "php" + v, Label: "PHP " + v + " FPM Log", Path: p})
			}
		}

		if _, err := os.Stat("/var/log/mysql/error.log"); err == nil {
			candidates = append(candidates, LogSource{ID: "mysql", Label: "MySQL Error Log", Path: "/var/log/mysql/error.log"})
		}

		result := make([]LogSource, 0, len(candidates))
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
