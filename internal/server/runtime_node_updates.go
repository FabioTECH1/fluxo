package server

import (
	"database/sql"
	"errors"
	"fluxo/internal/services/nodeupdate"
	"github.com/google/uuid"
	"net/http"
)

func nodeError(w http.ResponseWriter, err error) {
	status, code := 500, "maintenance_unavailable"
	switch {
	case errors.Is(err, sql.ErrNoRows):
		status, code = 404, "not_found"
	case errors.Is(err, nodeupdate.ErrBusy):
		status, code = 409, "maintenance_busy"
	case errors.Is(err, nodeupdate.ErrChanged):
		status, code = 409, "review_changed"
	case errors.Is(err, nodeupdate.ErrUnsupported):
		status, code = 422, "unsupported_configuration"
	case errors.Is(err, nodeupdate.ErrSupervisor):
		status, code = 503, "supervisor_unavailable"
	}
	phpJSON(w, status, map[string]string{"code": code, "message": err.Error()})
}
func nodeUUID(w http.ResponseWriter, id string) bool {
	if _, err := uuid.Parse(id); err != nil {
		phpJSON(w, 400, map[string]string{"code": "invalid_request", "message": "A valid UUID is required"})
		return false
	}
	return true
}
func (s *Server) registerNodeUpdateRoutes() {
	s.mux.HandleFunc("GET /api/v1/server/node/updates", func(w http.ResponseWriter, r *http.Request) {
		status, err := nodeupdate.ReadStatus()
		if err != nil {
			nodeError(w, err)
			return
		}
		phpJSON(w, 200, status)
	})
	s.mux.HandleFunc("GET /api/v1/server/node/updates/operations", func(w http.ResponseWriter, r *http.Request) {
		status, err := nodeupdate.ReadStatus()
		if err != nil {
			nodeError(w, err)
			return
		}
		phpJSON(w, 200, status.Operations)
	})
	s.mux.HandleFunc("GET /api/v1/server/node/updates/operations/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !nodeUUID(w, id) {
			return
		}
		o, err := nodeupdate.GetOperation(id)
		if err != nil {
			nodeError(w, err)
			return
		}
		phpJSON(w, 200, o)
	})
	s.mux.HandleFunc("POST /api/v1/server/node/updates/check", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			RequestID string `json:"request_id"`
		}
		if !decodePHPRequest(w, r, &req) || !nodeUUID(w, req.RequestID) {
			return
		}
		o, err := s.nodeUpdates.Enqueue(r.Context(), "check", "", req.RequestID, usernameFromContext(r.Context()), getClientIP(r))
		if err != nil {
			nodeError(w, err)
			return
		}
		phpJSON(w, 202, o)
	})
	s.mux.HandleFunc("POST /api/v1/server/node/updates/plans/{id}/review", func(w http.ResponseWriter, r *http.Request) {
		var req struct{}
		if !nodeUUID(w, r.PathValue("id")) || !decodePHPRequest(w, r, &req) {
			return
		}
		p, err := s.nodeUpdates.Review(r.Context(), r.PathValue("id"))
		if err != nil {
			nodeError(w, err)
			return
		}
		phpJSON(w, 200, p)
	})
	s.mux.HandleFunc("POST /api/v1/server/node/updates/apply", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			PlanID    string `json:"plan_id"`
			RequestID string `json:"request_id"`
		}
		if !decodePHPRequest(w, r, &req) || !nodeUUID(w, req.PlanID) || !nodeUUID(w, req.RequestID) {
			return
		}
		o, err := s.nodeUpdates.Enqueue(r.Context(), "apply", req.PlanID, req.RequestID, usernameFromContext(r.Context()), getClientIP(r))
		if err != nil {
			nodeError(w, err)
			return
		}
		phpJSON(w, 202, o)
	})
	s.mux.HandleFunc("POST /api/v1/server/node/updates/operations/{id}/recovery-plan", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Kind string `json:"kind"`
		}
		if !nodeUUID(w, r.PathValue("id")) || !decodePHPRequest(w, r, &req) {
			return
		}
		if req.Kind != "repair" && req.Kind != "rollback" {
			phpJSON(w, 400, map[string]string{"code": "invalid_request", "message": "Choose repair or rollback"})
			return
		}
		status, err := nodeupdate.ReadStatus()
		if err != nil {
			nodeError(w, err)
			return
		}
		generation := ""
		for _, g := range status.Recoveries {
			if g.Head == r.PathValue("id") {
				generation = g.ID
			}
		}
		if generation == "" {
			nodeError(w, sql.ErrNoRows)
			return
		}
		p, err := s.nodeUpdates.ReviewRecovery(r.Context(), generation, req.Kind)
		if err != nil {
			nodeError(w, err)
			return
		}
		phpJSON(w, 200, p)
	})
	s.mux.HandleFunc("POST /api/v1/server/node/updates/recovery/apply", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			PlanID    string `json:"plan_id"`
			RequestID string `json:"request_id"`
		}
		if !decodePHPRequest(w, r, &req) || !nodeUUID(w, req.PlanID) || !nodeUUID(w, req.RequestID) {
			return
		}
		p, err := nodeupdate.GetPlan(req.PlanID)
		if err != nil {
			nodeError(w, err)
			return
		}
		if p.Kind != "repair" && p.Kind != "rollback" {
			nodeError(w, nodeupdate.ErrUnsupported)
			return
		}
		o, err := s.nodeUpdates.Enqueue(r.Context(), p.Kind, p.ID, req.RequestID, usernameFromContext(r.Context()), getClientIP(r))
		if err != nil {
			nodeError(w, err)
			return
		}
		phpJSON(w, 202, o)
	})
	s.mux.HandleFunc("POST /api/v1/server/node/updates/notices/dismiss", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			CandidateID string `json:"candidate_id"`
		}
		if !decodePHPRequest(w, r, &req) {
			return
		}
		if req.CandidateID == "" {
			phpJSON(w, 400, map[string]string{"code": "invalid_request", "message": "A candidate fingerprint is required"})
			return
		}
		if err := nodeupdate.Dismiss(req.CandidateID); err != nil {
			nodeError(w, err)
			return
		}
		w.WriteHeader(204)
	})
}
