package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"

	"fluxo/internal/config"
	"fluxo/internal/database"
	"fluxo/internal/safeinput"
	"fluxo/internal/services/git"
)

type SettingsRequest struct {
	GitHubPAT  string `json:"github_pat"`
	AdminEmail string `json:"admin_email"`
	DefaultPHP string `json:"default_php"`
}

type UpdatePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// handleGetSettings returns the current admin settings (PAT, email, default PHP).
func (s *Server) handleGetSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var pat, email, defaultPhp string
		err := database.DB.QueryRow("SELECT github_pat, admin_email, default_php FROM users ORDER BY id ASC LIMIT 1").Scan(&pat, &email, &defaultPhp)
		if err != nil {
			pat = ""
			email = ""
			defaultPhp = "8.4"
		} else {
			pat = config.Decrypt(pat)
		}
		if defaultPhp == "" {
			defaultPhp = "8.4"
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"github_pat":  pat,
			"admin_email": email,
			"default_php": defaultPhp,
		})
	}
}

// handleGetBootstrapCredentials returns the one-time bootstrap credentials before first login.
func (s *Server) handleGetBootstrapCredentials() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var mysqlPass, postgresPass, sudoPass, pendingEngine string
		var credentialsCopied int
		err := database.DB.QueryRow("SELECT fluxo_mysql_password, fluxo_postgres_password, fluxo_sudo_password, credentials_copied, pending_new_password_engine FROM users ORDER BY id ASC LIMIT 1").Scan(&mysqlPass, &postgresPass, &sudoPass, &credentialsCopied, &pendingEngine)
		if err != nil {
			http.Error(w, "Failed to retrieve credentials", http.StatusInternalServerError)
			return
		}

		sudoPass = config.Decrypt(sudoPass)
		mysqlPass = config.Decrypt(mysqlPass)
		postgresPass = config.Decrypt(postgresPass)

		// If a specific engine was installed at runtime, show one-time popup for just that engine
		if pendingEngine != "" {
			database.DB.Exec("UPDATE users SET pending_new_password_engine = '' WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)")
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]string{
				"fluxo_sudo_password": sudoPass,
			}
			if pendingEngine == "mysql" {
				if _, err := exec.LookPath("mysql"); err == nil {
					resp["fluxo_mysql_password"] = mysqlPass
				}
			} else if pendingEngine == "postgres" {
				if _, err := exec.LookPath("psql"); err == nil {
					resp["fluxo_postgres_password"] = postgresPass
				}
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		if credentialsCopied != 0 {
			http.Error(w, "Credentials have already been acknowledged and cleared", http.StatusForbidden)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]string{
			"fluxo_sudo_password": sudoPass,
		}
		if _, err := exec.LookPath("mysql"); err == nil {
			resp["fluxo_mysql_password"] = mysqlPass
		}
		if _, err := exec.LookPath("psql"); err == nil {
			resp["fluxo_postgres_password"] = postgresPass
		}
		json.NewEncoder(w).Encode(resp)
	}
}

// handleMarkCredentialsCopied marks the bootstrap credentials as acknowledged.
func (s *Server) handleMarkCredentialsCopied() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var pendingEngine string
		database.DB.QueryRow("SELECT pending_new_password_engine FROM users ORDER BY id ASC LIMIT 1").Scan(&pendingEngine)
		if pendingEngine != "" {
			database.DB.Exec("UPDATE users SET pending_new_password_engine = '' WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)")
		} else {
			database.DB.Exec("UPDATE users SET credentials_copied = 1 WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)")
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleUpdateSettings saves admin settings (GitHub PAT, email, default PHP).
func (s *Server) handleUpdateSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req SettingsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.DefaultPHP == "" {
			req.DefaultPHP = "8.4"
		}
		if !safeinput.ValidatePHPVersion(req.DefaultPHP) {
			http.Error(w, "Invalid PHP version", http.StatusBadRequest)
			return
		}

		if req.GitHubPAT != "" {
			provider := git.NewGitHubProvider(req.GitHubPAT)
			if _, err := provider.ListRepositories(); err != nil {
				http.Error(w, "Invalid GitHub token: "+err.Error(), http.StatusBadRequest)
				return
			}
		}

		encPat := config.Encrypt(req.GitHubPAT)

		_, err := database.DB.Exec("UPDATE users SET github_pat = ?, admin_email = ?, default_php = ? WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)", encPat, req.AdminEmail, req.DefaultPHP)
		if err != nil {
			http.Error(w, "Failed to update settings", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// getGitHubTokenForAccount resolves an account ID query parameter to the database record.
// If accountIDStr is empty or invalid, it falls back to the first available GitHub account.
func getGitHubTokenForAccount(accountIDStr string) (int, string, error) {
	var row *sql.Row
	if accountIDStr != "" {
		accountID, err := strconv.Atoi(accountIDStr)
		if err == nil && accountID > 0 {
			row = database.DB.QueryRow("SELECT id, token FROM github_accounts WHERE id = ?", accountID)
		}
	}
	if row == nil {
		row = database.DB.QueryRow("SELECT id, token FROM github_accounts ORDER BY id ASC LIMIT 1")
	}

	var id int
	var token string
	err := row.Scan(&id, &token)
	if err != nil {
		return 0, "", err
	}

	decrypted := config.Decrypt(token)
	return id, decrypted, nil
}

// handleGetGitHubRepos returns the list of GitHub repos from cache or live API.
func (s *Server) handleGetGitHubRepos() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accountIDStr := r.URL.Query().Get("account_id")
		id, pat, err := getGitHubTokenForAccount(accountIDStr)
		if err != nil || pat == "" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("[]"))
			return
		}

		cacheKey := fmt.Sprintf("repos:account:%d", id)
		forceRefresh := r.URL.Query().Get("refresh") == "1"

		// Serve from DB cache
		if !forceRefresh {
			if cached, ok := database.GetCachedGitHubData(cacheKey); ok {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(cached))
				go refreshGitHubRepos(cacheKey, pat)
				return
			}
		}

		// Fetch live from GitHub
		provider := git.NewGitHubProvider(pat)
		repos, err := provider.ListRepositories()
		if err != nil {
			if cached, ok := database.GetCachedGitHubData(cacheKey); ok {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(cached))
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		data, _ := json.Marshal(repos)
		database.SetCachedGitHubData(cacheKey, string(data))

		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}
}

// refreshGitHubRepos updates the cached repo list in the background.
func refreshGitHubRepos(cacheKey, pat string) {
	provider := git.NewGitHubProvider(pat)
	repos, err := provider.ListRepositories()
	if err != nil {
		return
	}
	data, _ := json.Marshal(repos)
	database.SetCachedGitHubData(cacheKey, string(data))
}

// handleGetGitHubBranches returns branches for a repo from cache or live API.
func (s *Server) handleGetGitHubBranches() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repo := r.URL.Query().Get("repo")
		if repo == "" {
			http.Error(w, "Missing repo parameter", http.StatusBadRequest)
			return
		}

		accountIDStr := r.URL.Query().Get("account_id")
		id, pat, err := getGitHubTokenForAccount(accountIDStr)
		if err != nil || pat == "" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("[]"))
			return
		}

		cacheKey := fmt.Sprintf("branches:%s:account:%d", repo, id)
		forceRefresh := r.URL.Query().Get("refresh") == "1"

		// Serve from DB cache
		if !forceRefresh {
			if cached, ok := database.GetCachedGitHubData(cacheKey); ok {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(cached))
				go refreshGitHubBranches(cacheKey, repo, pat)
				return
			}
		}

		// Fetch live from GitHub
		provider := git.NewGitHubProvider(pat)
		branches, err := provider.ListBranches(repo)
		if err != nil {
			if cached, ok := database.GetCachedGitHubData(cacheKey); ok {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(cached))
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		data, _ := json.Marshal(branches)
		database.SetCachedGitHubData(cacheKey, string(data))

		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}
}

// refreshGitHubBranches updates the cached branch list in the background.
func refreshGitHubBranches(cacheKey, repo, pat string) {
	provider := git.NewGitHubProvider(pat)
	branches, err := provider.ListBranches(repo)
	if err != nil {
		return
	}
	data, _ := json.Marshal(branches)
	database.SetCachedGitHubData(cacheKey, string(data))
}

// handleUpdatePassword changes the admin password and bumps the token version.
func (s *Server) handleUpdatePassword() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req UpdatePasswordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		if len(req.NewPassword) < 8 {
			http.Error(w, "New password must be at least 8 characters long", http.StatusBadRequest)
			return
		}

		var tokenHash string
		err := database.DB.QueryRow("SELECT token_hash FROM users ORDER BY id ASC LIMIT 1").Scan(&tokenHash)
		if err != nil {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}

		// Verify current password (supports both legacy SHA-256 and bcrypt)
		if !verifyPassword(req.CurrentPassword, tokenHash) {
			http.Error(w, "Incorrect current password", http.StatusUnauthorized)
			return
		}

		// Hash and save new password
		newHashBytes, err := hashPassword(req.NewPassword)
		if err != nil {
			http.Error(w, "Failed to hash new password", http.StatusInternalServerError)
			return
		}

		_, err = database.DB.Exec("UPDATE users SET token_hash = ?, token_version = token_version + 1 WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)", string(newHashBytes))
		if err != nil {
			http.Error(w, "Failed to update password", http.StatusInternalServerError)
			return
		}

		ip := getClientIP(r)
		LogActivityWithUser(0, "password_changed", "Admin password was changed", usernameFromContext(r.Context()), ip)

		w.WriteHeader(http.StatusNoContent)
	}
}
