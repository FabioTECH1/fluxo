package traffic

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	MaxLineBytes     = 4096
	MaxPathBytes     = 512
	MaxReferrerBytes = 253
)

// record exists only for the current bounded batch; no request events are stored.
type record struct {
	Version     int     `json:"v"`
	Generation  string  `json:"generation"`
	Timestamp   float64 `json:"ts"`
	Method      string  `json:"method"`
	Path        string  `json:"path"`
	Status      int     `json:"status"`
	Bytes       int64   `json:"bytes"`
	RequestTime float64 `json:"request_time"`
	Bot         int     `json:"bot"`
	HTML        int     `json:"html"`
	Referrer    string  `json:"referrer"`
	pageview    bool
	hour        int64
}

var errRecord = errors.New("invalid traffic record")
var errOutsideWindow = errors.New("traffic record outside collection window")

// start includes the generation, delete cutoff and current retention floor.
func parseRecord(line []byte, generation string, start, now time.Time) (record, error) {
	var r record
	if len(line) == 0 || len(line) > MaxLineBytes {
		return r, errRecord
	}
	dec := json.NewDecoder(bytes.NewReader(line))
	if err := dec.Decode(&r); err != nil {
		return r, errRecord
	}
	// Reject extra JSON and trailing non-whitespace, while allowing forward-compatible fields.
	if err := dec.Decode(new(any)); err != io.EOF {
		return r, errRecord
	}
	if r.Version != 1 || r.Generation != generation || len(r.Generation) > 64 || r.Status < 100 || r.Status > 599 || r.Bytes < 0 || r.Bytes > 1<<40 || math.IsNaN(r.Timestamp) || math.IsInf(r.Timestamp, 0) || math.IsNaN(r.RequestTime) || math.IsInf(r.RequestTime, 0) || r.RequestTime < 0 || r.RequestTime > 3600 || (r.Bot != 0 && r.Bot != 1) || (r.HTML != 0 && r.HTML != 1) {
		return r, errRecord
	}
	if !validHTTPMethod(r.Method) {
		return r, errRecord
	}
	ts := time.Unix(int64(r.Timestamp), int64((r.Timestamp-math.Floor(r.Timestamp))*1e9))
	if ts.Before(start) {
		return r, errOutsideWindow
	}
	if ts.After(now.Add(5 * time.Minute)) {
		return r, errRecord
	}
	r.Path = sanitizePath(r.Path)
	if r.Path == "" {
		return r, errRecord
	}
	r.Referrer = sanitizeReferrer(r.Referrer)
	r.pageview = r.Method == "GET" && r.Status >= 200 && r.Status < 400 && r.Bot == 0 && !isAssetOrAPI(r.Path) && (r.HTML == 1 || path.Ext(r.Path) == "" || strings.EqualFold(path.Ext(r.Path), ".html") || strings.EqualFold(path.Ext(r.Path), ".php"))
	r.hour = ts.UTC().Truncate(time.Hour).Unix()
	return r, nil
}

// HTTP methods are case-sensitive ASCII tokens. Unknown tokens are counted in
// the fixed OTHER API group, without retaining arbitrary method dimensions.
func validHTTPMethod(method string) bool {
	if len(method) == 0 || len(method) > 16 {
		return false
	}
	for i := 0; i < len(method); i++ {
		c := method[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c))) {
			return false
		}
	}
	return true
}

func sanitizePath(v string) string {
	if i := strings.IndexAny(v, "?#"); i >= 0 {
		v = v[:i]
	}
	if v == "" || v[0] != '/' || strings.HasPrefix(v, "//") || strings.IndexFunc(v, unicode.IsControl) >= 0 {
		return ""
	}
	// Bound dimension storage without retaining request query strings or fragments.
	if len(v) > MaxPathBytes {
		v = v[:MaxPathBytes-len("…")]
		for !utf8.ValidString(v) {
			v = v[:len(v)-1]
		}
		v += "…"
	}
	return v
}

func sanitizeReferrer(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" || v == "-" {
		return ""
	}
	// Tolerate an unsanitized producer defensively; keep hostname only, never URL details.
	if strings.Contains(v, "://") {
		u, e := url.Parse(v)
		if e != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return ""
		}
		v = u.Hostname()
	} else {
		if i := strings.IndexAny(v, "/?#"); i >= 0 {
			v = v[:i]
		}
		if strings.Contains(v, "@") {
			return ""
		}
		if host, _, e := net.SplitHostPort(v); e == nil {
			v = host
		}
	}
	v = strings.TrimSuffix(v, ".")
	// IP addresses are deliberately not a referring-domain dimension.
	if v == "" || len(v) > MaxReferrerBytes || net.ParseIP(strings.Trim(v, "[]")) != nil || !strings.Contains(v, ".") {
		return ""
	}
	for _, label := range strings.Split(v, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return ""
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return ""
			}
		}
	}
	return v
}

func isAssetOrAPI(v string) bool {
	p := strings.ToLower(v)
	for _, prefix := range []string{"/api/", "/api", "/graphql"} {
		if p == prefix || strings.HasSuffix(prefix, "/") && strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return isAsset(v)
}

func isAsset(v string) bool {
	p := strings.ToLower(v)
	for _, prefix := range []string{"/assets/", "/static/", "/_next/", "/_nuxt/", "/.well-known/"} {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	switch path.Ext(p) {
	case ".js", ".mjs", ".css", ".map", ".json", ".xml", ".txt", ".ico", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".svg", ".woff", ".woff2", ".ttf", ".eot", ".mp4", ".webm", ".mp3", ".pdf", ".zip", ".gz":
		return true
	}
	return false
}

func estimatePageView(r record, api bool) bool {
	ext := path.Ext(r.Path)
	return r.Method == "GET" && r.Status >= 200 && r.Status < 400 && r.Bot == 0 && !api && !isAssetOrAPI(r.Path) && (r.HTML == 1 || ext == "" || strings.EqualFold(ext, ".html") || strings.EqualFold(ext, ".php"))
}
