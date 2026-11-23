package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/wordpresscron"
)

func (s *Server) handleWordPressCron() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil || id <= 0 {
			http.Error(w, "Invalid site ID", 400)
			return
		}
		service := wordpresscron.New(database.DB)
		if r.Method == http.MethodGet {
			status, err := service.Status(r.Context(), id)
			if err != nil {
				wordpressCronError(w, err)
				return
			}
			phpJSON(w, 200, status)
			return
		}
		var req struct {
			Enabled *bool `json:"enabled"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&req); err != nil || req.Enabled == nil || decoder.Decode(new(any)) != io.EOF {
			http.Error(w, "Provide enabled as a boolean", 400)
			return
		}
		unlock, err := s.lockExistingGitHubSite(r.Context(), id)
		if err != nil {
			writeGitHubSiteLockError(w, err)
			return
		}
		defer unlock()
		ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
		defer cancel()
		status, err := service.Set(ctx, id, *req.Enabled)
		if err != nil {
			wordpressCronError(w, err)
			return
		}
		LogActivity(id, "wordpress_cron", map[bool]string{true: "WordPress Cron enabled", false: "WordPress Cron disabled"}[*req.Enabled])
		phpJSON(w, 200, status)
	}
}

func wordpressCronError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "Site not found", 404)
		return
	}
	phpJSON(w, 409, map[string]string{"code": "wordpress_cron_review", "message": err.Error()})
}

// Older schema fixtures/qualified predecessors may lack the whole additive
// feature table. Existing rows, when present, must never be guessed away.
func wordpressCronOwned(where string, arg int) (bool, error) {
	var tables int
	if err := database.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='wordpress_cron_settings'").Scan(&tables); err != nil {
		return false, err
	}
	if tables == 0 {
		return false, nil
	}
	var count int
	err := database.DB.QueryRow("SELECT count(*) FROM wordpress_cron_settings WHERE "+where+"=?", arg).Scan(&count)
	return count > 0, err
}
