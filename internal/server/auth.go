package server

import (
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"

	"fluxo/internal/database"

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

type loginAttempt struct {
	count     int
	lastError time.Time
}

var (
	loginAttempts = make(map[string]*loginAttempt)
	loginMutex    sync.Mutex
)

func getClientIP(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.Header.Get("X-Real-IP")
	}
	if ip == "" {
		ip, _, _ = net.SplitHostPort(r.RemoteAddr)
	}
	return ip
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
// It is hashed with bcrypt (SHA-256 for legacy installs, auto-upgraded).
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

		ip := getClientIP(r)

		loginMutex.Lock()
		attempt, ok := loginAttempts[ip]
		if ok {
			if time.Since(attempt.lastError) > 15*time.Minute {
				attempt.count = 0
			} else if attempt.count >= 5 {
				loginMutex.Unlock()
				http.Error(w, "Too many login attempts. Please try again later.", http.StatusTooManyRequests)
				return
			}
		} else {
			attempt = &loginAttempt{}
			loginAttempts[ip] = attempt
		}
		loginMutex.Unlock()

		bootstrapClaim := false
		var tokenHash string

		// Path 1: normal login — look up the user by username.
		err := database.DB.QueryRow("SELECT token_hash FROM users WHERE username = ?", req.Username).Scan(&tokenHash)
		if err != nil {
			// Path 2: bootstrap — look for the __bootstrap__ sentinel.
			err = database.DB.QueryRow("SELECT token_hash FROM users WHERE username = '__bootstrap__'").Scan(&tokenHash)
			if err != nil {
				loginMutex.Lock()
				attempt.count++
				attempt.lastError = time.Now()
				loginMutex.Unlock()
				LogActivityWithUser(0, "login_failed", "Failed login attempt for user \""+req.Username+"\"", req.Username, ip)
				http.Error(w, "Invalid credentials", http.StatusUnauthorized)
				return
			}
			bootstrapClaim = true
		}

		// Verify the password against the stored hash (bcrypt or legacy SHA-256).
		if !verifyPassword(req.Password, tokenHash) {
			loginMutex.Lock()
			attempt.count++
			attempt.lastError = time.Now()
			loginMutex.Unlock()
			LogActivityWithUser(0, "login_failed", "Failed login attempt for user \""+req.Username+"\"", req.Username, ip)
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
			return
		}

		// Auto-upgrade legacy SHA-256 hashes to bcrypt on successful login.
		if isLegacySHA256(tokenHash) {
			newHash, err := hashPassword(req.Password)
			if err == nil {
				if bootstrapClaim {
					database.DB.Exec("UPDATE users SET token_hash = ? WHERE username = '__bootstrap__'", newHash)
				} else {
					database.DB.Exec("UPDATE users SET token_hash = ? WHERE username = ?", newHash, req.Username)
				}
				tokenHash = newHash
			}
		}

		// On first-ever login, claim the bootstrap account with the user's
		// chosen username. After this, the bootstrap row is a normal user.
		if bootstrapClaim {
			_, err = database.DB.Exec("UPDATE users SET username = ? WHERE username = '__bootstrap__'", req.Username)
			if err != nil {
				http.Error(w, "Failed to claim account", http.StatusInternalServerError)
				return
			}
			LogActivityWithUser(0, "login_bootstrap", "Bootstrap account claimed as \""+req.Username+"\"", req.Username, ip)
		} else {
			LogActivityWithUser(0, "login", "Successful login for \""+req.Username+"\"", req.Username, ip)
		}

		// Issue JWT signed with the user's own token_hash.
		// 24-hour expiry; the frontend's apiClient auto-redirects to /login on 401.
		// Includes token_version to invalidate all tokens on password change.
		var tokenVersion int
		if bootstrapClaim {
			database.DB.QueryRow("SELECT token_version FROM users WHERE username = '__bootstrap__'").Scan(&tokenVersion)
		} else {
			database.DB.QueryRow("SELECT token_version FROM users WHERE username = ?", req.Username).Scan(&tokenVersion)
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": req.Username,
			"ver": tokenVersion,
			"exp": time.Now().Add(24 * time.Hour).Unix(),
		})

		tokenString, err := token.SignedString([]byte(tokenHash))
		if err != nil {
			http.Error(w, "Failed to generate token", http.StatusInternalServerError)
			return
		}

		loginMutex.Lock()
		delete(loginAttempts, ip)
		loginMutex.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(LoginResponse{Token: tokenString})
	}
}

func (s *Server) handleBootstrapStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var count int
		err := database.DB.QueryRow("SELECT COUNT(*) FROM users WHERE username = '__bootstrap__'").Scan(&count)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			json.NewEncoder(w).Encode(map[string]bool{"bootstrap": false})
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"bootstrap": count > 0})
	}
}
