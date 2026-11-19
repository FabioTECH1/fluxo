// Package traffic collects bounded origin-request analytics independently of the
// panel database. It does not identify visitors or execute application code.
package traffic

import "time"

type Range string

const (
	RangeToday  Range = "today"
	Range7Days  Range = "7d"
	Range30Days Range = "30d"
)

// Source identifies a single successfully activated logging interval. A new
// generation is required on re-enable; old files never become current again.
type Source struct {
	SiteID     int64     `json:"site_id"`
	Generation string    `json:"generation"`
	Path       string    `json:"path"`
	StartTime  time.Time `json:"start_time"`
}

type Metrics struct {
	Requests         int64   `json:"requests"`
	PageViews        int64   `json:"page_views"`
	NotFound         int64   `json:"not_found"`
	Other4xx         int64   `json:"other_4xx"`
	ServerErrors     int64   `json:"server_errors"`
	ResponseBytes    int64   `json:"response_bytes"`
	AvgRequestTimeMS float64 `json:"avg_request_time_ms"`
}

type SiteStatus struct {
	SiteID           int64      `json:"site_id"`
	Domain           string     `json:"domain"`
	Enabled          bool       `json:"enabled"`
	EffectiveEnabled bool       `json:"effective_enabled"`
	Status           string     `json:"status"`
	Pending          bool       `json:"pending"`
	Error            string     `json:"error,omitempty"`
	LastProcessedAt  *time.Time `json:"last_processed_at"`
	LastCheckedAt    *time.Time `json:"last_checked_at"`
	BacklogBytes     int64      `json:"backlog_bytes"`
	Incomplete       bool       `json:"incomplete"`
	DroppedBytes     int64      `json:"dropped_bytes"`
}

type State struct {
	MasterEnabled    bool         `json:"master_enabled"`
	Pending          bool         `json:"pending"`
	Status           string       `json:"status"`
	Error            string       `json:"error,omitempty"`
	RetentionDays    int          `json:"retention_days"`
	FreshnessSeconds int          `json:"freshness_seconds"`
	Sites            []SiteStatus `json:"sites"`
}

type Bucket struct {
	Hour       time.Time `json:"hour"`
	Metrics    *Metrics  `json:"metrics"`
	Collection string    `json:"collection"`
	Incomplete bool      `json:"incomplete"`
}

type Rank struct {
	Value    string `json:"value"`
	Requests int64  `json:"requests"`
}

type Gap struct {
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
	Reason string    `json:"reason"`
}

type Report struct {
	Range         Range      `json:"range"`
	From          time.Time  `json:"from"`
	To            time.Time  `json:"to"`
	MasterEnabled bool       `json:"master_enabled"`
	Site          SiteStatus `json:"site"`
	Totals        Metrics    `json:"totals"`
	Buckets       []Bucket   `json:"buckets"`
	TopPaths      []Rank     `json:"top_paths"`
	Referrers     []Rank     `json:"referrers"`
	Gaps          []Gap      `json:"gaps"`
	Notes         []string   `json:"notes"`
	API           APIReport  `json:"api"`
}

type APISettings struct {
	Prefixes []string `json:"prefixes"`
	Routes   []string `json:"routes"`
}

type APISettingsState struct {
	Settings      APISettings `json:"settings"`
	EffectiveFrom *time.Time  `json:"effective_from"`
}

type APIMetrics struct {
	Requests         int64   `json:"requests"`
	Status1xx        int64   `json:"status_1xx"`
	Status2xx        int64   `json:"status_2xx"`
	Status3xx        int64   `json:"status_3xx"`
	Status4xx        int64   `json:"status_4xx"`
	Status5xx        int64   `json:"status_5xx"`
	ResponseBytes    int64   `json:"response_bytes"`
	AvgRequestTimeMS float64 `json:"avg_request_time_ms"`
}

type APIBucket struct {
	Hour       time.Time   `json:"hour"`
	Metrics    *APIMetrics `json:"metrics"`
	Collection string      `json:"collection"`
	Incomplete bool        `json:"incomplete"`
}

type APIEndpoint struct {
	Method  string     `json:"method"`
	Path    string     `json:"path"`
	Metrics APIMetrics `json:"metrics"`
}

type APIReport struct {
	Settings      APISettings   `json:"settings"`
	EffectiveFrom *time.Time    `json:"effective_from"`
	AvailableFrom *time.Time    `json:"available_from"`
	Totals        APIMetrics    `json:"totals"`
	Buckets       []APIBucket   `json:"buckets"`
	Endpoints     []APIEndpoint `json:"endpoints"`
	Gaps          []Gap         `json:"gaps"`
	Notes         []string      `json:"notes"`
	Incomplete    bool          `json:"incomplete"`
}
