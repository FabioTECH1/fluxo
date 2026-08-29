package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"fluxo/internal/database"
	"fluxo/internal/services/ssh"
)

// isValidSSHKey checks whether a string is a supported SSH public key type.
func isValidSSHKey(key string) bool {
	return ssh.ValidatePublicKey(key) == nil
}

// handleListSSHKeys returns all stored SSH public keys.
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

// handleCreateSSHKey validates and installs an SSH public key, then stores it in DB.
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
			status := http.StatusInternalServerError
			if strings.Contains(err.Error(), "already installed") {
				status = http.StatusConflict
			}
			http.Error(w, err.Error(), status)
			return
		}

		res, err := database.DB.Exec("INSERT INTO ssh_keys (name, public_key) VALUES (?, ?)", req.Name, req.PublicKey)
		if err != nil {
			// Roll back only when doing so cannot remove the final key under a
			// concurrently activated key-only policy.
			_ = ssh.RemoveKeySafely(r.Context(), req.PublicKey)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		id, _ := res.LastInsertId()

		var k database.SSHKey
		database.DB.QueryRow("SELECT id, name, public_key, created_at FROM ssh_keys WHERE id = ?", id).Scan(&k.ID, &k.Name, &k.PublicKey, &k.CreatedAt)

		LogActivityWithUser(0, "ssh_key_added", "SSH key "+req.Name+" was added for the fluxo user", usernameFromContext(r.Context()), getClientIP(r))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(k)
	}
}

// handleDeleteSSHKey removes a key from authorized_keys and deletes the DB record.
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
		if err := ssh.RemoveKeySafely(r.Context(), pubKey); err != nil {
			switch {
			case errors.Is(err, ssh.ErrSSHPolicyUnavailable):
				http.Error(w, "Unable to verify the effective SSH access policy; the key was not removed", http.StatusServiceUnavailable)
			case errors.Is(err, ssh.ErrFinalAuthorizedKey):
				http.Error(w, "Cannot remove the final SSH key while password login is disabled. Restore the server SSH policy or add and test another key first.", http.StatusConflict)
			default:
				http.Error(w, "Failed to safely remove SSH key from authorized_keys", http.StatusInternalServerError)
			}
			return
		}

		result, err := database.DB.Exec("DELETE FROM ssh_keys WHERE id = ?", id)
		if err != nil {
			_ = ssh.AddKey(pubKey)
			http.Error(w, "Failed to delete SSH key record", http.StatusInternalServerError)
			return
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			_ = ssh.AddKey(pubKey)
			http.Error(w, "SSH key record changed while it was being removed", http.StatusConflict)
			return
		}

		LogActivityWithUser(0, "ssh_key_deleted", "An SSH key was removed from the fluxo user", usernameFromContext(r.Context()), getClientIP(r))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}
}

// handleGetSSHSecurity reports effective sshd settings rather than assuming a
// managed configuration file is active.
func (s *Server) handleGetSSHSecurity() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ssh.GetSecurityStatus(r.Context()))
	}
}

// handleEnableSSHHardening requires explicit proof-of-access acknowledgements
// before activating Fluxo's validated key-only SSH policy.
func (s *Server) handleEnableSSHHardening() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			KeyAccessConfirmed bool `json:"key_access_confirmed"`
			RecoveryConfirmed  bool `json:"recovery_access_confirmed"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		if !request.KeyAccessConfirmed || !request.RecoveryConfirmed {
			http.Error(w, "Confirm tested key access and provider-console recovery access before disabling password login", http.StatusBadRequest)
			return
		}
		status, err := ssh.EnableHardening(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		LogActivityWithUser(0, "ssh_hardened", "SSH password login was disabled after staged validation", usernameFromContext(r.Context()), getClientIP(r))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	}
}

// handleDisableSSHHardening removes only Fluxo's managed drop-in. An external
// provider policy may continue to require public keys.
func (s *Server) handleDisableSSHHardening() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, err := ssh.DisableManagedHardening(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		LogActivityWithUser(0, "ssh_policy_restored", "Fluxo SSH hardening was removed and the underlying server policy restored", usernameFromContext(r.Context()), getClientIP(r))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	}
}
