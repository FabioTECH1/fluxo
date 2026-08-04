package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"fluxo/internal/database"
)

type deploymentPageResponse struct {
	Data              []database.Deployment `json:"data"`
	UnresolvedFailure *database.Deployment  `json:"unresolved_failure"`
}

func TestDeploymentFailureLifecycle(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { _ = database.DB.Close() })

	server := &Server{}
	siteID := insertDeploymentTestSite(t, "failure.example.com")
	failedID := insertDeployment(t, siteID, "failed", "composer install failed")

	page := listDeploymentsForTest(t, server, siteID)
	if page.UnresolvedFailure == nil || page.UnresolvedFailure.ID != failedID {
		t.Fatalf("unresolved failure = %#v, want deployment %d", page.UnresolvedFailure, failedID)
	}
	if page.UnresolvedFailure.FailureReason != "composer install failed" {
		t.Fatalf("failure reason = %q", page.UnresolvedFailure.FailureReason)
	}

	insertDeployment(t, siteID, "pending", "")
	page = listDeploymentsForTest(t, server, siteID)
	if page.UnresolvedFailure == nil || page.UnresolvedFailure.ID != failedID {
		t.Fatal("a queued retry must not hide the previous failure")
	}

	insertDeploymentWithSource(t, siteID, "success", "", "repo_sync")
	page = listDeploymentsForTest(t, server, siteID)
	if page.UnresolvedFailure == nil || page.UnresolvedFailure.ID != failedID {
		t.Fatal("a repository sync must not resolve a deployment failure")
	}

	insertDeployment(t, siteID, "success", "")
	page = listDeploymentsForTest(t, server, siteID)
	if page.UnresolvedFailure != nil {
		t.Fatal("a newer successful deployment must resolve the previous failure")
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sites/1/deployments/1/dismiss", nil)
	request.SetPathValue("id", strconv.Itoa(siteID))
	request.SetPathValue("depId", strconv.Itoa(failedID))
	response := httptest.NewRecorder()
	server.handleDismissDeploymentFailure().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("resolved failure dismiss status = %d, want %d: %s", response.Code, http.StatusNoContent, response.Body.String())
	}

	latestFailedID := insertDeployment(t, siteID, "failed", "node health check failed")
	if _, err := database.DB.Exec("UPDATE deployments SET updated_at = '2020-01-02 03:04:05' WHERE id = ?", latestFailedID); err != nil {
		t.Fatal(err)
	}
	repoFailureID := insertDeploymentWithSource(t, siteID, "failed", "repository sync failed", "repo_sync")
	page = listDeploymentsForTest(t, server, siteID)
	if page.UnresolvedFailure == nil || page.UnresolvedFailure.ID != latestFailedID {
		t.Fatal("a repository-sync failure must not replace the latest deployment failure")
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/sites/1/deployments/1/dismiss", nil)
	request.SetPathValue("id", strconv.Itoa(siteID))
	request.SetPathValue("depId", strconv.Itoa(repoFailureID))
	response = httptest.NewRecorder()
	server.handleDismissDeploymentFailure().ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("repository-sync dismiss status = %d, want %d: %s", response.Code, http.StatusUnprocessableEntity, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/sites/1/deployments/1/dismiss", nil)
	request.SetPathValue("id", strconv.Itoa(siteID))
	request.SetPathValue("depId", strconv.Itoa(latestFailedID))
	response = httptest.NewRecorder()
	server.handleDismissDeploymentFailure().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("dismiss status = %d, want %d: %s", response.Code, http.StatusNoContent, response.Body.String())
	}

	page = listDeploymentsForTest(t, server, siteID)
	if page.UnresolvedFailure != nil {
		t.Fatal("dismissed failure must no longer be unresolved")
	}
	if len(page.Data) != 6 {
		t.Fatalf("deployment history length = %d, want 6", len(page.Data))
	}
	var updatedAt string
	if err := database.DB.QueryRow("SELECT CAST(updated_at AS TEXT) FROM deployments WHERE id = ?", latestFailedID).Scan(&updatedAt); err != nil {
		t.Fatal(err)
	}
	if updatedAt != "2020-01-02 03:04:05" {
		t.Fatalf("dismissal changed deployment updated_at to %q", updatedAt)
	}
}

func TestDismissDeploymentFailureRejectsSupersededIncident(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { _ = database.DB.Close() })

	server := &Server{}
	siteID := insertDeploymentTestSite(t, "newer-failure.example.com")
	oldFailureID := insertDeployment(t, siteID, "failed", "old failure")
	newFailureID := insertDeployment(t, siteID, "failed", "new failure")

	request := httptest.NewRequest(http.MethodPost, "/api/v1/sites/1/deployments/1/dismiss", nil)
	request.SetPathValue("id", strconv.Itoa(siteID))
	request.SetPathValue("depId", strconv.Itoa(oldFailureID))
	response := httptest.NewRecorder()
	server.handleDismissDeploymentFailure().ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("dismiss status = %d, want %d: %s", response.Code, http.StatusConflict, response.Body.String())
	}

	page := listDeploymentsForTest(t, server, siteID)
	if page.UnresolvedFailure == nil || page.UnresolvedFailure.ID != newFailureID {
		t.Fatalf("unresolved failure = %#v, want deployment %d", page.UnresolvedFailure, newFailureID)
	}
}

func listDeploymentsForTest(t *testing.T, server *Server, siteID int) deploymentPageResponse {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sites/1/deployments", nil)
	request.SetPathValue("id", strconv.Itoa(siteID))
	response := httptest.NewRecorder()
	server.handleListDeployments().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", response.Code, response.Body.String())
	}

	var page deploymentPageResponse
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return page
}

func insertDeployment(t *testing.T, siteID int, status, failureReason string) int {
	t.Helper()
	return insertDeploymentWithSource(t, siteID, status, failureReason, "manual")
}

func insertDeploymentWithSource(t *testing.T, siteID int, status, failureReason, triggerSource string) int {
	t.Helper()
	result, err := database.DB.Exec(`INSERT INTO deployments
		(site_id, status, output, failure_reason, trigger_source)
		VALUES (?, ?, ?, ?, ?)`, siteID, status, failureReason, failureReason, triggerSource)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return int(id)
}

func insertDeploymentTestSite(t *testing.T, domain string) int {
	t.Helper()
	result, err := database.DB.Exec(
		"INSERT INTO sites (domain, path) VALUES (?, ?)",
		domain,
		"/home/fluxo/"+domain,
	)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return int(id)
}
