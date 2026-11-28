package releasenotes

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string, status int) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

const nodeNotes = "## 2026-11-11, Version 22.23.5 'Jod' (LTS), @aduh95\nThis is a security release.\n### Notable Changes\n* (CVE-2026-123863) http2: release headers when a stream is removed\n* **deps**: update sqlite\n### Commits\n* private implementation details\n## 2026-10-01, Version 22.23.4\n### Notable Changes\n* Old release only\n"

func TestExactTargetParsers(t *testing.T) {
	n := parseNode(nodeNotes, "22.23.5")
	if !n.Security || n.Date != "2026-11-11" || len(n.Highlights) != 2 || strings.Contains(strings.Join(n.Highlights, " "), "private") {
		t.Fatal(n)
	}
	if len(parseNode(nodeNotes, "22.23.50").Highlights) != 0 {
		t.Fatal("partial version matched")
	}
	php := `<h3>Version 8.4.28</h3><p>19 Nov 2026</p><ul><li>Core:<ul><li>Fixed CVE-2026-1234 &amp; parser.</li></ul></li></ul><h3>Version 8.4.27</h3><ul><li>Older change.</li></ul>`
	p := parsePHP(php, "8.4.28")
	if p.Date != "2026-11-19" || !p.Security || len(p.Highlights) != 1 || p.Highlights[0] != "Fixed CVE-2026-1234 & parser." {
		t.Fatal(p)
	}
	if len(parsePHP(php, "8.4.2").Highlights) != 0 {
		t.Fatal("partial PHP version matched")
	}
	if parseNode(strings.ReplaceAll(nodeNotes, "This is a security release.", "ordinary release"), "22.23.4").Security {
		t.Fatal("security leaked across releases")
	}
}
func TestOfficialURLsCacheCopiesAndInvalidRequests(t *testing.T) {
	r := New()
	count := 0
	r.client.Transport = transport(func(req *http.Request) (*http.Response, error) {
		count++
		if req.URL.String() != "https://raw.githubusercontent.com/nodejs/node/v22.23.5/doc/changelogs/CHANGELOG_V22.md" {
			t.Fatal(req.URL)
		}
		return response(nodeNotes, 200), nil
	})
	n := r.Get(context.Background(), "node", "22.23.5")
	if n.State != "available" || !strings.HasPrefix(n.URL, "https://nodejs.org/en/blog/release/") {
		t.Fatal(n)
	}
	n.Highlights[0] = "changed caller"
	again := r.Get(context.Background(), "node", "22.23.5")
	if count != 1 || again.Highlights[0] == "changed caller" {
		t.Fatal("cache was altered", count)
	}
	for _, v := range []string{"../../x", "22.23", "22.23.5?url=http://localhost", "22.23.5-rc1"} {
		r.Get(context.Background(), "node", v)
	}
	r.Get(context.Background(), "custom", "22.23.5")
	if count != 1 {
		t.Fatal("untrusted input fetched")
	}
}
func TestUnavailableBoundsNegativeCacheAndCapacity(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{{"missing", "", 404}, {"oversize", strings.Repeat("x", maxBody+1), 200}, {"malformed", "<html>not release notes</html>", 200}, {"redirect", "", 302}} {
		t.Run(tc.name, func(t *testing.T) {
			r := New()
			count := 0
			r.client.Transport = transport(func(*http.Request) (*http.Response, error) {
				count++
				res := response(tc.body, tc.status)
				res.Header.Set("Location", "http://127.0.0.1/")
				return res, nil
			})
			for i := 0; i < 2; i++ {
				if n := r.Get(context.Background(), "node", "22.23.5"); n.State != "unavailable" || n.Security {
					t.Fatal(n)
				}
			}
			if count != 1 {
				t.Fatal(count)
			}
		})
	}
	r := New()
	r.client.Transport = transport(func(*http.Request) (*http.Response, error) { return response("", 404), nil })
	for i := 0; i < 40; i++ {
		r.Get(context.Background(), "php", "8.4."+stringVersion(i))
	}
	if len(r.cache) > 32 {
		t.Fatal(len(r.cache))
	}
}
func stringVersion(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}
func TestFlightsCoalesceCancellationAndBoundConcurrency(t *testing.T) {
	r := New()
	var count atomic.Int32
	entered := make(chan struct{}, 5)
	release := make(chan struct{})
	r.client.Transport = transport(func(req *http.Request) (*http.Response, error) {
		count.Add(1)
		entered <- struct{}{}
		select {
		case <-release:
			return response(nodeNotes, 200), nil
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	})
	done := make(chan Notes, 1)
	go func() { done <- r.Get(context.Background(), "node", "22.23.5") }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if n := r.Get(ctx, "node", "22.23.5"); n.State != "unavailable" {
		t.Fatal(n)
	}
	waiterCtx, waiterCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer waiterCancel()
	if n := r.Get(waiterCtx, "node", "22.23.5"); n.State != "unavailable" {
		t.Fatal(n)
	}
	for _, v := range []string{"8.4.1", "8.4.2", "8.4.3"} {
		go r.Get(context.Background(), "php", v)
		<-entered
	}
	if n := r.Get(context.Background(), "php", "8.4.4"); n.State != "unavailable" {
		t.Fatal(n)
	}
	close(release)
	if n := <-done; n.State != "available" {
		t.Fatal(n)
	}
	if count.Load() != 4 {
		t.Fatal(count.Load())
	}
}
