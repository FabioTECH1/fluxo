package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/daemon"
	"fluxo/internal/services/pythontoolchain"
)

var pythonSiteLifecycleMu sync.Mutex

func writePythonRuntimeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) handleGetPythonInfo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		writePythonRuntimeJSON(w, http.StatusOK, pythontoolchain.Inspect(ctx))
	}
}

func (s *Server) handleInstallPython() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pythonSiteLifecycleMu.Lock()
		defer pythonSiteLifecycleMu.Unlock()
		runtimePackageMutationMu.Lock()
		defer runtimePackageMutationMu.Unlock()
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
		defer cancel()
		status, err := pythontoolchain.Install(ctx)
		if err != nil {
			w.Header().Set("Cache-Control", "no-store, max-age=0")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writePythonRuntimeJSON(w, http.StatusOK, map[string]any{
			"status": "ok", "message": "Python application support installed successfully", "runtime": status,
		})
	}
}

func (s *Server) handleRemovePythonTools() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pythonSiteLifecycleMu.Lock()
		defer pythonSiteLifecycleMu.Unlock()
		runtimePackageMutationMu.Lock()
		defer runtimePackageMutationMu.Unlock()
		var count int
		if err := database.DB.QueryRow("SELECT COUNT(*) FROM sites WHERE LOWER(COALESCE(app_type, '')) = 'python'").Scan(&count); err != nil {
			http.Error(w, "Failed to check Python site usage", http.StatusInternalServerError)
			return
		}
		if count > 0 {
			http.Error(w, "Remove all Python sites before removing Fluxo-managed Python tools", http.StatusConflict)
			return
		}
		if err := pythontoolchain.RemoveManagedTools(r.Context()); err != nil {
			http.Error(w, "Python tool removal failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writePythonRuntimeJSON(w, http.StatusOK, map[string]string{
			"status": "ok", "message": "Fluxo-managed Python tools removed; Ubuntu's system Python was left unchanged",
		})
	}
}

func (s *Server) handleRestartPython() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pythonSiteLifecycleMu.Lock()
		defer pythonSiteLifecycleMu.Unlock()
		rows, err := database.DB.Query(`SELECT d.id FROM daemons d JOIN sites s ON s.id = d.site_id
			WHERE d.managed_kind = 'python_app' AND LOWER(COALESCE(s.app_type, '')) = 'python'
			AND COALESCE(s.deletion_status, '') = '' ORDER BY d.id`)
		if err != nil {
			http.Error(w, "Failed to load Python sites", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		var ids []int
		for rows.Next() {
			var id int
			if err := rows.Scan(&id); err != nil {
				http.Error(w, "Failed to read Python sites", http.StatusInternalServerError)
				return
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			http.Error(w, "Failed to read Python sites", http.StatusInternalServerError)
			return
		}
		var failures []string
		for _, id := range ids {
			if err := daemon.RestartAndWait(r.Context(), id); err != nil {
				failures = append(failures, fmt.Sprintf("daemon %d: %v", id, err))
				continue
			}
			_, _ = database.DB.Exec("UPDATE daemons SET status = 'active' WHERE id = ?", id)
		}
		if len(failures) > 0 {
			http.Error(w, "Failed to restart some Python applications: "+strings.Join(failures, "; "), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func requirePythonToolchain(ctx context.Context) error {
	status := pythontoolchain.Inspect(ctx)
	if status.ToolchainReady {
		return nil
	}
	missing := strings.Join(status.Missing, ", ")
	if missing == "" {
		missing = "required components"
	}
	return fmt.Errorf("Python application support is not ready (missing: %s). Install or repair it from Runtime > Python", missing)
}
