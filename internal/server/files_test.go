package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"fluxo/internal/services/filemanager"
)

func TestUploadRejectsOversizedContentLengthBeforeSiteLookup(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sites/1/files/upload", strings.NewReader(""))
	request.ContentLength = filemanager.MaxUploadBytes + 1
	response := httptest.NewRecorder()

	server := &Server{}
	server.handleUploadSiteFile().ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("upload status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestUploadHTTPBodyLimitMatchesManagerLimit(t *testing.T) {
	if fileUploadRequestLimit != filemanager.MaxUploadBytes {
		t.Fatalf("HTTP upload limit = %d, manager limit = %d", fileUploadRequestLimit, filemanager.MaxUploadBytes)
	}
}
