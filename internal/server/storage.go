// Storage handlers: global database and user management (MySQL + PostgreSQL).
// Unlike the site-scoped handlers in databases.go, these operate at the
// server level across all engines. Every endpoint accepts or returns an
// "engine" field so the UI can display per-engine databases and users.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/mysql"
	"fluxo/internal/services/postgres"
	"fluxo/internal/syscmd"
)

var safeIdentRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func isValidDBIdent(s string) bool {
	return s != "" && safeIdentRegex.MatchString(s)
}

func (s *Server) handleGetDatabaseSizes() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		result := make([]map[string]interface{}, 0)

		if _, err := exec.LookPath("mysql"); err == nil {
			out, err := syscmd.Run(ctx, 10*time.Second, "mysql", "-e", "SELECT table_schema AS name, ROUND(SUM(data_length + index_length) / 1024 / 1024, 2) AS size_mb FROM information_schema.tables GROUP BY table_schema ORDER BY size_mb DESC")
			if err == nil {
				lines := strings.Split(strings.TrimSpace(out), "\n")
				for i, line := range lines {
					if i == 0 {
						continue
					}
					fields := strings.Fields(line)
					if len(fields) >= 2 {
						result = append(result, map[string]interface{}{
							"name":    fields[0],
							"size_mb": fields[1],
						})
					}
				}
			}
		}

		if _, err := exec.LookPath("psql"); err == nil {
			out, err := syscmd.Run(ctx, 10*time.Second, "sudo", "-u", "postgres", "psql", "-t", "-A", "-c", "SELECT datname, pg_size_pretty(pg_database_size(datname)) FROM pg_database WHERE datistemplate = false ORDER BY pg_database_size(datname) DESC")
			if err == nil {
				lines := strings.Split(strings.TrimSpace(out), "\n")
				for _, line := range lines {
					parts := strings.Split(line, "|")
					if len(parts) >= 2 {
						result = append(result, map[string]interface{}{
							"name":    strings.TrimSpace(parts[0]),
							"size_mb": strings.TrimSpace(parts[1]),
						})
					}
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func (s *Server) handleGetDatabaseUsers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		result := make([]map[string]interface{}, 0)

		if _, err := exec.LookPath("mysql"); err == nil {
			out, err := syscmd.Run(ctx, 10*time.Second, "mysql", "-e", "SELECT User, Host FROM mysql.user WHERE User NOT IN ('root', 'mysql.sys', 'mysql.session', 'mysql.infoschema', 'mariadb.sys', 'mysql', 'debian-sys-maint') ORDER BY User")
			if err == nil {
				lines := strings.Split(strings.TrimSpace(out), "\n")
				for i, line := range lines {
					if i == 0 {
						continue
					}
					fields := strings.Fields(line)
					if len(fields) >= 2 {
						result = append(result, map[string]interface{}{
							"user":   fields[0],
							"host":   fields[1],
							"engine": "mysql",
						})
					}
				}
			}
		}

		if _, err := exec.LookPath("psql"); err == nil {
			out, err := syscmd.Run(ctx, 10*time.Second, "sudo", "-u", "postgres", "psql", "-t", "-A", "-c", "SELECT rolname FROM pg_roles WHERE rolname NOT LIKE 'pg_%' AND rolname != 'postgres' ORDER BY rolname")
			if err == nil {
				lines := strings.Split(strings.TrimSpace(out), "\n")
				for _, line := range lines {
					user := strings.TrimSpace(line)
					if user != "" {
						result = append(result, map[string]interface{}{
							"user":   user,
							"host":   "localhost",
							"engine": "postgres",
						})
					}
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func (s *Server) handleGetUserGrants() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := r.URL.Query().Get("user")
		engine := r.URL.Query().Get("engine")
		if user == "" || !isValidDBIdent(user) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]string{})
			return
		}

		if engine == "postgres" {
			ctx := r.Context()
			out, err := syscmd.Run(ctx, 10*time.Second, "sudo", "-u", "postgres", "psql", "-t", "-A", "-c", fmt.Sprintf("SELECT datname FROM pg_database WHERE datistemplate = false AND has_database_privilege('%s', datname, 'CONNECT')", user))
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode([]string{})
				return
			}
			dbs := make([]string, 0)
			for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
				if db := strings.TrimSpace(line); db != "" {
					dbs = append(dbs, db)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(dbs)
			return
		}

		ctx := r.Context()
		out, err := syscmd.Run(ctx, 10*time.Second, "mysql", "-e", fmt.Sprintf("SHOW GRANTS FOR '%s'@'%%'", user))
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]string{})
			return
		}
		dbs := make([]string, 0)
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if strings.Contains(line, "GRANT ALL PRIVILEGES ON `") {
				parts := strings.Split(line, "`")
				if len(parts) >= 2 {
					dbName := parts[1]
					if dbName != "*" {
						dbs = append(dbs, dbName)
					}
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(dbs)
	}
}

func (s *Server) handleCreateDatabaseUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			User      string   `json:"user"`
			Password  string   `json:"password"`
			Databases []string `json:"databases"`
			Engine    string   `json:"engine"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.User == "" {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		if !isValidDBIdent(req.User) {
			http.Error(w, "Invalid username format. Only alphanumeric characters, underscores, and hyphens are allowed.", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		pass := req.Password
		if pass == "" {
			pass = fmt.Sprintf("%x", time.Now().UnixNano())[:16]
		}
		engine := req.Engine
		if engine == "" {
			engine = "mysql"
		}

		if engine == "postgres" {
			_, err := syscmd.RunStdin(ctx, 10*time.Second, fmt.Sprintf("CREATE ROLE \"%s\" WITH LOGIN PASSWORD '%s'", req.User, pass), "sudo", "-u", "postgres", "psql")
			if err != nil {
				http.Error(w, "Failed to create user: "+err.Error(), http.StatusInternalServerError)
				return
			}
			for _, db := range req.Databases {
				syscmd.RunStdin(ctx, 5*time.Second, fmt.Sprintf("GRANT ALL PRIVILEGES ON DATABASE \"%s\" TO \"%s\"", db, req.User), "sudo", "-u", "postgres", "psql")
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"user":      req.User,
				"password":  pass,
				"databases": req.Databases,
				"engine":    "postgres",
			})
			return
		}

		_, err := syscmd.RunStdin(ctx, 10*time.Second, fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'%%' IDENTIFIED BY '%s'", req.User, pass), "mysql")
		if err != nil {
			http.Error(w, "Failed to create user", http.StatusInternalServerError)
			return
		}
		for _, db := range req.Databases {
			syscmd.RunStdin(ctx, 5*time.Second, fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%%'", db, req.User), "mysql")
		}
		syscmd.RunStdin(ctx, 5*time.Second, "FLUSH PRIVILEGES", "mysql")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"user":      req.User,
			"password":  pass,
			"databases": req.Databases,
			"engine":    "mysql",
		})
	}
}

func (s *Server) handleUpdateUserGrants() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			User      string   `json:"user"`
			Databases []string `json:"databases"`
			Engine    string   `json:"engine"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.User == "" {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		if !isValidDBIdent(req.User) {
			http.Error(w, "Invalid username format", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		engine := req.Engine
		if engine == "" {
			engine = "mysql"
		}

		if engine == "postgres" {
			syscmd.Run(ctx, 10*time.Second, "sudo", "-u", "postgres", "psql", "-c", fmt.Sprintf("REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM \"%s\"", req.User))
			syscmd.Run(ctx, 10*time.Second, "sudo", "-u", "postgres", "psql", "-c", fmt.Sprintf("REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public FROM \"%s\"", req.User))
			for _, db := range req.Databases {
				syscmd.Run(ctx, 5*time.Second, "sudo", "-u", "postgres", "psql", "-c", fmt.Sprintf("GRANT ALL PRIVILEGES ON DATABASE \"%s\" TO \"%s\"", db, req.User))
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		syscmd.Run(ctx, 10*time.Second, "mysql", "-e", fmt.Sprintf("REVOKE ALL PRIVILEGES, GRANT OPTION FROM '%s'@'%%'", req.User))
		for _, db := range req.Databases {
			syscmd.Run(ctx, 5*time.Second, "mysql", "-e", fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%%'", db, req.User))
		}
		syscmd.Run(ctx, 5*time.Second, "mysql", "-e", "FLUSH PRIVILEGES")
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleCreateGlobalDatabase() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name   string `json:"name"`
			Engine string `json:"engine"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		engine := req.Engine
		if engine == "" {
			engine = "mysql"
		}

		if engine != "mysql" && engine != "postgres" {
			http.Error(w, "Invalid engine. Must be mysql or postgres.", http.StatusBadRequest)
			return
		}

		if engine == "postgres" {
			if _, err := exec.LookPath("psql"); err != nil {
				http.Error(w, "PostgreSQL is not installed.", http.StatusBadRequest)
				return
			}
			if err := postgres.CreateDatabase(req.Name, req.Name+"_user", "secret"); err != nil {
				http.Error(w, "Failed to create PostgreSQL database: "+err.Error(), http.StatusInternalServerError)
				return
			}
			database.DB.Exec("INSERT INTO databases (site_id, engine, name, username) VALUES (?, ?, ?, ?)", 0, "postgres", req.Name, req.Name+"_user")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"name": req.Name, "engine": "postgres"})
			return
		}

		if err := mysql.CreateDatabase(req.Name, req.Name+"_user", "secret"); err != nil {
			http.Error(w, "Failed to create MySQL database: "+err.Error(), http.StatusInternalServerError)
			return
		}
		database.DB.Exec("INSERT INTO databases (site_id, engine, name, username) VALUES (?, ?, ?, ?)", 0, "mysql", req.Name, req.Name+"_user")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"name": req.Name, "engine": "mysql"})
	}
}

func (s *Server) handleDeleteDatabaseUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := r.URL.Query().Get("user")
		engine := r.URL.Query().Get("engine")
		if user == "" || !isValidDBIdent(user) {
			http.Error(w, "Missing or invalid user", http.StatusBadRequest)
			return
		}

		ctx := r.Context()

		if engine == "postgres" {
			syscmd.Run(ctx, 10*time.Second, "sudo", "-u", "postgres", "psql", "-c", fmt.Sprintf("REASSIGN OWNED BY \"%s\" TO postgres", user))
			syscmd.Run(ctx, 10*time.Second, "sudo", "-u", "postgres", "psql", "-c", fmt.Sprintf("DROP OWNED BY \"%s\"", user))
			_, err := syscmd.Run(ctx, 10*time.Second, "sudo", "-u", "postgres", "psql", "-c", fmt.Sprintf("DROP ROLE IF EXISTS \"%s\"", user))
			if err != nil {
				http.Error(w, "Failed to drop user: "+err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		_, err := syscmd.Run(ctx, 10*time.Second, "mysql", "-e", fmt.Sprintf("DROP USER IF EXISTS '%s'@'%%'", user))
		if err != nil {
			http.Error(w, "Failed to drop user", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
