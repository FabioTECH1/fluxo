package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"fluxo/internal/database"
	"fluxo/internal/services/ssh"
)

func isValidSSHKey(key string) bool {
	if strings.ContainsAny(key, "\r\n") {
		return false
	}
	prefixes := []string{
		"ssh-rsa",
		"ssh-ed25519",
		"ecdsa-sha2-nistp256",
		"ecdsa-sha2-nistp384",
		"ecdsa-sha2-nistp521",
	}
	key = strings.TrimSpace(key)
	for _, prefix := range prefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func (s *Server) handleListSSHKeys() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := database.DB.Query("SELECT id, name, public_key, created_at FROM ssh_keys ORDER BY created_at DESC")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		keys := make([]database.SSHKey, 0)
		for rows.Next() {
			var k database.SSHKey
			if err := rows.Scan(&k.ID, &k.Name, &k.PublicKey, &k.CreatedAt); err != nil {
				continue
			}
			keys = append(keys, k)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(keys)
	}
}

func (s *Server) handleCreateSSHKey() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name      string `json:"name"`
			PublicKey string `json:"public_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		req.Name = strings.TrimSpace(req.Name)
		req.PublicKey = strings.TrimSpace(req.PublicKey)

		if req.Name == "" || req.PublicKey == "" {
			http.Error(w, "Name and public key are required", http.StatusBadRequest)
			return
		}

		if !isValidSSHKey(req.PublicKey) {
			http.Error(w, "Invalid SSH key format", http.StatusBadRequest)
			return
		}

		if err := ssh.AddKey(req.PublicKey); err != nil {
			http.Error(w, "Failed to add SSH key to authorized_keys", http.StatusInternalServerError)
			return
		}

		res, err := database.DB.Exec("INSERT INTO ssh_keys (name, public_key) VALUES (?, ?)", req.Name, req.PublicKey)
		if err != nil {
			// Rollback key if db insertion fails (best effort)
			_ = ssh.RemoveKey(req.PublicKey)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		id, _ := res.LastInsertId()

		var k database.SSHKey
		database.DB.QueryRow("SELECT id, name, public_key, created_at FROM ssh_keys WHERE id = ?", id).Scan(&k.ID, &k.Name, &k.PublicKey, &k.CreatedAt)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(k)
	}
}

func (s *Server) handleDeleteSSHKey() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "Invalid ID", http.StatusBadRequest)
			return
		}

		var pubKey string
		if err := database.DB.QueryRow("SELECT public_key FROM ssh_keys WHERE id = ?", id).Scan(&pubKey); err != nil {
			http.Error(w, "Key not found", http.StatusNotFound)
			return
		}

		if err := ssh.RemoveKey(pubKey); err != nil {
			http.Error(w, "Failed to remove SSH key from authorized_keys", http.StatusInternalServerError)
			return
		}

		database.DB.Exec("DELETE FROM ssh_keys WHERE id = ?", id)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}
}
