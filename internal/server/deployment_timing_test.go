package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"fluxo/internal/database"
)

func TestDeploymentTimingAPIAndDismissalPreserveAttemptBoundaries(t *testing.T) {
	previous := database.DB
	if err := database.InitBaseDB(t.TempDir() + "/fluxo.db"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.DB.Close(); database.DB = previous })
	server := &Server{}
	siteID := insertDeploymentTestSite(t, "timing-api.test")
	historical := insertDeployment(t, siteID, "success", "")
	syncID := insertDeploymentWithSource(t, siteID, "success", "", "repo_sync")
	failed := insertDeployment(t, siteID, "failed", "health check failed")
	pending := insertDeployment(t, siteID, "pending", "")
	started := time.Date(2026, 11, 20, 12, 0, 0, 125000000, time.UTC)
	finished := started.Add(67*time.Second + 250*time.Millisecond)
	if _, err := database.DB.Exec("UPDATE deployments SET started_at=?,finished_at=? WHERE id=?", started, finished, failed); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/sites/1/deployments", nil)
	request.SetPathValue("id", strconv.Itoa(siteID))
	response := httptest.NewRecorder()
	server.handleListDeployments().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	var raw struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, row := range raw.Data {
		var id int
		if err := json.Unmarshal(row["id"], &id); err != nil {
			t.Fatal(err)
		}
		if id != failed {
			if string(row["started_at"]) != "null" || string(row["finished_at"]) != "null" {
				t.Fatal("missing boundaries must be explicit null, not inferred or omitted", row)
			}
		} else if string(row["started_at"]) != `"2026-11-20T12:00:00.125Z"` || string(row["finished_at"]) != `"2026-11-20T12:01:07.375Z"` {
			t.Fatal("execution boundaries are not UTC RFC3339", row)
		}
	}
	page := listDeploymentsForTest(t, server, siteID)
	if page.UnresolvedFailure == nil || page.UnresolvedFailure.StartedAt == nil || page.UnresolvedFailure.FinishedAt == nil || !page.UnresolvedFailure.StartedAt.Equal(started) || !page.UnresolvedFailure.FinishedAt.Equal(finished) {
		t.Fatal("unresolved failure lost execution boundaries", page.UnresolvedFailure)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/sites/1/deployments/1/dismiss", nil)
	request.SetPathValue("id", strconv.Itoa(siteID))
	request.SetPathValue("depId", strconv.Itoa(failed))
	response = httptest.NewRecorder()
	server.handleDismissDeploymentFailure().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatal(response.Code, response.Body.String())
	}
	page = listDeploymentsForTest(t, server, siteID)
	if len(page.Data) != 4 || page.UnresolvedFailure != nil {
		t.Fatal("dismissal changed history", page)
	}
	for _, row := range page.Data {
		switch row.ID {
		case failed:
			if row.StartedAt == nil || row.FinishedAt == nil || !row.StartedAt.Equal(started) || !row.FinishedAt.Equal(finished) {
				t.Fatal("dismissal changed attempt boundaries", row)
			}
		case historical, syncID, pending:
			if row.StartedAt != nil || row.FinishedAt != nil {
				t.Fatal("another attempt inherited execution boundaries", row)
			}
		}
	}
}

func TestDeploymentTimingDetailRefreshIsScopedAndIndependentOfPagination(t *testing.T) {
	previous := database.DB
	if err := database.InitBaseDB(t.TempDir() + "/fluxo.db"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.DB.Close(); database.DB = previous })
	server := &Server{}
	siteID := insertDeploymentTestSite(t, "detail.test")
	otherSite := insertDeploymentTestSite(t, "other-detail.test")
	target := insertDeployment(t, siteID, "running", "")
	start := time.Now().UTC().Add(-67 * time.Second)
	finish := start.Add(67 * time.Second)
	if _, err := database.DB.Exec("UPDATE deployments SET started_at=? WHERE id=?", start, target); err != nil {
		t.Fatal(err)
	}
	for range 13 {
		insertDeployment(t, siteID, "pending", "")
	}
	page := listDeploymentsForTest(t, server, siteID)
	for _, row := range page.Data {
		if row.ID == target {
			t.Fatal("fixture target stayed on first page")
		}
	}
	get := func(site, dep, query string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sites/1/deployments/1"+query, nil)
		req.SetPathValue("id", site)
		req.SetPathValue("depId", dep)
		w := httptest.NewRecorder()
		server.handleGetDeployment()(w, req)
		return w
	}
	response := get(strconv.Itoa(siteID), strconv.Itoa(target), "")
	var row database.Deployment
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &row) != nil || row.Status != "running" || row.StartedAt == nil || row.FinishedAt != nil {
		t.Fatal("running detail unavailable", response.Code, response.Body.String())
	}
	if _, err := database.DB.Exec("UPDATE deployments SET status='success',finished_at=? WHERE id=?", finish, target); err != nil {
		t.Fatal(err)
	}
	response = get(strconv.Itoa(siteID), strconv.Itoa(target), "")
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &row) != nil || row.Status != "success" || row.FinishedAt == nil || !row.FinishedAt.Equal(finish) || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("terminal detail stale", response.Code, response.Body.String())
	}
	for _, tc := range []struct {
		site, dep, query string
		code             int
	}{
		{strconv.Itoa(otherSite), strconv.Itoa(target), "", 404}, {"999", "999", "", 404}, {"-1", "1", "", 400}, {"1", "invalid", "", 400}, {"1", "1", "?page=1", 400},
	} {
		if w := get(tc.site, tc.dep, tc.query); w.Code != tc.code {
			t.Fatalf("scoping/validation failed: %+v %d %s", tc, w.Code, w.Body.String())
		}
	}
}
