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
	"sync/atomic"
	"testing"
	"time"

	"fluxo/internal/services/nginx"
	"fluxo/internal/services/traffic"
)

func trafficTestControl(t *testing.T) *trafficControl {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	m := newTrafficControl(root)
	m.listSites = func(context.Context) ([]trafficSiteInfo, error) {
		return []trafficSiteInfo{{1, "one.test", "one.test"}, {2, "two.test", "two.test"}}, nil
	}
	m.drained = func(context.Context, []nginx.TrafficWorker) (bool, error) { return true, nil }
	m.retired = func(context.Context, string) (bool, error) { return true, nil }
	m.apply = func(_ context.Context, sites []nginx.TrafficSite) (nginx.TrafficApplyResult, error) {
		for _, site := range sites {
			file, err := os.OpenFile(site.LogPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
			if err != nil {
				return nginx.TrafficApplyResult{}, err
			}
			file.Close()
		}
		return nginx.TrafficApplyResult{AppliedAt: time.Now().UTC()}, nil
	}
	t.Cleanup(func() {
		if m.store != nil && !m.started {
			_ = m.store.Close()
		}
	})
	return m
}

func mustTrafficChange(t *testing.T, m *trafficControl, id int64, on bool) traffic.State {
	t.Helper()
	state, err := m.change(context.Background(), id, on)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestTrafficDefaultOffStartAndReadCreateNoStorage(t *testing.T) {
	m := trafficTestControl(t)
	m.apply = func(context.Context, []nginx.TrafficSite) (nginx.TrafficApplyResult, error) {
		t.Error("OFF called Nginx")
		return nginx.TrafficApplyResult{}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.Start(ctx)
	state, err := m.Status(ctx)
	if err != nil || state.MasterEnabled || state.Sites[0].EffectiveEnabled {
		t.Fatalf("%+v %v", state, err)
	}
	report, err := m.Report(ctx, 1, traffic.RangeToday)
	if err != nil || report.Totals.Requests != 0 || len(report.Gaps) == 0 {
		t.Fatalf("%+v %v", report, err)
	}
	cancel()
	select {
	case <-m.done:
	case <-time.After(2 * time.Second):
		t.Fatal("collector did not stop")
	}
	if _, err = os.Lstat(filepath.Join(m.dataDir, "traffic")); !os.IsNotExist(err) {
		t.Fatalf("OFF created storage: %v", err)
	}
}

func TestTrafficToggleDrainsAndReenableUsesFreshGeneration(t *testing.T) {
	m := trafficTestControl(t)
	mustTrafficChange(t, m, 1, true)
	state := mustTrafficChange(t, m, 0, true)
	if !state.Sites[0].EffectiveEnabled || state.Pending {
		t.Fatalf("%+v", state)
	}
	first := *m.state.Sites[1].Source
	m.drained = func(context.Context, []nginx.TrafficWorker) (bool, error) { return false, nil }
	state = mustTrafficChange(t, m, 1, false)
	if !state.Pending || state.Sites[0].EffectiveEnabled || state.Sites[0].Enabled || state.Status != "draining" {
		t.Fatalf("%+v", state)
	}
	if _, err := m.change(context.Background(), 1, true); !errors.Is(err, errTrafficPending) {
		t.Fatalf("reenable during drain: %v", err)
	}
	if _, err := os.Stat(first.Path); err != nil {
		t.Fatal("old log removed before its writer drained")
	}
	m.drained = func(context.Context, []nginx.TrafficWorker) (bool, error) { return true, nil }
	if err := m.applyLocked(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first.Path); !os.IsNotExist(err) {
		t.Fatal("retired log was retained")
	}
	mustTrafficChange(t, m, 1, true)
	second := *m.state.Sites[1].Source
	if second.Generation == first.Generation || second.Path == first.Path || second.StartTime.Before(first.StartTime) {
		t.Fatal("disabled generation reused")
	}
}

func TestTrafficReloadFailurePreservesEffectiveSelection(t *testing.T) {
	m := trafficTestControl(t)
	mustTrafficChange(t, m, 1, true)
	mustTrafficChange(t, m, 0, true)
	before := *m.state.Sites[1].Source
	m.apply = func(context.Context, []nginx.TrafficSite) (nginx.TrafficApplyResult, error) {
		return nginx.TrafficApplyResult{}, errors.New("validation rejected")
	}
	state, err := m.change(context.Background(), 1, false)
	if err == nil || !state.Sites[0].Enabled || !state.Sites[0].EffectiveEnabled || state.Pending || m.state.Sites[1].Source.Generation != before.Generation {
		t.Fatalf("%+v %v", state, err)
	}
	restart := newTrafficControl(m.dataDir)
	restart.listSites = m.listSites
	state, err = restart.Status(context.Background())
	if err != nil || !state.Sites[0].EffectiveEnabled || !strings.Contains(state.Error, "validation rejected") {
		t.Fatalf("%+v %v", state, err)
	}
}

func TestTrafficActivatedCrashReplaysCollectorWithoutAnotherReload(t *testing.T) {
	m := trafficTestControl(t)
	mustTrafficChange(t, m, 1, true)
	mustTrafficChange(t, m, 0, true)
	saved := cloneTrafficSelection(m.state.trafficSelection)
	m.state.trafficSelection = trafficSelection{Sites: map[int64]trafficPreference{}}
	m.state.Pending = &trafficTransition{Target: saved, Phase: "activated", AppliedAt: saved.Sites[1].Source.StartTime}
	if err := m.saveLocked(); err != nil {
		t.Fatal(err)
	}
	_ = m.store.Close()
	restart := newTrafficControl(m.dataDir)
	restart.listSites = m.listSites
	restart.drained = m.drained
	restart.apply = func(context.Context, []nginx.TrafficSite) (nginx.TrafficApplyResult, error) {
		t.Fatal("durable activated replay reloaded")
		return nginx.TrafficApplyResult{}, nil
	}
	if err := restart.loadLocked(); err != nil {
		t.Fatal(err)
	}
	if err := restart.applyLocked(context.Background(), []trafficSiteInfo{{1, "one.test", "one.test"}}); err != nil {
		t.Fatal(err)
	}
	defer restart.store.Close()
	if restart.state.Pending != nil || restart.state.Sites[1].Source.Generation != saved.Sites[1].Source.Generation {
		t.Fatal("replay changed generation")
	}
}

func TestTrafficHTTPValidationAuthAndSiteScope(t *testing.T) {
	m := trafficTestControl(t)
	s := &Server{traffic: m}
	for _, tc := range []struct {
		body string
		code int
	}{{`{}`, 400}, {`{"enabled":null}`, 400}, {`{"enabled":"true"}`, 400}, {`{"enabled":true,"extra":1}`, 400}, {`{"enabled":true} {}`, 400}, {`{"enabled":true}` + strings.Repeat(" ", 1100), 400}, {`{"enabled":true}`, 200}} {
		r := httptest.NewRequest("PUT", "/api/v1/traffic-analytics", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handleTrafficAnalytics()(w, r)
		if w.Code != tc.code {
			t.Fatalf("%q =>%d %s", tc.body, w.Code, w.Body.String())
		}
	}
	for _, id := range []string{"0", "-1", "bad", "99"} {
		r := httptest.NewRequest("GET", "/api/v1/sites/"+id+"/traffic-analytics", nil)
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		s.handleSiteTrafficAnalytics()(w, r)
		want := 400
		if id == "99" {
			want = 404
		}
		if w.Code != want {
			t.Fatalf("site %s =>%d", id, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/api/v1/sites/1/traffic-analytics?range=all", nil)
	r.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	s.handleSiteTrafficAnalytics()(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	for _, path := range []string{"/api/v1/traffic-analytics", "/api/v1/sites/1/traffic-analytics"} {
		w := httptest.NewRecorder()
		AuthMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("unauthorized handler reached") })).ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
}

func TestTrafficHistoryDeletePreservesToggleAndNoBackfill(t *testing.T) {
	m := trafficTestControl(t)
	mustTrafficChange(t, m, 1, true)
	mustTrafficChange(t, m, 0, true)
	src := *m.state.Sites[1].Source
	now := time.Now().UTC().Add(time.Second)
	line := fmt.Sprintf("{\"v\":1,\"generation\":%q,\"ts\":%f,\"method\":\"GET\",\"path\":\"/page?secret=hidden\",\"status\":200,\"bytes\":100,\"request_time\":0.01,\"bot\":0,\"html\":1,\"referrer\":\"example.com\"}\n", src.Generation, float64(now.UnixNano())/1e9)
	if err := os.WriteFile(src.Path, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.collector.CollectOnce(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	report, err := m.store.Query(context.Background(), 1, traffic.RangeToday, now.Add(time.Second))
	if err != nil || report.Totals.Requests != 1 {
		t.Fatalf("%+v %v", report, err)
	}
	raw, _ := json.Marshal(report)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "hidden") {
		t.Fatal("query leaked request query")
	}
	if err := m.DeleteHistory(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if err := m.collector.CollectOnce(context.Background(), now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	report, err = m.store.Query(context.Background(), 1, traffic.RangeToday, now.Add(3*time.Second))
	if err != nil || report.Totals.Requests != 0 || m.state.Sites[1].Source.Generation != src.Generation {
		t.Fatalf("history resurrected or switch changed %+v %v", report, err)
	}
}

func TestTrafficRejectsUnsafeControlAncestry(t *testing.T) {
	m := trafficTestControl(t)
	if err := os.Mkdir(filepath.Join(m.dataDir, "elsewhere"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(m.dataDir, "elsewhere"), filepath.Join(m.dataDir, "traffic")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Status(context.Background()); err == nil {
		t.Fatal("followed unsafe traffic directory")
	}
}

func TestTrafficFailedChangeStillSupervisesActiveProducer(t *testing.T) {
	m := trafficTestControl(t)
	mustTrafficChange(t, m, 1, true)
	mustTrafficChange(t, m, 0, true)
	path := m.state.Sites[1].Source.Path
	original := m.apply
	m.apply = func(context.Context, []nginx.TrafficSite) (nginx.TrafficApplyResult, error) {
		return nginx.TrafficApplyResult{}, errors.New("unsupported second vhost")
	}
	if _, err := m.change(context.Background(), 2, true); err == nil {
		t.Fatal("bad vhost accepted")
	}
	if err := os.Truncate(path, trafficSiteLogBudget); err != nil {
		t.Fatal(err)
	}
	m.apply = original
	if err := m.superviseLocked(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.state.Sites[1].Enabled || m.state.Sites[1].Source != nil || m.state.Notice == "" {
		t.Fatal("failed toggle disabled pressure handling")
	}
}

func TestTrafficRestartDuringDrainRestoresIngestion(t *testing.T) {
	m := trafficTestControl(t)
	mustTrafficChange(t, m, 1, true)
	mustTrafficChange(t, m, 0, true)
	m.drained = func(context.Context, []nginx.TrafficWorker) (bool, error) { return false, nil }
	mustTrafficChange(t, m, 2, true)
	if m.state.Pending == nil || m.state.Pending.Phase != "draining" {
		t.Fatal("fixture did not save draining")
	}
	src := *m.state.Sites[1].Source
	_ = m.store.Close()
	restart := newTrafficControl(m.dataDir)
	restart.listSites = m.listSites
	restart.drained = func(context.Context, []nginx.TrafficWorker) (bool, error) { return true, nil }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	restart.Start(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for {
		state, err := restart.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !state.Pending {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("did not settle drain")
		}
		time.Sleep(time.Millisecond)
	}
	now := time.Now().UTC()
	line := fmt.Sprintf("{\"v\":1,\"generation\":%q,\"ts\":%f,\"method\":\"GET\",\"path\":\"/after-restart\",\"status\":200,\"bytes\":100,\"request_time\":0.01,\"bot\":0,\"html\":1,\"referrer\":\"\"}\n", src.Generation, float64(now.UnixNano())/1e9)
	if err := os.WriteFile(src.Path, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	if err := restart.collector.CollectOnce(ctx, now); err != nil {
		t.Fatal(err)
	}
	r, err := restart.Report(ctx, 1, traffic.RangeToday)
	if err != nil || r.Totals.Requests != 1 {
		t.Fatalf("restart lost ingestion: %+v %v", r, err)
	}
	cancel()
	select {
	case <-restart.done:
	case <-time.After(2 * time.Second):
		t.Fatal("stop timed out")
	}
}

func TestTrafficIngestionCapacityFailureStopsNewProducer(t *testing.T) {
	m := trafficTestControl(t)
	if err := m.ensureCollectorLocked(); err != nil {
		t.Fatal(err)
	}
	for batch := 0; batch < 2; batch++ {
		var sources []traffic.Source
		for i := 0; i < 128; i++ {
			id := int64(1000 + batch*128 + i)
			sources = append(sources, traffic.Source{SiteID: id, Generation: fmt.Sprintf("seed-%d", id), Path: filepath.Join(m.dataDir, fmt.Sprintf("seed-%d.jsonl", id)), StartTime: time.Now().Add(-time.Hour)})
		}
		if err := m.collector.SetSources(context.Background(), sources); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.collector.SetSources(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	mustTrafficChange(t, m, 1, true)
	var effective int
	original := m.apply
	m.apply = func(ctx context.Context, sources []nginx.TrafficSite) (nginx.TrafficApplyResult, error) {
		effective = len(sources)
		return original(ctx, sources)
	}
	state, err := m.change(context.Background(), 0, true)
	if err == nil || effective != 0 || state.MasterEnabled || !state.Pending || m.collector.Stats(1).EffectiveEnabled {
		t.Fatalf("uncollectable producer left ON: %+v err=%v sources=%d", state, err, effective)
	}
	if m.state.Pending.Phase != "draining" || m.state.Notice == "" {
		t.Fatal("storage failure was hidden")
	}
}

func TestTrafficRotatesTwoSourcesWithNonreentrantLifecycleLease(t *testing.T) {
	m := trafficTestControl(t)
	mustTrafficChange(t, m, 1, true)
	mustTrafficChange(t, m, 2, true)
	mustTrafficChange(t, m, 0, true)
	for _, p := range m.state.Sites {
		if p.Source != nil {
			if err := os.Truncate(p.Source.Path, trafficRotateBytes); err != nil {
				t.Fatal(err)
			}
		}
	}
	leased := false
	acquisitions := 0
	m.admit = func(context.Context) (func(), error) {
		if leased {
			t.Fatal("rotation nested lifecycle leases")
		}
		leased = true
		acquisitions++
		return func() { leased = false }, nil
	}
	if err := m.superviseLocked(context.Background()); err != nil {
		t.Fatal(err)
	}
	if acquisitions != 2 || leased {
		t.Fatalf("leases=%d active=%v", acquisitions, leased)
	}
	for _, p := range m.state.Sites {
		if p.Source != nil {
			if _, err := os.Stat(p.Source.Path + ".1"); err != nil {
				t.Fatal("first rotation did not create .1:", err)
			}
		}
	}
}

func TestTrafficCorruptStoreRestartPublishesPendingOff(t *testing.T) {
	m := trafficTestControl(t)
	mustTrafficChange(t, m, 1, true)
	mustTrafficChange(t, m, 0, true)
	target := cloneTrafficSelection(m.state.trafficSelection)
	target.MasterEnabled = false
	for id, p := range target.Sites {
		p.Source = nil
		target.Sites[id] = p
	}
	if err := m.beginTargetLocked(target, ""); err != nil {
		t.Fatal(err)
	}
	_ = m.store.Close()
	if err := os.WriteFile(filepath.Join(m.dataDir, "traffic", "analytics.db"), []byte("corrupt SQLite fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	restart := newTrafficControl(m.dataDir)
	restart.listSites = m.listSites
	var called atomic.Bool
	restart.apply = func(_ context.Context, sources []nginx.TrafficSite) (nginx.TrafficApplyResult, error) {
		called.Store(true)
		if len(sources) != 0 {
			t.Fatal("corrupt store activated logs")
		}
		return nginx.TrafficApplyResult{AppliedAt: time.Now()}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	restart.drained = func(context.Context, []nginx.TrafficWorker) (bool, error) { return false, nil }
	attempts := 0
	restart.admit = func(context.Context) (func(), error) {
		attempts++
		if attempts == 1 {
			return nil, errors.New("temporarily busy")
		}
		return func() {}, nil
	}
	restart.Start(ctx)
	deadline := time.Now().Add(3 * time.Second)
	for {
		state, err := restart.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if called.Load() && !state.MasterEnabled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pending OFF was not retried after busy admission")
		}
		time.Sleep(time.Millisecond)
	}
	restart.mu.Lock()
	phase := restart.state.Pending.Phase
	restart.mu.Unlock()
	if phase != "activated" {
		t.Fatal("pending OFF never reached producer")
	}
	cancel()
	select {
	case <-restart.done:
	case <-time.After(2 * time.Second):
		t.Fatal("failed-store controller did not stop")
	}
}
