package traffic

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func retainedRequests(t *testing.T, s *Store) int64 {
	t.Helper()
	var n int64
	if err := s.db.QueryRow("SELECT coalesce(sum(requests),0) FROM hourly").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRetentionPolicyControlsParserAndGenerationFloor(t *testing.T) {
	for _, days := range []int{30, 60, 90, 180, 365, 0} {
		t.Run(fmt.Sprint(days), func(t *testing.T) {
			s, c, src, now := testCollector(t, CollectorOptions{})
			src.Generation = "older-source"
			src.StartTime = now.Add(-800 * 24 * time.Hour)
			if err := c.SetSourcesAt(context.Background(), []Source{src}, now); err != nil {
				t.Fatal(err)
			}
			if err := c.SetRetentionDays(days); err != nil {
				t.Fatal(err)
			}
			age := days
			if days == 0 {
				age = 700
			}
			appendLog(t, src.Path, recordLine(now.Add(-time.Duration(age-1)*24*time.Hour), src.Generation, "/kept", 200), recordLine(now.Add(-time.Duration(age+1)*24*time.Hour), src.Generation, "/older", 200), recordLine(src.StartTime.Add(-time.Hour), src.Generation, "/before-enabled", 200))
			collect(t, c, now)
			want := int64(1)
			if days == 0 {
				want = 2
			}
			if n := retainedRequests(t, s); n != want {
				t.Fatalf("retained=%d want=%d", n, want)
			}
			cp, err := s.loadCheckpoint(context.Background(), src.SiteID)
			st, statErr := os.Stat(src.Path)
			if err != nil || statErr != nil || cp.offset != st.Size() || cp.invalid != 0 {
				t.Fatalf("expired frames must advance cursor without invalid counts: %+v %v %v", cp, err, statErr)
			}
		})
	}
}

func TestRetentionChangesPruneAllAggregateTablesAndManualDoesNoAgeWrites(t *testing.T) {
	s, c, src, now := testCollector(t, CollectorOptions{})
	ctx := context.Background()
	if err := c.SetRetentionDays(0); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-70 * 24 * time.Hour).Truncate(time.Hour).Unix()
	api := &apiAggregate{endpoints: map[apiKey]*apiCounters{}}
	api.addEndpoint(record{Method: "GET", Status: 200, Bytes: 1, RequestTime: .1}, "/api/kept")
	cp := c.sources[1].cp
	if err := s.apply(ctx, &batch{cp: cp, hours: map[int64]*aggregate{old: {requests: 3, pages: map[string]int64{"/kept": 3}, referrers: map[string]int64{"example.com": 3}, api: api}}}); err != nil {
		t.Fatal(err)
	}
	// A closed API rule is needed by old queued records while manually retained.
	if _, err := s.db.Exec("INSERT INTO api_rules(site,start_ns,stop_ns,prefixes,routes) VALUES(2,?,?,?,?)", old*int64(time.Second), (old+3600)*int64(time.Second), `["/api"]`, `[]`); err != nil {
		t.Fatal(err)
	}
	var before, after int
	_ = s.db.QueryRow("SELECT total_changes()").Scan(&before)
	if err := s.Prune(ctx, now.AddDate(10, 0, 0)); err != nil {
		t.Fatal(err)
	}
	_ = s.db.QueryRow("SELECT total_changes()").Scan(&after)
	if before != after {
		t.Fatal("manual retention performed age cleanup writes")
	}
	if _, err := c.SetAPISettings(ctx, 2, APISettings{Prefixes: []string{"/v2"}, Routes: []string{}}); err != nil {
		t.Fatal(err)
	}
	for _, days := range []int{90, 30} {
		if err := c.SetRetentionDays(days); err != nil {
			t.Fatal(err)
		}
		if err := s.Prune(ctx, now); err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"hourly", "dimensions", "api_hourly", "api_endpoints"} {
			var n int
			if err := s.db.QueryRow("SELECT count(*) FROM "+table+" WHERE hour=?", old).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if (n > 0) != (days == 90) {
				t.Fatalf("%s retention %d rows %d", table, days, n)
			}
		}
		var n int
		if err := s.db.QueryRow("SELECT count(*) FROM api_rules WHERE site=2 AND stop_ns=?", (old+3600)*int64(time.Second)).Scan(&n); err != nil || (n > 0) != (days == 90) {
			t.Fatalf("rule retention %d rows %d err %v", days, n, err)
		}
	}
	if err := c.SetRetentionDays(0); err != nil {
		t.Fatal(err)
	}
	if err := s.Prune(ctx, now); err != nil {
		t.Fatal(err)
	}
	if retainedRequests(t, s) != 0 {
		t.Fatal("longer retention resurrected expired aggregates")
	}
	if cp, err := s.loadCheckpoint(ctx, src.SiteID); err != nil || cp.generation != src.Generation {
		t.Fatal("cleanup changed active cursor", err)
	}
}

func TestManualRetentionRestartAndDeleteCannotReplayOldFrames(t *testing.T) {
	s, c, src, now := testCollector(t, CollectorOptions{})
	ctx := context.Background()
	if err := c.SetRetentionDays(0); err != nil {
		t.Fatal(err)
	}
	appendLog(t, src.Path, recordLine(now, src.Generation, "/old", 200))
	collect(t, c, now)
	c.closeFiles()
	root := filepath.Dir(s.dir)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	next := NewCollector(restarted, CollectorOptions{})
	defer next.closeFiles()
	if err := next.SetRetentionDays(0); err != nil {
		t.Fatal(err)
	}
	if err := next.SetSourcesAt(ctx, []Source{src}, now); err != nil {
		t.Fatal(err)
	}
	collect(t, next, now.Add(500*24*time.Hour))
	if err := restarted.Prune(ctx, now.Add(500*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if retainedRequests(t, restarted) != 1 {
		t.Fatal("restart lost or duplicated manual history")
	}
	appendLog(t, src.Path, recordLine(now, src.Generation, "/queued", 200))
	if err := next.DeleteHistory(ctx, src.SiteID); err != nil {
		t.Fatal(err)
	}
	// Appending an old timestamp after the delete still cannot bypass its cutoff.
	appendLog(t, src.Path, recordLine(now.Add(-time.Minute), src.Generation, "/late-old", 200))
	collect(t, next, now.Add(time.Second))
	if retainedRequests(t, restarted) != 0 {
		t.Fatal("explicit deletion replayed history")
	}
	appendLog(t, src.Path, recordLine(now.Add(2*time.Second), src.Generation, "/new", 200))
	collect(t, next, now.Add(3*time.Second))
	if retainedRequests(t, restarted) != 1 {
		t.Fatal("deletion stopped new collection")
	}
}

func TestStorageCapacityPausesWithoutDeletingHistoryOrAdvancingCheckpoint(t *testing.T) {
	s, c, src, now := testCollector(t, CollectorOptions{})
	if err := c.SetRetentionDays(0); err != nil {
		t.Fatal(err)
	}
	appendLog(t, src.Path, recordLine(now, src.Generation, "/retained", 200))
	collect(t, c, now)
	ctx := context.Background()
	cp, err := s.loadCheckpoint(ctx, src.SiteID)
	if err != nil {
		t.Fatal(err)
	}
	// Constrain SQLite's real connection limit to exercise the production guard
	// without allocating a 256 MiB fixture.
	if _, err = s.db.Exec("PRAGMA max_page_count=128"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("CREATE TABLE storage_fixture(payload BLOB)"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 128; i++ {
		if err = s.ingestionCapacityLocked(ctx); err != nil {
			break
		}
		if _, err = s.db.Exec("INSERT INTO storage_fixture VALUES(zeroblob(4096))"); err != nil {
			t.Fatal(err)
		}
	}
	appendLog(t, src.Path, recordLine(now.Add(time.Second), src.Generation, "/pending", 200))
	if err = c.CollectOnce(ctx, now.Add(2*time.Second)); err == nil || !strings.Contains(err.Error(), "storage capacity") {
		t.Fatalf("expected capacity pause: %v", err)
	}
	status := c.Stats(src.SiteID)
	if !status.Incomplete || status.Error == "" || status.Status != "delayed" {
		t.Fatalf("hidden pause %+v", status)
	}
	after, err := s.loadCheckpoint(ctx, src.SiteID)
	if err != nil || after.offset != cp.offset || retainedRequests(t, s) != 1 {
		t.Fatal("capacity pause changed retained history or checkpoint", err)
	}
	if err = s.Prune(ctx, now.AddDate(10, 0, 0)); err != nil || retainedRequests(t, s) != 1 {
		t.Fatal("manual history evicted under pressure", err)
	}
	if _, err = s.db.Exec("DROP TABLE storage_fixture"); err != nil {
		t.Fatal(err)
	}
	collect(t, c, now.Add(3*time.Second))
	if retainedRequests(t, s) != 2 {
		t.Fatal("space recovery failed or duplicated backlog")
	}
}

func TestManualRetentionDoesNotBackfillDisabledIntervals(t *testing.T) {
	s, c, src, now := testCollector(t, CollectorOptions{})
	ctx := context.Background()
	if err := c.SetRetentionDays(0); err != nil {
		t.Fatal(err)
	}
	appendLog(t, src.Path, recordLine(now, src.Generation, "/before", 200))
	collect(t, c, now)
	if err := c.SetSourcesAt(ctx, nil, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	appendLog(t, src.Path, recordLine(now.Add(2*time.Second), src.Generation, "/disabled", 200))
	collect(t, c, now.Add(2*time.Second))
	if retainedRequests(t, s) != 1 {
		t.Fatal("collected while disabled")
	}
	next := src
	next.Generation = "after-disabled"
	next.StartTime = now.Add(3 * time.Second)
	next.Path = src.Path + ".next"
	if err := os.WriteFile(next.Path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.SetSourcesAt(ctx, []Source{next}, next.StartTime); err != nil {
		t.Fatal(err)
	}
	appendLog(t, next.Path, recordLine(now.Add(2*time.Second), next.Generation, "/backfill", 200), recordLine(now.Add(4*time.Second), next.Generation, "/after", 200))
	collect(t, c, now.Add(5*time.Second))
	if retainedRequests(t, s) != 2 {
		t.Fatal("reenabling backfilled disabled history")
	}
}

func TestFiniteRetentionCleanupKeepsPerPassRowBudget(t *testing.T) {
	s, c, _, now := testCollector(t, CollectorOptions{})
	ctx := context.Background()
	if err := c.SetRetentionDays(365); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	old := now.Add(-366 * 24 * time.Hour).Unix()
	for i := int64(0); i < 4100; i++ {
		if _, err = tx.Exec("INSERT INTO hourly(site,hour,requests,pageviews,not_found,other_4xx,server_errors,bytes,time_ms) VALUES(1,?,1,0,0,0,0,0,0)", old-i*3600); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = s.Prune(ctx, now); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err = s.db.QueryRow("SELECT count(*) FROM hourly").Scan(&rows); err != nil || rows != 4 {
		t.Fatalf("cleanup exceeded budget: %d %v", rows, err)
	}
}

func TestDatabaseCeilingUsesExistingSQLitePageSize(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "traffic")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, "analytics.db")
	db, err := sql.Open("sqlite", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("PRAGMA page_size=8192; CREATE TABLE fixture(n INTEGER)"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(name, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var size, pages int64
	if err = store.db.QueryRow("PRAGMA page_size").Scan(&size); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow("PRAGMA max_page_count").Scan(&pages); err != nil {
		t.Fatal(err)
	}
	if size != 8192 || size*pages != MaxDatabaseBytes {
		t.Fatalf("incorrect existing-page ceiling %d * %d", size, pages)
	}
}
