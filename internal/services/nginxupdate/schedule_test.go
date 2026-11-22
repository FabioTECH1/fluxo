package nginxupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestNginxScheduledDiscoveryPersistsDailyDueTimeAndManualRefresh(t *testing.T) {
	m, store, count := setupManager(t)
	now := time.Unix(100000, 0)
	m.Now = func() time.Time { return now }
	if _, e := m.check(context.Background(), true); e != nil || *count != 1 {
		t.Fatal(*count, e)
	}
	v, _ := store.Load()
	if v.NextCheck != now.Add(dailyInterval).Unix() {
		t.Fatal(v)
	}
	if _, e := m.Dismiss("candidate-a"); e != nil {
		t.Fatal(e)
	}
	// Use the persisted JSON, including the dismissal, in a new manager.
	v, _ = store.Load()
	raw, _ := json.Marshal(v)
	v, err := DecodeSaved(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	reopened := New(&memoryStore{value: v})
	reopened.Now, reopened.Admit, reopened.Refresh, reopened.Inspect = m.Now, m.Admit, m.Refresh, m.Inspect
	now = now.Add(23 * time.Hour)
	if _, e := reopened.check(context.Background(), true); e != nil || *count != 1 {
		t.Fatal("restart refreshed a not-due discovery", *count, e)
	}
	if _, e := reopened.Check(context.Background()); e != nil || *count != 2 {
		t.Fatal("manual check was suppressed by schedule", *count, e)
	}
	now = now.Add(dailyInterval)
	s, e := reopened.check(context.Background(), true)
	if e != nil || *count != 3 || s.Notice {
		t.Fatal("daily refresh lost candidate dismissal", *count, s, e)
	}
	// A different candidate from the next automatic check becomes visible.
	reopened.Inspect = func(context.Context) (Snapshot, error) {
		return Snapshot{State: "update_available", Identity: "candidate-b"}, nil
	}
	now = now.Add(dailyInterval)
	s, e = reopened.check(context.Background(), true)
	if e != nil || !s.Notice {
		t.Fatal(s, e)
	}
}

func TestNginxScheduledDiscoveryDefersBusyWithoutClaimingNewFacts(t *testing.T) {
	m, store, count := setupManager(t)
	now := time.Unix(100000, 0)
	m.Now = func() time.Time { return now }
	m.Check(context.Background())
	before, _ := store.Load()
	// An already-due check can overlap a short maintenance window while the
	// previous result is still fresh. Deferral must not manufacture a failure.
	before.NextCheck = now.Unix()
	store.Save(before)
	m.Admit = func(context.Context) (func(), error) {
		return nil, fmt.Errorf("package lock: %w", errMaintenanceBusy)
	}
	s, e := m.check(context.Background(), true)
	after, _ := store.Load()
	if e != nil || *count != 1 || s.Stale || !s.Notice || s.Error != "" || after.AttemptedAt != before.AttemptedAt || after.LastSuccessfulCheck != before.LastSuccessfulCheck || after.NextCheck != now.Add(busyRetry).Unix() {
		t.Fatal("contention changed the last-known facts", s, after, e)
	}
	now = now.Add(busyRetry - time.Second)
	m.check(context.Background(), true)
	if *count != 1 {
		t.Fatal("retry ignored persisted maintenance delay")
	}
	now = now.Add(time.Second)
	m.Admit = func(context.Context) (func(), error) { return func() {}, nil }
	m.check(context.Background(), true)
	if *count != 2 {
		t.Fatal("retry did not resume after maintenance")
	}
}

func TestNginxScheduledDiscoveryFailureIsStaleAndRetriesHourly(t *testing.T) {
	m, store, count := setupManager(t)
	now := time.Unix(100000, 0)
	m.Now = func() time.Time { return now }
	m.Check(context.Background())
	before, _ := store.Load()
	now = now.Add(dailyInterval)
	m.Refresh = func(context.Context) error { *count++; return errors.New("repository unavailable") }
	s, e := m.check(context.Background(), true)
	after, _ := store.Load()
	if e != nil || !s.Stale || s.Notice || s.Error == "" || after.LastSuccessfulCheck != before.LastSuccessfulCheck || after.NextCheck != now.Add(failedRetry).Unix() {
		t.Fatal(s, after, e)
	}
	now = now.Add(failedRetry - time.Second)
	m.check(context.Background(), true)
	if *count != 2 {
		t.Fatal("retried a failed refresh too soon", *count)
	}
	now = now.Add(time.Second)
	m.check(context.Background(), true)
	if *count != 3 {
		t.Fatal("hourly retry did not run", *count)
	}
	// Integrity/read failures remain errors; they are not maintenance deferral.
	m.Admit = func(context.Context) (func(), error) { return nil, errors.New("unsafe package lock") }
	now = now.Add(failedRetry)
	s, e = m.check(context.Background(), true)
	if e != nil || s.Error != "unsafe package lock" || !s.Stale {
		t.Fatal(s, e)
	}
}

func TestNginxScheduleLegacySnapshotsAndClockChanges(t *testing.T) {
	now := time.Unix(100000, 0)
	for _, tc := range []struct {
		name  string
		value Saved
		due   bool
	}{
		{"no discovery", Saved{}, true},
		{"legacy recent success", Saved{LastSuccessfulCheck: 99999}, false},
		{"legacy stale success", Saved{LastSuccessfulCheck: 1000}, true},
		{"legacy recent failure", Saved{Error: "failed", AttemptedAt: 99999}, false},
		{"legacy old failure", Saved{Error: "failed", AttemptedAt: 90000}, true},
		{"legacy future clock", Saved{LastSuccessfulCheck: 200000}, true},
		{"new future clock", Saved{NextCheck: 200000}, true},
		{"persisted retry after clock change", Saved{LastSuccessfulCheck: 200000, NextCheck: 100300}, false},
		{"due retry", Saved{NextCheck: 100000}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := scheduledCheckDue(tc.value, now); got != tc.due {
				t.Fatalf("due=%v, want %v", got, tc.due)
			}
		})
	}
}

func TestNginxBackgroundScheduleRunsWithoutBrowserOnceDailyAndStops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, store, _ := setupManager(t)
		var count atomic.Int64
		m.Refresh = func(context.Context) error { count.Add(1); return nil }
		m.Now = time.Now
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		m.Start(ctx)
		m.Start(ctx) // Repeated lifecycle registration must not duplicate checks.
		time.Sleep(startupDelay - time.Second)
		synctest.Wait()
		if count.Load() != 0 {
			t.Fatal("checked during startup delay")
		}
		time.Sleep(startupJitter + 2*time.Second)
		synctest.Wait()
		if count.Load() != 1 {
			t.Fatal("first background check", count.Load())
		}
		v, _ := store.Load()
		time.Sleep(time.Until(time.Unix(v.NextCheck, 0)) - time.Second)
		synctest.Wait()
		if count.Load() != 1 {
			t.Fatal("refreshed before daily due time")
		}
		time.Sleep(schedulePoll + 2*time.Second)
		synctest.Wait()
		if count.Load() != 2 {
			t.Fatal("daily background check did not run", count.Load())
		}
		cancel()
		synctest.Wait()
		time.Sleep(2 * dailyInterval)
		synctest.Wait()
		if count.Load() != 2 {
			t.Fatal("discovery continued after shutdown", count.Load())
		}
	})
}

func TestNginxBackgroundCancellationPreservesSavedResult(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, store, _ := setupManager(t)
		m.Now = time.Now
		entered := make(chan struct{})
		m.Refresh = func(ctx context.Context) error { close(entered); <-ctx.Done(); return ctx.Err() }
		ctx, cancel := context.WithCancel(context.Background())
		m.Start(ctx)
		<-entered
		if _, e := m.Check(context.Background()); !errors.Is(e, ErrBusy) {
			t.Fatal("manual/background check overlapped", e)
		}
		cancel()
		synctest.Wait()
		v, _ := store.Load()
		s, _ := m.Status()
		if v.AttemptedAt != 0 || v.NextCheck != 0 || v.Error != "" || s.Checking {
			t.Fatal("shutdown persisted a misleading check result", v, s)
		}
	})
}

func TestNginxBackgroundRetriesUnsavedManualResultWithoutTightLoop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, store, _ := setupManager(t)
		var count atomic.Int64
		m.Refresh = func(context.Context) error { count.Add(1); return nil }
		m.Now = time.Now
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		m.Start(ctx)
		time.Sleep(startupDelay + startupJitter + time.Second)
		synctest.Wait()
		store.mu.Lock()
		store.fail = true
		store.mu.Unlock()
		if _, e := m.Check(ctx); e == nil || count.Load() != 2 {
			t.Fatal("did not exercise unsaved manual discovery", count.Load(), e)
		}
		store.mu.Lock()
		store.fail = false
		store.mu.Unlock()
		time.Sleep(failedRetry - time.Minute)
		synctest.Wait()
		if count.Load() != 2 {
			t.Fatal("retried an unsaved result in a tight loop", count.Load())
		}
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		s, e := m.Status()
		if e != nil || count.Load() != 3 || s.Stale || s.Error != "" {
			t.Fatal("did not recover unsaved discovery despite old daily due time", count.Load(), s, e)
		}
	})
}

func TestNginxScheduledCheckTimeoutIsRecordedAsFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, store, _ := setupManager(t)
		m.Now = time.Now
		m.Check(context.Background())
		before, _ := store.Load()
		before.NextCheck = time.Now().Unix()
		store.Save(before)
		m.Refresh = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
		s, e := m.check(context.Background(), true)
		after, _ := store.Load()
		if e != nil || !s.Stale || s.Notice || s.Error != context.DeadlineExceeded.Error() || after.LastSuccessfulCheck != before.LastSuccessfulCheck || after.NextCheck != time.Now().Add(failedRetry).Unix() {
			t.Fatal("check timeout was mistaken for panel shutdown", s, after, e)
		}
	})
}

func TestNginxAdmissionDefersOnlyRealContention(t *testing.T) {
	if !errors.Is(admissionError(fmt.Errorf("lock wait: %w", context.DeadlineExceeded)), errMaintenanceBusy) {
		t.Fatal("lock wait was not deferred")
	}
	unsafe := errors.New("unsafe ownership")
	if admissionError(unsafe) != unsafe || errors.Is(admissionError(unsafe), errMaintenanceBusy) || admissionError(context.Canceled) != context.Canceled {
		t.Fatal("masked cancellation or integrity failure as contention")
	}
}
