package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnableSSHHardeningRequiresBothAcknowledgements(t *testing.T) {
	server := &Server{}
	for _, body := range []string{
		`{}`,
		`{"key_access_confirmed":true}`,
		`{"recovery_access_confirmed":true}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/security/harden", strings.NewReader(body))
		response := httptest.NewRecorder()
		server.handleEnableSSHHardening().ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %s returned %d, want 400", body, response.Code)
		}
	}
}

func TestEnableSSHHardeningRejectsUnknownFields(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/security/harden", strings.NewReader(
		`{"key_access_confirmed":true,"recovery_access_confirmed":true,"skip_validation":true}`,
	))
	response := httptest.NewRecorder()
	server.handleEnableSSHHardening().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}
