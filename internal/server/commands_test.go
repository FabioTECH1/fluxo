package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"fluxo/internal/database"
)

func TestListCommandsPaginatesAtTen(t *testing.T) {
	server, siteID := setupCommandsTest(t)
	for i := 1; i <= 12; i++ {
		insertCommand(t, siteID, "cmd-"+strconv.Itoa(i))
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sites/1/commands?page=1", nil)
	request.SetPathValue("id", strconv.Itoa(siteID))
	server.handleListCommands().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", response.Code, http.StatusOK)
	}
	var page commandPageResponse
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if page.Total != 12 || page.Page != 1 || page.PerPage != 10 || len(page.Data) != 10 {
		t.Fatalf("page response = total %d page %d per_page %d len %d, want 12/1/10/10", page.Total, page.Page, page.PerPage, len(page.Data))
	}
	if page.Data[0].Command != "cmd-12" {
		t.Fatalf("first command = %q, want latest command", page.Data[0].Command)
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/v1/sites/1/commands?page=2", nil)
	request.SetPathValue("id", strconv.Itoa(siteID))
	server.handleListCommands().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("list page 2 status = %d, want %d", response.Code, http.StatusOK)
	}
	page = commandPageResponse{}
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode page 2 response: %v", err)
	}
	if page.Total != 12 || page.Page != 2 || page.PerPage != 10 || len(page.Data) != 2 {
		t.Fatalf("page 2 response = total %d page %d per_page %d len %d, want 12/2/10/2", page.Total, page.Page, page.PerPage, len(page.Data))
	}
	if page.Data[0].Command != "cmd-2" || page.Data[1].Command != "cmd-1" {
		t.Fatalf("page 2 commands = %q, %q; want cmd-2, cmd-1", page.Data[0].Command, page.Data[1].Command)
	}
}

func TestListCommandsRejectsInvalidPage(t *testing.T) {
	server, siteID := setupCommandsTest(t)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sites/1/commands?page=0", nil)
	request.SetPathValue("id", strconv.Itoa(siteID))
	server.handleListCommands().ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("list status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestDeleteCommandRemovesOnlySiteScopedCommand(t *testing.T) {
	server, siteID := setupCommandsTest(t)
	otherSiteID := insertSite(t, "other.example.com")
	commandID := insertCommand(t, siteID, "php artisan queue:restart")
	otherCommandID := insertCommand(t, otherSiteID, "npm run build")

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/sites/1/commands/1", nil)
	request.SetPathValue("id", strconv.Itoa(siteID))
	request.SetPathValue("command_id", strconv.Itoa(commandID))
	server.handleDeleteCommand().ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d", response.Code, http.StatusNoContent)
	}
	assertCommandCount(t, siteID, 0)

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodDelete, "/api/v1/sites/1/commands/2", nil)
	request.SetPathValue("id", strconv.Itoa(siteID))
	request.SetPathValue("command_id", strconv.Itoa(otherCommandID))
	server.handleDeleteCommand().ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("cross-site delete status = %d, want %d", response.Code, http.StatusNotFound)
	}
	assertCommandCount(t, otherSiteID, 1)
}

func TestGetCommandReturnsOnlySiteScopedCommand(t *testing.T) {
	server, siteID := setupCommandsTest(t)
	otherSiteID := insertSite(t, "other.example.com")
	commandID := insertCommand(t, siteID, "php -v")
	otherCommandID := insertCommand(t, otherSiteID, "npm -v")

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sites/1/commands/1", nil)
	request.SetPathValue("id", strconv.Itoa(siteID))
	request.SetPathValue("command_id", strconv.Itoa(commandID))
	server.handleGetCommand().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d", response.Code, http.StatusOK)
	}
	var command database.Command
	if err := json.NewDecoder(response.Body).Decode(&command); err != nil {
		t.Fatalf("decode command: %v", err)
	}
	if command.ID != commandID || command.SiteID != siteID || command.Command != "php -v" {
		t.Fatalf("command = %#v, want site-scoped php command", command)
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/v1/sites/1/commands/2", nil)
	request.SetPathValue("id", strconv.Itoa(siteID))
	request.SetPathValue("command_id", strconv.Itoa(otherCommandID))
	server.handleGetCommand().ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("cross-site get status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestCommandHandlersRejectInvalidPathIDs(t *testing.T) {
	server, _ := setupCommandsTest(t)

	tests := []struct {
		name    string
		handler http.HandlerFunc
		request *http.Request
		setPath func(*http.Request)
	}{
		{
			name:    "list site id",
			handler: server.handleListCommands(),
			request: httptest.NewRequest(http.MethodGet, "/api/v1/sites/nope/commands", nil),
			setPath: func(r *http.Request) {
				r.SetPathValue("id", "nope")
			},
		},
		{
			name:    "execute site id",
			handler: server.handleExecuteCommand(),
			request: httptest.NewRequest(http.MethodPost, "/api/v1/sites/nope/commands", strings.NewReader(`{"command":"php -v"}`)),
			setPath: func(r *http.Request) {
				r.SetPathValue("id", "nope")
			},
		},
		{
			name:    "delete site id",
			handler: server.handleDeleteCommand(),
			request: httptest.NewRequest(http.MethodDelete, "/api/v1/sites/nope/commands/1", nil),
			setPath: func(r *http.Request) {
				r.SetPathValue("id", "nope")
				r.SetPathValue("command_id", "1")
			},
		},
		{
			name:    "get command id",
			handler: server.handleGetCommand(),
			request: httptest.NewRequest(http.MethodGet, "/api/v1/sites/1/commands/nope", nil),
			setPath: func(r *http.Request) {
				r.SetPathValue("id", "1")
				r.SetPathValue("command_id", "nope")
			},
		},
		{
			name:    "delete command id",
			handler: server.handleDeleteCommand(),
			request: httptest.NewRequest(http.MethodDelete, "/api/v1/sites/1/commands/nope", nil),
			setPath: func(r *http.Request) {
				r.SetPathValue("id", "1")
				r.SetPathValue("command_id", "nope")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setPath(tt.request)
			response := httptest.NewRecorder()
			tt.handler.ServeHTTP(response, tt.request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
			}
		})
	}
}

type commandPageResponse struct {
	Data    []database.Command `json:"data"`
	Total   int                `json:"total"`
	Page    int                `json:"page"`
	PerPage int                `json:"per_page"`
}

func setupCommandsTest(t *testing.T) (*Server, int) {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { _ = database.DB.Close() })

	return &Server{}, insertSite(t, "example.com")
}

func insertSite(t *testing.T, domain string) int {
	t.Helper()
	result, err := database.DB.Exec(
		"INSERT INTO sites (domain, path) VALUES (?, ?)",
		domain,
		"/home/fluxo/"+domain,
	)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	return int(id)
}

func insertCommand(t *testing.T, siteID int, command string) int {
	t.Helper()
	result, err := database.DB.Exec(
		"INSERT INTO commands (site_id, command, status, output) VALUES (?, ?, ?, ?)",
		siteID,
		command,
		"success",
		"ok",
	)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	return int(id)
}

func assertCommandCount(t *testing.T, siteID, want int) {
	t.Helper()
	var got int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM commands WHERE site_id = ?", siteID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("command count for site %d = %d, want %d", siteID, got, want)
	}
}
