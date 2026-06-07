package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"fluxo/database"
	"fluxo/services/git"
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

func (s *Server) handleGetSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var pat, email, defaultPhp, fluxoDbPass, fluxoSudoPass string
		err := database.DB.QueryRow("SELECT github_pat, admin_email, default_php, fluxo_db_password, fluxo_sudo_password FROM users ORDER BY id ASC LIMIT 1").Scan(&pat, &email, &defaultPhp, &fluxoDbPass, &fluxoSudoPass)
		if err != nil {
			pat = ""
			email = ""
			defaultPhp = "8.4"
			fluxoDbPass = ""
		}
		if defaultPhp == "" {
			defaultPhp = "8.4"
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"github_pat":         pat,
			"admin_email":        email,
			"default_php":        defaultPhp,
			"fluxo_db_password":  fluxoDbPass,
			"fluxo_sudo_password": fluxoSudoPass,
		})
	}
}

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
		_, err := database.DB.Exec("UPDATE users SET github_pat = ?, admin_email = ?, default_php = ? WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)", req.GitHubPAT, req.AdminEmail, req.DefaultPHP)
		if err != nil {
			http.Error(w, "Failed to update settings", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleGetGitHubRepos() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var pat string
		err := database.DB.QueryRow("SELECT github_pat FROM users ORDER BY id ASC LIMIT 1").Scan(&pat)
		if err != nil || pat == "" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("[]"))
			return
		}

		provider := git.NewGitHubProvider(pat)
		repos, err := provider.ListRepositories()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(repos)
	}
}

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
		err := database.DB.QueryRow("SELECT token_hash FROM users WHERE username = 'admin'").Scan(&tokenHash)
		if err != nil {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}

		// Verify current password
		currHash := sha256.Sum256([]byte(req.CurrentPassword))
		currHashStr := hex.EncodeToString(currHash[:])
		if currHashStr != tokenHash {
			http.Error(w, "Incorrect current password", http.StatusUnauthorized)
			return
		}

		// Hash and save new password
		newHash := sha256.Sum256([]byte(req.NewPassword))
		newHashStr := hex.EncodeToString(newHash[:])

		_, err = database.DB.Exec("UPDATE users SET token_hash = ? WHERE username = 'admin'", newHashStr)
		if err != nil {
			http.Error(w, "Failed to update password", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
