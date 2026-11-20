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
