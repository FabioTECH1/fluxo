package traffic

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func apiTestTime(s *Store) time.Time {
	return time.Unix(0, s.apiSupportNS).UTC().Add(10 * time.Second).Truncate(time.Second)
}

func apiLog(now time.Time, generation, method, path string, status int, bytes int64, seconds float64, bot int) []byte {
	r := record{Version: 1, Generation: generation, Timestamp: float64(now.UnixNano()) / 1e9, Method: method, Path: path, Status: status, Bytes: bytes, RequestTime: seconds, Bot: bot, HTML: 1}
	b, _ := json.Marshal(r)
	return append(b, '\n')
}

func apiSettingsAt(t *testing.T, c *Collector, now time.Time, settings APISettings) APISettingsState {
	t.Helper()
	state, err := c.setAPISettingsAt(context.Background(), 1, settings, now)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestAPIAggregatesMethodsStatusesBytesAndWeightedTime(t *testing.T) {
	s, c, src, _ := testCollector(t, CollectorOptions{})
	now := apiTestTime(s)
	apiSettingsAt(t, c, now, APISettings{Prefixes: []string{"/api"}, Routes: []string{"/api/users/:id", "/outside/:id"}})
	now = now.Add(time.Second)
	appendLog(t, src.Path,
		apiLog(now, src.Generation, "GET", "/api/users/1", 101, 10, .1, 1),
		apiLog(now, src.Generation, "GET", "/api/users/asset.js?secret=discard", 201, 20, .3, 0),
	)
	collect(t, c, now)
	appendLog(t, src.Path,
		apiLog(now.Add(time.Second), src.Generation, "GET", "/api/users/three", 302, 30, .5, 0),
		apiLog(now.Add(time.Second), src.Generation, "POST", "/api/users/4", 404, 40, .2, 0),
		apiLog(now.Add(time.Second), src.Generation, "PROPFIND", "/api/users/5", 503, 50, .4, 0),
		apiLog(now.Add(time.Second), src.Generation, "GET", "/outside/42", 200, 60, .6, 0),
		apiLog(now.Add(time.Second), src.Generation, "GET", "/apiculture", 200, 70, .7, 0),
	)
	collect(t, c, now.Add(2*time.Second))
	r := query(t, s, now.Add(2*time.Second))
	if r.Totals.Requests != 7 || r.Totals.PageViews != 1 || r.API.Totals.Requests != 6 || r.API.Totals.ResponseBytes != 210 || r.API.Totals.Status1xx != 1 || r.API.Totals.Status2xx != 2 || r.API.Totals.Status3xx != 1 || r.API.Totals.Status4xx != 1 || r.API.Totals.Status5xx != 1 || math.Abs(r.API.Totals.AvgRequestTimeMS-350) > .001 {
		t.Fatalf("bad base/API metrics: %+v / %+v", r.Totals, r.API.Totals)
	}
	if len(r.API.Endpoints) != 4 || r.API.Endpoints[0].Path != "/api/users/:id" || r.API.Endpoints[0].Method != "GET" || r.API.Endpoints[0].Metrics.Requests != 3 || math.Abs(r.API.Endpoints[0].Metrics.AvgRequestTimeMS-300) > .001 {
		t.Fatalf("bad endpoint weighted metrics: %+v", r.API.Endpoints)
	}
	for _, endpoint := range r.API.Endpoints {
		if endpoint.Method == "PROPFIND" || endpoint.Path == "/api/users/asset.js?secret=discard" {
			t.Fatalf("unbounded method or query survived: %+v", endpoint)
		}
	}
}

func TestAPISettingsBacklogUsesHistoricalRulesAcrossRestartAndRotation(t *testing.T) {
	s, c, src, _ := testCollector(t, CollectorOptions{RotatedFileRetired: alwaysRetired, MaxLinesPerSite: 1})
	now := apiTestTime(s)
	old := APISettings{Prefixes: []string{"/old"}, Routes: []string{"/old/:id"}}
	apiSettingsAt(t, c, now, old)
	appendLog(t, src.Path, apiLog(now.Add(time.Second), src.Generation, "GET", "/old/1", 200, 1, .1, 0), apiLog(now.Add(2*time.Second), src.Generation, "POST", "/old/2", 201, 2, .2, 0))
	collect(t, c, now.Add(3*time.Second))
	changed := apiSettingsAt(t, c, now.Add(4*time.Second), APISettings{Prefixes: []string{"/new"}, Routes: []string{"/new/:slug"}})
	if changed.EffectiveFrom == nil || !changed.EffectiveFrom.Equal(now.Add(4*time.Second)) {
		t.Fatal("settings effective point missing")
	}
	if err := os.Rename(src.Path, src.Path+".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src.Path, append(apiLog(now.Add(5*time.Second), src.Generation, "GET", "/old/3", 200, 3, .3, 0), apiLog(now.Add(6*time.Second), src.Generation, "GET", "/new/4", 200, 4, .4, 0)...), 0600); err != nil {
		t.Fatal(err)
	}
	c.closeFiles()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(filepath.Dir(s.dir))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted := NewCollector(reopened, CollectorOptions{RotatedFileRetired: alwaysRetired, MaxLinesPerSite: 1})
	defer restarted.closeFiles()
	if err = restarted.SetSources(context.Background(), []Source{src}); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		collect(t, restarted, now.Add(7*time.Second))
	}
	r := query(t, reopened, now.Add(7*time.Second))
	if r.Totals.Requests != 4 || r.API.Totals.Requests != 3 || r.API.Totals.ResponseBytes != 7 || len(r.API.Endpoints) != 3 {
		t.Fatalf("settings reclassified backlog or restart duplicated data: %+v", r)
	}
	if !reflect.DeepEqual(r.API.Settings, changed.Settings) || len(r.TopPaths) != 1 || r.TopPaths[0].Value != "/old/3" {
		t.Fatalf("current settings or forward pageview semantics: %+v", r)
	}
}

func TestAPIHistoricalTotalsNotReconstructedAndUnavailableBeforeSupport(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "traffic")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "analytics.db")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Hour)
	if _, err = legacy.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err = legacy.Exec("INSERT INTO hourly VALUES(?,?,?,?,?,?,?,?,?,?)", 1, old.Unix(), 99, 50, 1, 2, 3, 900, 9900, 0); err != nil {
		t.Fatal(err)
	}
	legacy.Close()
	s, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := NewCollector(s, CollectorOptions{})
	defer c.closeFiles()
	src := Source{SiteID: 1, Generation: "upgrade", Path: filepath.Join(root, "upgrade.jsonl"), StartTime: old}
	if err = os.WriteFile(src.Path, apiLog(old.Add(time.Minute), src.Generation, "GET", "/api/users/old", 200, 1, .1, 0), 0600); err != nil {
		t.Fatal(err)
	}
	if err = c.SetSources(context.Background(), []Source{src}); err != nil {
		t.Fatal(err)
	}
	now := apiTestTime(s)
	collect(t, c, now)
	r := query(t, s, now)
	if r.Totals.Requests != 100 || r.Totals.PageViews != 50 || r.API.Totals.Requests != 0 || len(r.API.Endpoints) != 0 || r.API.AvailableFrom == nil || r.API.AvailableFrom.UnixNano() != s.apiSupportNS {
		t.Fatalf("historical totals were reconstructed: %+v", r)
	}
	for _, bucket := range r.API.Buckets {
		if !bucket.Hour.Add(time.Hour).After(time.Unix(0, s.apiSupportNS)) && (bucket.Metrics != nil || bucket.Collection != "unknown") {
			t.Fatalf("unavailable past became zero API traffic: %+v", bucket)
		}
	}
}

func TestAPISettingsOffReadsStayLazyAndDoNotEnableLogging(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := NewCollector(s, CollectorOptions{})
	state, err := s.APISettings(context.Background(), 22)
	if err != nil || state.EffectiveFrom != nil || !reflect.DeepEqual(state.Settings, DefaultAPISettings()) {
		t.Fatalf("lazy defaults: %+v %v", state, err)
	}
	if _, err = s.Query(context.Background(), 22, RangeToday, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(s.dir); !os.IsNotExist(err) {
		t.Fatalf("read created files: %v", err)
	}
	if _, err = c.SetAPISettings(context.Background(), 22, APISettings{Prefixes: []string{"invalid"}}); err == nil {
		t.Fatal("invalid settings accepted")
	}
	if _, err = os.Stat(s.dir); !os.IsNotExist(err) {
		t.Fatalf("invalid write created files: %v", err)
	}
	saved, err := c.SetAPISettings(context.Background(), 22, APISettings{Routes: []string{"/custom/:id"}})
	if err != nil || saved.EffectiveFrom == nil {
		t.Fatalf("explicit off settings: %+v %v", saved, err)
	}
	collect(t, c, time.Now())
	for _, table := range []string{"sources", "intervals", "api_rules", "hourly", "api_hourly", "api_endpoints"} {
		var count int
		if err = s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("off settings created collection records in %s: %d %v", table, count, err)
		}
	}
	r, err := s.Query(context.Background(), 22, RangeToday, time.Now())
	if err != nil || r.API.AvailableFrom != nil || r.API.Totals.Requests != 0 {
		t.Fatalf("off availability: %+v %v", r.API, err)
	}
}

func TestAPIPageViewsRetainLegacyExclusionsWithEmptyRules(t *testing.T) {
	s, c, src, _ := testCollector(t, CollectorOptions{})
	now := apiTestTime(s)
	apiSettingsAt(t, c, now, APISettings{})
	for _, path := range []string{"/api", "/api/users", "/graphql", "/assets/view", "/x.js", "/apiculture", "/normal"} {
		appendLog(t, src.Path, apiLog(now.Add(time.Second), src.Generation, "GET", path, 200, 1, .1, 0))
	}
	collect(t, c, now.Add(2*time.Second))
	r := query(t, s, now.Add(2*time.Second))
	if r.Totals.PageViews != 2 || r.API.Totals.Requests != 0 {
		t.Fatalf("empty API settings changed legacy page estimates: %+v / %+v", r.Totals, r.API.Totals)
	}
}

func TestAPIHTTPTokenMethodsAreBoundedWithoutRejectingValidMethods(t *testing.T) {
	s, c, src, _ := testCollector(t, CollectorOptions{})
	now := apiTestTime(s)
	for _, method := range []string{"GET", "M-SEARCH", "get", "x!#$%&'*+-.^_`|~"} {
		appendLog(t, src.Path, apiLog(now, src.Generation, method, "/api/tokens", 200, 1, .1, 0))
	}
	for _, method := range []string{"", "BAD METHOD", "BAD\tMETHOD", "BAD/METHOD", "BAD:METHOD", "BAD(METHOD", "BAD\x00METHOD", "MÉTHOD", "12345678901234567"} {
		appendLog(t, src.Path, apiLog(now, src.Generation, method, "/api/tokens", 200, 1, .1, 0))
	}
	collect(t, c, now.Add(time.Second))
	r := query(t, s, now.Add(time.Second))
	if r.API.Totals.Requests != 4 || r.Totals.Requests != 4 || len(r.API.Endpoints) != 2 || r.API.Endpoints[0].Method != "OTHER" || r.API.Endpoints[0].Metrics.Requests != 3 || !r.Site.Incomplete {
		t.Fatalf("valid token methods lost or invalid separators accepted: %+v", r.API)
	}
}

func TestAPIPruneRemovedSiteSettingsRecoversCapacityAndPreservesLiveConfigAndHistory(t *testing.T) {
	s, c, src, _ := testCollector(t, CollectorOptions{})
	now := apiTestTime(s)
	live := APISettings{Prefixes: []string{"/living"}, Routes: []string{}}
	apiSettingsAt(t, c, now, live)
	appendLog(t, src.Path, apiLog(now.Add(time.Second), src.Generation, "GET", "/living/history", 200, 1, .1, 0))
	collect(t, c, now.Add(2*time.Second))
	if err := c.SetSourcesAt(context.Background(), nil, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for id := int64(2); id <= MaxRetainedSites; id++ {
		if _, err = tx.Exec("INSERT INTO api_settings(site,prefixes,routes,effective_ns) VALUES(?,'[\"/deleted\"]','[]',?)", id, now.UnixNano()); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if _, err = tx.Exec("INSERT INTO api_rules(site,start_ns,prefixes,routes) VALUES(2,?,'[\"/deleted\"]','[]')", now.UnixNano()); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = c.SetAPISettings(context.Background(), MaxRetainedSites+1, live); !errors.Is(err, ErrAPISettingsCapacity) {
		t.Fatalf("settings site cap not enforced: %v", err)
	}
	if err = s.PruneAPISettings(context.Background(), []int64{1}); err != nil {
		t.Fatal(err)
	}
	retained, err := s.APISettings(context.Background(), 1)
	if err != nil || !reflect.DeepEqual(retained.Settings, live) {
		t.Fatalf("orphan pruning lost disabled living settings: %+v %v", retained, err)
	}
	var rules, configs int
	if err = s.db.QueryRow("SELECT count(*) FROM api_rules WHERE site=2").Scan(&rules); err != nil || rules != 1 {
		t.Fatalf("orphan pruning removed historical classifier rules: %d %v", rules, err)
	}
	if err = s.db.QueryRow("SELECT count(*) FROM api_settings").Scan(&configs); err != nil || configs != 1 {
		t.Fatalf("orphan settings were retained: %d %v", configs, err)
	}
	if got := query(t, s, now.Add(4*time.Second)).API.Totals.Requests; got != 1 {
		t.Fatalf("settings pruning removed history: %d", got)
	}
	if _, err = c.SetAPISettings(context.Background(), MaxRetainedSites+1, live); err != nil {
		t.Fatalf("settings pruning did not recover capacity: %v", err)
	}
}

func TestAPIEndpointBoundsPreserveTotalsAndOverflowMetrics(t *testing.T) {
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprint("global=", global), func(t *testing.T) {
			s, c, src, _ := testCollector(t, CollectorOptions{MaxLinesPerSite: 16})
			now := apiTestTime(s)
			if global {
				s.apiRows = MaxAPIEndpointRows
			}
			for i := 0; i < 96; i++ {
				appendLog(t, src.Path, apiLog(now, src.Generation, "GET", fmt.Sprintf("/api/%03d", i), 500, 2, .2, 1))
			}
			for range 6 {
				collect(t, c, now.Add(time.Second))
			}
			r := query(t, s, now.Add(time.Second))
			if r.API.Totals.Requests != 96 || r.API.Totals.Status5xx != 96 || r.API.Totals.ResponseBytes != 192 || !r.API.Incomplete || r.API.Totals.AvgRequestTimeMS != 200 || len(r.API.Endpoints) > MaxAPIEndpointsPerHour+1 {
				t.Fatalf("overflow lost API totals: %+v", r.API)
			}
			var sum int64
			var overflow *APIEndpoint
			for i := range r.API.Endpoints {
				ep := &r.API.Endpoints[i]
				sum += ep.Metrics.Requests
				if ep.Path == overflowValue {
					overflow = ep
				}
			}
			if sum != 96 || overflow == nil || overflow.Method != "OTHER" || overflow.Metrics.Status5xx != overflow.Metrics.Requests || overflow.Metrics.AvgRequestTimeMS != 200 || (global && overflow.Metrics.Requests != 96) {
				t.Fatalf("overflow metrics lost: %+v", r.API.Endpoints)
			}
			if r.Site.Incomplete {
				t.Fatal("API endpoint capacity changed unrelated origin status")
			}
		})
	}
}

func TestAPISettingsCapacityIsAtomicAndUnchangedSettingsAreIdempotent(t *testing.T) {
	s, c, _, _ := testCollector(t, CollectorOptions{})
	now := apiTestTime(s)
	var state APISettingsState
	// The default interval already consumes one slot.
	for i := 1; i < MaxAPIRuleIntervals; i++ {
		state = apiSettingsAt(t, c, now.Add(time.Duration(i)*time.Second), APISettings{Prefixes: []string{fmt.Sprintf("/v%d", i)}})
	}
	if _, err := c.setAPISettingsAt(context.Background(), 1, APISettings{Prefixes: []string{"/rejected"}}, now.Add(200*time.Second)); !errors.Is(err, ErrAPISettingsCapacity) {
		t.Fatalf("settings capacity: %v", err)
	}
	got, err := s.APISettings(context.Background(), 1)
	if err != nil || !reflect.DeepEqual(got, state) {
		t.Fatalf("failed setting changed durable state: %+v %v", got, err)
	}
	if _, err = c.setAPISettingsAt(context.Background(), 1, got.Settings, now.Add(300*time.Second)); err != nil {
		t.Fatalf("no-op settings exhausted capacity: %v", err)
	}
	var count int
	if err = s.db.QueryRow("SELECT count(*) FROM api_rules WHERE site=1").Scan(&count); err != nil || count != MaxAPIRuleIntervals {
		t.Fatalf("rule interval bound: %d %v", count, err)
	}
}

func TestAPIHistoricalRuleByteBudgetBoundsWorstAcceptedPatterns(t *testing.T) {
	s, c, _, _ := testCollector(t, CollectorOptions{})
	now := apiTestTime(s)
	var retained APISettingsState
	accepted := 0
	for revision := 0; revision < MaxAPIRuleIntervals; revision++ {
		settings := APISettings{Prefixes: []string{}, Routes: []string{}}
		// 31 maximum-length routes fit the HTTP settings request's 16 KiB
		// payload limit while each contains 255 segments.
		for i := 0; i < 31; i++ {
			settings.Routes = append(settings.Routes, strings.Repeat("/a", 254)+fmt.Sprintf("/%c%02d", 'a'+revision, i))
		}
		state, err := c.setAPISettingsAt(context.Background(), 1, settings, now.Add(time.Duration(revision)*time.Second))
		if errors.Is(err, ErrAPISettingsCapacity) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		retained = state
		accepted++
	}
	if accepted != 4 {
		t.Fatalf("worst-case rule payload admitted %d revisions, expected 4 within 64 KiB", accepted)
	}
	var bytes, intervals int64
	if err := s.db.QueryRow("SELECT count(*),sum(length(CAST(prefixes AS BLOB))+length(CAST(routes AS BLOB))) FROM api_rules WHERE site=1").Scan(&intervals, &bytes); err != nil || bytes > MaxAPIRuleBytesPerSite || intervals != int64(accepted+1) {
		t.Fatalf("retained classifier memory budget: %d intervals, %d bytes, %v", intervals, bytes, err)
	}
	got, err := s.APISettings(context.Background(), 1)
	if err != nil || !reflect.DeepEqual(got, retained) {
		t.Fatalf("capacity rejection replaced needed current/historical rules: %+v %v", got, err)
	}
	rules, err := s.loadAPIRules(context.Background(), 1)
	if err != nil || len(rules) != accepted+1 {
		t.Fatalf("bounded historical classifier load: %d %v", len(rules), err)
	}
	// The oldest retained rules remain usable after newer rules exhaust capacity.
	classifier := classifierAt(rules, float64(now.Add(time.Millisecond).UnixNano())/1e9)
	firstRoute := strings.Repeat("/a", 254) + "/a00"
	if classifier == nil {
		t.Fatal("byte cap discarded an interval still needed by pending requests")
	}
	if got, matched := classifier.classify(firstRoute); !matched || got != firstRoute {
		t.Fatalf("old classifier corrupted by capacity rejection: %q %v", got, matched)
	}
}

func TestAPIBatchCheckpointFailureRollsBackAllAggregates(t *testing.T) {
	s, c, src, _ := testCollector(t, CollectorOptions{})
	now := apiTestTime(s)
	appendLog(t, src.Path, apiLog(now, src.Generation, "GET", "/api/atomic", 200, 17, .2, 0))
	if _, err := s.db.Exec("CREATE TRIGGER fail_api_checkpoint BEFORE UPDATE ON sources BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectOnce(context.Background(), now.Add(time.Second)); err == nil {
		t.Fatal("injected checkpoint failure succeeded")
	}
	for _, table := range []string{"hourly", "api_hourly", "api_endpoints"} {
		var count int
		if err := s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("failed checkpoint leaked %s: %d %v", table, count, err)
		}
	}
	if s.apiRows != 0 || c.sources[1].cp.offset != 0 {
		t.Fatal("failed batch advanced in-memory counts/checkpoint")
	}
	if _, err := s.db.Exec("DROP TRIGGER fail_api_checkpoint"); err != nil {
		t.Fatal(err)
	}
	collect(t, c, now.Add(time.Second))
	collect(t, c, now.Add(2*time.Second))
	r := query(t, s, now.Add(2*time.Second))
	if r.API.Totals.Requests != 1 || r.Totals.Requests != 1 || s.apiRows != 1 {
		t.Fatalf("retry did not collect exactly once: %+v", r)
	}
}

func TestAPISettingsAndDeleteRollbackPreserveRuleCacheAndCursor(t *testing.T) {
	s, c, src, _ := testCollector(t, CollectorOptions{})
	now := apiTestTime(s)
	appendLog(t, src.Path, apiLog(now, src.Generation, "GET", "/api/warm", 200, 1, .1, 0))
	collect(t, c, now.Add(time.Second))
	source := c.sources[1]
	beforeCP := source.cp
	beforeRules := &source.apiRules[0]
	if !source.apiRulesLoaded {
		t.Fatal("initial collection did not warm classifier cache")
	}
	if _, err := s.db.Exec("CREATE TRIGGER fail_api_settings BEFORE UPDATE ON api_rules BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.setAPISettingsAt(context.Background(), 1, APISettings{Prefixes: []string{"/rejected"}}, now.Add(2*time.Second)); err == nil {
		t.Fatal("injected settings failure succeeded")
	}
	settings, err := s.APISettings(context.Background(), 1)
	if err != nil || !reflect.DeepEqual(settings.Settings, DefaultAPISettings()) || !source.apiRulesLoaded || beforeRules != &source.apiRules[0] || source.cp != beforeCP {
		t.Fatalf("settings rollback changed rules/cache/cursor: %+v %v", settings, err)
	}
	if _, err = s.db.Exec("DROP TRIGGER fail_api_settings"); err != nil {
		t.Fatal(err)
	}
	appendLog(t, src.Path, apiLog(now.Add(3*time.Second), src.Generation, "GET", "/api/pending", 200, 1, .1, 0))
	if _, err = s.db.Exec("CREATE TRIGGER fail_api_delete BEFORE DELETE ON api_hourly BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	if err = c.DeleteHistory(context.Background(), 1); err == nil {
		t.Fatal("injected deletion failure succeeded")
	}
	if !source.apiRulesLoaded || beforeRules != &source.apiRules[0] || source.cp != beforeCP {
		t.Fatal("failed delete changed classifier cache or EOF cursor")
	}
	if got := query(t, s, now.Add(4*time.Second)).API.Totals.Requests; got != 1 {
		t.Fatalf("failed delete removed committed API history: %d", got)
	}
	if _, err = s.db.Exec("DROP TRIGGER fail_api_delete"); err != nil {
		t.Fatal(err)
	}
	collect(t, c, now.Add(4*time.Second))
	if got := query(t, s, now.Add(4*time.Second)).API.Totals.Requests; got != 2 {
		t.Fatalf("failed deletion lost or duplicated pending request: %d", got)
	}
}

func TestAPIDeleteHistoryKeepsSettingsAndPreventsBacklogResurrection(t *testing.T) {
	s, c, src, _ := testCollector(t, CollectorOptions{MaxLinesPerSite: 1})
	now := apiTestTime(s)
	settings := APISettings{Prefixes: []string{"/custom"}, Routes: []string{"/custom/:id"}}
	apiSettingsAt(t, c, now, settings)
	appendLog(t, src.Path, apiLog(now.Add(time.Second), src.Generation, "GET", "/custom/1", 200, 1, .1, 0), apiLog(now.Add(2*time.Second), src.Generation, "GET", "/custom/2", 200, 1, .1, 0))
	collect(t, c, now.Add(3*time.Second))
	if err := c.DeleteHistory(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	c.closeFiles()
	restarted := NewCollector(s, CollectorOptions{})
	defer restarted.closeFiles()
	if err := restarted.SetSources(context.Background(), []Source{src}); err != nil {
		t.Fatal(err)
	}
	collect(t, restarted, now.Add(4*time.Second))
	r := query(t, s, now.Add(4*time.Second))
	if r.API.Totals.Requests != 0 || r.Totals.Requests != 0 || s.apiRows != 0 || !reflect.DeepEqual(r.API.Settings, settings) {
		t.Fatalf("API delete lost settings or resurrected backlog: %+v", r)
	}
	appendLog(t, src.Path, apiLog(now.Add(5*time.Second), src.Generation, "GET", "/custom/3", 200, 1, .1, 0))
	collect(t, restarted, now.Add(6*time.Second))
	r = query(t, s, now.Add(6*time.Second))
	if r.API.Totals.Requests != 1 || len(r.API.Endpoints) != 1 || r.API.Endpoints[0].Path != "/custom/:id" {
		t.Fatalf("collection did not resume after deletion: %+v", r.API)
	}
}

func TestAPIIdleRetentionAndDisabledPeriods(t *testing.T) {
	s, c, src, _ := testCollector(t, CollectorOptions{})
	now := apiTestTime(s)
	appendLog(t, src.Path, apiLog(now, src.Generation, "GET", "/api/current", 200, 1, .1, 0))
	collect(t, c, now.Add(time.Second))
	var before, after int
	if err := s.db.QueryRow("SELECT total_changes()").Scan(&before); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		collect(t, c, now.Add(2*time.Second))
	}
	if err := s.db.QueryRow("SELECT total_changes()").Scan(&after); err != nil || before != after {
		t.Fatalf("idle API collection wrote rows: %d -> %d %v", before, after, err)
	}
	old := now.Add(-31 * 24 * time.Hour).Truncate(time.Hour).Unix()
	oldAPI := &apiAggregate{endpoints: map[apiKey]*apiCounters{}}
	oldAPI.addEndpoint(record{Method: "GET", Status: 201, Bytes: 2, RequestTime: .2}, "/api/expired")
	if err := s.apply(context.Background(), &batch{cp: c.sources[1].cp, hours: map[int64]*aggregate{old: {api: oldAPI}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Prune(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"api_hourly", "api_endpoints"} {
		var count int
		if err := s.db.QueryRow("SELECT count(*) FROM "+table+" WHERE hour=?", old).Scan(&count); err != nil || count != 0 {
			t.Fatalf("API retention failed in %s: %d %v", table, count, err)
		}
	}
	if s.apiRows != 1 {
		t.Fatalf("pruning did not recover API row capacity: %d", s.apiRows)
	}
	if err := c.SetSourcesAt(context.Background(), nil, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	appendLog(t, src.Path, apiLog(now.Add(4*time.Second), src.Generation, "GET", "/api/off", 200, 1, .1, 0))
	collect(t, c, now.Add(5*time.Second))
	next := src
	next.Generation = "api-new-generation"
	next.Path = filepath.Join(filepath.Dir(src.Path), "api-next.jsonl")
	next.StartTime = now.Add(time.Hour)
	if err := os.WriteFile(next.Path, apiLog(now.Add(6*time.Second), next.Generation, "GET", "/api/disabled", 200, 1, .1, 0), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.SetSourcesAt(context.Background(), []Source{next}, next.StartTime); err != nil {
		t.Fatal(err)
	}
	collect(t, c, next.StartTime.Add(time.Second))
	r := query(t, s, next.StartTime.Add(2*time.Second))
	if r.API.Totals.Requests != 1 || len(r.API.Gaps) == 0 {
		t.Fatalf("disabled period backfilled API traffic: %+v", r.API)
	}
}

// Exercise maximum-length, nearly identical configured routes through the
// complete bounded collector, rather than just timing a trivial matcher.
func BenchmarkAPIAdversarialRoutes(b *testing.B) {
	root := b.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		b.Fatal(err)
	}
	s, err := OpenStore(root)
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	c := NewCollector(s, CollectorOptions{})
	defer c.closeFiles()
	src := Source{SiteID: 1, Generation: "api-benchmark", Path: filepath.Join(root, "benchmark.jsonl"), StartTime: time.Now().Add(-time.Second)}
	f, err := os.OpenFile(src.Path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	if err = c.SetSources(context.Background(), []Source{src}); err != nil {
		b.Fatal(err)
	}
	now := apiTestTime(s)
	settings := APISettings{Prefixes: []string{"/"}}
	for i := 0; i < MaxAPIRoutes; i++ {
		settings.Routes = append(settings.Routes, strings.Repeat("/a", 254)+fmt.Sprintf("/r%02d", i))
	}
	if _, err = c.setAPISettingsAt(context.Background(), 1, settings, now); err != nil {
		b.Fatal(err)
	}
	now = now.Add(time.Second)
	payload := []byte{}
	for i := 0; i < 128; i++ {
		payload = append(payload, apiLog(now, src.Generation, "GET", settings.Routes[i%MaxAPIRoutes], 200, 3, .003, 0)...)
	}
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err = f.Write(payload); err != nil {
			b.Fatal(err)
		}
		if err = c.CollectOnce(context.Background(), now.Add(time.Second)); err != nil {
			b.Fatal(err)
		}
	}
}
