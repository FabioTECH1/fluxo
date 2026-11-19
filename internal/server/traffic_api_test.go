package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/services/nginx"
	"fluxo/internal/services/traffic"
)

func trafficAPIRequest(s *Server, method, id, body, media string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/v1/sites/"+id+"/traffic-analytics/api-settings", strings.NewReader(body))
	r.SetPathValue("id", id)
	if media != "" {
		r.Header.Set("Content-Type", media)
	}
	w := httptest.NewRecorder()
	s.handleTrafficAPISettings()(w, r)
	return w
}

func TestTrafficAPISettingsHTTPValidationAndIsolation(t *testing.T) {
	m := trafficTestControl(t)
	s := &Server{traffic: m}
	for _, tc := range []struct {
		name, body, media string
		code              int
	}{
		{"missing-media", `{}`, "", 415},
		{"wrong-media", `{}`, "text/plain", 415},
		{"missing-routes", `{"prefixes":["/api"]}`, "application/json", 400},
		{"null", `{"prefixes":null,"routes":[]}`, "application/json", 400},
		{"wrong-type", `{"prefixes":[true],"routes":[]}`, "application/json", 400},
		{"unknown", `{"prefixes":[],"routes":[],"enabled":true}`, "application/json", 400},
		{"trailing", `{"prefixes":[],"routes":[]} {}`, "application/json", 400},
		{"large", strings.Repeat(" ", maxTrafficAPISettingsBody+1), "application/json", 413},
		{"query", `{"prefixes":["/api?secret=1"],"routes":[]}`, "application/json", 422},
		{"regex", `{"prefixes":[],"routes":["/api/.*"]}`, "application/json", 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := trafficAPIRequest(s, "PUT", "1", tc.body, tc.media)
			if w.Code != tc.code || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("got %d %s", w.Code, w.Body.String())
			}
		})
	}
	for _, id := range []string{"0", "bad", "999"} {
		want := 400
		if id == "999" {
			want = 404
		}
		if w := trafficAPIRequest(s, "GET", id, "", ""); w.Code != want {
			t.Fatalf("site %s: %d", id, w.Code)
		}
	}
	if w := trafficAPIRequest(s, "GET", "1", "", ""); w.Code != 200 {
		t.Fatalf("default: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(m.dataDir, "traffic")); !os.IsNotExist(err) {
		t.Fatalf("default settings read created storage: %v", err)
	}
	w := trafficAPIRequest(s, "PUT", "1", `{"prefixes":["/services"],"routes":["/api/users/:id"]}`, "application/json; charset=utf-8")
	if w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	var one, two traffic.APISettingsState
	if err := json.Unmarshal(w.Body.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	w = trafficAPIRequest(s, "GET", "2", "", "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &two) != nil || len(two.Settings.Prefixes) != 1 || two.Settings.Prefixes[0] != "/api" || one.Settings.Prefixes[0] != "/services" {
		t.Fatalf("site settings crossed boundaries: %+v %+v %s", one, two, w.Body.String())
	}
	state, err := m.Status(context.Background())
	if err != nil || state.MasterEnabled || state.Sites[0].Enabled || state.Sites[0].EffectiveEnabled {
		t.Fatalf("settings enabled analytics: %+v %v", state, err)
	}
	if _, err := os.Stat(filepath.Join(m.dataDir, "traffic", "logs")); !os.IsNotExist(err) {
		t.Fatalf("settings created producer: %v", err)
	}
}

func TestTrafficAPISettingsPendingAndAuth(t *testing.T) {
	m := trafficTestControl(t)
	mustTrafficChange(t, m, 1, true)
	mustTrafficChange(t, m, 0, true)
	m.drained = func(context.Context, []nginx.TrafficWorker) (bool, error) { return false, nil }
	mustTrafficChange(t, m, 1, false)
	if m.state.Pending == nil {
		t.Fatal("expected pending transition")
	}
	s := &Server{traffic: m}
	w := trafficAPIRequest(s, "PUT", "1", `{"prefixes":["/services"],"routes":[]}`, "application/json")
	if w.Code != 409 {
		t.Fatalf("changed rules during transition: %d %s", w.Code, w.Body.String())
	}
	for _, method := range []string{"GET", "PUT"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/api/v1/sites/1/traffic-analytics/api-settings", nil)
		AuthMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("unauthenticated API settings reached handler") })).ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("%s without JWT: %d", method, w.Code)
		}
	}
}

func TestTrafficAPISettingsCleanupUsesAuthoritativeLivingSites(t *testing.T) {
	m := trafficTestControl(t)
	ctx := context.Background()
	one := traffic.APISettings{Prefixes: []string{"/one"}, Routes: []string{}}
	two := traffic.APISettings{Prefixes: []string{"/two"}, Routes: []string{}}
	if _, err := m.SetAPISettings(ctx, 1, one); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetAPISettings(ctx, 2, two); err != nil {
		t.Fatal(err)
	}
	m.listSites = func(context.Context) ([]trafficSiteInfo, error) {
		return nil, errors.New("site listing unavailable")
	}
	if _, err := m.SetAPISettings(ctx, 3, one); err == nil {
		t.Fatal("failed site listing authorized settings cleanup")
	}
	before, err := m.APISettings(ctx, 1)
	if err != nil || before.EffectiveFrom == nil || before.Settings.Prefixes[0] != "/one" {
		t.Fatalf("failed listing changed settings: %+v %v", before, err)
	}
	m.listSites = func(context.Context) ([]trafficSiteInfo, error) {
		return []trafficSiteInfo{{2, "two.test", "two.test"}, {3, "three.test", "three.test"}}, nil
	}
	if _, err := m.SetAPISettings(ctx, 3, one); err != nil {
		t.Fatal(err)
	}
	removed, err := m.APISettings(ctx, 1)
	if err != nil || removed.EffectiveFrom != nil || removed.Settings.Prefixes[0] != "/api" {
		t.Fatalf("deleted-site settings retained: %+v %v", removed, err)
	}
	living, err := m.APISettings(ctx, 2)
	if err != nil || living.EffectiveFrom == nil || living.Settings.Prefixes[0] != "/two" {
		t.Fatalf("disabled living-site settings removed: %+v %v", living, err)
	}
}
