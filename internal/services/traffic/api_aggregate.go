package traffic

import (
	"context"
	"database/sql"
	"sort"
)

type apiCounters struct {
	requests int64
	status   [5]int64
	bytes    int64
	timeMS   float64
}

type apiKey struct{ method, path string }

type apiAggregate struct {
	apiCounters
	endpoints map[apiKey]*apiCounters
	capped    bool
}

func (a *apiCounters) addRecord(r record) {
	a.requests++
	a.status[r.Status/100-1]++
	a.bytes += r.Bytes
	a.timeMS += r.RequestTime * 1000
}

func (a *apiCounters) add(b *apiCounters) {
	a.requests += b.requests
	for i := range a.status {
		a.status[i] += b.status[i]
	}
	a.bytes += b.bytes
	a.timeMS += b.timeMS
}

func (a *apiCounters) metrics() APIMetrics {
	m := APIMetrics{Requests: a.requests, Status1xx: a.status[0], Status2xx: a.status[1], Status3xx: a.status[2], Status4xx: a.status[3], Status5xx: a.status[4], ResponseBytes: a.bytes}
	if a.requests > 0 {
		m.AvgRequestTimeMS = a.timeMS / float64(a.requests)
	}
	return m
}

func (a *apiAggregate) addEndpoint(r record, path string) {
	a.addRecord(r)
	key := apiKey{apiMethod(r.Method), path}
	if a.endpoints[key] == nil && len(a.endpoints) >= MaxAPIEndpointsPerHour {
		key = apiKey{"OTHER", overflowValue}
		a.capped = true
	}
	if a.endpoints[key] == nil {
		a.endpoints[key] = &apiCounters{}
	}
	a.endpoints[key].addRecord(r)
}

func (s *Store) applyAPIAggregate(ctx context.Context, tx *sql.Tx, siteID, hour int64, a *apiAggregate, pending int64) (int64, error) {
	rows, err := tx.QueryContext(ctx, "SELECT method,path FROM api_endpoints WHERE site=? AND hour=? AND path<>?", siteID, hour, overflowValue)
	if err != nil {
		return 0, err
	}
	known := map[apiKey]bool{}
	for rows.Next() {
		var key apiKey
		if err = rows.Scan(&key.method, &key.path); err != nil {
			rows.Close()
			return 0, err
		}
		known[key] = true
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	if err = rows.Close(); err != nil {
		return 0, err
	}
	keys := make([]apiKey, 0, len(a.endpoints))
	for key := range a.endpoints {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].path == keys[j].path {
			return keys[i].method < keys[j].method
		}
		return keys[i].path < keys[j].path
	})
	added := int64(0)
	overflow := &apiCounters{}
	for _, key := range keys {
		counters := a.endpoints[key]
		if key.path == overflowValue || !known[key] && (len(known) >= MaxAPIEndpointsPerHour || s.apiRows+pending+added >= MaxAPIEndpointRows) {
			overflow.add(counters)
			a.capped = true
			continue
		}
		if !known[key] {
			known[key] = true
			added++
		}
		if err = writeAPIEndpoint(ctx, tx, siteID, hour, key, counters); err != nil {
			return 0, err
		}
	}
	if overflow.requests > 0 {
		if err = writeAPIEndpoint(ctx, tx, siteID, hour, apiKey{"OTHER", overflowValue}, overflow); err != nil {
			return 0, err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO api_hourly(site,hour,requests,status_1xx,status_2xx,status_3xx,status_4xx,status_5xx,bytes,time_ms,capped) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(site,hour) DO UPDATE SET requests=min(9000000000000000,api_hourly.requests+excluded.requests),status_1xx=min(9000000000000000,api_hourly.status_1xx+excluded.status_1xx),status_2xx=min(9000000000000000,api_hourly.status_2xx+excluded.status_2xx),status_3xx=min(9000000000000000,api_hourly.status_3xx+excluded.status_3xx),status_4xx=min(9000000000000000,api_hourly.status_4xx+excluded.status_4xx),status_5xx=min(9000000000000000,api_hourly.status_5xx+excluded.status_5xx),bytes=min(9000000000000000,api_hourly.bytes+excluded.bytes),time_ms=min(9000000000000000,api_hourly.time_ms+excluded.time_ms),capped=max(api_hourly.capped,excluded.capped)`, siteID, hour, a.requests, a.status[0], a.status[1], a.status[2], a.status[3], a.status[4], a.bytes, a.timeMS, a.capped)
	return added, err
}

func writeAPIEndpoint(ctx context.Context, tx *sql.Tx, siteID, hour int64, key apiKey, a *apiCounters) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO api_endpoints(site,hour,method,path,requests,status_1xx,status_2xx,status_3xx,status_4xx,status_5xx,bytes,time_ms) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(site,hour,method,path) DO UPDATE SET requests=min(9000000000000000,api_endpoints.requests+excluded.requests),status_1xx=min(9000000000000000,api_endpoints.status_1xx+excluded.status_1xx),status_2xx=min(9000000000000000,api_endpoints.status_2xx+excluded.status_2xx),status_3xx=min(9000000000000000,api_endpoints.status_3xx+excluded.status_3xx),status_4xx=min(9000000000000000,api_endpoints.status_4xx+excluded.status_4xx),status_5xx=min(9000000000000000,api_endpoints.status_5xx+excluded.status_5xx),bytes=min(9000000000000000,api_endpoints.bytes+excluded.bytes),time_ms=min(9000000000000000,api_endpoints.time_ms+excluded.time_ms)`, siteID, hour, key.method, key.path, a.requests, a.status[0], a.status[1], a.status[2], a.status[3], a.status[4], a.bytes, a.timeMS)
	return err
}
