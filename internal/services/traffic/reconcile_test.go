package traffic

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotCapacityFailureIsAtomicAndInactiveDeleteFreesCapacity(t *testing.T) {
	s, c, src, now := testCollector(t, CollectorOptions{})
	collect(t, c, now)
	oldState := c.sources[1]
	oldFile := oldState.file
	tx, e := s.db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	for id := int64(2); id <= MaxRetainedSites; id++ {
		cp := checkpoint{site: id, generation: "retained", path: src.Path, start: src.StartTime.Unix(), stop: now.Unix()}
		if e = writeCheckpoint(context.Background(), tx, cp); e != nil {
			tx.Rollback()
			t.Fatal(e)
		}
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	next := Source{SiteID: MaxRetainedSites + 1, Generation: "new", Path: filepath.Join(filepath.Dir(src.Path), "next.jsonl"), StartTime: now}
	if e = os.WriteFile(next.Path, nil, 0600); e != nil {
		t.Fatal(e)
	}
	if e = c.SetSourcesAt(context.Background(), []Source{next}, now); e == nil {
		t.Fatal("expected retained capacity failure")
	}
	cp, e := s.loadCheckpoint(context.Background(), 1)
	if e != nil {
		t.Fatal(e)
	}
	if cp.stop != 0 || c.sources[1] != oldState || c.sources[1].file != oldFile {
		t.Fatal("failed complete snapshot changed the old active source")
	}
	if _, e = oldFile.Stat(); e != nil {
		t.Fatalf("rollback closed active file: %v", e)
	}
	var stop int64
	if e = s.db.QueryRow("SELECT stop FROM intervals WHERE site=1 AND generation=?", src.Generation).Scan(&stop); e != nil || stop != 0 {
		t.Fatalf("old interval changed: %d %v", stop, e)
	}
	if e = c.DeleteHistory(context.Background(), 2); e != nil {
		t.Fatal(e)
	}
	if e = c.SetSourcesAt(context.Background(), []Source{next}, now); e != nil {
		t.Fatalf("inactive delete did not free capacity: %v", e)
	}
}

func TestSnapshotRollbackAfterClosingOldInterval(t *testing.T) {
	s, c, src, now := testCollector(t, CollectorOptions{})
	collect(t, c, now)
	oldState := c.sources[1]
	oldFile := oldState.file
	if _, e := s.db.Exec("CREATE TRIGGER fail_interval BEFORE INSERT ON intervals WHEN NEW.generation='replacement' BEGIN SELECT RAISE(ABORT,'injected'); END"); e != nil {
		t.Fatal(e)
	}
	next := src
	next.Generation = "replacement"
	next.Path = filepath.Join(filepath.Dir(src.Path), "replacement.jsonl")
	next.StartTime = now
	if e := c.SetSourcesAt(context.Background(), []Source{next}, now); e == nil {
		t.Fatal("expected interval failure")
	}
	cp, e := s.loadCheckpoint(context.Background(), 1)
	if e != nil {
		t.Fatal(e)
	}
	if cp.generation != src.Generation || cp.stop != 0 || c.sources[1] != oldState || c.sources[1].file != oldFile {
		t.Fatalf("failed transaction changed active generation: %+v", cp)
	}
	var stop int64
	if e = s.db.QueryRow("SELECT stop FROM intervals WHERE site=1 AND generation=?", src.Generation).Scan(&stop); e != nil || stop != 0 {
		t.Fatalf("old interval not rolled back: %d %v", stop, e)
	}
	appendLog(t, src.Path, recordLine(now, src.Generation, "/still-active", 200))
	collect(t, c, now)
	if got := query(t, s, now).Totals.Requests; got != 1 {
		t.Fatalf("old collector could not continue after rollback: %d", got)
	}
}

func TestRestartReconcileUsesProducerEffectiveTime(t *testing.T) {
	s, c, src, now := testCollector(t, CollectorOptions{})
	appendLog(t, src.Path, recordLine(now.Add(-30*time.Minute), src.Generation, "/before-stop", 200))
	collect(t, c, now.Add(-30*time.Minute))
	c.closeFiles()
	if e := os.Remove(src.Path); e != nil {
		t.Fatal(e)
	} // Restarted OFF reconciliation never reads the log.
	restarted := NewCollector(s, CollectorOptions{})
	effective := now.Add(-15 * time.Minute)
	if e := restarted.SetSourcesAt(context.Background(), nil, effective); e != nil {
		t.Fatal(e)
	}
	cp, e := s.loadCheckpoint(context.Background(), 1)
	if e != nil {
		t.Fatal(e)
	}
	var stop int64
	if e = s.db.QueryRow("SELECT stop FROM intervals WHERE site=1 AND generation=?", src.Generation).Scan(&stop); e != nil {
		t.Fatal(e)
	}
	if cp.stop != effective.Unix() || stop != effective.Unix() {
		t.Fatalf("restart time replaced applied time: checkpoint %d interval %d want %d", cp.stop, stop, effective.Unix())
	}
}

func TestSuspendClosesFilesWithoutDatabaseWritesOrLogReads(t *testing.T) {
	s, c, src, now := testCollector(t, CollectorOptions{})
	collect(t, c, now)
	file := c.sources[1].file
	if _, e := s.db.Exec("CREATE TRIGGER fail_stop BEFORE UPDATE ON sources BEGIN SELECT RAISE(ABORT,'database full'); END"); e != nil {
		t.Fatal(e)
	}
	var before, after int
	if e := s.db.QueryRow("SELECT total_changes()").Scan(&before); e != nil {
		t.Fatal(e)
	}
	if e := os.Remove(src.Path); e != nil {
		t.Fatal(e)
	}
	c.Suspend()
	collect(t, c, now)
	if e := s.db.QueryRow("SELECT total_changes()").Scan(&after); e != nil {
		t.Fatal(e)
	}
	if before != after || len(c.sources) != 0 {
		t.Fatal("Suspend changed durable state or retained active sources")
	}
	if _, e := file.Stat(); e == nil {
		t.Fatal("Suspend retained an open log descriptor")
	}
	cp, e := s.loadCheckpoint(context.Background(), 1)
	if e != nil {
		t.Fatal(e)
	}
	if cp.stop != 0 {
		t.Fatal("Suspend wrote a durable stop")
	}
	if _, e = s.db.Exec("DROP TRIGGER fail_stop"); e != nil {
		t.Fatal(e)
	}
	effective := now.Add(time.Second)
	if e = c.SetSourcesAt(context.Background(), nil, effective); e != nil {
		t.Fatal(e)
	}
	cp, e = s.loadCheckpoint(context.Background(), 1)
	if e != nil || cp.stop != effective.Unix() || !cp.capped {
		t.Fatalf("durable retry failed: %+v %v", cp, e)
	}
}

func TestRotationWaitsForWriterRetirementAndCountsLateAppendOnce(t *testing.T) {
	retired := false
	s, c, src, now := testCollector(t, CollectorOptions{RotatedFileRetired: func(context.Context, string) (bool, error) { return retired, nil }})
	appendLog(t, src.Path, recordLine(now, src.Generation, "/first", 200))
	collect(t, c, now)
	oldWriter, e := os.OpenFile(src.Path, os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer oldWriter.Close()
	if e = os.Rename(src.Path, src.Path+".1"); e != nil {
		t.Fatal(e)
	}
	newLine := recordLine(now, src.Generation, "/new-file", 200)
	if e = os.WriteFile(src.Path, newLine, 0600); e != nil {
		t.Fatal(e)
	}
	collect(t, c, now)
	if got := query(t, s, now).Totals.Requests; got != 1 {
		t.Fatalf("switched before old writer retirement: %d", got)
	}
	status := c.Stats(1)
	if status.Status != "delayed" || status.BacklogBytes < int64(len(newLine)) {
		t.Fatalf("new-file backlog hidden: %+v", status)
	}
	late := recordLine(now, src.Generation, "/late-old-worker", 200)
	half := len(late) / 2
	if _, e = oldWriter.Write(late[:half]); e != nil {
		t.Fatal(e)
	}
	collect(t, c, now)
	if c.sources[1].cp.invalid != 0 {
		t.Fatal("discarded a fragment while its old writer was alive")
	}
	if _, e = oldWriter.Write(late[half:]); e != nil {
		t.Fatal(e)
	}
	collect(t, c, now)
	if got := query(t, s, now).Totals.Requests; got != 2 {
		t.Fatalf("late append lost: %d", got)
	}
	if c.RotationReady(src) {
		t.Fatal("EOF without retirement allowed replacing .1")
	}
	oldWriter.Close()
	retired = true
	collect(t, c, now)
	collect(t, c, now)
	if got := query(t, s, now).Totals.Requests; got != 3 {
		t.Fatalf("rotation lost or duplicated requests: %d", got)
	}
	if c.Stats(1).Incomplete {
		t.Fatal("complete late frame marked incomplete")
	}
}

func TestMissingRotationFileIsReadyWithoutProducerProbe(t *testing.T) {
	calls := 0
	_, c, src, now := testCollector(t, CollectorOptions{RotatedFileRetired: func(context.Context, string) (bool, error) { calls++; return false, os.ErrNotExist }})
	collect(t, c, now)
	if !c.RotationReady(src) {
		t.Fatal("absent .1 blocked first rotation")
	}
	if calls != 0 {
		t.Fatal("producer retirement probe called for absent .1")
	}
}

func TestRotationRechecksBytesAppendedDuringRetirementProof(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "EOF", true: "fragment"}[partial], func(t *testing.T) {
			s, c, src, now := testCollector(t, CollectorOptions{})
			appendLog(t, src.Path, recordLine(now, src.Generation, "/first", 200))
			collect(t, c, now)
			writer, e := os.OpenFile(src.Path, os.O_WRONLY|os.O_APPEND, 0600)
			if e != nil {
				t.Fatal(e)
			}
			defer writer.Close()
			late := recordLine(now, src.Generation, "/during-proof", 200)
			remaining := late
			if partial {
				half := len(late) / 2
				if _, e = writer.Write(late[:half]); e != nil {
					t.Fatal(e)
				}
				remaining = late[half:]
			}
			if e = os.Rename(src.Path, src.Path+".1"); e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(src.Path, recordLine(now, src.Generation, "/new", 200), 0600); e != nil {
				t.Fatal(e)
			}
			called := false
			c.opts.RotatedFileRetired = func(context.Context, string) (bool, error) {
				if !called {
					called = true
					if _, e = writer.Write(remaining); e != nil {
						return false, e
					}
					if e = writer.Close(); e != nil {
						return false, e
					}
				}
				return true, nil
			}
			for i := 0; i < 4; i++ {
				collect(t, c, now)
			}
			r := query(t, s, now)
			if r.Totals.Requests != 3 || r.Site.Incomplete {
				t.Fatalf("append during proof was lost/discarded: requests=%d incomplete=%v", r.Totals.Requests, r.Site.Incomplete)
			}
		})
	}
}
