package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"fluxo/internal/services/packages"
	"fluxo/internal/services/phpupdate"
	"github.com/google/uuid"
)

func phpJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func acquirePHPCommandLifecycle(ctx context.Context) (func(), error) {
	lockCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	unlock, err := packages.Lifecycle(lockCtx, true)
	if err != nil {
		return nil, err
	}
	if busy, err := phpupdate.Applying(); err != nil || busy {
		unlock()
		if err != nil {
			return nil, err
		}
		return nil, phpupdate.ErrBusy
	}
	return unlock, nil
}

// Called while a site mutation already holds the HTTP lifecycle lease. Acquire
// the background lease before returning the response, so maintenance cannot
// overtake deferred configuration/process reconciliation from that request.
func (s *Server) runRuntimeMutationAsync(work func()) bool {
	release := func() {}
	if s.migrateLegacyCredentials {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		unlock, err := packages.Lifecycle(ctx, true)
		cancel()
		if err != nil {
			log.Printf("Deferred runtime mutation could not acquire maintenance lease: %v", err)
			return false
		}
		release = unlock
	}
	go func() { defer release(); work() }()
	return true
}
func phpError(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "maintenance_unavailable"
	switch {
	case errors.Is(err, sql.ErrNoRows):
		status, code = 404, "not_found"
	case errors.Is(err, phpupdate.ErrBusy):
		status, code = 409, "maintenance_busy"
	case errors.Is(err, phpupdate.ErrReviewChanged):
		status, code = 409, "review_changed"
	case errors.Is(err, phpupdate.ErrPlanChanged):
		status, code = 409, "plan_changed"
	case errors.Is(err, phpupdate.ErrRecoveryRequired):
		status, code = 409, "recovery_required"
	case errors.Is(err, phpupdate.ErrRollbackAcknowledgement):
		status, code = 422, "rollback_acknowledgement_required"
	case errors.Is(err, phpupdate.ErrUnsupported):
		status, code = 422, "unsupported_configuration"
	case errors.Is(err, phpupdate.ErrSupervisor):
		status, code = 503, "supervisor_unavailable"
	}
	phpJSON(w, status, map[string]string{"code": code, "message": err.Error()})
}
func decodePHPRequest(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		phpJSON(w, 400, map[string]string{"code": "invalid_request", "message": "Invalid JSON request"})
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		phpJSON(w, 400, map[string]string{"code": "invalid_request", "message": "Only one JSON object is allowed"})
		return false
	}
	return true
}
func (s *Server) registerPHPUpdateRoutes() {
	s.mux.HandleFunc("GET /api/v1/server/php/updates/repository", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		phpJSON(w, 200, s.phpUpdates.RepositoryStatus(ctx))
	})
	for _, kind := range []string{"review", "apply"} {
		s.mux.HandleFunc("POST /api/v1/server/php/updates/repository/"+kind, func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				PlanID    string `json:"plan_id"`
				RequestID string `json:"request_id"`
			}
			if !decodePHPRequest(w, r, &req) {
				return
			}
			if _, err := uuid.Parse(req.RequestID); err != nil {
				phpJSON(w, 400, map[string]string{"code": "invalid_request", "message": "A request UUID is required"})
				return
			}
			if kind == "apply" {
				if _, err := uuid.Parse(req.PlanID); err != nil {
					phpJSON(w, 400, map[string]string{"code": "invalid_request", "message": "A repository plan UUID is required"})
					return
				}
			} else if req.PlanID != "" {
				phpJSON(w, 400, map[string]string{"code": "invalid_request", "message": "Review does not accept a plan ID"})
				return
			}
			o, err := s.phpUpdates.EnqueueRepository(r.Context(), "repository_"+kind, req.PlanID, req.RequestID, usernameFromContext(r.Context()), getClientIP(r))
			if err != nil {
				phpError(w, err)
				return
			}
			phpJSON(w, 202, o)
		})
	}
	s.mux.HandleFunc("POST /api/v1/server/php/updates/plans/{plan_id}/review", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("plan_id")
		if _, err := uuid.Parse(id); err != nil {
			phpJSON(w, 400, map[string]string{"code": "invalid_id", "message": "Invalid plan ID"})
			return
		}
		var req struct{}
		if !decodePHPRequest(w, r, &req) {
			return
		}
		p, err := s.phpUpdates.ReviewPlan(r.Context(), id)
		if err != nil {
			phpError(w, err)
			return
		}
		phpJSON(w, 200, p)
	})
	s.mux.HandleFunc("POST /api/v1/server/php/updates/operations/{operation_id}/recovery-plan", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("operation_id")
		if _, err := uuid.Parse(id); err != nil {
			phpJSON(w, 400, map[string]string{"code": "invalid_id", "message": "Invalid operation ID"})
			return
		}
		var req struct {
			Kind string `json:"kind"`
		}
		if !decodePHPRequest(w, r, &req) {
			return
		}
		if req.Kind != "repair" && req.Kind != "rollback" {
			phpJSON(w, 400, map[string]string{"code": "invalid_request", "message": "Choose repair or rollback"})
			return
		}
		p, err := s.phpUpdates.ReviewRecovery(r.Context(), id, req.Kind)
		if err != nil {
			phpError(w, err)
			return
		}
		phpJSON(w, 200, p)
	})
	s.mux.HandleFunc("POST /api/v1/server/php/updates/recovery/apply", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			PlanID    string `json:"plan_id"`
			RequestID string `json:"request_id"`
		}
		if !decodePHPRequest(w, r, &req) {
			return
		}
		_, a := uuid.Parse(req.PlanID)
		_, b := uuid.Parse(req.RequestID)
		if a != nil || b != nil {
			phpJSON(w, 400, map[string]string{"code": "invalid_request", "message": "A recovery plan UUID and request UUID are required"})
			return
		}
		o, err := s.phpUpdates.EnqueueRecovery(r.Context(), req.PlanID, req.RequestID, usernameFromContext(r.Context()), getClientIP(r))
		if err != nil {
			phpError(w, err)
			return
		}
		phpJSON(w, 202, o)
	})
	s.mux.HandleFunc("POST /api/v1/server/php/updates/operations/{operation_id}/verify", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("operation_id")
		if _, err := uuid.Parse(id); err != nil {
			phpJSON(w, 400, map[string]string{"code": "invalid_id", "message": "Invalid operation ID"})
			return
		}
		o, err := s.phpUpdates.EnqueueVerification(r.Context(), id, usernameFromContext(r.Context()), getClientIP(r))
		if err != nil {
			phpError(w, err)
			return
		}
		phpJSON(w, 202, o)
	})
	s.mux.HandleFunc("GET /api/v1/server/php/updates", func(w http.ResponseWriter, r *http.Request) {
		items, err := phpupdate.Snapshots()
		if err != nil {
			phpError(w, err)
			return
		}
		active, err := phpupdate.ActiveOperation()
		if err != nil {
			phpError(w, err)
			return
		}
		failures, err := phpupdate.Failures()
		if err != nil {
			phpError(w, err)
			return
		}
		recoveries, err := phpupdate.Recoveries()
		if err != nil {
			phpError(w, err)
			return
		}
		phpJSON(w, 200, map[string]any{"branches": items, "active_operation": active, "failures": failures, "recoveries": recoveries})
	})
	s.mux.HandleFunc("POST /api/v1/server/php/updates/check", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Version string `json:"version"`
		}
		if !decodePHPRequest(w, r, &req) {
			return
		}
		if !phpupdate.ValidBranch(req.Version) {
			phpJSON(w, 400, map[string]string{"code": "invalid_version", "message": "An installed PHP branch is required"})
			return
		}
		o, err := s.phpUpdates.Enqueue(r.Context(), "check", req.Version, "", "", usernameFromContext(r.Context()), getClientIP(r))
		if err != nil {
			phpError(w, err)
			return
		}
		phpJSON(w, 202, o)
	})
	s.mux.HandleFunc("GET /api/v1/server/php/updates/plans/{plan_id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("plan_id")
		if _, err := uuid.Parse(id); err != nil {
			phpJSON(w, 400, map[string]string{"code": "invalid_id", "message": "Invalid plan ID"})
			return
		}
		p, err := phpupdate.GetPlan(id)
		if err != nil {
			phpError(w, err)
			return
		}
		phpJSON(w, 200, p)
	})
	s.mux.HandleFunc("POST /api/v1/server/php/updates/apply", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Version               string `json:"version"`
			PlanID                string `json:"plan_id"`
			RequestID             string `json:"request_id"`
			AcknowledgeNoRollback bool   `json:"acknowledge_no_rollback"`
		}
		if !decodePHPRequest(w, r, &req) {
			return
		}
		_, a := uuid.Parse(req.PlanID)
		_, b := uuid.Parse(req.RequestID)
		if !phpupdate.ValidBranch(req.Version) || a != nil || b != nil {
			phpJSON(w, 400, map[string]string{"code": "invalid_request", "message": "A PHP branch, plan UUID and request UUID are required"})
			return
		}
		o, err := s.phpUpdates.Enqueue(r.Context(), "apply", req.Version, req.PlanID, req.RequestID, usernameFromContext(r.Context()), getClientIP(r), req.AcknowledgeNoRollback)
		if err != nil {
			phpError(w, err)
			return
		}
		phpJSON(w, 202, o)
	})
	s.mux.HandleFunc("GET /api/v1/server/php/updates/operations/{operation_id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("operation_id")
		if _, err := uuid.Parse(id); err != nil {
			phpJSON(w, 400, map[string]string{"code": "invalid_id", "message": "Invalid operation ID"})
			return
		}
		o, err := phpupdate.GetOperation(id)
		if err != nil {
			phpError(w, err)
			return
		}
		phpJSON(w, 200, o)
	})
	s.mux.HandleFunc("POST /api/v1/server/php/updates/notices/dismiss", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Version     string `json:"version"`
			CandidateID string `json:"candidate_id"`
		}
		if !decodePHPRequest(w, r, &req) {
			return
		}
		if !phpupdate.ValidBranch(req.Version) || len(req.CandidateID) != 64 {
			phpJSON(w, 400, map[string]string{"code": "invalid_request", "message": "A branch and candidate fingerprint are required"})
			return
		}
		if err := phpupdate.Dismiss(req.Version, req.CandidateID); err != nil {
			phpError(w, err)
			return
		}
		w.WriteHeader(204)
	})
}

func (s *Server) phpMaintenanceGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions || strings.HasPrefix(r.URL.Path, "/api/v1/server/php/updates") {
			next.ServeHTTP(w, r)
			return
		}
		guarded := false
		for _, prefix := range []string{"/api/v1/sites", "/api/v1/daemons", "/api/v1/crons", "/api/v1/server/php", "/api/v1/settings", "/api/v1/tools/phpmyadmin"} {
			if strings.HasPrefix(r.URL.Path, prefix) {
				guarded = true
			}
		}
		if !guarded {
			next.ServeHTTP(w, r)
			return
		}
		if busy, err := phpupdate.Applying(); err != nil {
			phpError(w, err)
			return
		} else if busy {
			phpError(w, phpupdate.ErrBusy)
			return
		}
		// Production operations and supervised workers share this flock. A second
		// check closes the race between admission and claiming the exclusive gate.
		if s.migrateLegacyCredentials {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			unlock, err := packages.Lifecycle(ctx, true)
			if err != nil {
				phpError(w, phpupdate.ErrBusy)
				return
			}
			defer unlock()
			if busy, err := phpupdate.Applying(); err != nil {
				phpError(w, err)
				return
			} else if busy {
				phpError(w, phpupdate.ErrBusy)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
