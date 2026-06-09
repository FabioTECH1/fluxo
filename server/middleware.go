package server

import (
	"context"
	"net/http"
	"strings"

	"fluxo/database"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const userContextKey = contextKey("user")

// AuthMiddleware wraps an http.Handler with JWT Bearer token verification.
// It is the single authentication gate for all API routes.
//
// Bypass rules (no token required):
//   - POST /api/v1/auth/login   (login endpoint itself)
//   - GET  /api/v1/ws            (WebSocket — browsers can't set auth headers)
//   - Any path not under /api/   (SPA static assets + Vue routes)
//
// For protected routes, the flow is:
//  1. Extract Bearer token from Authorization header
//  2. Parse the JWT unverified to extract the "sub" (username) claim
//  3. Look up that user's token_hash from the database
//  4. Re-verify the JWT with the correct per-user HMAC secret
//  5. If valid, inject the username into the request context
//
// This per-user secret approach means each login rotates its own signing
// key, and a compromised token_hash invalidates only that user's sessions
// (not all users, as would happen with a single global secret).
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/ws" || !strings.HasPrefix(r.URL.Path, "/api/") {
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

		// Look up the per-user signing secret.
		var tokenHash string
		err = database.DB.QueryRow("SELECT token_hash FROM users WHERE username = ?", username).Scan(&tokenHash)
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

		ctx := context.WithValue(r.Context(), userContextKey, claims["sub"])
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
