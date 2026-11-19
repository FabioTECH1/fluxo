package traffic

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"
)

type coverageInterval struct {
	from, to   time.Time
	incomplete bool
}

func rangeBounds(r Range, now time.Time) (time.Time, time.Time, error) {
	now = now.UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	switch r {
	case RangeToday:
		return day, now, nil
	case Range7Days:
		return day.AddDate(0, 0, -6), now, nil
	case Range30Days:
		return day.AddDate(0, 0, -29), now, nil
	default:
		return time.Time{}, time.Time{}, errors.New("invalid traffic range; use today, 7d or 30d")
	}
}

// Query reads bounded aggregate tables only. It never opens or rescans a log.
func (s *Store) Query(ctx context.Context, siteID int64, r Range, now time.Time) (Report, error) {
	from, to, err := rangeBounds(r, now)
	if err != nil {
		return Report{}, err
	}
	out := Report{Range: r, From: from, To: to, Site: SiteStatus{SiteID: siteID, Status: "disabled"}, Buckets: []Bucket{}, TopPaths: []Rank{}, Referrers: []Rank{}, Gaps: []Gap{}, Notes: []string{"Requests are observed at this Nginx origin, not visitors. CDN cache hits that do not reach the origin are absent.", "Page views are estimates excluding known bots, common assets and API paths; no cookies, sessions or visitor identifiers are collected.", "Times and daily boundaries use UTC. Dedicated log collection normally refreshes about every 30 seconds."}}
	for h := from; h.Before(to); h = h.Add(time.Hour) {
		out.Buckets = append(out.Buckets, Bucket{Hour: h, Collection: "unknown"})
	}
	out.API = emptyAPIReport(from, to)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		out.Gaps = append(out.Gaps, Gap{From: from, To: to, Reason: "Analytics was not collected"})
		out.API.Gaps = append(out.API.Gaps, Gap{From: from, To: to, Reason: "API analytics was not collected"})
		return out, nil
	}
	cp, err := scanCheckpoint(s.db.QueryRowContext(ctx, "SELECT "+checkpointColumns+" FROM sources WHERE site=?", siteID))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	known := err == nil
	lastChecked := time.Time{}
	if live, ok := s.live[siteID]; ok && live.cp.generation == cp.generation {
		cp = live.cp
		lastChecked = live.checked
		if live.failure != "" {
			out.Site.Error = live.failure
			out.Site.Incomplete = true
		}
	}
	if lastChecked.IsZero() && cp.observed > 0 {
		lastChecked = time.Unix(cp.observed, 0).UTC()
	}
	if !lastChecked.IsZero() {
		out.Site.LastCheckedAt = &lastChecked
	}
	if known {
		out.Site.Enabled = cp.stop == 0
		out.Site.EffectiveEnabled = cp.stop == 0
		out.Site.BacklogBytes = cp.backlog
		out.Site.DroppedBytes = cp.dropped
		out.Site.Incomplete = out.Site.Incomplete || cp.capped || cp.invalid > 0 || cp.dropped > 0 || cp.issue != ""
		if cp.observed > 0 {
			at := time.Unix(cp.observed, 0).UTC()
			out.Site.LastProcessedAt = &at
		}
		if cp.stop == 0 {
			out.Site.Status = "collecting"
			if lastChecked.IsZero() || to.Sub(lastChecked) > time.Minute || cp.backlog > 0 || out.Site.Error != "" {
				out.Site.Status = "delayed"
			}
		}
		if out.Site.Incomplete {
			out.Site.Status = "incomplete"
		}
	}
	periods := []coverageInterval{}
	rows, err := s.db.QueryContext(ctx, "SELECT start,stop,incomplete FROM intervals WHERE site=? AND verified=1 AND start<? AND (stop=0 OR stop>?) ORDER BY start LIMIT 128", siteID, to.Unix()+1, from.Unix())
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var start, stop int64
		var incomplete bool
		if err = rows.Scan(&start, &stop, &incomplete); err != nil {
			rows.Close()
			return out, err
		}
		a := maxTime(from, time.Unix(start, 0).UTC())
		b := to
		if stop > 0 {
			b = minTime(to, time.Unix(stop, 0).UTC())
		}
		if stop == 0 && known {
			if !lastChecked.IsZero() {
				b = minTime(b, lastChecked)
			}
			if cp.ino == 0 {
				b = a
			} else if cp.backlog > 0 {
				b = minTime(b, time.Unix(int64(cp.lastTS), 0).UTC())
			}
		}
		if b.After(a) {
			periods = append(periods, coverageInterval{a, b, incomplete})
			if incomplete {
				out.Gaps = append(out.Gaps, Gap{From: a, To: b, Reason: "This enabled interval has incomplete or capped collection"})
			}
		}
	}
	if err = rows.Close(); err != nil {
		return out, err
	}
	cursor := from
	for _, p := range periods {
		if p.from.After(cursor) {
			out.Gaps = append(out.Gaps, Gap{From: cursor, To: p.from, Reason: "Analytics was disabled or collection is unknown"})
		}
		cursor = maxTime(cursor, p.to)
	}
	if cursor.Before(to) {
		out.Gaps = append(out.Gaps, Gap{From: cursor, To: to, Reason: "Analytics was disabled or collection is delayed"})
	}
	for i := range out.Buckets {
		bucket := &out.Buckets[i]
		end := minTime(bucket.Hour.Add(time.Hour), to)
		covered := time.Duration(0)
		for _, p := range periods {
			a := maxTime(bucket.Hour, p.from)
			b := minTime(end, p.to)
			if b.After(a) {
				if p.incomplete {
					bucket.Incomplete = true
				}
				covered += b.Sub(a)
			}
		}
		if covered > 0 {
			bucket.Metrics = &Metrics{}
			if covered >= end.Sub(bucket.Hour) {
				bucket.Collection = "enabled"
			} else {
				bucket.Collection = "partial"
			}
		}
	}
	rows, err = s.db.QueryContext(ctx, "SELECT hour,requests,pageviews,not_found,other_4xx,server_errors,bytes,time_ms,capped FROM hourly WHERE site=? AND hour>=? AND hour<=? ORDER BY hour LIMIT 721", siteID, from.Unix(), to.Unix())
	if err != nil {
		return out, err
	}
	totalTime := float64(0)
	for rows.Next() {
		var h int64
		var m Metrics
		var sumTime float64
		var capped bool
		if err = rows.Scan(&h, &m.Requests, &m.PageViews, &m.NotFound, &m.Other4xx, &m.ServerErrors, &m.ResponseBytes, &sumTime, &capped); err != nil {
			rows.Close()
			return out, err
		}
		if m.Requests > 0 {
			m.AvgRequestTimeMS = sumTime / float64(m.Requests)
		}
		index := int((h - from.Unix()) / 3600)
		if index >= 0 && index < len(out.Buckets) {
			b := &out.Buckets[index]
			b.Metrics = &m
			if b.Collection == "unknown" {
				b.Collection = "partial"
			}
			b.Incomplete = b.Incomplete || capped
		}
		out.Totals.Requests += m.Requests
		out.Totals.PageViews += m.PageViews
		out.Totals.NotFound += m.NotFound
		out.Totals.Other4xx += m.Other4xx
		out.Totals.ServerErrors += m.ServerErrors
		out.Totals.ResponseBytes += m.ResponseBytes
		totalTime += sumTime
	}
	if err = rows.Close(); err != nil {
		return out, err
	}
	if out.Totals.Requests > 0 {
		out.Totals.AvgRequestTimeMS = totalTime / float64(out.Totals.Requests)
	}
	for kind := 0; kind < 2; kind++ {
		rows, err = s.db.QueryContext(ctx, "SELECT value,SUM(n) AS total FROM dimensions WHERE site=? AND hour>=? AND hour<=? AND kind=? GROUP BY value ORDER BY total DESC,value ASC LIMIT 20", siteID, from.Unix(), to.Unix(), kind)
		if err != nil {
			return out, err
		}
		ranks := []Rank{}
		for rows.Next() {
			var rank Rank
			if err = rows.Scan(&rank.Value, &rank.Requests); err != nil {
				rows.Close()
				return out, err
			}
			ranks = append(ranks, rank)
		}
		if err = rows.Close(); err != nil {
			return out, err
		}
		if kind == 0 {
			out.TopPaths = ranks
		} else {
			out.Referrers = ranks
		}
	}
	if known && out.Site.Incomplete {
		reason := "One or more collection batches were incomplete or dimension counts were capped"
		if cp.issue != "" {
			reason = cp.issue
		}
		a := maxTime(from, time.Unix(cp.start, 0).UTC())
		b := to
		if cp.stop > 0 {
			b = minTime(b, time.Unix(cp.stop, 0).UTC())
		}
		if b.After(a) {
			out.Gaps = append(out.Gaps, Gap{From: a, To: b, Reason: reason})
			for i := range out.Buckets {
				if out.Buckets[i].Hour.Before(b) && out.Buckets[i].Hour.Add(time.Hour).After(a) {
					out.Buckets[i].Incomplete = true
				}
			}
		}
		out.Notes = append(out.Notes, "Dimensions are capped at 64 page paths and 32 referring domains per site/hour, with an (other) overflow bucket; totals include valid requests whose dimensions overflow.")
	}
	sort.Slice(out.Gaps, func(i, j int) bool { return out.Gaps[i].From.Before(out.Gaps[j].From) })
	out.API, err = s.queryAPILocked(ctx, siteID, from, to, periods, out.Site.Incomplete)
	return out, err
}
func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
