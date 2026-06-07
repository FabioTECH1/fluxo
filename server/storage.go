package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"fluxo/database"
	"fluxo/syscmd"
)

func (s *Server) handleGetDatabaseSizes() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		out, err := syscmd.Run(ctx, 10*time.Second, "mysql", "-e", "SELECT table_schema AS name, ROUND(SUM(data_length + index_length) / 1024 / 1024, 2) AS size_mb FROM information_schema.tables GROUP BY table_schema ORDER BY size_mb DESC")
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]interface{}{})
			return
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		result := make([]map[string]interface{}, 0)
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
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func (s *Server) handleGetDatabaseUsers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		out, err := syscmd.Run(ctx, 10*time.Second, "mysql", "-e", "SELECT User, Host FROM mysql.user WHERE User NOT IN ('root', 'mysql.sys', 'mysql.session', 'mysql.infoschema') ORDER BY User")
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]interface{}{})
			return
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		result := make([]map[string]interface{}, 0)
		for i, line := range lines {
			if i == 0 {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				result = append(result, map[string]interface{}{
					"user": fields[0],
					"host": fields[1],
				})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func (s *Server) handleGetUserGrants() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := r.URL.Query().Get("user")
		if user == "" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]string{})
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
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.User == "" {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		pass := req.Password
		if pass == "" {
			pass = fmt.Sprintf("%x", time.Now().UnixNano())[:16]
		}
		_, err := syscmd.Run(ctx, 10*time.Second, "mysql", "-e", fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'%%' IDENTIFIED BY '%s'", req.User, pass))
		if err != nil {
			http.Error(w, "Failed to create user", http.StatusInternalServerError)
			return
		}
		for _, db := range req.Databases {
			syscmd.Run(ctx, 5*time.Second, "mysql", "-e", fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%%'", db, req.User))
		}
		syscmd.Run(ctx, 5*time.Second, "mysql", "-e", "FLUSH PRIVILEGES")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"user":      req.User,
			"password":  pass,
			"databases": req.Databases,
		})
	}
}

func (s *Server) handleUpdateUserGrants() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			User      string   `json:"user"`
			Databases []string `json:"databases"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.User == "" {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		// Revoke all, then grant specified
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
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		_, err := syscmd.Run(ctx, 10*time.Second, "mysql", "-e", fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", req.Name))
		if err != nil {
			http.Error(w, "Failed to create database", http.StatusInternalServerError)
			return
		}
		database.DB.Exec("INSERT INTO databases (site_id, engine, name, username) VALUES (?, ?, ?, ?)", 0, "mysql", req.Name, "")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"name": req.Name, "engine": "mysql"})
	}
}

func (s *Server) handleDeleteDatabaseUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := r.URL.Query().Get("user")
		if user == "" {
			http.Error(w, "Missing user", http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		_, err := syscmd.Run(ctx, 10*time.Second, "mysql", "-e", fmt.Sprintf("DROP USER IF EXISTS '%s'@'%%'", user))
		if err != nil {
			http.Error(w, "Failed to drop user", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
