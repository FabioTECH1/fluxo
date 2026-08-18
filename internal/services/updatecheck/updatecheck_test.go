package updatecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func testChecker(currentVersion, endpoint string, now func() time.Time) *Checker {
	checker := New(currentVersion)
	checker.endpoint = endpoint
	checker.now = now
	checker.successTTL = time.Hour
	checker.failureTTL = time.Minute
	return checker
}

func TestStatusReportsNewerReleaseAndCachesIt(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"0.4.19","tag":"v0.4.19","published_at":"2026-08-18T12:00:00Z","release_url":"https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.19"}`))
	}))
	defer server.Close()

	now := time.Date(2026, 8, 18, 13, 0, 0, 0, time.UTC)
	checker := testChecker("0.4.18", server.URL, func() time.Time { return now })

	first := checker.Status(context.Background())
	second := checker.Status(context.Background())
	if !first.CheckAvailable || !first.UpdateAvailable || first.LatestVersion != "0.4.19" {
		t.Fatalf("unexpected update status: %+v", first)
	}
	if second != first {
		t.Fatalf("cached status changed: first=%+v second=%+v", first, second)
	}
	if requests.Load() != 1 {
		t.Fatalf("expected one upstream request, got %d", requests.Load())
	}
}

func TestStatusDoesNotReportEqualOrOlderRelease(t *testing.T) {
	for _, latest := range []string{"0.4.18", "0.4.17"} {
		t.Run(latest, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"version":"` + latest + `","tag":"v` + latest + `","published_at":"2026-08-18T12:00:00Z","release_url":"https://github.com/FabioTECH1/fluxo/releases/tag/v` + latest + `"}`))
			}))
			defer server.Close()

			checker := testChecker("0.4.18", server.URL, time.Now)
			status := checker.Status(context.Background())
			if !status.CheckAvailable || status.UpdateAvailable {
				t.Fatalf("unexpected update status: %+v", status)
			}
		})
	}
}

func TestStatusRejectsUntrustedReleaseMetadata(t *testing.T) {
	tests := map[string]string{
		"mismatched tag": `{"version":"0.4.19","tag":"v9.9.9","published_at":"2026-08-18T12:00:00Z","release_url":"https://github.com/FabioTECH1/fluxo/releases/tag/v9.9.9"}`,
		"foreign URL":    `{"version":"0.4.19","tag":"v0.4.19","published_at":"2026-08-18T12:00:00Z","release_url":"https://example.com/releases/tag/v0.4.19"}`,
		"invalid date":   `{"version":"0.4.19","tag":"v0.4.19","published_at":"tomorrow","release_url":"https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.19"}`,
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(payload))
			}))
			defer server.Close()

			checker := testChecker("0.4.18", server.URL, time.Now)
			status := checker.Status(context.Background())
			if status.CheckAvailable || status.UpdateAvailable {
				t.Fatalf("untrusted metadata was accepted: %+v", status)
			}
		})
	}
}

func TestStatusSkipsNetworkForDevelopmentBuild(t *testing.T) {
	checker := testChecker("dev", "http://127.0.0.1:1", time.Now)
	status := checker.Status(context.Background())
	if status.CurrentVersion != "dev" || status.CheckAvailable || status.UpdateAvailable {
		t.Fatalf("unexpected development status: %+v", status)
	}
}
