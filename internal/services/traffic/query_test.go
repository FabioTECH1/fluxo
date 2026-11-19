package traffic

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQueryUTCBoundsCoverageAndNoLogRescan(t *testing.T) {
	s, c, src, now := testCollector(t, CollectorOptions{})
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	start := day.Add(37 * time.Minute)
	if start.After(now) {
		start = now.Add(-10 * time.Minute)
	}
	src.StartTime = start
	if e := c.SetSources(context.Background(), nil); e != nil {
		t.Fatal(e)
	}
	src.Generation = "coverage"
	if e := c.SetSources(context.Background(), []Source{src}); e != nil {
		t.Fatal(e)
	}
	collect(t, c, now)
	report, e := s.Query(context.Background(), 1, RangeToday, now)
	if e != nil {
		t.Fatal(e)
	}
	if !report.From.Equal(day) || report.From.Location() != time.UTC {
		t.Fatalf("UTC range: %v", report.From)
	}
	index := int(start.Sub(day) / time.Hour)
	if index >= 0 && index < len(report.Buckets) {
		bucket := report.Buckets[index]
		if bucket.Collection != "partial" || bucket.Incomplete || bucket.Metrics == nil {
			t.Fatalf("partial coverage conflated with dropped data: %+v", bucket)
		}
	}
	// Queries remain available even after the source is removed and cannot read it.
	if e = os.Remove(src.Path); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Query(context.Background(), 1, Range7Days, now); e != nil {
		t.Fatal(e)
	}
	for _, r := range []Range{RangeToday, Range7Days, Range30Days} {
		report, e = s.Query(context.Background(), 1, r, now.In(time.FixedZone("offset", -7*3600)))
		if e != nil {
			t.Fatal(e)
		}
		want := 1
		if r == Range7Days {
			want = 7
		} else if r == Range30Days {
			want = 30
		}
		if !report.From.Equal(day.AddDate(0, 0, -want+1)) {
			t.Fatalf("%s start %v", r, report.From)
		}
	}
	if _, e = s.Query(context.Background(), 1, "bad", now); e == nil {
		t.Fatal("accepted unknown range")
	}
}
func TestGlobalDimensionBudgetKeepsTotalsAndOverflow(t *testing.T) {
	s, c, src, now := testCollector(t, CollectorOptions{})
	s.mu.Lock()
	s.dimensionRows = MaxDimensionRows
	s.mu.Unlock()
	appendLog(t, src.Path, recordLine(now, src.Generation, "/unseen", 200))
	collect(t, c, now)
	report := query(t, s, now)
	if report.Totals.Requests != 1 || report.Totals.PageViews != 1 || !report.Site.Incomplete || len(report.TopPaths) != 1 || report.TopPaths[0].Value != overflowValue {
		t.Fatalf("global cap lost totals: %+v", report)
	}
}
func TestDeleteHistorySurvivesActiveRestart(t *testing.T) {
	s, c, src, now := testCollector(t, CollectorOptions{})
	appendLog(t, src.Path, recordLine(now, src.Generation, "/delete", 200))
	collect(t, c, now)
	if e := c.DeleteHistory(context.Background(), 1); e != nil {
		t.Fatal(e)
	}
	c.closeFiles()
	restarted := NewCollector(s, CollectorOptions{})
	defer restarted.closeFiles()
	if e := restarted.SetSources(context.Background(), []Source{src}); e != nil {
		t.Fatalf("active delete changed generation identity: %v", e)
	}
	collect(t, restarted, time.Now())
	if got := query(t, s, time.Now()).Totals.Requests; got != 0 {
		t.Fatalf("restart resurrected deleted history: %d", got)
	}
}
func TestDisableRecordsUnprocessedBacklog(t *testing.T) {
	s, c, src, now := testCollector(t, CollectorOptions{MaxLinesPerSite: 1})
	appendLog(t, src.Path, recordLine(now, src.Generation, "/done", 200), recordLine(now, src.Generation, "/pending", 200))
	collect(t, c, now)
	if e := c.SetSources(context.Background(), nil); e != nil {
		t.Fatal(e)
	}
	cp, e := s.loadCheckpoint(context.Background(), 1)
	if e != nil {
		t.Fatal(e)
	}
	if cp.dropped == 0 || !cp.capped || cp.stop == 0 {
		t.Fatalf("disable discarded silently: %+v", cp)
	}
	report := query(t, s, time.Now())
	if !report.Site.Incomplete {
		t.Fatal("disabled history incomplete state missing")
	}
	// The historical gap must survive a fresh generation with healthy collection.
	src.Generation = "next"
	src.Path = filepath.Join(filepath.Dir(src.Path), "next.jsonl")
	src.StartTime = time.Now()
	if e = os.WriteFile(src.Path, nil, 0600); e != nil {
		t.Fatal(e)
	}
	if e = c.SetSources(context.Background(), []Source{src}); e != nil {
		t.Fatal(e)
	}
	collect(t, c, time.Now())
	report = query(t, s, time.Now())
	found := false
	for _, gap := range report.Gaps {
		if gap.Reason == "This enabled interval has incomplete or capped collection" {
			found = true
		}
	}
	if !found {
		t.Fatal("re-enable erased historical collection gap")
	}
}
func TestRotatedFinalPartialDoesNotBlockNewFile(t *testing.T) {
	s, c, src, now := testCollector(t, CollectorOptions{RotatedFileRetired: alwaysRetired})
	line := recordLine(now, src.Generation, "/partial", 200)
	appendLog(t, src.Path, line[:len(line)/2])
	collect(t, c, now)
	if e := os.Rename(src.Path, src.Path+".1"); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(src.Path, recordLine(now, src.Generation, "/complete", 200), 0600); e != nil {
		t.Fatal(e)
	}
	collect(t, c, now)
	collect(t, c, now)
	r := query(t, s, now)
	if r.Totals.Requests != 1 || !r.Site.Incomplete {
		t.Fatalf("partial rotation wedged: %+v", r)
	}
}

func BenchmarkCollectBatch(b *testing.B) {
	root := b.TempDir()
	if e := os.Chmod(root, 0700); e != nil {
		b.Fatal(e)
	}
	s, e := OpenStore(root)
	if e != nil {
		b.Fatal(e)
	}
	defer s.Close()
	c := NewCollector(s, CollectorOptions{})
	defer c.closeFiles()
	now := time.Now().UTC()
	src := Source{SiteID: 1, Generation: "benchmark", Path: filepath.Join(root, "benchmark.jsonl"), StartTime: now.Add(-time.Hour)}
	f, e := os.OpenFile(src.Path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		b.Fatal(e)
	}
	defer f.Close()
	if e = c.SetSources(context.Background(), []Source{src}); e != nil {
		b.Fatal(e)
	}
	var payload []byte
	for i := 0; i < 512; i++ {
		payload = append(payload, recordLine(now, src.Generation, fmt.Sprintf("/page/%d", i), 200)...)
	}
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, e = f.Write(payload); e != nil {
			b.Fatal(e)
		}
		if e = c.CollectOnce(context.Background(), now); e != nil {
			b.Fatal(e)
		}
	}
}

func TestSQLiteBudgetsSurviveConnectionReplacement(t *testing.T) {
	s, _, _, _ := testCollector(t, CollectorOptions{})
	s.db.SetMaxIdleConns(0)
	defer s.db.SetMaxIdleConns(1)
	for pragma, want := range map[string]int64{"max_page_count": 65536, "cache_size": -4096, "mmap_size": 0, "wal_autocheckpoint": 256, "journal_size_limit": 8388608} {
		var got int64
		if e := s.db.QueryRow("PRAGMA " + pragma).Scan(&got); e != nil {
			t.Fatal(e)
		}
		if got != want {
			t.Fatalf("replacement connection %s=%d want %d", pragma, got, want)
		}
	}
}
