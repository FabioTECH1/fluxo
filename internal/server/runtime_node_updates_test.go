package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/database"
	"fluxo/internal/services/nodeupdate"
)

func TestNodeUpdateRoutesRequireAuthentication(t *testing.T) {
	s := NewServer(nil, t.TempDir(), false)
	for _, route := range []struct{ method, path string }{{"GET", ""}, {"POST", "/check"}, {"POST", "/apply"}, {"GET", "/operations"}, {"GET", "/operations/00000000-0000-4000-8000-000000000001"}, {"POST", "/plans/00000000-0000-4000-8000-000000000001/review"}, {"POST", "/operations/00000000-0000-4000-8000-000000000001/recovery-plan"}, {"POST", "/recovery/apply"}, {"POST", "/notices/dismiss"}} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(route.method, "/api/v1/server/node/updates"+route.path, nil))
		if w.Code != 401 {
			t.Fatalf("public route %s %s: %d", route.method, route.path, w.Code)
		}
	}
}
func TestNodeUpdateRequestsRejectExecutableOverridesAndMalformedIntent(t *testing.T) {
	s := NewServer(nil, t.TempDir(), false)
	for _, tc := range []struct{ path, body string }{{"/check", `{"request_id":"invalid"}`}, {"/check", `{"request_id":"00000000-0000-4000-8000-000000000001","url":"https://evil.example"}`}, {"/check", `{} {}`}, {"/apply", `{"plan_id":"invalid","request_id":"invalid"}`}, {"/recovery/apply", `{"plan_id":"00000000-0000-4000-8000-000000000001","request_id":"00000000-0000-4000-8000-000000000002","command":"id"}`}, {"/operations/00000000-0000-4000-8000-000000000001/recovery-plan", `{"kind":"automatic"}`}, {"/plans/00000000-0000-4000-8000-000000000001/review", `{"version":"26.0.0"}`}, {"/check", strings.Repeat("x", 5000)}} {
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/server/node/updates"+tc.path, strings.NewReader(tc.body)))
		if w.Code != 400 {
			t.Fatalf("invalid %s accepted %d %s", tc.path, w.Code, w.Body.String())
		}
	}
}
func TestNodeUpdateGETIsCacheOnly(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	defer database.DB.Close()
	s := NewServer(nil, t.TempDir(), false)
	s.nodeUpdates.Launch = func(context.Context, nodeupdate.Operation) error { t.Fatal("GET launched a worker"); return nil }
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/server/node/updates", nil))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"state":"unchecked"`) {
		t.Fatalf("cached status: %d %s", w.Code, w.Body.String())
	}
}
func TestNodeMaintenanceGatesConflictingRuntimeAndApplicationMutations(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	defer database.DB.Close()
	if _, err := database.DB.Exec(`INSERT INTO node_update_generations(id,head,state,payload) VALUES('generation','head','recovery_required','{}')`); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	for _, path := range []string{"/api/v1/sites/1/deploy", "/api/v1/sites/1/commands", "/api/v1/sites/1", "/api/v1/daemons/1/restart", "/api/v1/crons/1/run", "/api/v1/server/node/install", "/api/v1/server/python/install", "/api/v1/server/php/updates/apply", "/api/v1/settings"} {
		called := false
		h := s.phpMaintenanceGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", path, nil))
		if called || w.Code != 409 {
			t.Fatalf("mutation %s escaped pending generation: %v %d", path, called, w.Code)
		}
	}
}
