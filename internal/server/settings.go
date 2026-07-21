package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"fluxo/internal/config"
	"fluxo/internal/database"
	"fluxo/internal/safeinput"
	"fluxo/internal/services/bootstrap"
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

type bootstrapCredentialState struct {
	mysqlPass          string
	postgresPass       string
	sudoPass           string
	pendingEngines     string
	credentialsCopied  int
	generation         int64
	downloadGeneration int64
}

func loadBootstrapCredentialState() (bootstrapCredentialState, error) {
	var state bootstrapCredentialState
	err := database.DB.QueryRow(`SELECT fluxo_mysql_password, fluxo_postgres_password,
		fluxo_sudo_password, pending_new_password_engine, credentials_copied,
		credentials_generation, credentials_download_generation
		FROM users ORDER BY id ASC LIMIT 1`).Scan(
		&state.mysqlPass, &state.postgresPass, &state.sudoPass, &state.pendingEngines,
		&state.credentialsCopied, &state.generation, &state.downloadGeneration,
	)
	return state, err
}

func pendingEngineSet(value string) map[string]bool {
	engines := make(map[string]bool)
	for _, engine := range strings.Split(value, ",") {
		engine = strings.TrimSpace(engine)
		if engine != "" {
			engines[engine] = true
		}
	}
	return engines
}

func availableBootstrapCredentials(state bootstrapCredentialState) map[string]string {
	resp := make(map[string]string)
	if state.credentialsCopied == 0 {
		if value := config.Decrypt(state.sudoPass); value != "" {
			resp["fluxo_sudo_password"] = value
		}
		if _, err := exec.LookPath("mysql"); err == nil {
			if value := config.Decrypt(state.mysqlPass); value != "" {
				resp["fluxo_mysql_password"] = value
			}
		}
		if _, err := exec.LookPath("psql"); err == nil {
			if value := config.Decrypt(state.postgresPass); value != "" {
				resp["fluxo_postgres_password"] = value
			}
		}
		return resp
	}

	pending := pendingEngineSet(state.pendingEngines)
	if pending["mysql"] {
		if value := config.Decrypt(state.mysqlPass); value != "" {
			resp["fluxo_mysql_password"] = value
		}
	}
	if pending["postgres"] {
		if value := config.Decrypt(state.postgresPass); value != "" {
			resp["fluxo_postgres_password"] = value
		}
	}
	return resp
}

func bootstrapCredentialsAvailable(state bootstrapCredentialState) bool {
	if state.credentialsCopied == 0 {
		if state.sudoPass != "" {
			return true
		}
		if state.mysqlPass != "" {
			if _, err := exec.LookPath("mysql"); err == nil {
				return true
			}
		}
		if state.postgresPass != "" {
			if _, err := exec.LookPath("psql"); err == nil {
				return true
			}
		}
		return false
	}

	pending := pendingEngineSet(state.pendingEngines)
	return pending["mysql"] && state.mysqlPass != "" || pending["postgres"] && state.postgresPass != ""
}

func handleCredentialStateError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if err == sql.ErrNoRows {
		http.Error(w, "Credentials are unavailable", http.StatusNotFound)
	} else {
		http.Error(w, "Failed to retrieve credentials", http.StatusInternalServerError)
	}
	return true
}

func setCredentialResponseHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

// handleGetBootstrapCredentials returns credentials for display without consuming them.
func (s *Server) handleGetBootstrapCredentials() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCredentialResponseHeaders(w)
		state, err := loadBootstrapCredentialState()
		if handleCredentialStateError(w, err) {
			return
		}
		resp := availableBootstrapCredentials(state)
		if len(resp) == 0 {
			http.Error(w, "Credentials have already been acknowledged and are no longer available", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// handleGetBootstrapCredentialsStatus lets the UI rediscover credentials after
// navigation or a reload without repeatedly returning the secrets themselves.
func (s *Server) handleGetBootstrapCredentialsStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCredentialResponseHeaders(w)
		state, err := loadBootstrapCredentialState()
		if handleCredentialStateError(w, err) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"available": bootstrapCredentialsAvailable(state)})
	}
}

// handleDownloadBootstrapCredentials delivers the server-generated text file
// and records exactly which credential generation was delivered.
func (s *Server) handleDownloadBootstrapCredentials() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCredentialResponseHeaders(w)
		state, err := loadBootstrapCredentialState()
		if handleCredentialStateError(w, err) {
			return
		}
		credentials := availableBootstrapCredentials(state)
		if len(credentials) == 0 {
			http.Error(w, "Credentials are no longer available", http.StatusForbidden)
			return
		}
		bootstrapToken := bootstrap.ReadBootstrapToken(s.dataDir)
		if state.credentialsCopied == 0 && bootstrapToken == "" {
			http.Error(w, "The administrator token is unavailable; restore the credentials file or reset the admin token before acknowledging credentials", http.StatusConflict)
			return
		}

		lines := []string{
			"Fluxo Administrative Credentials",
			"================================",
			"",
			"Store this file securely. Fluxo will not show these credentials again after acknowledgement.",
			"",
		}
		if bootstrapToken != "" {
			lines = append(lines, "Administrator login token: "+bootstrapToken)
		}
		if value := credentials["fluxo_sudo_password"]; value != "" {
			lines = append(lines, "System user 'fluxo' sudo password: "+value)
		}
		if value := credentials["fluxo_mysql_password"]; value != "" {
			lines = append(lines, "MySQL superuser 'fluxo' password: "+value)
		}
		if value := credentials["fluxo_postgres_password"]; value != "" {
			lines = append(lines, "PostgreSQL superuser 'fluxo' password: "+value)
		}
		contents := []byte(strings.Join(append(lines, ""), "\n"))

		result, err := database.DB.Exec(`UPDATE users SET credentials_download_generation = ?
			WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)
			AND credentials_generation = ?`, state.generation, state.generation)
		if err != nil {
			http.Error(w, "Failed to record the credentials download", http.StatusInternalServerError)
			return
		}
		affected, err := result.RowsAffected()
		if err != nil {
			http.Error(w, "Failed to verify the credentials download", http.StatusInternalServerError)
			return
		}
		if affected != 1 {
			http.Error(w, "Credentials changed; request the latest file", http.StatusConflict)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="fluxo-administrative-credentials.txt"`)
		written, writeErr := w.Write(contents)
		if writeErr != nil || written != len(contents) {
			result, rollbackErr := database.DB.Exec(`UPDATE users SET credentials_download_generation = -1
				WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)
				AND credentials_generation = ? AND credentials_download_generation = ?`,
				state.generation, state.generation)
			if rollbackErr != nil {
				log.Printf("Failed to invalidate incomplete credential download: %v", rollbackErr)
			} else if affected, rowsErr := result.RowsAffected(); rowsErr != nil || affected != 1 {
				log.Printf("Incomplete credential download proof was not invalidated (affected=%d, err=%v)", affected, rowsErr)
			}
		}
	}
}

// handleMarkCredentialsCopied only acknowledges the exact generation most
// recently delivered by the download endpoint.
func (s *Server) handleMarkCredentialsCopied() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state, err := loadBootstrapCredentialState()
		if handleCredentialStateError(w, err) {
			return
		}
		if state.downloadGeneration != state.generation {
			http.Error(w, "Download the current credentials file before acknowledging it", http.StatusConflict)
			return
		}

		tx, err := database.DB.BeginTx(r.Context(), nil)
		if err != nil {
			http.Error(w, "Failed to begin credential acknowledgement", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		result, err := tx.ExecContext(r.Context(), `UPDATE users SET credentials_copied = 1,
			pending_new_password_engine = '', credentials_download_generation = -1
			WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)
			AND credentials_generation = ? AND credentials_download_generation = ?`,
			state.generation, state.generation)
		if err != nil {
			http.Error(w, "Failed to acknowledge credentials", http.StatusInternalServerError)
			return
		}
		affected, err := result.RowsAffected()
		if err != nil {
			http.Error(w, "Failed to verify credential acknowledgement", http.StatusInternalServerError)
			return
		}
		if affected != 1 {
			http.Error(w, "Credentials changed; download the latest file before acknowledging it", http.StatusConflict)
			return
		}

		if err := tx.Commit(); err != nil {
			http.Error(w, "Failed to commit credential acknowledgement", http.StatusInternalServerError)
			return
		}

		// The database acknowledgement is committed first so a process crash can
		// never erase the only recoverable copy while leaving credentials pending.
		// Any root-file cleanup failure is retried by startup sanitation.
		if state.credentialsCopied == 0 {
			paths := []string{bootstrap.CredentialsPath(s.dataDir)}
			const legacyPath = "/home/fluxo/.fluxo_credentials"
			if s.migrateLegacyCredentials && filepath.Clean(paths[0]) != legacyPath {
				paths = append(paths, legacyPath)
			}
			for _, path := range paths {
				if err := scrubBootstrapCredentialsPath(path); err != nil {
					log.Printf("Credential acknowledgement committed; startup will retry cleanup of %s: %v", path, err)
				}
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func readCredentialFile(path string) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("credentials path is not a regular file")
	}
	if info.Size() > 64*1024 {
		return nil, fmt.Errorf("credentials file exceeds the 64 KiB safety limit")
	}
	return io.ReadAll(io.LimitReader(f, 64*1024))
}

func rewriteCredentialFile(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".fluxo-credentials-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(contents); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func scrubCredentialPath(path string, secretPrefixes []string) error {
	contents, err := readCredentialFile(path)
	if err != nil || contents == nil {
		return err
	}

	lines := strings.Split(string(contents), "\n")
	kept := lines[:0]
	removed := false
	for _, line := range lines {
		secret := false
		for _, prefix := range secretPrefixes {
			if strings.HasPrefix(line, prefix) {
				secret = true
				break
			}
		}
		if !secret {
			kept = append(kept, line)
		} else {
			removed = true
		}
	}
	if !removed {
		return nil
	}

	return rewriteCredentialFile(path, []byte(strings.Join(kept, "\n")))
}

func scrubBootstrapCredentialsPath(path string) error {
	return scrubCredentialPath(path, []string{
		"Fluxo bootstrap token",
		"Fluxo sudo password:",
		"MySQL fluxo user password:",
		"PostgreSQL fluxo user password:",
	})
}

func scrubBootstrapTokenFile(dataDir string) error {
	return scrubCredentialPath(bootstrap.CredentialsPath(dataDir), []string{"Fluxo bootstrap token"})
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
