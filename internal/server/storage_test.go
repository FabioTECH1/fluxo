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

func TestManagedDatabaseUserGrantsCannotBeChanged(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/databases/users/grants", strings.NewReader(`{"user":"fluxo","databases":[],"engine":"mysql"}`))
	response := httptest.NewRecorder()

	new(Server).handleUpdateUserGrants().ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestAssignedDatabaseUserCannotBeDeleted(t *testing.T) {
	previousDB := database.DB
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	t.Cleanup(func() {
		_ = database.DB.Close()
		database.DB = previousDB
	})
	if _, err := database.DB.Exec("INSERT INTO sites (domain, path) VALUES ('assigned.example.com', '/home/fluxo/assigned.example.com')"); err != nil {
		t.Fatalf("insert site: %v", err)
	}
	if _, err := database.DB.Exec("INSERT INTO databases (site_id, engine, name, username) VALUES (1, 'mysql', 'app_db', 'app_user')"); err != nil {
		t.Fatalf("insert database: %v", err)
	}

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/databases/users?user=app_user&engine=mysql", nil)
	response := httptest.NewRecorder()
	new(Server).handleDeleteDatabaseUser().ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusConflict)
	}
	if !strings.Contains(response.Body.String(), "Disconnect") {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
}

func TestSiteDatabaseCreationRequiresDedicatedCredentials(t *testing.T) {
	previousDB := database.DB
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	t.Cleanup(func() {
		_ = database.DB.Close()
		database.DB = previousDB
	})
	result, err := database.DB.Exec("INSERT INTO sites (domain, path) VALUES ('app.example.com', '/home/fluxo/app.example.com')")
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	siteID, _ := result.LastInsertId()

	request := httptest.NewRequest(http.MethodPost, "/api/v1/sites/1/databases", strings.NewReader(`{"name":"app_db","engine":"mysql"}`))
	request.SetPathValue("id", strconv.FormatInt(siteID, 10))
	response := httptest.NewRecorder()
	new(Server).handleCreateDatabase().ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if !strings.Contains(response.Body.String(), "dedicated database username") {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
}

func TestManagedDatabaseUserCannotBeDeleted(t *testing.T) {
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/databases/users?user=fluxo&engine=mysql", nil)
	response := httptest.NewRecorder()

	new(Server).handleDeleteDatabaseUser().ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}
