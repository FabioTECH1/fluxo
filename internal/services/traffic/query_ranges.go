package traffic

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"
)

const MaxReportBuckets = 720

func ValidateRange(r Range) error {
	switch r {
	case RangeToday, Range7Days, Range30Days, Range60Days, Range90Days, Range180Days, Range365Days, RangeAll:
		return nil
	default:
		return errors.New("invalid traffic range; use today, 7d, 30d, 60d, 90d, 180d, 365d or all")
	}
}

func rangeBounds(r Range, now time.Time) (time.Time, time.Time, error) {
	if err := ValidateRange(r); err != nil {
		return time.Time{}, time.Time{}, err
	}
	now = now.UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	days := map[Range]int{RangeToday: 1, Range7Days: 7, Range30Days: 30, Range60Days: 60, Range90Days: 90, Range180Days: 180, Range365Days: 365, RangeAll: 1}[r]
	return day.AddDate(0, 0, 1-days), now, nil
}

// Width grows with the selected span; neither keep-forever history nor a long
// disabled period can allocate an unbounded hourly array. All widths are exact
// multiples of UTC hours/days, with the final period clipped at Report.To.
func reportBucketSeconds(from, to time.Time) int64 {
	span := max(int64(0), reportEndSecond(to)-from.Unix())
	if span <= 30*86400 {
		return 3600
	}
	if span <= 180*86400 {
		return 86400
	}
	const week = int64(7 * 86400)
	weeks := max(int64(1), (span+MaxReportBuckets*week-1)/(MaxReportBuckets*week))
	return weeks * week
}

// Include the current fractional second while preserving an exclusive exact boundary.
func reportEndSecond(to time.Time) int64 {
	end := to.Unix()
	if to.Nanosecond() > 0 {
		end++
	}
	return end
}

// Retained zero-request coverage counts as history. API settings alone do not.
// The reads use existing site/hour indexes and never initialize a missing DB.
func (s *Store) availableFromLocked(ctx context.Context, siteID int64, to time.Time) (*time.Time, error) {
	if s.db == nil {
		return nil, nil
	}
	floor := int64(0)
	if s.retentionDays > 0 {
		floor = to.AddDate(0, 0, -s.retentionDays).Truncate(time.Hour).Unix()
	}
	var first sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MIN(stamp) FROM (
SELECT MIN(hour) AS stamp FROM hourly WHERE site=? AND hour>=? AND hour<?
UNION ALL SELECT MIN(hour) FROM api_hourly WHERE site=? AND hour>=? AND hour<?
UNION ALL SELECT MIN(MAX(start,?)) FROM intervals WHERE site=? AND verified=1 AND start<? AND (stop=0 OR stop>?))`,
		siteID, floor, reportEndSecond(to), siteID, floor, reportEndSecond(to), floor, siteID, reportEndSecond(to), floor).Scan(&first)
	if err != nil || !first.Valid {
		return nil, err
	}
	at := time.Unix(max(floor, first.Int64), 0).UTC()
	return &at, nil
}

func checkMetricSum(a, b Metrics) error {
	for _, pair := range [][2]int64{{a.Requests, b.Requests}, {a.PageViews, b.PageViews}, {a.NotFound, b.NotFound}, {a.Other4xx, b.Other4xx}, {a.ServerErrors, b.ServerErrors}, {a.ResponseBytes, b.ResponseBytes}} {
		if pair[1] < 0 || pair[0] > math.MaxInt64-pair[1] {
			return errors.New("traffic report exceeds supported totals")
		}
	}
	return nil
}

func checkAPISum(a, b *apiCounters) error {
	if b.requests < 0 || b.bytes < 0 || a.requests > math.MaxInt64-b.requests || a.bytes > math.MaxInt64-b.bytes {
		return errors.New("API traffic report exceeds supported totals")
	}
	for i := range a.status {
		if b.status[i] < 0 || a.status[i] > math.MaxInt64-b.status[i] {
			return errors.New("API traffic report exceeds supported totals")
		}
	}
	return nil
}
