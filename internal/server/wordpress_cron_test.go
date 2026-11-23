package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/database"
)

func wordpressServerFixture(t *testing.T) *Server {
	t.Helper()
	old := database.DB
	if err := database.InitBaseDB(filepath.Join(t.TempDir(), "base.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.DB.Close(); database.DB = old })
	if _, err := database.DB.Exec("INSERT INTO sites(id,domain,path,app_type,php_version,web_root,deployment_strategy) VALUES(1,'wp.test','/home/fluxo/wp.test','wordpress','8.4','/public','standard')"); err != nil {
		t.Fatal(err)
	}
	return NewServer(nil, t.TempDir(), false)
}

func TestWordPressCronRequiresAuthenticationAndGETHasNoEffects(t *testing.T) {
	s := wordpressServerFixture(t)
	for _, method := range []string{"GET", "PUT"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(method, "/api/v1/sites/1/wordpress-cron", strings.NewReader(`{"enabled":true}`)))
		if w.Code != 401 {
			t.Fatal("public mutation/read", method, w.Code)
		}
	}
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/sites/1/wordpress-cron", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"phase":"off"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	var count int
	database.DB.QueryRow("SELECT count(*) FROM crons").Scan(&count)
	if count != 0 {
		t.Fatal("GET created scheduling authority")
	}
}

func TestWordPressCronRejectsAmbiguousRequests(t *testing.T) {
	s := wordpressServerFixture(t)
	for _, body := range []string{`{}`, `{"enabled":"true"}`, `{"enabled":true,"command":"id"}`, `{"enabled":true} {}`, `{"enabled":null}`, strings.Repeat("x", 2000)} {
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, httptest.NewRequest("PUT", "/api/v1/sites/1/wordpress-cron", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatal(body, w.Code, w.Body.String())
		}
	}
}

func TestWordPressCronMutationKeepsBothRuntimeMaintenanceGates(t *testing.T) {
	for _, runtime := range []string{"php", "node"} {
		t.Run(runtime, func(t *testing.T) {
			s := wordpressServerFixture(t)
			query := "INSERT INTO php_update_operations(id,kind,version,created_at,updated_at) VALUES('operation','apply','8.4',1,1)"
			if runtime == "node" {
				query = "INSERT INTO node_update_operations(id,kind,status,updated_at,payload) VALUES('operation','apply','queued',1,'{}')"
			}
			if _, err := database.DB.Exec(query); err != nil {
				t.Fatal(err)
			}
			called := false
			guard := s.phpMaintenanceGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) }))
			w := httptest.NewRecorder()
			guard.ServeHTTP(w, httptest.NewRequest("PUT", "/api/v1/sites/1/wordpress-cron", strings.NewReader(`{"enabled":true}`)))
			if called || w.Code != 409 {
				t.Fatal("toggle bypassed maintenance", runtime, w.Code, w.Body.String())
			}
			w = httptest.NewRecorder()
			guard.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/sites/1/wordpress-cron", nil))
			if !called || w.Code != 204 {
				t.Fatal("read blocked by maintenance")
			}
		})
	}
}

func TestWordPressCronOwnsRemovalAndPHPBranchConfigMutations(t *testing.T) {
	s := wordpressServerFixture(t)
	if _, err := database.DB.Exec("INSERT INTO crons(id,site_id,expression,command,user) VALUES(7,1,'* * * * *','owned','fluxo'); INSERT INTO wordpress_cron_settings VALUES(1,7,'enabled','digest')"); err != nil {
		t.Fatal(err)
	}
	for _, request := range []struct{ method, path, body string }{
		{"DELETE", "/api/v1/crons/7", ""},
		{"DELETE", "/api/v1/sites/1", ""},
		{"PUT", "/api/v1/sites/1", `{"php_version":"8.5"}`},
		{"PUT", "/api/v1/sites/1", `{"web_root":"/other"}`},
		{"POST", "/api/v1/sites/1/wordpress-config", `{"content":"<?php // removed owned block"}`},
		{"POST", "/api/v1/sites/1/crons", `{"command":"wp cron event run --due-now","expression":"* * * * *","user":"fluxo"}`},
	} {
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, httptest.NewRequest(request.method, request.path, strings.NewReader(request.body)))
		if w.Code != 409 || !strings.Contains(w.Body.String(), "WordPress") {
			t.Fatal(request, w.Code, w.Body.String())
		}
	}
	var version string
	database.DB.QueryRow("SELECT php_version FROM sites WHERE id=1").Scan(&version)
	if version != "8.4" {
		t.Fatal("changed branch before guard")
	}
}

func TestWordPressCronRenewsPolicyBeforeMutationReservation(t *testing.T) {
	s := wordpressServerFixture(t)
	called := false
	s.renewSchedulerPolicy = func(context.Context) error { called = true; return nil }
	w := httptest.NewRecorder()
	s.phpMaintenanceGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, httptest.NewRequest("PUT", "/api/v1/sites/1/wordpress-cron", nil))
	if !called || w.Code != 204 {
		t.Fatal("missing existing policy coordination", called, w.Code)
	}
}
