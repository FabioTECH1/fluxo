// Package releasenotes retrieves optional, target-release context from official
// upstream sources. It has no package-manager or service-control authority.
package releasenotes

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

const maxBody = 2 << 20

var versionPattern = regexp.MustCompile(`^[1-9][0-9]?\.[0-9]{1,3}\.[0-9]{1,3}$`)
var markdownLink = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
var cve = regexp.MustCompile(`CVE-[0-9]{4}-[0-9]+`)

type Notes struct {
	Runtime    string   `json:"runtime"`
	Version    string   `json:"version"`
	State      string   `json:"state"`
	Date       string   `json:"date,omitempty"`
	Security   bool     `json:"security"`
	Highlights []string `json:"highlights"`
	URL        string   `json:"url,omitempty"`
	Detail     string   `json:"detail,omitempty"`
}
type entry struct {
	notes   Notes
	expires time.Time
}
type flight struct {
	done  chan struct{}
	notes Notes
}
type Reader struct {
	mu       sync.Mutex
	cache    map[string]entry
	client   *http.Client
	inflight map[string]*flight
}

func New() *Reader {
	return &Reader{cache: make(map[string]entry), inflight: make(map[string]*flight), client: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func Valid(runtime, version string) bool {
	return (runtime == "php" || runtime == "node") && versionPattern.MatchString(version)
}
func unavailable(runtime, version string) Notes {
	return Notes{Runtime: runtime, Version: version, State: "unavailable", Highlights: []string{}, Detail: "Release highlights are unavailable. Exact package versions remain available below."}
}

// Get only fetches on explicit detail requests. Concurrent reads of the same
// target share a bounded flight; waiting callers retain their cancellation.
func (r *Reader) Get(ctx context.Context, runtime, version string) Notes {
	if !Valid(runtime, version) || ctx.Err() != nil {
		return unavailable(runtime, version)
	}
	key := runtime + ":" + version
	r.mu.Lock()
	if e, ok := r.cache[key]; ok && time.Now().Before(e.expires) {
		r.mu.Unlock()
		return copyNotes(e.notes)
	}
	if f, ok := r.inflight[key]; ok {
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return unavailable(runtime, version)
		case <-f.done:
			return copyNotes(f.notes)
		}
	}
	if len(r.inflight) >= 4 {
		r.mu.Unlock()
		return unavailable(runtime, version)
	}
	f := &flight{done: make(chan struct{})}
	r.inflight[key] = f
	r.mu.Unlock()
	n := r.fetch(ctx, runtime, version)
	ttl := 5 * time.Minute
	if n.State == "available" {
		ttl = 24 * time.Hour
	}
	r.mu.Lock()
	if len(r.cache) >= 32 {
		var oldest string
		var at time.Time
		for k, e := range r.cache {
			if oldest == "" || e.expires.Before(at) {
				oldest = k
				at = e.expires
			}
		}
		delete(r.cache, oldest)
	}
	if ctx.Err() == nil {
		r.cache[key] = entry{copyNotes(n), time.Now().Add(ttl)}
	}
	f.notes = copyNotes(n)
	delete(r.inflight, key)
	close(f.done)
	r.mu.Unlock()
	return n
}
func copyNotes(n Notes) Notes { n.Highlights = append([]string{}, n.Highlights...); return n }
func (r *Reader) fetch(ctx context.Context, runtime, version string) Notes {
	n := unavailable(runtime, version)
	major := strings.Split(version, ".")[0]
	source := "https://www.php.net/ChangeLog-" + major + ".php"
	n.URL = source + "#" + version
	if runtime == "node" {
		source = "https://raw.githubusercontent.com/nodejs/node/v" + version + "/doc/changelogs/CHANGELOG_V" + major + ".md"
		n.URL = "https://nodejs.org/en/blog/release/v" + version
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err == nil {
		req.Header.Set("User-Agent", "Fluxo-release-notes")
		var res *http.Response
		res, err = r.client.Do(req)
		if err == nil {
			if res.StatusCode == http.StatusOK {
				body, readErr := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
				if readErr == nil && len(body) <= maxBody {
					var parsed Notes
					if runtime == "php" {
						parsed = parsePHP(string(body), version)
					} else {
						parsed = parseNode(string(body), version)
					}
					if len(parsed.Highlights) > 0 {
						n.State = "available"
						n.Detail = ""
						n.Date = parsed.Date
						n.Security = parsed.Security
						n.Highlights = parsed.Highlights
					}
				}
			}
			res.Body.Close()
		}
	}
	return n
}
func clean(s string) string {
	s = markdownLink.ReplaceAllString(s, "$1")
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "`", "")
	return strings.Join(strings.Fields(s), " ")
}
func short(s string) string {
	r := []rune(clean(s))
	if len(r) > 320 {
		return string(r[:317]) + "…"
	}
	return string(r)
}
func parseNode(body, version string) Notes {
	n := Notes{Highlights: []string{}}
	lines := strings.Split(body, "\n")
	found := false
	notable := false
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			if found {
				break
			}
			if strings.Contains(line, ", Version "+version+" ") || strings.HasSuffix(line, ", Version "+version) {
				found = true
				date := strings.TrimPrefix(line, "## ")
				if len(date) >= 10 {
					if _, err := time.Parse("2006-01-02", date[:10]); err == nil {
						n.Date = date[:10]
					}
				}
			}
			continue
		}
		if !found {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(line), "This is a security release.") {
			n.Security = true
		}
		if strings.HasPrefix(line, "### ") {
			notable = strings.TrimSpace(line) == "### Notable Changes"
			continue
		}
		if notable && (strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "- ")) {
			text := clean(line[2:])
			if cve.MatchString(text) {
				n.Security = true
			}
			if len(n.Highlights) < 6 {
				n.Highlights = append(n.Highlights, short(text))
			}
		}
	}
	return n
}
func textOf(n *html.Node) string {
	var s strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			s.WriteString(n.Data)
			s.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return clean(s.String())
}
func parsePHP(body, version string) Notes {
	n := Notes{Highlights: []string{}}
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return n
	}
	found, done := false, false
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if done {
			return
		}
		if node.Type == html.ElementNode && node.Data == "h3" {
			if found {
				done = true
				return
			}
			if textOf(node) == "Version "+version {
				found = true
			}
		}
		if found && node.Type == html.ElementNode {
			if node.Data == "time" || node.Data == "p" {
				date := textOf(node)
				for _, format := range []string{"02 Jan 2006", "2 Jan 2006", "2006-01-02"} {
					if t, e := time.Parse(format, date); e == nil {
						n.Date = t.Format("2006-01-02")
					}
				}
			}
			if node.Data == "li" {
				// PHP groups changes by component; collect only leaf bullet items.
				leaf := true
				for c := node.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.ElementNode && (c.Data == "ul" || c.Data == "ol") {
						leaf = false
					}
				}
				if leaf {
					text := textOf(node)
					if cve.MatchString(text) {
						n.Security = true
					}
					if text != "" && len(n.Highlights) < 6 {
						n.Highlights = append(n.Highlights, short(text))
					}
				}
			}
		}
		for c := node.FirstChild; c != nil && !done; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return n
}
