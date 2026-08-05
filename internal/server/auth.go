package server

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/safeinput"

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
	loginAttempts         = make(map[string]*loginAttempt)
	loginMutex            sync.Mutex
	lastLoginAttemptSweep time.Time
)

const (
	loginAttemptWindow = 15 * time.Minute
	maxTrackedLoginIPs = 10000
)

func loginAttemptForIP(ip string, now time.Time) *loginAttempt {
	loginMutex.Lock()
	defer loginMutex.Unlock()
	if lastLoginAttemptSweep.IsZero() || now.Sub(lastLoginAttemptSweep) >= time.Minute {
		for trackedIP, attempt := range loginAttempts {
			if now.Sub(attempt.lastError) > loginAttemptWindow {
				delete(loginAttempts, trackedIP)
			}
		}
		lastLoginAttemptSweep = now
	}
	if attempt, ok := loginAttempts[ip]; ok {
		if now.Sub(attempt.lastError) > loginAttemptWindow {
			attempt.count = 0
			attempt.lastError = now
		}
		return attempt
	}
	if len(loginAttempts) >= maxTrackedLoginIPs {
		var oldestIP string
		var oldest time.Time
		for trackedIP, attempt := range loginAttempts {
			if oldestIP == "" || attempt.lastError.Before(oldest) {
				oldestIP = trackedIP
				oldest = attempt.lastError
			}
		}
		delete(loginAttempts, oldestIP)
	}
	attempt := &loginAttempt{lastError: now}
	loginAttempts[ip] = attempt
	return attempt
}

// getClientIP extracts the client IP from RemoteAddr.
func getClientIP(r *http.Request) string {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}

func recordLoginFailure(attempt *loginAttempt, ip, username string) {
	loginMutex.Lock()
	attempt.count++
	attempt.lastError = time.Now()
	loginMutex.Unlock()
	LogActivityWithUser(0, "login_failed", "Failed login attempt for user \""+username+"\"", username, ip)
	log.Printf("fluxo_auth_failed remote=%s username=%s", ip, strconv.Quote(username))
}

// handleLogin authenticates a user and returns a JWT (supports bootstrap first-run and normal login).
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
		if req.Username == "__bootstrap__" {
			http.Error(w, "Invalid username", http.StatusBadRequest)
			return
		}

		ip := getClientIP(r)

		attempt := loginAttemptForIP(ip, time.Now())
		loginMutex.Lock()
		blocked := attempt.count >= 5 && time.Since(attempt.lastError) <= loginAttemptWindow
		loginMutex.Unlock()
		if blocked {
			http.Error(w, "Too many login attempts. Please try again later.", http.StatusTooManyRequests)
			return
		}

		bootstrapClaim := false
		var tokenHash string

		// Path 1: normal login — look up the user by username.
		err := database.DB.QueryRow("SELECT token_hash FROM users WHERE username = ?", req.Username).Scan(&tokenHash)
		if err != nil {
			// Path 2: bootstrap — look for the __bootstrap__ sentinel.
			err = database.DB.QueryRow("SELECT token_hash FROM users WHERE username = '__bootstrap__'").Scan(&tokenHash)
			if err != nil {
				recordLoginFailure(attempt, ip, req.Username)
				http.Error(w, "Invalid credentials", http.StatusUnauthorized)
				return
			}
			bootstrapClaim = true
		}
		if bootstrapClaim && !safeinput.ValidateAdminUsername(req.Username) {
			http.Error(w, "Username must be 1-64 characters, cannot use the reserved bootstrap name, and cannot contain surrounding or control whitespace", http.StatusBadRequest)
			return
		}

		// Verify the password against the stored hash (bcrypt or legacy SHA-256).
		if !verifyPassword(req.Password, tokenHash) {
			recordLoginFailure(attempt, ip, req.Username)
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

		// On first-ever login, claim the bootstrap account with the user's chosen username.
		var bootstrapID int
		if bootstrapClaim {
			err = database.DB.QueryRow("SELECT id FROM users WHERE username = '__bootstrap__'").Scan(&bootstrapID)
			if err != nil {
				http.Error(w, "Failed to claim account", http.StatusInternalServerError)
				return
			}
			_, err = database.DB.Exec("UPDATE users SET username = ? WHERE id = ?", req.Username, bootstrapID)
			if err != nil {
				http.Error(w, "Failed to claim account", http.StatusInternalServerError)
				return
			}
		}

		// A reset token is needed only until its first successful login. Initial
		// bootstrap tokens remain until the one-time credentials download is acknowledged.
		var credentialsCopied int
		if err := database.DB.QueryRow("SELECT credentials_copied FROM users WHERE username = ?", req.Username).Scan(&credentialsCopied); err == nil && credentialsCopied != 0 {
			if err := scrubBootstrapTokenFile(s.dataDir); err != nil {
				http.Error(w, "Login succeeded but the consumed reset token could not be removed securely", http.StatusInternalServerError)
				return
			}
		}

		// Issue JWT signed with the user's own token_hash (24h expiry, includes token_version for invalidation).
		var tokenVersion int
		if bootstrapClaim {
			database.DB.QueryRow("SELECT token_version FROM users WHERE id = ?", bootstrapID).Scan(&tokenVersion)
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

// handleBootstrapStatus returns whether a bootstrap user still exists.
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
