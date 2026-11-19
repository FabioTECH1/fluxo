package traffic

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func recordLine(now time.Time, generation, p string, status int) []byte {
	r := record{Version: 1, Generation: generation, Timestamp: float64(now.Unix()) + .125, Method: "GET", Path: p, Status: status, Bytes: 120, RequestTime: .25, HTML: 1, Referrer: "https://Example.com/private?q=secret#fragment"}
	b, _ := json.Marshal(r)
	return append(b, '\n')
}

func TestParserSanitizesAndEstimates(t *testing.T) {
	now := time.Now().UTC()
	line := recordLine(now, "g1", "/pricing?email=private#secret", 200)
	r, err := parseRecord(line, "g1", now.Add(-time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Path != "/pricing" || r.Referrer != "example.com" || !r.pageview {
		t.Fatalf("unexpected sanitized record: %+v", r)
	}
	for _, p := range []string{"/api/users", "/api", "/app.js", "/assets/a", "/photo.png", "/graphql"} {
		r, err = parseRecord(recordLine(now, "g1", p, 200), "g1", now.Add(-time.Hour), now)
		if err != nil || r.pageview {
			t.Fatalf("%q pageview=%v error=%v", p, r.pageview, err)
		}
	}
	for _, edit := range []func(*record){func(r *record) { r.Bot = 1 }, func(r *record) { r.Status = 404 }, func(r *record) { r.Method = "POST" }} {
		var r record
		_ = json.Unmarshal(line, &r)
		edit(&r)
		b, _ := json.Marshal(r)
		parsed, e := parseRecord(b, "g1", now.Add(-time.Hour), now)
		if e != nil || parsed.pageview {
			t.Fatalf("unexpected pageview=%v err=%v", parsed.pageview, e)
		}
	}
	for _, ref := range []string{"192.0.2.1", "http://192.0.2.1/path", "https://user:secret@example.com/path?x=y", "evil.example/path?x=private", "example.com:443", "-", "https://[::1]/"} {
		got := sanitizeReferrer(ref)
		if strings.ContainsAny(got, "/?#@:") || strings.Contains(got, "secret") {
			t.Fatalf("unsafe referrer %q => %q", ref, got)
		}
	}
}
func TestParserRejectsMalformedAndBoundsValues(t *testing.T) {
	now := time.Now()
	valid := recordLine(now, "g", "/", 200)
	for _, line := range [][]byte{[]byte("{}"), append(append([]byte{}, valid...), []byte("{}")...), append(append([]byte{}, valid...), []byte("garbage")...), []byte(strings.Repeat("x", MaxLineBytes+1)), recordLine(now, "other", "/", 200), recordLine(now, "g", "//evil", 200), recordLine(now, "g", "/", 999), recordLine(now.Add(6*time.Minute), "g", "/", 200), recordLine(now.Add(-31*24*time.Hour), "g", "/", 200)} {
		if _, err := parseRecord(line, "g", now.Add(-32*24*time.Hour), now); err == nil {
			t.Fatalf("accepted malformed input %q", line[:min(len(line), 100)])
		}
	}
	r, e := parseRecord(recordLine(now, "g", "/"+strings.Repeat("a", 1000)+"?secret=x", 200), "g", now.Add(-time.Hour), now)
	if e != nil || len(r.Path) > MaxPathBytes || strings.Contains(r.Path, "secret") {
		t.Fatalf("path bound failed: %q %v", r.Path, e)
	}
}
