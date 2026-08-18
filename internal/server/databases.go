// Database management handlers for MySQL/PostgreSQL databases.
package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"fluxo/internal/database"
	"fluxo/internal/safeinput"
	"fluxo/internal/services/mysql"
	"fluxo/internal/services/postgres"
)

type CreateDatabaseRequest struct {
	Name     string `json:"name"`
	Engine   string `json:"engine"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type CreateDatabaseResponse struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Password string `json:"password"`
	Engine   string `json:"engine"`
}

var dbNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
var databaseMutationMu sync.Mutex
var errAttachedDatabaseSetChanged = errors.New("the site's attached databases changed")

func cleanupCreatedDatabase(engine, name, username string, deleteRecord bool) error {
	var cleanupErr error
	if deleteRecord {
		if _, err := database.DB.Exec("DELETE FROM databases WHERE engine = ? AND name = ?", engine, name); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove database record: %w", err))
		}
	}
	if err := dropDatabase(engine, name); err != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove database: %w", err))
	}
	if username != "" && username != "fluxo" {
		var err error
		if engine == "postgres" {
			err = postgres.DropRole(username)
		} else {
			err = mysql.DropUser(username)
		}
		if err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove database user: %w", err))
		} else if engine == "mysql" {
			if markerErr := releaseManagedMySQLUser(username); markerErr != nil {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("release database user ownership: %w", markerErr))
			}
		}
	}
	return cleanupErr
}

func parseExpectedDatabaseIDs(raw string) ([]int, error) {
	if strings.TrimSpace(raw) == "" {
		return []int{}, nil
	}
	seen := make(map[int]struct{})
	ids := make([]int, 0)
	for _, part := range strings.Split(raw, ",") {
		id, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("invalid database ID %q", part)
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, fmt.Errorf("duplicate database ID %d", id)
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids, nil
}

func formatDatabaseIDs(ids []int) string {
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = strconv.Itoa(id)
	}
	return strings.Join(values, ",")
}

func databaseIDsMatch(databases []database.Database, expected []int) bool {
	if len(databases) != len(expected) {
		return false
	}
	current := make([]int, len(databases))
	for i, item := range databases {
		current[i] = item.ID
	}
	sort.Ints(current)
	for i := range current {
		if current[i] != expected[i] {
			return false
		}
	}
	return true
}

func validateAttachedDatabasesForDeletion(siteID int, expected []int) error {
	attached, err := getDatabasesForSite(siteID)
	if err != nil {
		return err
	}
	if err := validateDatabasesForDrop(attached); err != nil {
		return err
	}
	if !databaseIDsMatch(attached, expected) {
		return errAttachedDatabaseSetChanged
	}
	return nil
}

func validateRemainingDatabasesForDeletion(siteID int, expected []int) error {
	attached, err := getDatabasesForSite(siteID)
	if err != nil {
		return err
	}
	if err := validateDatabasesForDrop(attached); err != nil {
		return err
	}
	allowed := make(map[int]struct{}, len(expected))
	for _, id := range expected {
		allowed[id] = struct{}{}
	}
	for _, item := range attached {
		if _, ok := allowed[item.ID]; !ok {
			return errAttachedDatabaseSetChanged
		}
	}
	return nil
}

func preflightDatabaseEngines(siteID int) error {
	attached, err := getDatabasesForSite(siteID)
	if err != nil {
		return err
	}
	engines := make(map[string]struct{})
	for _, item := range attached {
		engines[item.Engine] = struct{}{}
	}
	if _, ok := engines["mysql"]; ok {
		if err := mysql.CheckConnection(); err != nil {
			return err
		}
	}
	if _, ok := engines["postgres"]; ok {
		if err := postgres.CheckConnection(); err != nil {
			return err
		}
	}
	return nil
}

func getDatabasesForSite(siteID int) ([]database.Database, error) {
	rows, err := database.DB.Query("SELECT id, site_id, engine, name, username, created_at FROM databases WHERE site_id = ? ORDER BY id", siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dbs := make([]database.Database, 0)
	for rows.Next() {
		var d database.Database
		if err := rows.Scan(&d.ID, &d.SiteID, &d.Engine, &d.Name, &d.Username, &d.CreatedAt); err != nil {
			return nil, err
		}
		dbs = append(dbs, d)
	}
	return dbs, rows.Err()
}

func dropDatabase(engine, name string) error {
	if err := validateDatabaseForDrop(engine, name); err != nil {
		return err
	}
	switch engine {
	case "mysql":
		return mysql.DeleteDatabase(name)
	case "postgres":
		return postgres.DeleteDatabase(name)
	}
	return nil
}

func validateDatabaseForDrop(engine, name string) error {
	if engine != "mysql" && engine != "postgres" {
		return fmt.Errorf("unsupported database engine %q", engine)
	}
	if !safeinput.ValidateDBIdent(name) {
		return fmt.Errorf("invalid database name %q", name)
	}
	return nil
}

func validateDatabasesForDrop(databases []database.Database) error {
	for _, item := range databases {
		if err := validateDatabaseForDrop(item.Engine, item.Name); err != nil {
			return fmt.Errorf("database %d: %w", item.ID, err)
		}
	}
	return nil
}

func dropDatabasesForSite(siteID int) error {
	candidates, err := getDatabasesForSite(siteID)
	if err != nil {
		return fmt.Errorf("load attached databases: %w", err)
	}
	if err := validateDatabasesForDrop(candidates); err != nil {
		return err
	}
	for _, candidate := range candidates {
		var engine, name string
		err = database.DB.QueryRow(
			"SELECT engine, name FROM databases WHERE id = ? AND site_id = ?",
			candidate.ID, siteID,
		).Scan(&engine, &name)
		if err == sql.ErrNoRows {
			return fmt.Errorf("database %q is no longer attached to this site", candidate.Name)
		}
		if err != nil {
			return fmt.Errorf("inspect database %q: %w", candidate.Name, err)
		}
		if err := dropDatabase(engine, name); err != nil {
			return fmt.Errorf("drop database %q: %w", name, err)
		}
		result, err := database.DB.Exec("DELETE FROM databases WHERE id = ? AND site_id = ?", candidate.ID, siteID)
		if err != nil {
			return fmt.Errorf("remove database record %q: %w", name, err)
		}
		if affected, err := result.RowsAffected(); err != nil || affected != 1 {
			if err != nil {
				return fmt.Errorf("confirm removal of database record %q: %w", name, err)
			}
			return fmt.Errorf("database %q changed while the site was being deleted", name)
		}
	}
	return nil
}

// handleListDatabases returns all databases for a site.
func (s *Server) handleListDatabases() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		dbs, err := getDatabasesForSite(siteID)
		if err != nil {
			http.Error(w, "DB error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(dbs)
	}
}

// handleCreateDatabase creates a database and optional user in MySQL or PostgreSQL.
func (s *Server) handleCreateDatabase() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))
		databaseMutationMu.Lock()
		defer databaseMutationMu.Unlock()

		var deletionStatus string
		if err := database.DB.QueryRow("SELECT COALESCE(deletion_status, '') FROM sites WHERE id = ?", siteID).Scan(&deletionStatus); err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		if deletionStatus != "" {
			http.Error(w, "Site deletion is in progress; retry the site deletion before changing databases", http.StatusConflict)
			return
		}

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

		var existing int
		if err := database.DB.QueryRow("SELECT COUNT(*) FROM databases WHERE engine = ? AND name = ?", req.Engine, req.Name).Scan(&existing); err != nil {
			http.Error(w, "Failed to check database records", http.StatusInternalServerError)
			return
		}
		if existing > 0 {
			http.Error(w, "A database with this name already exists for the selected engine.", http.StatusConflict)
			return
		}

		username := strings.TrimSpace(req.Username)
		password := req.Password
		if !dbNameRegex.MatchString(username) || password == "" || safeinput.HasControlChars(password) {
			http.Error(w, "A valid dedicated database username and password are required", http.StatusBadRequest)
			return
		}
		if username == "fluxo" || username == "root" || username == "postgres" {
			http.Error(w, "The database control-plane account cannot be used by an application", http.StatusBadRequest)
			return
		}

		if req.Engine == "mysql" {
			if err := reserveManagedMySQLUser(username); err != nil {
				if errors.Is(err, errManagedDatabaseUserExists) {
					http.Error(w, "A database user with this name is already managed by Fluxo", http.StatusConflict)
					return
				}
				http.Error(w, "Failed to reserve database user ownership", http.StatusInternalServerError)
				return
			}
			if err := mysql.CreateDatabase(req.Name, username, password); err != nil {
				if exists, inspectErr := mysql.LocalAccountExists(username, mysql.LocalTCPHost); inspectErr == nil && !exists {
					_ = releaseManagedMySQLUser(username)
				}
				if errors.Is(err, mysql.ErrUserExists) {
					http.Error(w, "A database user with this name already exists", http.StatusConflict)
					return
				}
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
			cleanupErr := cleanupCreatedDatabase(req.Engine, req.Name, username, false)
			message := "Failed to insert into sqlite: " + err.Error()
			if cleanupErr != nil {
				message += "; cleanup also failed: " + cleanupErr.Error()
			}
			http.Error(w, message, http.StatusInternalServerError)
			return
		}
		id, err := res.LastInsertId()
		if err != nil {
			cleanupErr := cleanupCreatedDatabase(req.Engine, req.Name, username, true)
			message := "Failed to identify the created database"
			if cleanupErr != nil {
				message += "; cleanup also failed: " + cleanupErr.Error()
			}
			http.Error(w, message, http.StatusInternalServerError)
			return
		}
		if req.Engine == "mysql" {
			if err := activateManagedMySQLUser(username); err != nil {
				cleanupErr := cleanupCreatedDatabase(req.Engine, req.Name, username, true)
				message := "Database was created, but Fluxo could not record database user ownership: " + err.Error()
				if cleanupErr != nil {
					message += "; cleanup also failed: " + cleanupErr.Error()
				}
				http.Error(w, message, http.StatusInternalServerError)
				return
			}
		}

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
		databaseMutationMu.Lock()
		defer databaseMutationMu.Unlock()

		var engine, name, deletionStatus string
		err := database.DB.QueryRow(`
			SELECT d.engine, d.name, COALESCE(s.deletion_status, '')
			FROM databases d
			LEFT JOIN sites s ON s.id = d.site_id
			WHERE d.id = ?`, dbID).Scan(&engine, &name, &deletionStatus)
		if err != nil {
			http.Error(w, "Database not found", http.StatusNotFound)
			return
		}
		if deletionStatus != "" {
			http.Error(w, "The database belongs to a site whose deletion has started", http.StatusConflict)
			return
		}
		err = s.backupManager.DeleteDatabase(dbID, func() error {
			if err := dropDatabase(engine, name); err != nil {
				return err
			}
			_, err := database.DB.Exec("DELETE FROM databases WHERE id = ?", dbID)
			return err
		})
		if err != nil {
			if strings.Contains(err.Error(), "backup plan") {
				http.Error(w, "Remove this database from its backup plans before deleting it", http.StatusConflict)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

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
