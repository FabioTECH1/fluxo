package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/database"
	"fluxo/internal/services/phpupdate"
)

func TestPHPUpdateEndpointsRequireAuthentication(t *testing.T) {
	s := NewServer(nil, t.TempDir(), false)
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/v1/server/php/updates/repository"}, {"POST", "/api/v1/server/php/updates/repository/review"}, {"POST", "/api/v1/server/php/updates/repository/apply"},
		{"GET", "/api/v1/server/php/updates"}, {"POST", "/api/v1/server/php/updates/check"}, {"POST", "/api/v1/server/php/updates/apply"}, {"GET", "/api/v1/server/php/updates/plans/00000000-0000-4000-8000-000000000001"}, {"GET", "/api/v1/server/php/updates/operations/00000000-0000-4000-8000-000000000001"}, {"POST", "/api/v1/server/php/updates/notices/dismiss"}, {"POST", "/api/v1/server/php/updates/operations/00000000-0000-4000-8000-000000000001/verify"},
		{"POST", "/api/v1/server/php/updates/operations/00000000-0000-4000-8000-000000000001/recovery-plan"}, {"POST", "/api/v1/server/php/updates/recovery/apply"},
	} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(route.method, route.path, nil))
		if w.Code != 401 {
			t.Fatalf("%s %s was public: %d", route.method, route.path, w.Code)
		}
	}
}

func TestPHPRecoveryRequestsRejectCommandsPathsAndMalformedIntent(t *testing.T) {
	s := NewServer(nil, t.TempDir(), false)
	for _, request := range []struct{ path, body string }{
		{"/api/v1/server/php/updates/repository/review", `{"request_id":"00000000-0000-4000-8000-000000000001","url":"https://evil.example"}`},
		{"/api/v1/server/php/updates/repository/review", `{"request_id":"invalid"}`},
		{"/api/v1/server/php/updates/repository/apply", `{"request_id":"00000000-0000-4000-8000-000000000001","plan_id":"invalid"}`},
		{"/api/v1/server/php/updates/repository/apply", `{"request_id":"00000000-0000-4000-8000-000000000001","plan_id":"00000000-0000-4000-8000-000000000002","key":"forged"}`},
		{"/api/v1/server/php/updates/plans/00000000-0000-4000-8000-000000000001/review", `{"reviewed":true}`},
		{"/api/v1/server/php/updates/plans/00000000-0000-4000-8000-000000000001/review", `{"command":"systemctl stop malicious"}`},
		{"/api/v1/server/php/updates/operations/00000000-0000-4000-8000-000000000001/recovery-plan", `{"kind":"repair","command":"dpkg --configure -a"}`},
		{"/api/v1/server/php/updates/operations/00000000-0000-4000-8000-000000000001/recovery-plan", `{"kind":"automatic"}`},
		{"/api/v1/server/php/updates/recovery/apply", `{"plan_id":"invalid","request_id":"invalid"}`},
		{"/api/v1/server/php/updates/recovery/apply", `{"plan_id":"00000000-0000-4000-8000-000000000001","request_id":"00000000-0000-4000-8000-000000000002","directory":"/tmp/packages"}`},
	} {
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, httptest.NewRequest("POST", request.path, strings.NewReader(request.body)))
		if w.Code != 400 {
			t.Fatalf("invalid recovery intent was accepted: %d %s", w.Code, w.Body.String())
		}
	}
}
func TestPHPUpdateGETNeverLaunchesCheck(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	defer database.DB.Close()
	s := NewServer(nil, t.TempDir(), false)
	s.phpUpdates.Launch = func(context.Context, phpupdate.Operation) error {
		t.Fatal("GET launched package maintenance")
		return nil
	}
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/server/php/updates", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"branches":[]`) {
		t.Fatalf("cached GET: %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cached maintenance metadata was browser-cacheable")
	}
}
func TestPHPUpdateStrictRequestValidation(t *testing.T) {
	s := NewServer(nil, t.TempDir(), false)
	for _, body := range []string{`{"version":"8.4","command":"apt upgrade"}`, `{"version":"8.4"} {}`, `{"version":"8.4;id"}`, `{`, strings.Repeat("x", 5000)} {
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/server/php/updates/check", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("invalid request accepted: %d %s", w.Code, w.Body.String())
		}
	}
}
func TestPHPMaintenanceGateBlocksMutationButKeepsReads(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	defer database.DB.Close()
	_, err := database.DB.Exec("INSERT INTO php_update_operations(id,kind,version,created_at,updated_at) VALUES ('operation','apply','8.4',1,1)")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	called := false
	handler := s.phpMaintenanceGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) }))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/sites/1/commands", nil))
	if called || w.Code != 409 {
		t.Fatalf("mutation escaped gate: %v %d", called, w.Code)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/sites/1", nil))
	if !called || w.Code != 204 {
		t.Fatal("maintenance blocked reads")
	}
}

func TestRuntimeMutationLeaseErrorsDistinguishContentionFromUnavailable(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{fmt.Errorf("wait: %w", context.DeadlineExceeded), 409, "maintenance_busy"},
		{context.Canceled, 409, "maintenance_busy"},
		{errors.New("unsafe maintenance lock ownership or mode"), 503, "maintenance_unavailable"},
		{os.ErrPermission, 503, "maintenance_unavailable"},
	} {
		w := httptest.NewRecorder()
		runtimeMutationLeaseError(w, tc.err)
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if w.Code != tc.status || body["code"] != tc.code || strings.Contains(body["message"], "PHP maintenance operation is in progress") {
			t.Fatalf("misclassified lock failure: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestCompletedRuntimeMaintenancePermitsAllLaravelFeatureMutations(t *testing.T) {
	oldDB := database.DB
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	defer func() { database.DB.Close(); database.DB = oldDB }()
	for _, path := range []string{"scheduler", "nightwatch", "queue-worker", "horizon", "octane", "maintenance"} {
		t.Run(path, func(t *testing.T) {
			if _, err := database.DB.Exec("INSERT INTO node_update_operations(id,kind,status,updated_at,payload) VALUES('completed','migrate','succeeded',1,'{}')"); err != nil {
				t.Fatal(err)
			}
			defer database.DB.Exec("DELETE FROM node_update_operations WHERE id='completed'")
			called := false
			h := (&Server{}).phpMaintenanceGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) }))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/sites/1/features/"+path+"/enable", nil))
			if !called || w.Code != 204 {
				t.Fatalf("completed maintenance blocked feature: %d %s", w.Code, w.Body.String())
			}
			if _, err := database.DB.Exec("UPDATE node_update_operations SET status='running' WHERE id='completed'"); err != nil {
				t.Fatal(err)
			}
			called = false
			w = httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/sites/1/features/"+path+"/enable", nil))
			if called || w.Code != 409 {
				t.Fatalf("active maintenance admitted feature: %d", w.Code)
			}
		})
	}
}

func TestSchedulerRenewalOnlyPrecedesConfigurationMutations(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		want         bool
	}{
		{"POST", "/api/v1/sites/1/features/scheduler/enable", true},
		{"POST", "/api/v1/sites/1/features/scheduler/enable/", true},
		{"POST", "/api/v1/sites/1/crons", true},
		{"POST", "/api/v1/crons", true},
		{"PUT", "/api/v1/sites/1/crons/2", true},
		{"GET", "/api/v1/sites/1/features", false},
		{"POST", "/api/v1/sites/1/features/scheduler/disable", false},
		{"DELETE", "/api/v1/sites/1/crons/2", false},
		{"POST", "/api/v1/sites/1/crons/2/run", false},
		{"POST", "/api/v1/sites/1/features/nightwatch/enable", false},
		{"POST", "/api/v1/sites/invalid/crons", false},
		{"POST", "/api/v1/unknown/crons", false},
	} {
		if got := schedulerConfigurationRequest(httptest.NewRequest(tc.method, tc.path, nil)); got != tc.want {
			t.Fatalf("wrong renewal routing %s %s", tc.method, tc.path)
		}
	}
}

func TestSchedulerPolicyRenewalPrecedesHandlerAndPreservesRefusal(t *testing.T) {
	old := database.DB
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	defer func() { database.DB.Close(); database.DB = old }()
	renews, calls := 0, 0
	fail := true
	s := &Server{renewSchedulerPolicy: func(ctx context.Context) error {
		renews++
		if fail {
			return errors.New("unsupported policy remains refused")
		}
		return nil
	}}
	h := s.phpMaintenanceGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(201) }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/sites/1/features/scheduler/enable", nil))
	if calls != 0 || renews != 1 || w.Code != 409 || !strings.Contains(w.Body.String(), "unsupported policy") {
		t.Fatal("renewal refusal lost", calls, renews, w.Code, w.Body.String())
	}
	fail = false
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/sites/1/features/scheduler/enable", nil))
	if calls != 1 || renews != 2 || w.Code != 201 {
		t.Fatal("configuration not admitted after successful renewal", calls, renews, w.Code)
	}
}
