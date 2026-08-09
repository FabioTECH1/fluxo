package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestLoginAttemptForIPPrunesExpiredEntries(t *testing.T) {
	loginMutex.Lock()
	previous := loginAttempts
	previousSweep := lastLoginAttemptSweep
	loginAttempts = map[string]*loginAttempt{
		"expired": {count: 5, lastError: time.Now().Add(-loginAttemptWindow - time.Second)},
	}
	lastLoginAttemptSweep = time.Time{}
	loginMutex.Unlock()
	t.Cleanup(func() {
		loginMutex.Lock()
		loginAttempts = previous
		lastLoginAttemptSweep = previousSweep
		loginMutex.Unlock()
	})

	attempt := loginAttemptForIP("new", time.Now())
	if attempt == nil {
		t.Fatal("loginAttemptForIP() returned nil")
	}
	loginMutex.Lock()
	_, expiredStillTracked := loginAttempts["expired"]
	loginMutex.Unlock()
	if expiredStillTracked {
		t.Fatal("expired login-attempt state was not pruned")
	}
}

func TestGetClientIPTrustsPanelProxyOnlyFromLoopback(t *testing.T) {
	proxied := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	proxied.RemoteAddr = "127.0.0.1:42000"
	proxied.Header.Set("X-Real-IP", "203.0.113.10")
	if got := getClientIP(proxied); got != "203.0.113.10" {
		t.Fatalf("proxied client IP = %q", got)
	}

	direct := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	direct.RemoteAddr = "198.51.100.5:42000"
	direct.Header.Set("X-Real-IP", "203.0.113.10")
	if got := getClientIP(direct); got != "198.51.100.5" {
		t.Fatalf("direct client spoofed its IP as %q", got)
	}
}

func reservedBootstrapJWT(t *testing.T) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "__bootstrap__"}).SignedString([]byte("test-key"))
	if err != nil {
		t.Fatalf("sign reserved bootstrap token: %v", err)
	}
	return token
}

func TestLoginRejectsReservedBootstrapUsername(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{
		"username": "__bootstrap__",
		"password": "secret"
	}`))
	recorder := httptest.NewRecorder()

	(&Server{}).handleLogin().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("login status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestAuthMiddlewareRejectsReservedBootstrapSubject(t *testing.T) {
	called := false
	handler := AuthMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sites", nil)
	req.Header.Set("Authorization", "Bearer "+reservedBootstrapJWT(t))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("middleware status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if called {
		t.Fatal("middleware passed a reserved bootstrap subject to the protected handler")
	}
}

func TestWebSocketRejectsReservedBootstrapSubject(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ws?site_id=1&token="+reservedBootstrapJWT(t), nil)
	recorder := httptest.NewRecorder()

	(&Server{}).handleWebSocket().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("websocket status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}
