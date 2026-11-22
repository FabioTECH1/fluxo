package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"fluxo/internal/database"
	"fluxo/internal/services/nginxupdate"
)

type nginxDiscoveryStore struct{}

func (nginxDiscoveryStore) Load() (nginxupdate.Saved, error) {
	var raw string
	e := database.DB.QueryRow("SELECT value FROM system_metadata WHERE key='nginx_update_discovery'").Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		e = nil
	}
	if e != nil {
		return nginxupdate.Saved{}, e
	}
	return nginxupdate.DecodeSaved(raw)
}
func (nginxDiscoveryStore) Save(v nginxupdate.Saved) error {
	raw, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = database.DB.Exec("INSERT INTO system_metadata(key,value) VALUES('nginx_update_discovery',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", string(raw))
	return e
}
func nginxDiscoveryError(w http.ResponseWriter, e error) {
	code := http.StatusInternalServerError
	if errors.Is(e, nginxupdate.ErrBusy) || errors.Is(e, nginxupdate.ErrChanged) {
		code = http.StatusConflict
	}
	phpJSON(w, code, map[string]string{"message": e.Error()})
}
func (s *Server) nginxUpdateRoutes() {
	s.mux.HandleFunc("GET /api/v1/server/nginx/updates", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.nginxUpdates.Status()
		if e != nil {
			nginxDiscoveryError(w, e)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		phpJSON(w, 200, v)
	})
	s.mux.HandleFunc("POST /api/v1/server/nginx/updates/check", func(w http.ResponseWriter, r *http.Request) {
		var req struct{}
		if !decodePHPRequest(w, r, &req) {
			return
		}
		v, e := s.nginxUpdates.Check(r.Context())
		if e != nil {
			nginxDiscoveryError(w, e)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		phpJSON(w, 200, v)
	})
	s.mux.HandleFunc("POST /api/v1/server/nginx/updates/notices/dismiss", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Identity string `json:"identity"`
		}
		if !decodePHPRequest(w, r, &req) {
			return
		}
		v, e := s.nginxUpdates.Dismiss(req.Identity)
		if e != nil {
			nginxDiscoveryError(w, e)
			return
		}
		phpJSON(w, 200, v)
	})
}
