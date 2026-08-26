package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"fluxo/internal/database"
)

func TestManagedDaemonCannotBeDeletedThroughGenericEndpoint(t *testing.T) {
	server, siteID, daemonID := setupManagedDaemonTest(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/sites/1/daemons/1", nil)
	request.SetPathValue("id", strconv.Itoa(siteID))
	request.SetPathValue("daemon_id", strconv.Itoa(daemonID))

	server.handleDeleteDaemon().ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("delete status = %d, want %d", response.Code, http.StatusConflict)
	}
	var count int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM daemons WHERE id = ?", daemonID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("managed daemon record was deleted")
	}
}

func TestManagedDaemonDeploymentPolicyIsFixed(t *testing.T) {
	server, siteID, daemonID := setupManagedDaemonTest(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/v1/sites/1/daemons/1/deployment-policy", strings.NewReader(`{"restart_on_deploy":false}`))
	request.SetPathValue("id", strconv.Itoa(siteID))
	request.SetPathValue("daemon_id", strconv.Itoa(daemonID))

	server.handleUpdateDaemonDeploymentPolicy().ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("policy status = %d, want %d", response.Code, http.StatusConflict)
	}
	var restartOnDeploy bool
	if err := database.DB.QueryRow("SELECT restart_on_deploy FROM daemons WHERE id = ?", daemonID).Scan(&restartOnDeploy); err != nil {
		t.Fatal(err)
	}
	if !restartOnDeploy {
		t.Fatal("managed deployment policy was changed")
	}
}

func TestDaemonProcessLimitIsEnforced(t *testing.T) {
	server, siteID, _ := setupManagedDaemonTest(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sites/1/daemons", strings.NewReader(`{
		"name":"Too Many Workers",
		"command":"php8.4 artisan queue:work",
		"user":"fluxo",
		"instances":65,
		"start_seconds":1,
		"stop_seconds":15,
		"stop_signal":"SIGTERM"
	}`))
	request.SetPathValue("id", strconv.Itoa(siteID))

	server.handleCreateDaemon().ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("create status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	var count int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM daemons WHERE site_id = ? AND name = 'Too Many Workers'", siteID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("daemon exceeding the process limit was inserted")
	}
}

func setupManagedDaemonTest(t *testing.T) (*Server, int, int) {
	t.Helper()
	previousDB := database.DB
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() {
		_ = database.DB.Close()
		database.DB = previousDB
	})
	result, err := database.DB.Exec("INSERT INTO sites (domain, path) VALUES ('queue.example.com', '/home/fluxo/queue.example.com')")
	if err != nil {
		t.Fatal(err)
	}
	siteID64, _ := result.LastInsertId()
	result, err = database.DB.Exec(`INSERT INTO daemons
		(site_id, name, managed_kind, command, directory, user, instances, restart_on_deploy)
		VALUES (?, 'Laravel Queue Worker', 'laravel_queue', 'php8.4 artisan queue:work database', '/home/fluxo/queue.example.com', 'fluxo', 2, 1)`, siteID64)
	if err != nil {
		t.Fatal(err)
	}
	daemonID64, _ := result.LastInsertId()
	return &Server{}, int(siteID64), int(daemonID64)
}
