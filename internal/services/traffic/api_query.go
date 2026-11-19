package traffic

import (
	"context"
	"database/sql"
	"sort"
	"time"
)

func emptyAPIReport(from, to time.Time) APIReport {
	out := APIReport{Settings: DefaultAPISettings(), Buckets: []APIBucket{}, Endpoints: []APIEndpoint{}, Gaps: []Gap{}, Notes: []string{
		"API requests include matching bots and assets at this Nginx origin; CDN cache hits are absent. Paths and HTTP methods use the configured, case-sensitive rules without guessing identifiers.",
		"Settings apply forward from their effective time. Earlier API data is unavailable; existing traffic totals are never reconstructed as API history. Historical endpoints retain the rules active at each request timestamp.",
		"Endpoint detail is capped at 64 method/path pairs per site/hour and 50,000 retained endpoint rows, with an (other) overflow row. Totals include matching requests whose endpoint details overflow. Rankings show up to 100 endpoints.",
		"Historical classification retains up to 128 settings intervals and 64 KiB of rule settings per site. Changes exceeding these bounds are rejected until old rules expire; required historical rules are never discarded to admit new settings.",
	}}
	for h := from; h.Before(to); h = h.Add(time.Hour) {
		out.Buckets = append(out.Buckets, APIBucket{Hour: h, Collection: "unknown"})
	}
	return out
}

// queryAPILocked shares the already bounded origin coverage intervals. Rule
// availability is intersected with verified logging coverage, so neither old
// overview totals nor disabled windows can become invented API zeros.
func (s *Store) queryAPILocked(ctx context.Context, siteID int64, from, to time.Time, origin []coverageInterval, sourceIncomplete bool) (APIReport, error) {
	out := emptyAPIReport(from, to)
	settings, err := s.apiSettingsLocked(ctx, siteID)
	if err != nil {
		return out, err
	}
	out.Settings, out.EffectiveFrom = settings.Settings, settings.EffectiveFrom
	rules, err := s.apiRulesLocked(ctx, siteID)
	if err != nil {
		return out, err
	}
	var available sql.NullInt64
	err = s.db.QueryRowContext(ctx, `SELECT min(max(i.start*1000000000,r.start_ns)) FROM intervals i JOIN api_rules r ON r.site=i.site WHERE i.site=? AND i.verified=1 AND i.start*1000000000<? AND r.start_ns<? AND (i.stop=0 OR i.stop*1000000000>r.start_ns) AND (r.stop_ns=0 OR r.stop_ns>i.start*1000000000)`, siteID, to.UnixNano(), to.UnixNano()).Scan(&available)
	if err != nil {
		return out, err
	}
	if available.Valid {
		at := time.Unix(0, available.Int64).UTC()
		out.AvailableFrom = &at
	}
	periods := []coverageInterval{}
	for _, p := range origin {
		for _, rule := range rules {
			a := maxTime(p.from, time.Unix(0, rule.start).UTC())
			b := p.to
			if rule.stop > 0 {
				b = minTime(b, time.Unix(0, rule.stop).UTC())
			}
			if b.After(a) {
				periods = append(periods, coverageInterval{from: a, to: b, incomplete: p.incomplete})
			}
		}
	}
	sort.Slice(periods, func(i, j int) bool { return periods[i].from.Before(periods[j].from) })
	cursor := from
	for _, p := range periods {
		if p.from.After(cursor) {
			out.Gaps = append(out.Gaps, Gap{From: cursor, To: p.from, Reason: "API analytics was unavailable, disabled or collection is unknown"})
		}
		if p.incomplete {
			out.Gaps = append(out.Gaps, Gap{From: p.from, To: p.to, Reason: "This enabled interval has incomplete collection"})
			out.Incomplete = true
		}
		cursor = maxTime(cursor, p.to)
	}
	if cursor.Before(to) {
		out.Gaps = append(out.Gaps, Gap{From: cursor, To: to, Reason: "API analytics was unavailable, disabled or collection is delayed"})
	}
	for i := range out.Buckets {
		bucket := &out.Buckets[i]
		end := minTime(to, bucket.Hour.Add(time.Hour))
		covered := time.Duration(0)
		for _, p := range periods {
			a, b := maxTime(bucket.Hour, p.from), minTime(end, p.to)
			if b.After(a) {
				covered += b.Sub(a)
				bucket.Incomplete = bucket.Incomplete || p.incomplete
			}
		}
		if covered > 0 {
			bucket.Metrics = &APIMetrics{}
			bucket.Collection = "partial"
			if covered >= end.Sub(bucket.Hour) {
				bucket.Collection = "enabled"
			}
		}
	}
	rows, err := s.db.QueryContext(ctx, "SELECT hour,requests,status_1xx,status_2xx,status_3xx,status_4xx,status_5xx,bytes,time_ms,capped FROM api_hourly WHERE site=? AND hour>=? AND hour<=? ORDER BY hour LIMIT 721", siteID, from.Unix(), to.Unix())
	if err != nil {
		return out, err
	}
	total := &apiCounters{}
	for rows.Next() {
		var h int64
		var counters apiCounters
		var capped bool
		if err = rows.Scan(&h, &counters.requests, &counters.status[0], &counters.status[1], &counters.status[2], &counters.status[3], &counters.status[4], &counters.bytes, &counters.timeMS, &capped); err != nil {
			rows.Close()
			return out, err
		}
		total.add(&counters)
		out.Incomplete = out.Incomplete || capped
		index := int((h - from.Unix()) / 3600)
		if index >= 0 && index < len(out.Buckets) {
			bucket := &out.Buckets[index]
			m := counters.metrics()
			bucket.Metrics = &m
			bucket.Incomplete = bucket.Incomplete || capped
			if bucket.Collection == "unknown" {
				bucket.Collection = "partial"
			}
			if capped {
				out.Gaps = append(out.Gaps, Gap{From: bucket.Hour, To: minTime(to, bucket.Hour.Add(time.Hour)), Reason: "API endpoint detail exceeded its bounded capacity; request totals are preserved"})
			}
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	if err = rows.Close(); err != nil {
		return out, err
	}
	out.Totals = total.metrics()
	rows, err = s.db.QueryContext(ctx, `SELECT method,path,SUM(requests) AS n,SUM(status_1xx),SUM(status_2xx),SUM(status_3xx),SUM(status_4xx),SUM(status_5xx),SUM(bytes),SUM(time_ms) FROM api_endpoints WHERE site=? AND hour>=? AND hour<=? GROUP BY method,path ORDER BY n DESC,path ASC,method ASC LIMIT ?`, siteID, from.Unix(), to.Unix(), MaxAPIEndpointResults)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var endpoint APIEndpoint
		var counters apiCounters
		if err = rows.Scan(&endpoint.Method, &endpoint.Path, &counters.requests, &counters.status[0], &counters.status[1], &counters.status[2], &counters.status[3], &counters.status[4], &counters.bytes, &counters.timeMS); err != nil {
			return out, err
		}
		endpoint.Metrics = counters.metrics()
		out.Endpoints = append(out.Endpoints, endpoint)
	}
	out.Incomplete = out.Incomplete || sourceIncomplete
	sort.Slice(out.Gaps, func(i, j int) bool { return out.Gaps[i].From.Before(out.Gaps[j].From) })
	return out, rows.Err()
}
