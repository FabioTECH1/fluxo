// Global database and user management handlers (MySQL + PostgreSQL).
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"fluxo/internal/config"
	"fluxo/internal/database"
	"fluxo/internal/safeinput"
	"fluxo/internal/services/mysql"
	"fluxo/internal/services/postgres"
	"fluxo/internal/syscmd"
)

var safeIdentRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// isValidDBIdent checks if a string is a safe database identifier (alphanumeric, underscores, hyphens).
func isValidDBIdent(s string) bool {
	return s != "" && safeIdentRegex.MatchString(s)
}

// handleGetDatabaseSizes returns sizes for all MySQL and PostgreSQL databases.
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
							"engine":  "mysql",
						})
					}
				}
			}
		}

		if _, err := exec.LookPath("psql"); err == nil {
			out, err := syscmd.Run(ctx, 10*time.Second, "sudo", "-u", "postgres", "psql", "-t", "-A", "-c", "SELECT datname, ROUND(pg_database_size(datname) / 1024.0 / 1024.0, 2) AS size_mb FROM pg_database WHERE datistemplate = false ORDER BY pg_database_size(datname) DESC")
			if err == nil {
				lines := strings.Split(strings.TrimSpace(out), "\n")
				for _, line := range lines {
					parts := strings.Split(line, "|")
					if len(parts) >= 2 {
						result = append(result, map[string]interface{}{
							"name":    strings.TrimSpace(parts[0]),
							"size_mb": strings.TrimSpace(parts[1]),
							"engine":  "postgres",
						})
					}
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

// handleGetDatabaseUsers returns all MySQL and PostgreSQL users.
func (s *Server) handleGetDatabaseUsers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		result := make([]map[string]interface{}, 0)

		if _, err := exec.LookPath("mysql"); err == nil {
			out, err := syscmd.Run(ctx, 10*time.Second, "mysql", "-e", "SELECT User, GROUP_CONCAT(Host ORDER BY Host SEPARATOR ',') AS Hosts FROM mysql.user WHERE User NOT IN ('root', 'mysql.sys', 'mysql.session', 'mysql.infoschema', 'mariadb.sys', 'mysql', 'debian-sys-maint') AND Host IN ('127.0.0.1', 'localhost') GROUP BY User ORDER BY User")
			if err == nil {
				lines := strings.Split(strings.TrimSpace(out), "\n")
				for i, line := range lines {
					if i == 0 {
						continue
					}
					fields := strings.Fields(line)
					if len(fields) >= 2 {
						state, stateErr := database.ManagedDatabaseUserState("mysql", fields[0], mysql.LocalTCPHost)
						result = append(result, map[string]interface{}{
							"user":    fields[0],
							"host":    fields[1],
							"engine":  "mysql",
							"managed": stateErr == nil && state == database.ManagedDatabaseUserActive,
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

// handleGetUserGrants returns the database grants for a given user.
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
			query := fmt.Sprintf(
				`SELECT datname FROM pg_database d
				WHERE d.datistemplate = false
				AND (
					(SELECT rolsuper FROM pg_roles WHERE rolname = '%s')
					OR (SELECT rolname FROM pg_roles WHERE oid = d.datdba) = '%s'
					OR (
						NOT pg_catalog.has_database_privilege('public', d.datname, 'CONNECT')
						AND pg_catalog.has_database_privilege('%s', d.datname, 'CONNECT')
					)
				)
				ORDER BY datname`,
				user, user, user)
			out, err := syscmd.Run(ctx, 10*time.Second, "sudo", "-u", "postgres", "psql", "-t", "-A", "-c", query)
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
		escapedUser := safeinput.EscapeSQLString(user)
		hostOutput := ""
		state, stateErr := database.ManagedDatabaseUserState("mysql", user, mysql.LocalTCPHost)
		if stateErr == nil && state == database.ManagedDatabaseUserActive {
			hostOutput = mysql.LocalTCPHost
		} else {
			var err error
			hostOutput, err = syscmd.Run(ctx, 5*time.Second, "mysql", "-B", "-N", "-e", fmt.Sprintf("SELECT Host FROM mysql.user WHERE User = '%s' ORDER BY Host", escapedUser))
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode([]string{})
				return
			}
		}
		databaseSet := make(map[string]struct{})
		hasAll := false
		for _, rawHost := range strings.Split(strings.TrimSpace(hostOutput), "\n") {
			host := strings.TrimSpace(rawHost)
			if host == "" {
				continue
			}
			out, grantErr := syscmd.Run(ctx, 10*time.Second, "mysql", "-e", fmt.Sprintf("SHOW GRANTS FOR '%s'@'%s'", escapedUser, safeinput.EscapeSQLString(host)))
			if grantErr != nil {
				continue
			}
			for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
				if strings.Contains(line, "GRANT ALL PRIVILEGES ON *.*") {
					hasAll = true
				}
				if strings.Contains(line, "GRANT ALL PRIVILEGES ON `") {
					parts := strings.Split(line, "`")
					if len(parts) >= 2 && parts[1] != "*" {
						databaseSet[parts[1]] = struct{}{}
					}
				}
			}
		}
		dbs := make([]string, 0, len(databaseSet)+1)
		for dbName := range databaseSet {
			dbs = append(dbs, dbName)
		}
		sort.Strings(dbs)
		if hasAll {
			dbs = append([]string{"*"}, dbs...)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(dbs)
	}
}

// handleCreateDatabaseUser creates a DB user and grants access to specified databases.
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
		if req.User == "fluxo" || req.User == "root" || req.User == "postgres" {
			http.Error(w, "Choose a dedicated database username", http.StatusBadRequest)
			return
		}

		pass := req.Password
		if pass == "" {
			var genErr error
			pass, genErr = safeinput.GenerateSecretHex(8)
			if genErr != nil {
				http.Error(w, "Failed to generate password", http.StatusInternalServerError)
				return
			}
		}
		engine := req.Engine
		if engine == "" {
			engine = "mysql"
		}
		if engine != "mysql" && engine != "postgres" {
			http.Error(w, "Invalid engine", http.StatusBadRequest)
			return
		}
		for _, db := range req.Databases {
			if !isValidDBIdent(db) {
				http.Error(w, "Invalid database name", http.StatusBadRequest)
				return
			}
		}
		databaseMutationMu.Lock()
		defer databaseMutationMu.Unlock()
		if engine == "postgres" {
			if err := postgres.CreateRole(req.User, pass); err != nil {
				http.Error(w, "Failed to create user: "+err.Error(), http.StatusInternalServerError)
				return
			}
			for _, db := range req.Databases {
				if err := postgres.GrantDatabaseAccess(db, req.User); err != nil {
					if cleanupErr := postgres.DropRole(req.User); cleanupErr != nil {
						http.Error(w, "Failed to grant database access: "+err.Error()+"; cleanup also failed: "+cleanupErr.Error(), http.StatusInternalServerError)
						return
					}
					http.Error(w, "Failed to grant database access: "+err.Error(), http.StatusInternalServerError)
					return
				}
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

		if err := reserveManagedMySQLUser(req.User); err != nil {
			if errors.Is(err, errManagedDatabaseUserExists) {
				http.Error(w, "A database user with this name is already managed by Fluxo", http.StatusConflict)
				return
			}
			http.Error(w, "Failed to reserve database user ownership", http.StatusInternalServerError)
			return
		}
		if err := mysql.CreateUser(req.User, pass); err != nil {
			if exists, inspectErr := mysql.LocalAccountExists(req.User, mysql.LocalTCPHost); inspectErr == nil && !exists {
				_ = releaseManagedMySQLUser(req.User)
			}
			if errors.Is(err, mysql.ErrUserExists) {
				http.Error(w, "A database user with this name already exists", http.StatusConflict)
				return
			}
			http.Error(w, "Failed to create user: "+err.Error(), http.StatusInternalServerError)
			return
		}
		for _, db := range req.Databases {
			if err := mysql.GrantDatabaseAccess(db, req.User); err != nil {
				cleanupErr := mysql.DropUser(req.User)
				if cleanupErr == nil {
					cleanupErr = releaseManagedMySQLUser(req.User)
				}
				message := "Failed to grant database access: " + err.Error()
				if cleanupErr != nil {
					message += "; cleanup also failed: " + cleanupErr.Error()
				}
				http.Error(w, message, http.StatusInternalServerError)
				return
			}
		}
		if err := activateManagedMySQLUser(req.User); err != nil {
			cleanupErr := mysql.DropUser(req.User)
			if cleanupErr == nil {
				cleanupErr = releaseManagedMySQLUser(req.User)
			}
			message := "Database user was created, but Fluxo could not record ownership: " + err.Error()
			if cleanupErr != nil {
				message += "; cleanup also failed: " + cleanupErr.Error()
			}
			http.Error(w, message, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"user":      req.User,
			"password":  pass,
			"databases": req.Databases,
			"engine":    "mysql",
		})
	}
}

// handleUpdateUserGrants revokes and re-applies grants for a user across all hosts.
func (s *Server) handleUpdateUserGrants() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			User      string   `json:"user"`
			Databases []string `json:"databases"`
			Engine    string   `json:"engine"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil || req.User == "" {
			http.Error(w, "Invalid request. Database user updates only accept user, databases, and engine.", http.StatusBadRequest)
			return
		}

		if !isValidDBIdent(req.User) {
			http.Error(w, "Invalid username format", http.StatusBadRequest)
			return
		}
		if req.User == "fluxo" {
			http.Error(w, "Fluxo's managed database grants cannot be changed", http.StatusForbidden)
			return
		}
		for _, db := range req.Databases {
			if !isValidDBIdent(db) {
				http.Error(w, "Invalid database name", http.StatusBadRequest)
				return
			}
		}

		ctx := r.Context()
		engine := req.Engine
		if engine == "" {
			engine = "mysql"
		}
		if engine != "mysql" && engine != "postgres" {
			http.Error(w, "Invalid engine", http.StatusBadRequest)
			return
		}
		databaseMutationMu.Lock()
		defer databaseMutationMu.Unlock()
		if engine == "postgres" {
			// Revoke from databases no longer in the user's list
			out, err := syscmd.Run(ctx, 10*time.Second, "sudo", "-u", "postgres", "psql", "-t", "-A", "-c", "SELECT datname FROM pg_database WHERE datallowconn AND datistemplate = false")
			if err != nil {
				http.Error(w, "Failed to inspect current PostgreSQL grants: "+err.Error(), http.StatusInternalServerError)
				return
			}
			for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
				db := strings.TrimSpace(line)
				if db == "" {
					continue
				}
				wanted := false
				for _, d := range req.Databases {
					if d == db {
						wanted = true
						break
					}
				}
				if !wanted {
					if err := postgres.RevokeDatabaseAccess(db, req.User); err != nil {
						http.Error(w, "Failed to revoke database access: "+err.Error(), http.StatusInternalServerError)
						return
					}
				}
			}

			// Grant access to selected databases
			for _, db := range req.Databases {
				if err := postgres.GrantDatabaseAccess(db, req.User); err != nil {
					http.Error(w, "Failed to grant database access: "+err.Error(), http.StatusInternalServerError)
					return
				}
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if err := requireManagedMySQLUser(req.User); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if err := mysql.ReplaceDatabaseAccess(req.User, req.Databases); err != nil {
			http.Error(w, "Failed to update MySQL privileges: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleRotateDatabaseUserPassword changes a database account password without
// modifying any site's environment file. Dedicated passwords are not retained;
// the managed fluxo credential is synchronized for control-panel operations.
func (s *Server) handleRotateDatabaseUserPassword() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			User     string `json:"user"`
			Password string `json:"password"`
			Engine   string `json:"engine"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		if !isValidDBIdent(req.User) {
			http.Error(w, "Missing or invalid database user", http.StatusBadRequest)
			return
		}
		if req.User == "postgres" || req.User == "root" {
			http.Error(w, "Cannot rotate the database engine's root account here", http.StatusForbidden)
			return
		}
		if len(req.Password) < 8 || safeinput.HasControlChars(req.Password) {
			http.Error(w, "Password must be at least 8 characters and contain no control characters", http.StatusBadRequest)
			return
		}
		if req.Engine != "mysql" && req.Engine != "postgres" {
			http.Error(w, "Invalid engine", http.StatusBadRequest)
			return
		}

		databaseMutationMu.Lock()
		defer databaseMutationMu.Unlock()
		if req.Engine == "mysql" && req.User != "fluxo" {
			if err := requireManagedMySQLUser(req.User); err != nil {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
		}

		rotatePassword := func(password string) error {
			if req.Engine == "postgres" {
				return postgres.UpdateRolePassword(req.User, password)
			}
			return mysql.UpdateLocalUserPassword(req.User, password)
		}

		storedPasswordColumn := ""
		previousManagedPassword := ""
		newManagedPassword := ""
		if req.User == "fluxo" {
			storedPasswordColumn = "fluxo_mysql_password"
			if req.Engine == "postgres" {
				storedPasswordColumn = "fluxo_postgres_password"
			}
			var encryptedPrevious string
			if err := database.DB.QueryRow("SELECT " + storedPasswordColumn + " FROM users ORDER BY id ASC LIMIT 1").Scan(&encryptedPrevious); err != nil {
				http.Error(w, "Failed to load the managed database credential", http.StatusInternalServerError)
				return
			}
			previousManagedPassword = config.Decrypt(encryptedPrevious)
			if previousManagedPassword == "" || strings.HasPrefix(previousManagedPassword, "enc:") {
				http.Error(w, "The existing managed database credential cannot be decrypted", http.StatusConflict)
				return
			}
			var err error
			newManagedPassword, err = config.EncryptSecret(req.Password)
			if err != nil {
				http.Error(w, "Failed to encrypt the managed database credential", http.StatusInternalServerError)
				return
			}
		}

		if err := rotatePassword(req.Password); err != nil {
			http.Error(w, "Failed to rotate "+req.Engine+" user password: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if storedPasswordColumn != "" {
			result, updateErr := database.DB.Exec("UPDATE users SET "+storedPasswordColumn+" = ? WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)", newManagedPassword)
			updatedRows := int64(0)
			if updateErr == nil {
				updatedRows, updateErr = result.RowsAffected()
			}
			if updateErr != nil || updatedRows != 1 {
				rollbackErr := rotatePassword(previousManagedPassword)
				message := "Database password changed, but Fluxo could not save its managed credential"
				if rollbackErr != nil {
					message += "; restoring the previous database password also failed: " + rollbackErr.Error()
				}
				http.Error(w, message, http.StatusInternalServerError)
				return
			}
		}

		LogActivityWithUser(0, "database_user_password_rotated", "Password rotated for "+req.Engine+" database user "+req.User, usernameFromContext(r.Context()), getClientIP(r))
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleCreateGlobalDatabase creates a database not tied to any site.
func (s *Server) handleCreateGlobalDatabase() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name     string `json:"name"`
			Engine   string `json:"engine"`
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		if !isValidDBIdent(req.Name) {
			http.Error(w, "Invalid database name format. Only alphanumeric characters, underscores, and hyphens are allowed.", http.StatusBadRequest)
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

		username := strings.TrimSpace(req.Username)
		password := req.Password
		createUser := username != ""
		if createUser {
			if !isValidDBIdent(username) {
				http.Error(w, "Invalid username format", http.StatusBadRequest)
				return
			}
			if username == "fluxo" || username == "root" || username == "postgres" {
				http.Error(w, "Choose a dedicated database username", http.StatusBadRequest)
				return
			}
			if safeinput.HasControlChars(password) {
				http.Error(w, "Invalid database password", http.StatusBadRequest)
				return
			}
			if password == "" {
				var err error
				password, err = safeinput.GenerateSecretHex(8)
				if err != nil {
					http.Error(w, "Failed to generate password", http.StatusInternalServerError)
					return
				}
			}
		}

		if !createUser {
			username = "fluxo"
			password = ""
		}
		databaseMutationMu.Lock()
		defer databaseMutationMu.Unlock()

		var existing int
		if err := database.DB.QueryRow("SELECT COUNT(*) FROM databases WHERE engine = ? AND name = ?", engine, req.Name).Scan(&existing); err != nil {
			http.Error(w, "Failed to check database records", http.StatusInternalServerError)
			return
		}
		if existing > 0 {
			http.Error(w, "A database with this name already exists for the selected engine.", http.StatusConflict)
			return
		}

		if engine == "postgres" {
			if _, err := exec.LookPath("psql"); err != nil {
				http.Error(w, "PostgreSQL is not installed.", http.StatusBadRequest)
				return
			}
			var createErr error
			if createUser {
				createErr = postgres.CreateDatabase(req.Name, username, password)
			} else {
				createErr = postgres.CreateDatabaseOnly(req.Name)
			}
			if createErr != nil {
				http.Error(w, "Failed to create PostgreSQL database: "+createErr.Error(), http.StatusInternalServerError)
				return
			}
			if _, err := database.DB.Exec("INSERT INTO databases (site_id, engine, name, username) VALUES (?, ?, ?, ?)", 0, "postgres", req.Name, username); err != nil {
				cleanupErr := postgres.DeleteDatabase(req.Name)
				if createUser {
					cleanupErr = errors.Join(cleanupErr, postgres.DropRole(username))
				}
				message := "Failed to save database record: " + err.Error()
				if cleanupErr != nil {
					message += "; cleanup also failed: " + cleanupErr.Error()
				}
				http.Error(w, message, http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"name": req.Name, "engine": "postgres", "username": username, "password": password})
			return
		}

		var createErr error
		if createUser {
			if err := reserveManagedMySQLUser(username); err != nil {
				if errors.Is(err, errManagedDatabaseUserExists) {
					http.Error(w, "A database user with this name is already managed by Fluxo", http.StatusConflict)
					return
				}
				http.Error(w, "Failed to reserve database user ownership", http.StatusInternalServerError)
				return
			}
		}
		if createUser {
			createErr = mysql.CreateDatabase(req.Name, username, password)
		} else {
			createErr = mysql.CreateDatabaseOnly(req.Name)
		}
		if createErr != nil {
			if createUser {
				if exists, inspectErr := mysql.LocalAccountExists(username, mysql.LocalTCPHost); inspectErr == nil && !exists {
					_ = releaseManagedMySQLUser(username)
				}
			}
			if errors.Is(createErr, mysql.ErrUserExists) {
				http.Error(w, "A database user with this name already exists", http.StatusConflict)
				return
			}
			http.Error(w, "Failed to create MySQL database: "+createErr.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := database.DB.Exec("INSERT INTO databases (site_id, engine, name, username) VALUES (?, ?, ?, ?)", 0, "mysql", req.Name, username); err != nil {
			cleanupErr := mysql.DeleteDatabase(req.Name)
			if createUser {
				userErr := mysql.DropUser(username)
				markerErr := error(nil)
				if userErr == nil {
					markerErr = releaseManagedMySQLUser(username)
				}
				cleanupErr = errors.Join(cleanupErr, userErr, markerErr)
			}
			message := "Failed to save database record: " + err.Error()
			if cleanupErr != nil {
				message += "; cleanup also failed: " + cleanupErr.Error()
			}
			http.Error(w, message, http.StatusInternalServerError)
			return
		}
		if createUser {
			if err := activateManagedMySQLUser(username); err != nil {
				_, recordErr := database.DB.Exec("DELETE FROM databases WHERE engine = 'mysql' AND name = ?", req.Name)
				databaseErr := mysql.DeleteDatabase(req.Name)
				userErr := mysql.DropUser(username)
				markerErr := error(nil)
				if userErr == nil {
					markerErr = releaseManagedMySQLUser(username)
				}
				cleanupErr := errors.Join(recordErr, databaseErr, userErr, markerErr)
				message := "Database was created, but Fluxo could not record user ownership: " + err.Error()
				if cleanupErr != nil {
					message += "; cleanup also failed: " + cleanupErr.Error()
				}
				http.Error(w, message, http.StatusInternalServerError)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"name": req.Name, "engine": "mysql", "username": username, "password": password})
	}
}

// handleDeleteDatabaseUser drops a DB user and revokes all their privileges.
func (s *Server) handleDeleteDatabaseUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := r.URL.Query().Get("user")
		engine := r.URL.Query().Get("engine")
		if user == "" || !isValidDBIdent(user) {
			http.Error(w, "Missing or invalid user", http.StatusBadRequest)
			return
		}

		if user == "fluxo" || user == "postgres" || user == "root" {
			http.Error(w, "Cannot delete the system user", http.StatusForbidden)
			return
		}
		if engine != "mysql" && engine != "postgres" {
			http.Error(w, "Invalid engine", http.StatusBadRequest)
			return
		}
		databaseMutationMu.Lock()
		defer databaseMutationMu.Unlock()

		var assignedDatabases int
		if err := database.DB.QueryRow("SELECT COUNT(*) FROM databases WHERE engine = ? AND username = ? AND site_id != 0", engine, user).Scan(&assignedDatabases); err != nil {
			http.Error(w, "Failed to inspect database user assignments", http.StatusInternalServerError)
			return
		}
		if assignedDatabases > 0 {
			http.Error(w, "Disconnect the sites using this database user before deleting it", http.StatusConflict)
			return
		}

		if engine == "postgres" {
			if err := postgres.DropRole(user); err != nil {
				http.Error(w, "Failed to drop user: "+err.Error(), http.StatusInternalServerError)
				return
			}
			if _, err := database.DB.Exec("UPDATE databases SET username = '' WHERE engine = 'postgres' AND username = ? AND site_id = 0", user); err != nil {
				http.Error(w, "User was removed, but Fluxo could not clear unassigned database metadata", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if err := requireManagedMySQLUser(user); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if err := mysql.DropManagedLocalUser(user); err != nil {
			http.Error(w, "Failed to drop user: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := releaseManagedMySQLUser(user); err != nil {
			http.Error(w, "User was removed, but Fluxo could not clear its ownership record", http.StatusInternalServerError)
			return
		}
		if _, err := database.DB.Exec("UPDATE databases SET username = '' WHERE engine = 'mysql' AND username = ? AND site_id = 0", user); err != nil {
			http.Error(w, "User was removed, but Fluxo could not clear unassigned database metadata", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
