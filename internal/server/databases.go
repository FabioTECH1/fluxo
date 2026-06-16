// Database management handlers for MySQL/PostgreSQL databases.
package server

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"fluxo/internal/database"
	"fluxo/internal/services/mysql"
	"fluxo/internal/services/postgres"
)

type CreateDatabaseRequest struct {
	Name     string `json:"name"`
	Engine   string `json:"engine"`
	Username string `json:"username"`
}

type CreateDatabaseResponse struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Password string `json:"password"`
	Engine   string `json:"engine"`
}

var dbNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// generatePassword creates a hex-encoded random password of the given byte length.
func generatePassword(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return fmt.Sprintf("%x", b)[:length]
}

// handleListDatabases returns all databases for a site.
func (s *Server) handleListDatabases() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		rows, err := database.DB.Query("SELECT id, site_id, engine, name, username, created_at FROM databases WHERE site_id = ?", siteID)
		if err != nil {
			http.Error(w, "DB error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var dbs []database.Database
		for rows.Next() {
			var d database.Database
			rows.Scan(&d.ID, &d.SiteID, &d.Engine, &d.Name, &d.Username, &d.CreatedAt)
			dbs = append(dbs, d)
		}
		if dbs == nil {
			dbs = []database.Database{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(dbs)
	}
}

// handleCreateDatabase creates a database and optional user in MySQL or PostgreSQL.
func (s *Server) handleCreateDatabase() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var req CreateDatabaseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		if !dbNameRegex.MatchString(req.Name) {
			http.Error(w, "Invalid database name. Only alphanumeric characters and underscores are allowed.", http.StatusBadRequest)
			return
		}

		if req.Engine != "mysql" && req.Engine != "postgres" {
			http.Error(w, "Invalid engine. Must be mysql or postgres.", http.StatusBadRequest)
			return
		}

		if req.Engine == "postgres" {
			if _, err := exec.LookPath("psql"); err != nil {
				http.Error(w, "PostgreSQL is not installed on this server.", http.StatusBadRequest)
				return
			}
		}

		username := strings.TrimSpace(req.Username)
		password := ""
		createUser := username != ""

		if createUser {
			if !dbNameRegex.MatchString(username) {
				http.Error(w, "Invalid username format", http.StatusBadRequest)
				return
			}
			password = generatePassword(16)
		}

		if req.Engine == "mysql" {
			if createUser {
				if err := mysql.CreateDatabase(req.Name, username, password); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			} else {
				// Create database only — no dedicated user. Use fluxo admin account.
				if err := mysql.CreateDatabaseOnly(req.Name); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				username = "fluxo"
			}
		} else if req.Engine == "postgres" {
			if createUser {
				if err := postgres.CreateDatabase(req.Name, username, password); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			} else {
				if err := postgres.CreateDatabaseOnly(req.Name); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				username = "fluxo"
			}
		}

		res, err := database.DB.Exec("INSERT INTO databases (site_id, engine, name, username) VALUES (?, ?, ?, ?)", siteID, req.Engine, req.Name, username)
		if err != nil {
			http.Error(w, "Failed to insert into sqlite", http.StatusInternalServerError)
			return
		}
		id, _ := res.LastInsertId()

		resp := CreateDatabaseResponse{
			ID:       int(id),
			Name:     req.Name,
			Username: username,
			Password: password,
			Engine:   req.Engine,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(resp)
	}
}

// handleDeleteDatabase removes a database from the engine and the database record.
func (s *Server) handleDeleteDatabase() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dbID, _ := strconv.Atoi(r.PathValue("db_id"))

		var engine, name, username string
		err := database.DB.QueryRow("SELECT engine, name, username FROM databases WHERE id = ?", dbID).Scan(&engine, &name, &username)
		if err != nil {
			http.Error(w, "Database not found", http.StatusNotFound)
			return
		}

		if engine == "mysql" {
			if err := mysql.DeleteDatabase(name, username); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else if engine == "postgres" {
			if err := postgres.DeleteDatabase(name, username); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		database.DB.Exec("DELETE FROM databases WHERE id = ?", dbID)

		w.WriteHeader(http.StatusNoContent)
	}
}

// handleListAllDatabases returns all databases across all sites.
func (s *Server) handleListAllDatabases() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := database.DB.Query("SELECT id, site_id, engine, name, username, created_at FROM databases")
		if err != nil {
			http.Error(w, "DB error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var dbs []database.Database
		for rows.Next() {
			var d database.Database
			rows.Scan(&d.ID, &d.SiteID, &d.Engine, &d.Name, &d.Username, &d.CreatedAt)
			dbs = append(dbs, d)
		}
		if dbs == nil {
			dbs = []database.Database{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(dbs)
	}
}
