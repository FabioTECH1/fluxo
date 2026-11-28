package server

import (
	"context"
	"fluxo/internal/services/releasenotes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeReleaseNotes struct{ calls int }

func (f *fakeReleaseNotes) Get(_ context.Context, runtime, version string) releasenotes.Notes {
	f.calls++
	return releasenotes.Notes{Runtime: runtime, Version: version, State: "unavailable", Highlights: []string{}, Detail: "Unavailable"}
}
func TestRuntimeNotesValidationAndOptionalUnavailable(t *testing.T) {
	reader := &fakeReleaseNotes{}
	handler := runtimeReleaseNotesHandler(reader)
	for _, q := range []string{"", "runtime=php", "runtime=php&version=8.4", "runtime=php&runtime=node&version=8.4.28", "runtime=php&version=8.4.28&url=http://localhost", "runtime=php&version=8.4.28&bad=%xx", "runtime=nginx&version=1.2.3"} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("GET", "/?"+q, nil))
		if w.Code != 400 {
			t.Fatal(q, w.Code)
		}
	}
	if reader.calls != 0 {
		t.Fatal("invalid requests fetched")
	}
	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest("GET", "/?runtime=php&version=8.4.28", nil))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-store" || !strings.Contains(w.Body.String(), `"state":"unavailable"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestRuntimeNotesRemainAuthenticated(t *testing.T) {
	s := wordpressServerFixture(t)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/server/runtime-release-notes?runtime=php&version=8.4.28", nil))
	if w.Code != 401 {
		t.Fatal(w.Code, w.Body.String())
	}
}
