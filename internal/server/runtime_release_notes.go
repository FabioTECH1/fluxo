package server

import (
	"context"
	"encoding/json"
	"fluxo/internal/services/releasenotes"
	"net/http"
	"net/url"
)

func (s *Server) registerReleaseNoteRoutes() {
	reader := releasenotes.New()
	s.mux.HandleFunc("GET /api/v1/server/runtime-release-notes", runtimeReleaseNotesHandler(reader))
}

type releaseNoteReader interface {
	Get(context.Context, string, string) releasenotes.Notes
}

func runtimeReleaseNotesHandler(reader releaseNoteReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil || len(q) != 2 || len(q["runtime"]) != 1 || len(q["version"]) != 1 || !releasenotes.Valid(q.Get("runtime"), q.Get("version")) {
			releaseNotesJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request", "message": "runtime must be php or node and version must be an exact major.minor.patch version"})
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		releaseNotesJSON(w, http.StatusOK, reader.Get(r.Context(), q.Get("runtime"), q.Get("version")))
	}
}

func releaseNotesJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
