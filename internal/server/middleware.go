package server

import (
	"context"
	"net/http"
	"strings"

	"fluxo/internal/database"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const userContextKey = contextKey("user")

// usernameFromContext extracts the authenticated username from the request context.
func usernameFromContext(ctx context.Context) string {
	if u, ok := ctx.Value(userContextKey).(string); ok {
		return u
	}
	return ""
}

// AuthMiddleware verifies JWT Bearer tokens using per-user HMAC secrets
// and injects the username into the request context for downstream handlers.
// Bypasses auth for login, bootstrap, WebSocket, webhook, health, version, and non-API paths.
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/auth/bootstrap" || r.URL.Path == "/api/v1/ws" || r.URL.Path == "/api/v1/github/webhook" || r.URL.Path == "/api/v1/health" || r.URL.Path == "/api/v1/version" || !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		tokenString := parts[1]

		// Parse without verification to extract the username claim.
		// This is safe because the real verification happens below —
		// a tampered "sub" will fail against the wrong user's secret.
		parser := jwt.NewParser()
		unverified, _, err := parser.ParseUnverified(tokenString, jwt.MapClaims{})
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		unverifiedClaims, ok := unverified.Claims.(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		username, ok := unverifiedClaims["sub"].(string)
		if !ok || username == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Look up the per-user signing secret and token version.
		var tokenHash string
		var tokenVersion int
		err = database.DB.QueryRow("SELECT token_hash, token_version FROM users WHERE username = ?", username).Scan(&tokenHash, &tokenVersion)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Verify the JWT with the correct user-specific HMAC key.
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, http.ErrAbortHandler
			}
			return []byte(tokenHash), nil
		})

		if err != nil || !token.Valid {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Reject tokens issued before a password change.
		if ver, ok := claims["ver"].(float64); ok {
			if int(ver) != tokenVersion {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}

		ctx := context.WithValue(r.Context(), userContextKey, claims["sub"])
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
