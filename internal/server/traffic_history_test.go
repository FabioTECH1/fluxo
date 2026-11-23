package server

import (
	"encoding/json"
	"fluxo/internal/services/traffic"
	"net/http/httptest"
	"testing"
)

func TestTrafficHistoryHTTPRangesAndRetentionMetadata(t *testing.T) {
	m := trafficTestControl(t)
	if _, err := m.setRetentionDays(t.Context(), 180); err != nil {
		t.Fatal(err)
	}
	s := &Server{traffic: m}
	for _, period := range []string{"today", "7d", "30d", "60d", "90d", "180d", "365d", "all"} {
		r := httptest.NewRequest("GET", "/api/v1/sites/1/traffic-analytics?range="+period, nil)
		r.SetPathValue("id", "1")
		w := httptest.NewRecorder()
		s.handleSiteTrafficAnalytics()(w, r)
		var report traffic.Report
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &report) != nil {
			t.Fatalf("%s: %d %s", period, w.Code, w.Body.String())
		}
		if report.RetentionDays != 180 || report.Range != traffic.Range(period) || report.AvailableFrom != nil || report.BucketSeconds < 3600 || len(report.Buckets) > traffic.MaxReportBuckets {
			t.Fatalf("%s: %+v", period, report)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cached history")
		}
	}
	for _, period := range []string{"31d", "forever", "90D", "0", "-1"} {
		r := httptest.NewRequest("GET", "/api/v1/sites/1/traffic-analytics?range="+period, nil)
		r.SetPathValue("id", "1")
		w := httptest.NewRecorder()
		s.handleSiteTrafficAnalytics()(w, r)
		if w.Code != 400 {
			t.Fatalf("%s: %d", period, w.Code)
		}
	}
}
