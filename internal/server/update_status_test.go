package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpdateStatusRequiresAuthentication(t *testing.T) {
	server := NewServer(nil, t.TempDir(), false)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/update-status", nil)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected update status to require authentication, got HTTP %d", response.Code)
	}
}

func TestInstalledVersionRemainsPublic(t *testing.T) {
	previousVersion := Version
	Version = "0.4.18"
	t.Cleanup(func() { Version = previousVersion })

	server := NewServer(nil, t.TempDir(), false)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected version endpoint to remain public, got HTTP %d", response.Code)
	}
	if response.Body.String() != "{\"version\":\"0.4.18\"}\n" {
		t.Fatalf("unexpected version response: %s", response.Body.String())
	}
}
