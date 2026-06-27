package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"fluxo/internal/config"
	"fluxo/internal/database"
	"fluxo/internal/services/git"
)

type ConnectGitHubAccountRequest struct {
	Name  string `json:"name"`
	Token string `json:"token"`
}

// handleListGitHubAccounts returns all connected GitHub accounts.
func (s *Server) handleListGitHubAccounts() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := database.DB.Query("SELECT id, name, username, created_at, updated_at FROM github_accounts ORDER BY name ASC")
		if err != nil {
			http.Error(w, "Database error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		accounts := []database.GitHubAccount{}
		for rows.Next() {
			var acc database.GitHubAccount
			var createdAt, updatedAt time.Time
			if err := rows.Scan(&acc.ID, &acc.Name, &acc.Username, &createdAt, &updatedAt); err != nil {
				continue
			}
			acc.CreatedAt = createdAt
			acc.UpdatedAt = updatedAt
			accounts = append(accounts, acc)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(accounts)
	}
}

// handleConnectGitHubAccount validates a token, fetches its username if name is empty, and saves it.
func (s *Server) handleConnectGitHubAccount() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ConnectGitHubAccountRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		if req.Token == "" {
			http.Error(w, "Token is required", http.StatusBadRequest)
			return
		}

		// 1. Prevent duplicate token connections
		rows, err := database.DB.Query("SELECT token FROM github_accounts")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var encToken string
				if rows.Scan(&encToken) == nil {
					decToken := config.Decrypt(encToken)
					if decToken == req.Token {
						http.Error(w, "This GitHub token is already connected.", http.StatusConflict)
						return
					}
				}
			}
		}

		provider := git.NewGitHubProvider(req.Token)
		username, err := provider.GetAuthenticatedUsername()
		if err != nil {
			http.Error(w, "Invalid GitHub token: "+err.Error(), http.StatusBadRequest)
			return
		}

		// 2. Prevent duplicate account (username) connections
		var exists bool
		err = database.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM github_accounts WHERE username = ?)", username).Scan(&exists)
		if err == nil && exists {
			http.Error(w, fmt.Sprintf("The GitHub account '%s' is already connected.", username), http.StatusConflict)
			return
		}

		name := req.Name
		if name == "" {
			name = username
		}

		encryptedToken := config.Encrypt(req.Token)

		res, err := database.DB.Exec(
			"INSERT INTO github_accounts (name, username, token, created_at, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)",
			name, username, encryptedToken,
		)
		if err != nil {
			http.Error(w, "Failed to save account: "+err.Error(), http.StatusInternalServerError)
			return
		}

		id, _ := res.LastInsertId()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":   id,
			"name": name,
			"username": username,
		})
	}
}

// handleDeleteGitHubAccount removes a GitHub account, orphans its sites, and clears the cache.
func (s *Server) handleDeleteGitHubAccount() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "Invalid ID", http.StatusBadRequest)
			return
		}

		// 1. Delete from github_accounts
		res, err := database.DB.Exec("DELETE FROM github_accounts WHERE id = ?", id)
		if err != nil {
			http.Error(w, "Failed to delete account", http.StatusInternalServerError)
			return
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			http.Error(w, "Account not found", http.StatusNotFound)
			return
		}

		// 2. Clear caches associated with this account ID
		database.DB.Exec("DELETE FROM github_cache WHERE key LIKE ?", fmt.Sprintf("repos:account:%d%%", id))
		database.DB.Exec("DELETE FROM github_cache WHERE key LIKE ?", fmt.Sprintf("branches:%%:account:%d%%", id))

		// 3. Orphan affected sites
		database.DB.Exec("UPDATE sites SET github_account_id = 0 WHERE github_account_id = ?", id)

		w.WriteHeader(http.StatusNoContent)
	}
}
