package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"fluxo/database"

	"github.com/golang-jwt/jwt/v5"
)

// LoginRequest is the JSON body for POST /api/v1/auth/login.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse returns the JWT token on successful authentication.
type LoginResponse struct {
	Token string `json:"token"`
}

// handleLogin authenticates a user and returns a JWT signed with the
// user's own token_hash as the HMAC secret (per-user signing keys).
//
// Two login paths:
//  1. Normal login: username exists in users table → verify password hash
//     against stored token_hash → issue JWT.
//  2. Bootstrap (first-run) login: username doesn't exist, but a
//     "__bootstrap__" sentinel row exists → verify password hash → rename
//     the row to the user's chosen username → issue JWT.
//
// The password is the admin token (printed to stdout on first start).
// It is SHA-256 hashed before comparison; only the hash is stored.
func (s *Server) handleLogin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		if req.Username == "" || req.Password == "" {
			http.Error(w, "Username and password are required", http.StatusBadRequest)
			return
		}

		// Hash the provided password once for both paths.
		passwordHash := sha256.Sum256([]byte(req.Password))
		passwordHashStr := hex.EncodeToString(passwordHash[:])

		bootstrapClaim := false
		var tokenHash string

		// Path 1: normal login — look up the user by username.
		err := database.DB.QueryRow("SELECT token_hash FROM users WHERE username = ?", req.Username).Scan(&tokenHash)
		if err != nil {
			// Path 2: bootstrap — look for the __bootstrap__ sentinel.
			err = database.DB.QueryRow("SELECT token_hash FROM users WHERE username = '__bootstrap__'").Scan(&tokenHash)
			if err != nil {
				http.Error(w, "Invalid credentials", http.StatusUnauthorized)
				return
			}
			bootstrapClaim = true
		}

		// Verify the password hash against the stored token_hash.
		if passwordHashStr != tokenHash {
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
			return
		}

		// On first-ever login, claim the bootstrap account with the user's
		// chosen username. After this, the bootstrap row is a normal user.
		if bootstrapClaim {
			_, err = database.DB.Exec("UPDATE users SET username = ? WHERE username = '__bootstrap__'", req.Username)
			if err != nil {
				http.Error(w, "Failed to claim account", http.StatusInternalServerError)
				return
			}
		}

		// Issue JWT signed with the user's own token_hash.
		// 24-hour expiry; the frontend's apiClient auto-redirects to /login on 401.
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": req.Username,
			"exp": time.Now().Add(24 * time.Hour).Unix(),
		})

		tokenString, err := token.SignedString([]byte(tokenHash))
		if err != nil {
			http.Error(w, "Failed to generate token", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(LoginResponse{Token: tokenString})
	}
}
