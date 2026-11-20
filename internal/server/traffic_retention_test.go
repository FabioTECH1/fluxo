package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/services/traffic"
)

func TestTrafficRetentionFreshAndLegacyDefault(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint(legacy), func(t *testing.T) {
			m := trafficTestControl(t)
			if legacy {
				if err := os.Mkdir(filepath.Dir(m.path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(m.path, []byte(`{"format":1,"master_enabled":false,"sites":{}}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			state, err := m.Status(context.Background())
			if err != nil || state.RetentionDays != 30 || state.MasterEnabled {
				t.Fatalf("default %+v %v", state, err)
			}
		})
	}
}

func TestTrafficRetentionAPIValidationAndPersistence(t *testing.T) {
	m := trafficTestControl(t)
	s := &Server{traffic: m}
	put := func(raw string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPut, "/api/v1/traffic-analytics", strings.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handleTrafficAnalytics()(w, r)
		return w
	}
	for _, raw := range []string{`{}`, `null`, `[]`, `{"retention_days":null}`, `{"retention_days":"60"}`, `{"retention_days":true}`, `{"retention_days":60.0}`, `{"retention_days":6e1}`, `{"retention_days":-1}`, `{"retention_days":31}`, `{"retention_days":366}`, `{"retention_days":999999999999999999999}`, `{"retention_days":60,"enabled":false}`, `{"retention_days":60,"extra":1}`, `{"retention_days":60,"retention_days":0}`, `{"retention_days":60} {}`, `{"retention_days":`, `{"enabled":null}`} {
		w := put(raw)
		if w.Code != 400 {
			t.Fatalf("invalid %s status %d %s", raw, w.Code, w.Body.String())
		}
		state, err := m.Status(context.Background())
		if err != nil || state.RetentionDays != 30 {
			t.Fatal("invalid input changed policy", err)
		}
	}
	for _, days := range []int{0, 60, 90, 180, 365, 30} {
		w := put(fmt.Sprintf(`{"retention_days":%d}`, days))
		var state traffic.State
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &state) != nil || state.RetentionDays != days || state.Pending || state.MasterEnabled {
			t.Fatalf("saved %d: %d %s", days, w.Code, w.Body.String())
		}
		restart := newTrafficControl(m.dataDir)
		restart.listSites = m.listSites
		saved, err := restart.Status(context.Background())
		if err != nil || saved.RetentionDays != days || saved.MasterEnabled {
			t.Fatalf("restart %+v %v", saved, err)
		}
		if _, err = restart.Report(context.Background(), 1, traffic.RangeToday); err != nil {
			t.Fatal(err)
		}
		_ = restart.store.Close()
	}
	if _, err := os.Stat(filepath.Join(m.dataDir, "traffic", "analytics.db")); !os.IsNotExist(err) {
		t.Fatal("retention-only changes created aggregate storage", err)
	}
	// A site cannot override the server policy.
	r := httptest.NewRequest(http.MethodPut, "/api/v1/sites/1/traffic-analytics", strings.NewReader(`{"retention_days":0}`))
	r.Header.Set("Content-Type", "application/json")
	if _, err := trafficToggle(r); err == nil {
		t.Fatal("site accepted a retention policy")
	}
}

func TestTrafficRetentionSaveFailureAndPendingChange(t *testing.T) {
	m := trafficTestControl(t)
	ctx := context.Background()
	if _, err := m.Status(ctx); err != nil {
		t.Fatal(err)
	}
	original := m.path
	m.path = filepath.Join(m.dataDir, "non-directory", "control.json")
	if err := os.WriteFile(filepath.Dir(m.path), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.setRetentionDays(ctx, 0); err == nil {
		t.Fatal("failed save succeeded")
	}
	if m.state.RetentionDays != 30 {
		t.Fatal("failed save changed policy")
	}
	m.path = original
	m.state.Pending = &trafficTransition{Target: cloneTrafficSelection(m.state.trafficSelection), Phase: "applying"}
	if _, err := m.setRetentionDays(ctx, 60); err != errTrafficPending {
		t.Fatal("policy overlapped producer transition", err)
	}
}

func TestTrafficRetentionDiskRejectsMalformedPolicy(t *testing.T) {
	for _, value := range []string{"null", `"60"`, "60.0", "-1", "31"} {
		m := trafficTestControl(t)
		if err := os.Mkdir(filepath.Dir(m.path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(m.path, []byte(`{"format":1,"master_enabled":false,"sites":{},"retention_days":`+value+`}`), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := m.Status(context.Background()); err == nil {
			t.Fatal("accepted disk policy", value)
		}
	}
}
