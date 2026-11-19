package traffic

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// All tables are additive and live in the dedicated analytics database. There
// are no per-request rows. Endpoint rows have their own global 50,000-row budget
// in addition to the existing page/referrer budget and the SQLite file ceiling.
const apiSchema = `
CREATE TABLE IF NOT EXISTS api_meta(id INTEGER PRIMARY KEY CHECK(id=1),support_ns INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS api_settings(site INTEGER PRIMARY KEY,prefixes TEXT NOT NULL,routes TEXT NOT NULL,effective_ns INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS api_rules(site INTEGER NOT NULL,start_ns INTEGER NOT NULL,stop_ns INTEGER NOT NULL DEFAULT 0,prefixes TEXT NOT NULL,routes TEXT NOT NULL,PRIMARY KEY(site,start_ns));
CREATE INDEX IF NOT EXISTS api_rules_retention ON api_rules(stop_ns);
CREATE TABLE IF NOT EXISTS api_hourly(site INTEGER NOT NULL,hour INTEGER NOT NULL,requests INTEGER NOT NULL,status_1xx INTEGER NOT NULL,status_2xx INTEGER NOT NULL,status_3xx INTEGER NOT NULL,status_4xx INTEGER NOT NULL,status_5xx INTEGER NOT NULL,bytes INTEGER NOT NULL,time_ms REAL NOT NULL,capped INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(site,hour));
CREATE INDEX IF NOT EXISTS api_hourly_retention ON api_hourly(hour);
CREATE TABLE IF NOT EXISTS api_endpoints(site INTEGER NOT NULL,hour INTEGER NOT NULL,method TEXT NOT NULL,path TEXT NOT NULL,requests INTEGER NOT NULL,status_1xx INTEGER NOT NULL,status_2xx INTEGER NOT NULL,status_3xx INTEGER NOT NULL,status_4xx INTEGER NOT NULL,status_5xx INTEGER NOT NULL,bytes INTEGER NOT NULL,time_ms REAL NOT NULL,PRIMARY KEY(site,hour,method,path));
CREATE INDEX IF NOT EXISTS api_endpoints_retention ON api_endpoints(hour);`

type apiRule struct {
	start, stop int64
	classifier  apiClassifier
}

func normalizedAPISettings(settings APISettings) APISettings {
	return APISettings{Prefixes: append([]string{}, settings.Prefixes...), Routes: append([]string{}, settings.Routes...)}
}

// APISettings is a lazy read; viewing an unconfigured or disabled site creates
// neither files nor site/source records.
func (s *Store) APISettings(ctx context.Context, siteID int64) (APISettingsState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.apiSettingsLocked(ctx, siteID)
}

// PruneAPISettings accepts the complete authoritative set of living panel site
// IDs. Only settings for removed sites are discarded; disabled living settings,
// historical rules and aggregates remain intact. Call on normal reconciliation
// so failed deletion cleanup can be retried and cannot permanently exhaust the
// settings capacity. An empty set means that no panel sites remain.
func (s *Store) PruneAPISettings(ctx context.Context, validSiteIDs []int64) error {
	valid := make(map[int64]bool, len(validSiteIDs))
	for _, id := range validSiteIDs {
		if id <= 0 {
			return errors.New("invalid living traffic site ID")
		}
		valid[id] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, "SELECT site FROM api_settings LIMIT ?", MaxRetainedSites)
	if err != nil {
		return err
	}
	removed := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		if !valid[id] {
			removed = append(removed, id)
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil || len(removed) == 0 {
		return err
	}
	if err = s.boundWALLocked(ctx); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range removed {
		if _, err = tx.ExecContext(ctx, "DELETE FROM api_settings WHERE site=?", id); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	_, _ = s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	return nil
}

func (s *Store) apiSettingsLocked(ctx context.Context, siteID int64) (APISettingsState, error) {
	out := APISettingsState{Settings: DefaultAPISettings()}
	if s.db == nil {
		return out, nil
	}
	var prefixes, routes string
	var ns int64
	err := s.db.QueryRowContext(ctx, "SELECT prefixes,routes,effective_ns FROM api_settings WHERE site=?", siteID).Scan(&prefixes, &routes, &ns)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal([]byte(prefixes), &out.Settings.Prefixes); err != nil {
		return out, err
	}
	if err = json.Unmarshal([]byte(routes), &out.Settings.Routes); err != nil {
		return out, err
	}
	out.Settings = normalizedAPISettings(out.Settings)
	at := time.Unix(0, ns).UTC()
	out.EffectiveFrom = &at
	return out, nil
}

func (s *Store) ensureAPIRule(ctx context.Context, tx *sql.Tx, siteID, floor int64) error {
	var present int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM api_rules WHERE site=? AND stop_ns=0", siteID).Scan(&present); err != nil {
		return err
	}
	if present > 0 {
		return nil
	}
	prefixes, _ := json.Marshal(DefaultAPISettings().Prefixes)
	routes := []byte("[]")
	var effective int64
	err := tx.QueryRowContext(ctx, "SELECT prefixes,routes,effective_ns FROM api_settings WHERE site=?", siteID).Scan(&prefixes, &routes, &effective)
	if errors.Is(err, sql.ErrNoRows) {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM api_settings").Scan(&count); err != nil {
			return err
		}
		if count >= MaxRetainedSites {
			return fmt.Errorf("%w: at most %d retained site settings", ErrAPISettingsCapacity, MaxRetainedSites)
		}
		effective = floor
		if _, err = tx.ExecContext(ctx, "INSERT INTO api_settings(site,prefixes,routes,effective_ns) VALUES(?,?,?,?)", siteID, string(prefixes), string(routes), effective); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO api_rules(site,start_ns,prefixes,routes) VALUES(?,?,?,?)", siteID, max(floor, effective), string(prefixes), string(routes))
	return err
}

// SetAPISettings changes classification forward only, without enabling logging
// or opening any log. Keeping historical rules lets delayed batches use the
// exact settings active at the event timestamp after a restart or rotation.
func (c *Collector) SetAPISettings(ctx context.Context, siteID int64, settings APISettings) (APISettingsState, error) {
	return c.setAPISettingsAt(ctx, siteID, settings, time.Now().UTC())
}

func (c *Collector) setAPISettingsAt(ctx context.Context, siteID int64, settings APISettings, now time.Time) (APISettingsState, error) {
	if siteID <= 0 || now.IsZero() {
		return APISettingsState{}, errors.New("invalid API settings site or effective time")
	}
	if err := ValidateAPISettings(settings); err != nil {
		return APISettingsState{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.store
	s.mu.Lock()
	defer s.mu.Unlock()
	settings = normalizedAPISettings(settings)
	current, err := s.apiSettingsLocked(ctx, siteID)
	if err != nil {
		return current, err
	}
	prefixes, _ := json.Marshal(settings.Prefixes)
	routes, _ := json.Marshal(settings.Routes)
	oldPrefixes, _ := json.Marshal(current.Settings.Prefixes)
	oldRoutes, _ := json.Marshal(current.Settings.Routes)
	if current.EffectiveFrom != nil && string(prefixes) == string(oldPrefixes) && string(routes) == string(oldRoutes) {
		return current, nil
	}
	if err = s.ensureLocked(); err != nil {
		return APISettingsState{}, err
	}
	if err = s.boundWALLocked(ctx); err != nil {
		return APISettingsState{}, err
	}
	ns := max(now.UnixNano(), s.apiSupportNS)
	if current.EffectiveFrom != nil && ns <= current.EffectiveFrom.UnixNano() {
		return APISettingsState{}, errors.New("API settings effective time must advance")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return APISettingsState{}, err
	}
	defer tx.Rollback()
	var count int
	if current.EffectiveFrom == nil {
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM api_settings").Scan(&count); err != nil {
			return APISettingsState{}, err
		}
		if count >= MaxRetainedSites {
			return APISettingsState{}, fmt.Errorf("%w: at most %d retained site settings", ErrAPISettingsCapacity, MaxRetainedSites)
		}
	}
	// Delete only expired closed rules. The parser independently refuses requests
	// older than retention, so no collectible backlog can need these rules.
	if _, err = tx.ExecContext(ctx, "DELETE FROM api_rules WHERE site=? AND stop_ns>0 AND stop_ns<?", siteID, now.Add(-retention).UnixNano()); err != nil {
		return APISettingsState{}, err
	}
	var retainedBytes int64
	if err = tx.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(length(CAST(prefixes AS BLOB))+length(CAST(routes AS BLOB))),0) FROM api_rules WHERE site=?", siteID).Scan(&count, &retainedBytes); err != nil {
		return APISettingsState{}, err
	}
	if count >= MaxAPIRuleIntervals {
		return APISettingsState{}, fmt.Errorf("%w: at most %d rule intervals per site; retry after old intervals expire", ErrAPISettingsCapacity, MaxAPIRuleIntervals)
	}
	if retainedBytes+int64(len(prefixes)+len(routes)) > MaxAPIRuleBytesPerSite {
		return APISettingsState{}, fmt.Errorf("%w: at most %d KiB of retained rule settings per site; retry after old intervals expire", ErrAPISettingsCapacity, MaxAPIRuleBytesPerSite>>10)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE api_rules SET stop_ns=? WHERE site=? AND stop_ns=0", ns, siteID); err != nil {
		return APISettingsState{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO api_settings(site,prefixes,routes,effective_ns) VALUES(?,?,?,?) ON CONFLICT(site) DO UPDATE SET prefixes=excluded.prefixes,routes=excluded.routes,effective_ns=excluded.effective_ns", siteID, string(prefixes), string(routes), ns); err != nil {
		return APISettingsState{}, err
	}
	// An explicit off-site change stores settings, without creating collection
	// intervals. Existing historical rules are retained if this site had coverage.
	if count > 0 || c.sources[siteID] != nil {
		if _, err = tx.ExecContext(ctx, "INSERT INTO api_rules(site,start_ns,prefixes,routes) VALUES(?,?,?,?)", siteID, ns, string(prefixes), string(routes)); err != nil {
			return APISettingsState{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return APISettingsState{}, err
	}
	if source := c.sources[siteID]; source != nil {
		source.apiRulesLoaded = false
	}
	_, _ = s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	at := time.Unix(0, ns).UTC()
	return APISettingsState{Settings: settings, EffectiveFrom: &at}, nil
}

func (s *Store) loadAPIRules(ctx context.Context, siteID int64) ([]apiRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.apiRulesLocked(ctx, siteID)
}

func (s *Store) apiRulesLocked(ctx context.Context, siteID int64) ([]apiRule, error) {
	out := []apiRule{}
	if s.db == nil {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx, "SELECT start_ns,stop_ns,prefixes,routes FROM api_rules WHERE site=? ORDER BY start_ns LIMIT ?", siteID, MaxAPIRuleIntervals)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rule apiRule
		var prefixes, routes string
		var settings APISettings
		if err = rows.Scan(&rule.start, &rule.stop, &prefixes, &routes); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(prefixes), &settings.Prefixes); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(routes), &settings.Routes); err != nil {
			return nil, err
		}
		if rule.classifier, err = compileAPISettings(settings); err != nil {
			return nil, err
		}
		out = append(out, rule)
	}
	return out, rows.Err()
}

func classifierAt(rules []apiRule, timestamp float64) *apiClassifier {
	ns := int64(timestamp * 1e9)
	i := sort.Search(len(rules), func(i int) bool { return rules[i].start > ns }) - 1
	if i < 0 || rules[i].stop > 0 && ns >= rules[i].stop {
		return nil
	}
	return &rules[i].classifier
}
