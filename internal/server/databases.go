// Database management handlers: create, list, and delete MySQL/PostgreSQL
// databases. Each site can have multiple databases; credentials (username +
// password) are generated automatically and returned on creation.
// Database engine selection (mysql/postgres) is validated; PostgreSQL
// requires the psql binary to be installed.
package server

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"

	"fluxo/database"
	"fluxo/services/mysql"
	"fluxo/services/postgres"
)

type CreateDatabaseRequest struct {
	Name   string `json:"name"`
	Engine string `json:"engine"`
}

type CreateDatabaseResponse struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Password string `json:"password"`
	Engine   string `json:"engine"`
}

var dbNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

func generatePassword(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return fmt.Sprintf("%x", b)[:length]
}

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

		username := fmt.Sprintf("%s_user_%s", req.Name, generatePassword(4))
		password := generatePassword(16)

		if req.Engine == "mysql" {
			if err := mysql.CreateDatabase(req.Name, username, password); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else if req.Engine == "postgres" {
			if err := postgres.CreateDatabase(req.Name, username, password); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
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
