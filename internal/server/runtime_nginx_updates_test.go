package server

import (
	"context"
	"encoding/json"
	"errors"
	"fluxo/internal/services/nginxupdate"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNginxDiscoveryHTTPPersistenceAuthAndReadOnlyStatus(t *testing.T) {
	id := setupSiteVhostTest(t)
	s := NewServer(nil, t.TempDir(), false)
	count := 0
	s.nginxUpdates.Admit = func(context.Context) (func(), error) { return func() {}, nil }
	s.nginxUpdates.Refresh = func(context.Context) error { count++; return nil }
	s.nginxUpdates.Inspect = func(context.Context) (nginxupdate.Snapshot, error) {
		return nginxupdate.Snapshot{State: "update_available", Identity: "offered-package-source", Package: "nginx-core", InstalledVersion: "1:1.24.0-1", CandidateVersion: "1:1.24.0-2"}, nil
	}
	s.nginxUpdates.Now = func() time.Time { return time.Unix(100000, 0) }
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, vhostRequest(method, "/api/v1/server/nginx/updates"+path, id, body))
		return w
	}
	w := request("GET", "", nil)
	if w.Code != 200 || count != 0 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, vhostRequest("POST", "/api/v1/server/nginx/updates/check", id, map[string]any{}))
	if w.Code != 401 || count != 0 {
		t.Fatal("unauthenticated check", w.Code)
	}
	w = request("POST", "/check", map[string]any{"install": true})
	if w.Code != 400 || count != 0 {
		t.Fatal("accepted installation field", w.Code)
	}
	w = request("POST", "/check", map[string]any{})
	var v nginxupdate.Status
	json.Unmarshal(w.Body.Bytes(), &v)
	if w.Code != 200 || count != 1 || !v.Notice || v.RunningAdoption != "not_verified" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request("POST", "/notices/dismiss", map[string]any{"identity": "wrong"})
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
	w = request("POST", "/notices/dismiss", map[string]any{"identity": v.Snapshot.Identity})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	reopened := nginxupdate.New(nginxDiscoveryStore{})
	reopened.Now = s.nginxUpdates.Now
	stored, e := reopened.Status()
	if e != nil || stored.Notice || stored.Snapshot.Identity != v.Snapshot.Identity {
		t.Fatal(stored, e)
	}
	s.nginxUpdates.Refresh = func(context.Context) error { return errors.New("refresh failed") }
	w = request("POST", "/check", map[string]any{})
	json.Unmarshal(w.Body.Bytes(), &v)
	if w.Code != 200 || !v.Stale || v.Error == "" || v.LastSuccessfulCheck != 100000 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestNginxLegacyRestartRouteStillReloadsConfiguration(t *testing.T) {
	id := setupSiteVhostTest(t)
	s := NewServer(nil, t.TempDir(), false)
	old := reloadNginxConfiguration
	defer func() { reloadNginxConfiguration = old }()
	calls := 0
	reloadNginxConfiguration = func(context.Context) error { calls++; return nil }
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, vhostRequest("POST", "/api/v1/server/nginx/restart", id, nil))
	if w.Code != 204 || calls != 1 {
		t.Fatal(w.Code, calls)
	}
	reloadNginxConfiguration = func(context.Context) error { return errors.New("test config error") }
	w = httptest.NewRecorder()
	s.mux.ServeHTTP(w, vhostRequest("POST", "/api/v1/server/nginx/restart", id, nil))
	if w.Code != 500 {
		t.Fatal(w.Code)
	}
	if !strings.Contains(w.Body.String(), "Failed to reload Nginx") || strings.Contains(w.Body.String(), "restart Nginx") {
		t.Fatal(w.Body.String())
	}
}
