package traffic

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"
)

func TestHistoryRangesUTCBucketsAndLifetimeBound(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 30, 0, 0, time.FixedZone("offset", 3600))
	for r, days := range map[Range]int{RangeToday: 1, Range7Days: 7, Range30Days: 30, Range60Days: 60, Range90Days: 90, Range180Days: 180, Range365Days: 365} {
		from, to, err := rangeBounds(r, now)
		want := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1-days)
		if err != nil || !from.Equal(want) || !to.Equal(now) || from.Location() != time.UTC {
			t.Fatalf("%s: %v %v %v", r, from, to, err)
		}
		step := reportBucketSeconds(from, to)
		if n := (to.Unix() - from.Unix() + step - 1) / step; n > MaxReportBuckets {
			t.Fatalf("unbounded %s: %d", r, n)
		}
	}
	if step := reportBucketSeconds(now.AddDate(0, 0, -89), now); step != 86400 {
		t.Fatal(step)
	}
	if step := reportBucketSeconds(now.AddDate(-1, 0, 0), now); step != 604800 {
		t.Fatal(step)
	}
	from := time.Unix(0, 0).UTC()
	step := reportBucketSeconds(from, now)
	if (now.Unix()-from.Unix()+step-1)/step > MaxReportBuckets || step%604800 != 0 {
		t.Fatal("unbounded lifetime", step)
	}
}

func TestHistoryDailyTotalsWeightedAPIAndMissingCoverage(t *testing.T) {
	s, _, _, _ := testCollector(t, CollectorOptions{})
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	start := now.AddDate(0, 0, -59).Truncate(24 * time.Hour)
	s.retentionDays = 90
	// Private aggregate fixture: the real SQL reader must combine old hours,
	// keep gaps, and never reconstruct old API data from overview counters.
	for _, q := range []string{"DELETE FROM sources", "DELETE FROM intervals", "DELETE FROM api_rules", "DELETE FROM hourly", "DELETE FROM api_hourly"} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec("INSERT INTO intervals(site,generation,start,stop,incomplete,verified) VALUES(1,'old',?,?,0,1)", start.Unix(), start.Add(48*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("INSERT INTO api_rules(site,start_ns,stop_ns,prefixes,routes) VALUES(1,?,0,'[\"/api\"]','[]')", start.Add(time.Hour).UnixNano()); err != nil {
		t.Fatal(err)
	}
	for i, n := range []int{1, 4, 5} {
		h := start.Add(time.Duration(i) * time.Hour).Unix()
		if _, err := s.db.Exec("INSERT INTO hourly VALUES(?,?,?,?,?,?,?,?,?,?)", 1, h, n, n, 0, 0, 0, n*100, n*[]int{10, 100, 10}[i], 0); err != nil {
			t.Fatal(err)
		}
		if i > 0 {
			if _, err := s.db.Exec("INSERT INTO api_hourly VALUES(?,?,?,?,?,?,?,?,?,?,?)", 1, h, n, 0, n-1, 0, 1, 0, n*100, n*[]int{10, 100, 10}[i], 1); err != nil {
				t.Fatal(err)
			}
		}
	}
	r, err := s.Query(context.Background(), 1, Range90Days, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.BucketSeconds != 86400 || len(r.Buckets) != 90 || r.Totals.Requests != 10 || r.Totals.AvgRequestTimeMS != 46 {
		t.Fatalf("daily %+v", r)
	}
	if r.RetentionDays != 90 || r.AvailableFrom == nil || !r.AvailableFrom.Equal(start) {
		t.Fatal("retained origin", r.AvailableFrom)
	}
	if r.Buckets[0].Metrics != nil || r.Buckets[0].Collection != "unknown" || len(r.Gaps) == 0 {
		t.Fatal("invented precollection zero")
	}
	index := int(start.Sub(r.From) / (24 * time.Hour))
	if r.Buckets[index].Metrics.Requests != 10 || r.Buckets[index].Collection != "enabled" {
		t.Fatalf("daily aggregate %+v", r.Buckets[index])
	}
	if r.Buckets[index+1].Metrics == nil || r.Buckets[index+1].Metrics.Requests != 0 {
		t.Fatal("quiet covered day is not zero")
	}
	if r.API.Totals.Requests != 9 || r.API.Totals.AvgRequestTimeMS != 50 || r.API.Buckets[index].Collection != "partial" || !r.API.Buckets[index].Incomplete {
		t.Fatal("API coverage/counters changed", r.API)
	}
	if r.API.Buckets[0].Metrics != nil {
		t.Fatal("overview reconstructed API history")
	}
}

func TestHistoryAllRetainedBoundedAndDeletionDoesNotBackfill(t *testing.T) {
	s, c, _, now := testCollector(t, CollectorOptions{})
	if err := c.SetRetentionDays(0); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"DELETE FROM intervals", "DELETE FROM sources", "DELETE FROM hourly", "DELETE FROM api_hourly"} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := s.db.Exec("INSERT INTO hourly VALUES(1,?,1,1,0,0,0,100,10,0)", old.Unix()); err != nil {
		t.Fatal(err)
	}
	r, err := s.Query(context.Background(), 1, RangeAll, now)
	if err != nil {
		t.Fatal(err)
	}
	if !r.From.Equal(old) || len(r.Buckets) > MaxReportBuckets || len(r.API.Buckets) > MaxReportBuckets || r.Totals.Requests != 1 {
		t.Fatal("lifetime bound", len(r.Buckets), err)
	}
	raw, err := json.Marshal(r)
	if err != nil || len(raw) > 400000 {
		t.Fatal("unbounded payload", len(raw), err)
	}
	if err := c.DeleteHistory(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	r, err = s.Query(context.Background(), 1, RangeAll, now)
	if err != nil || r.AvailableFrom != nil || r.Totals.Requests != 0 || len(r.Buckets) > 24 {
		t.Fatal("deleted history resurrected", r.AvailableFrom, err)
	}
	for _, b := range r.Buckets {
		if b.Metrics != nil {
			t.Fatal("deleted history became quiet zero")
		}
	}
}

func TestHistoryExpiredCoverageAndCancelledRead(t *testing.T) {
	s, _, _, now := testCollector(t, CollectorOptions{})
	old := now.AddDate(0, 0, -80).Truncate(time.Hour)
	if _, err := s.db.Exec("INSERT INTO intervals(site,generation,start,stop,incomplete,verified) VALUES(1,'expired',?,?,0,1)", old.Unix(), old.Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("INSERT INTO hourly VALUES(1,?,123,123,0,0,0,100,10,0)", old.Unix()); err != nil {
		t.Fatal(err)
	}
	r, err := s.Query(context.Background(), 1, Range90Days, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Totals.Requests != 0 || r.Buckets[0].Metrics != nil {
		t.Fatal("cleanup backlog presented as retained/quiet history")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.Query(ctx, 1, RangeAll, now); err == nil {
		t.Fatal("cancelled report succeeded")
	}
	if checkMetricSum(Metrics{Requests: math.MaxInt64}, Metrics{Requests: 1}) == nil || checkAPISum(&apiCounters{requests: math.MaxInt64}, &apiCounters{requests: 1}) == nil {
		t.Fatal("totals overflow accepted")
	}
}

func TestHistorySQLCombinesMoreThanOldHourlyLimitAndKeepsEndExclusive(t *testing.T) {
	s, _, _, _ := testCollector(t, CollectorOptions{})
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	start := now.AddDate(0, 0, -80).Truncate(24 * time.Hour)
	s.retentionDays = 365
	for _, q := range []string{"DELETE FROM sources", "DELETE FROM intervals", "DELETE FROM api_rules", "DELETE FROM hourly", "DELETE FROM api_hourly"} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec("INSERT INTO intervals(site,generation,start,stop,incomplete,verified) VALUES(1,'long',?,?,0,1)", start.Unix(), now.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("INSERT INTO api_rules(site,start_ns,stop_ns,prefixes,routes) VALUES(1,?,0,'[\"/api\"]','[]')", start.UnixNano()); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 1000; i++ {
		h := start.Add(time.Duration(i) * time.Hour).Unix()
		if _, err = tx.Exec("INSERT INTO hourly VALUES(1,?,1,0,0,0,0,100,10,0)", h); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("INSERT INTO api_hourly VALUES(1,?,1,0,1,0,0,0,100,10,0)", h); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.Exec("INSERT INTO hourly VALUES(1,?,99,0,0,0,0,100,10,0)", now.Unix()); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, period := range []Range{Range90Days, Range365Days, RangeAll} {
		r, err := s.Query(t.Context(), 1, period, now)
		if err != nil {
			t.Fatal(err)
		}
		if r.Totals.Requests != 1000 || r.API.Totals.Requests != 1000 || r.Totals.AvgRequestTimeMS != 10 || r.API.Totals.AvgRequestTimeMS != 10 {
			t.Fatal("truncated old hours or included exclusive endpoint", r.Totals, r.API.Totals)
		}
		if period == Range365Days && r.BucketSeconds != 604800 {
			t.Fatal("year did not use weekly totals")
		}
	}
	// A sub-second current period still exists and uses the observed aggregate;
	// exactly-on-boundary queries exclude the next hour above.
	r, err := s.Query(t.Context(), 1, RangeToday, now.Add(time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if r.Totals.Requests != 99 || len(r.Buckets) != 13 {
		t.Fatalf("fractional last period: %d %d", r.Totals.Requests, len(r.Buckets))
	}
}
