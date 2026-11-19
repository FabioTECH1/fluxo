package traffic

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testCollector(t *testing.T, opts CollectorOptions) (*Store, *Collector, Source, time.Time) {
	t.Helper()
	root := t.TempDir()
	if e := os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	store, e := OpenStore(root)
	if e != nil {
		t.Fatal(e)
	}
	collector := NewCollector(store, opts)
	t.Cleanup(func() { collector.closeFiles(); store.Close() })
	now := time.Now().UTC().Truncate(time.Second)
	src := Source{SiteID: 1, Generation: "generation-1", Path: filepath.Join(root, "requests.jsonl"), StartTime: now.Add(-time.Hour)}
	if e = os.WriteFile(src.Path, nil, 0600); e != nil {
		t.Fatal(e)
	}
	if e = collector.SetSources(context.Background(), []Source{src}); e != nil {
		t.Fatal(e)
	}
	return store, collector, src, now
}
func appendLog(t *testing.T, name string, lines ...[]byte) {
	t.Helper()
	f, e := os.OpenFile(name, os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	for _, line := range lines {
		if _, e = f.Write(line); e != nil {
			t.Fatal(e)
		}
	}
}
func collect(t *testing.T, c *Collector, now time.Time) {
	t.Helper()
	if e := c.CollectOnce(context.Background(), now); e != nil {
		t.Fatal(e)
	}
}
func query(t *testing.T, s *Store, now time.Time) Report {
	t.Helper()
	report, e := s.Query(context.Background(), 1, Range7Days, now.Add(time.Second))
	if e != nil {
		t.Fatal(e)
	}
	return report
}

func TestDefaultOffIsLazyAndDoesNoLogWork(t *testing.T) {
	root := t.TempDir()
	s, e := OpenStore(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	c := NewCollector(s, CollectorOptions{})
	if e = c.SetSources(context.Background(), nil); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		collect(t, c, time.Now())
	}
	if e = s.Prune(context.Background(), time.Now()); e != nil {
		t.Fatal(e)
	}
	r, e := s.Query(context.Background(), 1, RangeToday, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(root, "traffic")); !os.IsNotExist(e) {
		t.Fatalf("OFF created state: %v", e)
	}
	for _, b := range r.Buckets {
		if b.Metrics != nil {
			t.Fatal("unknown hours must not become zero traffic")
		}
	}
}
func TestAggregateMathAndCheckpointRestart(t *testing.T) {
	s, c, src, now := testCollector(t, CollectorOptions{})
	for _, status := range []int{200, 201, 404, 403, 500} {
		appendLog(t, src.Path, recordLine(now, src.Generation, "/page?secret=x", status))
	}
	collect(t, c, now)
	r := query(t, s, now)
	if r.Totals.Requests != 5 || r.Totals.PageViews != 2 || r.Totals.NotFound != 1 || r.Totals.Other4xx != 1 || r.Totals.ServerErrors != 1 || r.Totals.ResponseBytes != 600 || r.Totals.AvgRequestTimeMS != 250 {
		t.Fatalf("bad totals: %+v", r.Totals)
	}
	if len(r.TopPaths) != 1 || r.TopPaths[0].Value != "/page" || r.TopPaths[0].Requests != 2 {
		t.Fatalf("bad paths: %+v", r.TopPaths)
	}
	cp, e := s.loadCheckpoint(context.Background(), 1)
	if e != nil || cp.offset == 0 {
		t.Fatalf("checkpoint: %+v %v", cp, e)
	}
	c.closeFiles()
	restarted := NewCollector(s, CollectorOptions{})
	defer restarted.closeFiles()
	if e = restarted.SetSources(context.Background(), []Source{src}); e != nil {
		t.Fatal(e)
	}
	collect(t, restarted, now.Add(time.Second))
	if got := query(t, s, now).Totals.Requests; got != 5 {
		t.Fatalf("restart duplicated requests: %d", got)
	}
	appendLog(t, src.Path, recordLine(now.Add(time.Second), src.Generation, "/new", 200))
	collect(t, restarted, now.Add(2*time.Second))
	if got := query(t, s, now.Add(2*time.Second)).Totals.Requests; got != 6 {
		t.Fatalf("new request missing: %d", got)
	}
}
func TestPartialFramesAndOversizedInput(t *testing.T) {
	s, c, src, now := testCollector(t, CollectorOptions{MaxBytesPerSite: 128})
	line := recordLine(now, src.Generation, "/partial", 200)
	half := len(line) / 2
	appendLog(t, src.Path, line[:half])
	collect(t, c, now)
	cp, _ := s.loadCheckpoint(context.Background(), 1)
	if cp.offset != 0 {
		t.Fatal("partial frame advanced cursor")
	}
	appendLog(t, src.Path, line[half:])
	for i := 0; i < 3; i++ {
		collect(t, c, now)
	}
	if got := query(t, s, now).Totals.Requests; got != 1 {
		t.Fatalf("completed partial frame got %d", got)
	}
	// Batches always permit at least one maximum-sized frame; use the default
	// budget here for giant-line drain checks.
	c.opts.MaxBytesPerSite = MaxBatchBytes
	collect(t, c, now)
	if got := query(t, s, now).Totals.Requests; got != 1 {
		t.Fatalf("partial completion got %d", got)
	}
	appendLog(t, src.Path, []byte(strings.Repeat("z", MaxBatchBytes+100)), []byte("\n"), recordLine(now, src.Generation, "/after", 200))
	collect(t, c, now)
	collect(t, c, now)
	r := query(t, s, now)
	if r.Totals.Requests != 2 || !r.Site.Incomplete {
		t.Fatalf("oversized recovery: %+v", r.Site)
	}
}
func TestRotationRestartAndCopyTruncate(t *testing.T) {
	s, c, src, now := testCollector(t, CollectorOptions{MaxLinesPerSite: 1, RotatedFileRetired: alwaysRetired})
	appendLog(t, src.Path, recordLine(now, src.Generation, "/a", 200), recordLine(now, src.Generation, "/b", 200))
	collect(t, c, now)
	if e := os.Rename(src.Path, src.Path+".1"); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(src.Path, recordLine(now, src.Generation, "/c", 200), 0600); e != nil {
		t.Fatal(e)
	}
	c.closeFiles()
	c = NewCollector(s, CollectorOptions{MaxLinesPerSite: 1, RotatedFileRetired: alwaysRetired})
	defer c.closeFiles()
	if e := c.SetSources(context.Background(), []Source{src}); e != nil {
		t.Fatal(e)
	}
	if c.RotationReady(src) {
		t.Fatal("undrained .1 reported rotation-ready")
	}
	collect(t, c, now)
	collect(t, c, now)
	if got := query(t, s, now).Totals.Requests; got != 3 {
		t.Fatalf("rotation lost/duplicated data: %d", got)
	}
	if !c.RotationReady(src) {
		t.Fatal("drained .1 not rotation-ready")
	}
	// Copy-truncate can regrow beyond the old offset before the next tick. The
	// durable boundary hash detects this rather than silently skipping new data.
	var replacement []byte
	for i := 0; i < 4; i++ {
		replacement = append(replacement, recordLine(now, src.Generation, fmt.Sprintf("/replacement-%d", i), 200)...)
	}
	if e := os.WriteFile(src.Path, replacement, 0600); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 5; i++ {
		collect(t, c, now)
	}
	r := query(t, s, now)
	if r.Totals.Requests != 7 || !r.Site.Incomplete {
		t.Fatalf("copy truncate result: %d %+v", r.Totals.Requests, r.Site)
	}
}
func TestAtomicAggregateAndCursorOnWriteFailure(t *testing.T) {
	s, c, src, now := testCollector(t, CollectorOptions{})
	appendLog(t, src.Path, recordLine(now, src.Generation, "/atomic", 200))
	if _, e := s.db.Exec("CREATE TRIGGER fail_hour BEFORE INSERT ON hourly BEGIN SELECT RAISE(ABORT,'injected'); END"); e != nil {
		t.Fatal(e)
	}
	if e := c.CollectOnce(context.Background(), now); e == nil {
		t.Fatal("expected injected write failure")
	}
	cp, _ := s.loadCheckpoint(context.Background(), 1)
	if cp.offset != 0 {
		t.Fatal("failed aggregate advanced checkpoint")
	}
	if _, e := s.db.Exec("DROP TRIGGER fail_hour"); e != nil {
		t.Fatal(e)
	}
	collect(t, c, now)
	collect(t, c, now)
	if got := query(t, s, now).Totals.Requests; got != 1 {
		t.Fatalf("retry counted %d requests", got)
	}
}
func TestDimensionFloodPreservesTotalsAndBoundsRows(t *testing.T) {
	s, c, src, now := testCollector(t, CollectorOptions{})
	for i := 0; i < 1000; i++ {
		appendLog(t, src.Path, recordLine(now, src.Generation, fmt.Sprintf("/page/%d", i), 200))
	}
	for i := 0; i < 3; i++ {
		collect(t, c, now)
	}
	r := query(t, s, now)
	if r.Totals.Requests != 1000 || r.Totals.PageViews != 1000 || !r.Site.Incomplete {
		t.Fatalf("flood totals: %+v %+v", r.Totals, r.Site)
	}
	var count, total int64
	if e := s.db.QueryRow("SELECT count(*),sum(n) FROM dimensions WHERE kind=0").Scan(&count, &total); e != nil {
		t.Fatal(e)
	}
	if count > MaxPageDimensionsPerHour+1 || total != 1000 {
		t.Fatalf("dimension rows=%d total=%d", count, total)
	}
}
func TestBacklogBudgetAndFairSites(t *testing.T) {
	s, c, src, now := testCollector(t, CollectorOptions{MaxBytesPerSite: 4096, MaxLinesPerSite: 8, MaxSitesPerPass: 1, MaxBacklogBytes: 16384})
	src2 := Source{SiteID: 2, Generation: "g2", Path: filepath.Join(filepath.Dir(src.Path), "second.jsonl"), StartTime: src.StartTime}
	if e := os.WriteFile(src2.Path, recordLine(now, src2.Generation, "/quiet", 200), 0600); e != nil {
		t.Fatal(e)
	}
	if e := c.SetSources(context.Background(), []Source{src, src2}); e != nil {
		t.Fatal(e)
	}
	appendLog(t, src.Path, bytes.Repeat(recordLine(now, src.Generation, "/flood", 200), 1000))
	collect(t, c, now)
	collect(t, c, now)
	r, e := s.Query(context.Background(), 2, RangeToday, now.Add(time.Second))
	if e != nil {
		t.Fatal(e)
	}
	if r.Totals.Requests != 1 {
		t.Fatalf("quiet source starved: %d", r.Totals.Requests)
	}
	status := c.Stats(1)
	if !status.Incomplete || status.DroppedBytes == 0 || status.BacklogBytes > 16384 {
		t.Fatalf("budget did not cap backlog: %+v", status)
	}
}
func TestDeletionAndDisabledGenerationDoNotBackfill(t *testing.T) {
	s, c, src, now := testCollector(t, CollectorOptions{MaxLinesPerSite: 1})
	appendLog(t, src.Path, recordLine(now, src.Generation, "/old-1", 200), recordLine(now, src.Generation, "/old-2", 200))
	collect(t, c, now)
	if e := c.DeleteHistory(context.Background(), 1); e != nil {
		t.Fatal(e)
	}
	collect(t, c, now)
	if got := query(t, s, now).Totals.Requests; got != 0 {
		t.Fatalf("deleted backlog resurrected %d", got)
	}
	appendLog(t, src.Path, recordLine(time.Now().Add(time.Second), src.Generation, "/new", 200))
	collect(t, c, time.Now().Add(2*time.Second))
	if got := query(t, s, now.Add(2*time.Second)).Totals.Requests; got != 1 {
		t.Fatalf("active collection failed after delete: %d", got)
	}
	if e := c.SetSources(context.Background(), nil); e != nil {
		t.Fatal(e)
	}
	appendLog(t, src.Path, recordLine(now, src.Generation, "/disabled", 200))
	collect(t, c, now)
	newSrc := src
	newSrc.Generation = "generation-2"
	newSrc.Path = filepath.Join(filepath.Dir(src.Path), "new-generation.jsonl")
	newSrc.StartTime = time.Now()
	if e := os.WriteFile(newSrc.Path, nil, 0600); e != nil {
		t.Fatal(e)
	}
	if e := c.SetSources(context.Background(), []Source{newSrc}); e != nil {
		t.Fatal(e)
	}
	collect(t, c, time.Now())
	if got := query(t, s, time.Now()).Totals.Requests; got != 1 {
		t.Fatalf("disabled gap was backfilled: %d", got)
	}
}
func TestIdleDoesNotWriteAndRetentionIsBounded(t *testing.T) {
	s, c, src, now := testCollector(t, CollectorOptions{})
	collect(t, c, now)
	var before, after int
	if e := s.db.QueryRow("SELECT total_changes()").Scan(&before); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 5; i++ {
		collect(t, c, now.Add(time.Duration(i)*time.Second))
	}
	if e := s.db.QueryRow("SELECT total_changes()").Scan(&after); e != nil {
		t.Fatal(e)
	}
	if before != after {
		t.Fatalf("idle collector wrote %d changes", after-before)
	}
	cp, _ := s.loadCheckpoint(context.Background(), src.SiteID)
	old := now.Add(-31 * 24 * time.Hour).Truncate(time.Hour).Unix()
	if e := s.apply(context.Background(), &batch{cp: cp, hours: map[int64]*aggregate{old: {requests: 3, pages: map[string]int64{"/expired": 3}, referrers: map[string]int64{}}}}); e != nil {
		t.Fatal(e)
	}
	if e := s.Prune(context.Background(), now); e != nil {
		t.Fatal(e)
	}
	var n int
	if e := s.db.QueryRow("SELECT count(*) FROM hourly WHERE hour=?", old).Scan(&n); e != nil || n != 0 {
		t.Fatalf("retention rows=%d err=%v", n, e)
	}
	if e := s.db.QueryRow("PRAGMA max_page_count").Scan(&n); e != nil || int64(n)*4096 != MaxDatabaseBytes {
		t.Fatalf("database page cap=%d err=%v", n, e)
	}
}
func TestProtectedSourceRejectsSymlinkHardlinkAndWritableAncestry(t *testing.T) {
	_, c, src, now := testCollector(t, CollectorOptions{})
	target := src.Path + ".target"
	if e := os.Rename(src.Path, target); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(target, src.Path); e != nil {
		t.Fatal(e)
	}
	if e := c.CollectOnce(context.Background(), now); e == nil {
		t.Fatal("accepted symlink")
	}
	os.Remove(src.Path)
	if e := os.Link(target, src.Path); e != nil {
		t.Fatal(e)
	}
	if e := c.CollectOnce(context.Background(), now); e == nil {
		t.Fatal("accepted hardlink")
	}
	os.Remove(src.Path)
	if e := os.Rename(target, src.Path); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(filepath.Dir(src.Path), 0777); e != nil {
		t.Fatal(e)
	}
	defer os.Chmod(filepath.Dir(src.Path), 0700)
	if e := c.CollectOnce(context.Background(), now); e == nil {
		t.Fatal("accepted writable ancestry")
	}
}

func alwaysRetired(context.Context, string) (bool, error) { return true, nil }
