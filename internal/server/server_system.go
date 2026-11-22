package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
)

func (s *Server) systemMutationScope() error {
	if s.hostSystem.Root == "" && (!s.migrateLegacyCredentials || s.dataDir != "/var/lib/fluxo") {
		return errors.New("System changes require the installed production Fluxo service")
	}
	return nil
}
func systemError(w http.ResponseWriter, err error) {
	phpJSON(w, http.StatusConflict, map[string]string{"code": "system_management_blocked", "message": err.Error()})
}
func (s *Server) systemRoutes() {
	s.mux.HandleFunc("GET /api/v1/server/system", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		v, e := s.hostSystem.Status(ctx)
		if e != nil {
			phpJSON(w, 503, map[string]string{"code": "system_status_unavailable", "message": e.Error()})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		phpJSON(w, 200, v)
	})
	s.mux.HandleFunc("PUT /api/v1/server/system/security-updates", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Enabled *bool `json:"enabled"`
		}
		if !decodePHPRequest(w, r, &req) {
			return
		}
		if req.Enabled == nil {
			phpJSON(w, 400, map[string]string{"message": "enabled must be a boolean"})
			return
		}
		if e := s.systemMutationScope(); e != nil {
			systemError(w, e)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		release, e := s.hostSystem.Gate(ctx)
		if e != nil {
			systemError(w, e)
			return
		}
		defer release()
		previous, e := s.hostSystem.RebootState()
		boot, be := s.hostSystem.BootID()
		if e != nil || be != nil {
			systemError(w, errors.New("Restart state is unavailable"))
			return
		}
		if previous != nil && previous.BootID == boot && previous.Phase != "failed" {
			systemError(w, errors.New("A server restart is pending"))
			return
		}
		admission, e := s.hostSystemAdmission(ctx)
		if e != nil {
			systemError(w, e)
			return
		}
		defer admission()
		v, e := s.hostSystem.SetUpdates(ctx, *req.Enabled)
		if e != nil {
			systemError(w, e)
			return
		}
		phpJSON(w, 200, v)
	})
	s.mux.HandleFunc("POST /api/v1/server/system/reboots", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			BootID  string `json:"boot_id"`
			Confirm bool   `json:"confirm"`
		}
		if !decodePHPRequest(w, r, &req) {
			return
		}
		if _, e := uuid.Parse(req.BootID); e != nil || !req.Confirm {
			phpJSON(w, 400, map[string]string{"message": "Current boot_id and confirm:true are required"})
			return
		}
		if e := s.systemMutationScope(); e != nil {
			systemError(w, e)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		// The request performs one current maintenance review, then releases all
		// gates. The helper must acquire/recheck them independently before READY.
		unlock, e := s.hostSystem.Gate(ctx)
		if e != nil {
			systemError(w, e)
			return
		}
		admission, e := s.hostSystemAdmission(ctx)
		if e != nil {
			unlock()
			systemError(w, e)
			return
		}
		v, e := s.hostSystem.Schedule(ctx, req.BootID)
		admission()
		unlock()
		if e != nil {
			systemError(w, e)
			return
		}
		v, e = s.hostSystem.AwaitAccepted(ctx, v.ID)
		if e != nil {
			phpJSON(w, 503, map[string]any{"code": "restart_unconfirmed", "message": e.Error(), "reboot": v})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		phpJSON(w, 202, v)
	})
	s.mux.HandleFunc("GET /api/v1/server/system/reboots/{id}/services", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if _, e := uuid.Parse(id); e != nil {
			phpJSON(w, 400, map[string]string{"message": "Invalid restart ID"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		v, e := s.hostSystem.Services(ctx, id)
		if e != nil {
			systemError(w, e)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		phpJSON(w, 200, v)
	})
}
