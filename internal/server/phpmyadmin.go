package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/phpmyadmin"
)

const (
	phpMyAdminCookieName = "fluxo_phpmyadmin"
	phpMyAdminAccessTTL  = 60 * time.Second
	phpMyAdminSessionTTL = 30 * time.Minute
	phpMyAdminMaxSession = 8 * time.Hour
)

type phpMyAdminGrant struct {
	Username  string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

type phpMyAdminAccessManager struct {
	mu       sync.Mutex
	pending  map[string]phpMyAdminGrant
	sessions map[string]phpMyAdminGrant
	proxy    *httputil.ReverseProxy
}

func newPHPMyAdminAccessManager() *phpMyAdminAccessManager {
	target, _ := url.Parse("http://127.0.0.1:9091")
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(request *http.Request) {
		originalHost := request.Host
		forwardedProto := "http"
		forwardedHTTPS := "off"
		if requestUsesHTTPS(request) {
			forwardedProto = "https"
			forwardedHTTPS = "on"
		}
		originalDirector(request)
		request.Host = originalHost
		request.Header.Set("X-Forwarded-Host", originalHost)
		request.Header.Set("X-Forwarded-Proto", forwardedProto)
		request.Header.Set("X-Forwarded-Https", forwardedHTTPS)
		request.Header.Set("X-Forwarded-Ssl", forwardedHTTPS)
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("phpMyAdmin proxy error: %v", err)
		http.Error(w, "phpMyAdmin is temporarily unavailable", http.StatusBadGateway)
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		cookies := response.Header.Values("Set-Cookie")
		if len(cookies) == 0 {
			return nil
		}
		response.Header.Del("Set-Cookie")
		for _, cookie := range cookies {
			if !strings.Contains(strings.ToLower(cookie), "samesite=") {
				cookie += "; SameSite=Strict"
			}
			response.Header.Add("Set-Cookie", cookie)
		}
		return nil
	}
	return &phpMyAdminAccessManager{
		pending:  make(map[string]phpMyAdminGrant),
		sessions: make(map[string]phpMyAdminGrant),
		proxy:    proxy,
	}
}

func (m *phpMyAdminAccessManager) create(username string) (string, error) {
	token, err := randomPHPMyAdminToken()
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked()
	m.pending[token] = phpMyAdminGrant{Username: username, ExpiresAt: time.Now().Add(phpMyAdminAccessTTL)}
	return token, nil
}

func (m *phpMyAdminAccessManager) consume(token string) (string, phpMyAdminGrant, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked()
	grant, ok := m.pending[token]
	if !ok {
		return "", phpMyAdminGrant{}, false
	}
	delete(m.pending, token)
	session, err := randomPHPMyAdminToken()
	if err != nil {
		return "", phpMyAdminGrant{}, false
	}
	now := time.Now()
	grant.IssuedAt = now
	grant.ExpiresAt = phpMyAdminSessionExpiry(now, now)
	m.sessions[session] = grant
	return session, grant, true
}

func (m *phpMyAdminAccessManager) touch(session string) (time.Time, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked()
	grant, ok := m.sessions[session]
	if !ok {
		return time.Time{}, false
	}

	now := time.Now()
	grant.ExpiresAt = phpMyAdminSessionExpiry(grant.IssuedAt, now)
	if !now.Before(grant.ExpiresAt) {
		delete(m.sessions, session)
		return time.Time{}, false
	}
	m.sessions[session] = grant
	return grant.ExpiresAt, true
}

func phpMyAdminSessionExpiry(issuedAt, now time.Time) time.Time {
	idleExpiry := now.Add(phpMyAdminSessionTTL)
	absoluteExpiry := issuedAt.Add(phpMyAdminMaxSession)
	if absoluteExpiry.Before(idleExpiry) {
		return absoluteExpiry
	}
	return idleExpiry
}

func (m *phpMyAdminAccessManager) clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending = make(map[string]phpMyAdminGrant)
	m.sessions = make(map[string]phpMyAdminGrant)
}

func (m *phpMyAdminAccessManager) cleanupLocked() {
	now := time.Now()
	for token, grant := range m.pending {
		if now.After(grant.ExpiresAt) {
			delete(m.pending, token)
		}
	}
	for token, grant := range m.sessions {
		if now.After(grant.ExpiresAt) {
			delete(m.sessions, token)
		}
	}
}

func randomPHPMyAdminToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

// requestUsesHTTPS recognizes direct TLS and the headers commonly set by
// Cloudflare, load balancers, and Nginx when TLS terminates upstream.
// The upstream proxy should overwrite client-supplied forwarding headers.
func requestUsesHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}

	// X-Forwarded-Proto can contain a comma-separated proxy chain. The first
	// value describes the browser-facing connection.
	forwardedProto := r.Header.Get("X-Forwarded-Proto")
	if first, _, _ := strings.Cut(forwardedProto, ","); strings.EqualFold(strings.TrimSpace(first), "https") {
		return true
	}

	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Ssl")), "on") ||
		strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Https")), "on")
}

func (s *Server) handleGetPHPMyAdminStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writePHPMyAdminJSON(w, http.StatusOK, phpmyadmin.GetStatus())
	}
}

func (s *Server) handleInstallPHPMyAdmin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		phpVersion := "8.4"
		_ = database.DB.QueryRow("SELECT default_php FROM users ORDER BY id ASC LIMIT 1").Scan(&phpVersion)
		if !phpVersionRegex.MatchString(phpVersion) {
			phpVersion = "8.4"
		}
		ctx, cancel := contextWithPHPMyAdminTimeout(r)
		defer cancel()
		if err := phpmyadmin.Install(ctx, phpVersion); err != nil {
			writePHPMyAdminError(w, err)
			return
		}
		LogActivityWithUser(0, "phpmyadmin_installed", "phpMyAdmin "+phpmyadmin.Version+" was installed", usernameFromContext(r.Context()), getClientIP(r))
		writePHPMyAdminJSON(w, http.StatusOK, phpmyadmin.GetStatus())
	}
}

func (s *Server) handleEnablePHPMyAdmin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := contextWithPHPMyAdminTimeout(r)
		defer cancel()
		if err := phpmyadmin.Enable(ctx); err != nil {
			writePHPMyAdminError(w, err)
			return
		}
		LogActivityWithUser(0, "phpmyadmin_enabled", "phpMyAdmin access was enabled", usernameFromContext(r.Context()), getClientIP(r))
		writePHPMyAdminJSON(w, http.StatusOK, phpmyadmin.GetStatus())
	}
}

func (s *Server) handleDisablePHPMyAdmin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := contextWithPHPMyAdminTimeout(r)
		defer cancel()
		if err := phpmyadmin.Disable(ctx); err != nil {
			writePHPMyAdminError(w, err)
			return
		}
		s.phpMyAdminAccess.clear()
		LogActivityWithUser(0, "phpmyadmin_disabled", "phpMyAdmin access was disabled", usernameFromContext(r.Context()), getClientIP(r))
		writePHPMyAdminJSON(w, http.StatusOK, phpmyadmin.GetStatus())
	}
}

func (s *Server) handleRemovePHPMyAdmin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := contextWithPHPMyAdminTimeout(r)
		defer cancel()
		if err := phpmyadmin.Remove(ctx); err != nil {
			writePHPMyAdminError(w, err)
			return
		}
		s.phpMyAdminAccess.clear()
		LogActivityWithUser(0, "phpmyadmin_removed", "phpMyAdmin was uninstalled", usernameFromContext(r.Context()), getClientIP(r))
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleCreatePHPMyAdminAccess() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := phpmyadmin.GetStatus()
		if !status.Installed || !status.Enabled {
			http.Error(w, "phpMyAdmin is not enabled", http.StatusConflict)
			return
		}
		token, err := s.phpMyAdminAccess.create(usernameFromContext(r.Context()))
		if err != nil {
			http.Error(w, "Failed to create phpMyAdmin access session", http.StatusInternalServerError)
			return
		}
		writePHPMyAdminJSON(w, http.StatusOK, map[string]string{"url": "/phpmyadmin/access/" + token})
	}
}

func (s *Server) handleConsumePHPMyAdminAccess() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !phpmyadmin.GetStatus().Enabled {
			http.Error(w, "phpMyAdmin is not enabled", http.StatusNotFound)
			return
		}
		session, grant, ok := s.phpMyAdminAccess.consume(r.PathValue("token"))
		if !ok {
			http.Error(w, "This phpMyAdmin access link is invalid or has expired", http.StatusUnauthorized)
			return
		}
		setPHPMyAdminSessionCookie(w, r, session, grant.ExpiresAt)
		http.Redirect(w, r, "/phpmyadmin/", http.StatusSeeOther)
	}
}

func (s *Server) handlePHPMyAdminProxy() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := phpmyadmin.GetStatus()
		if !status.Installed || !status.Enabled {
			http.Error(w, "phpMyAdmin is not enabled", http.StatusNotFound)
			return
		}
		cookie, err := r.Cookie(phpMyAdminCookieName)
		if err != nil {
			http.Error(w, "Your phpMyAdmin access session has expired. Open it again from Fluxo.", http.StatusUnauthorized)
			return
		}
		expiresAt, ok := s.phpMyAdminAccess.touch(cookie.Value)
		if !ok {
			http.Error(w, "Your phpMyAdmin access session has expired. Open it again from Fluxo.", http.StatusUnauthorized)
			return
		}
		setPHPMyAdminSessionCookie(w, r, cookie.Value, expiresAt)
		s.phpMyAdminAccess.proxy.ServeHTTP(w, r)
	})
}

func setPHPMyAdminSessionCookie(w http.ResponseWriter, r *http.Request, value string, expiresAt time.Time) {
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(w, &http.Cookie{
		Name:     phpMyAdminCookieName,
		Value:    value,
		Path:     "/phpmyadmin/",
		Expires:  expiresAt,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   requestUsesHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func contextWithPHPMyAdminTimeout(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 6*time.Minute)
}

func writePHPMyAdminError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, phpmyadmin.ErrBusy) {
		status = http.StatusConflict
	}
	message := err.Error()
	if strings.Contains(message, "already installed") || strings.Contains(message, "must be installed") || strings.Contains(message, "is not installed") {
		status = http.StatusConflict
	}
	http.Error(w, message, status)
}

func writePHPMyAdminJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
