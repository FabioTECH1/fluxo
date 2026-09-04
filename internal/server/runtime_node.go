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
	"fluxo/internal/services/nodetoolchain"
)

var nodeSiteLifecycleMu sync.Mutex

func writeNodeRuntimeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// handleRestartNode restarts existing Node.js and Bun application daemons.
func (s *Server) handleRestartNode() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeSiteLifecycleMu.Lock()
		defer nodeSiteLifecycleMu.Unlock()

		rows, err := database.DB.Query(`
			SELECT d.id
			FROM daemons d
			JOIN sites s ON s.id = d.site_id
			WHERE d.name = 'Node.js'
			  AND LOWER(COALESCE(s.app_type, '')) = 'node'
			  AND COALESCE(s.deletion_status, '') = ''
			ORDER BY d.id`)
		if err != nil {
			http.Error(w, "Failed to load Node.js sites", http.StatusInternalServerError)
			return
		}
		var daemonIDs []int
		for rows.Next() {
			var daemonID int
			if err := rows.Scan(&daemonID); err != nil {
				rows.Close()
				http.Error(w, "Failed to read Node.js sites", http.StatusInternalServerError)
				return
			}
			daemonIDs = append(daemonIDs, daemonID)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			http.Error(w, "Failed to read Node.js sites", http.StatusInternalServerError)
			return
		}
		if err := rows.Close(); err != nil {
			http.Error(w, "Failed to read Node.js sites", http.StatusInternalServerError)
			return
		}
		var restartErrors []string
		for _, daemonID := range daemonIDs {
			if err := daemon.Restart(r.Context(), daemonID); err != nil {
				restartErrors = append(restartErrors, fmt.Sprintf("daemon %d: %v", daemonID, err))
				continue
			}
			_, _ = database.DB.Exec("UPDATE daemons SET status = 'active' WHERE id = ?", daemonID)
		}
		if len(restartErrors) > 0 {
			http.Error(w, "Failed to restart some Node.js applications: "+strings.Join(restartErrors, "; "), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleInstallNode installs or repairs the complete Node.js toolchain.
func (s *Server) handleInstallNode() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeSiteLifecycleMu.Lock()
		defer nodeSiteLifecycleMu.Unlock()
		runtimePackageMutationMu.Lock()
		defer runtimePackageMutationMu.Unlock()

		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
		defer cancel()

		status, err := nodetoolchain.Install(ctx)
		if err != nil {
			w.Header().Set("Cache-Control", "no-store, max-age=0")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeNodeRuntimeJSON(w, http.StatusOK, map[string]any{
			"status":  "ok",
			"message": "Node.js toolchain installed successfully",
			"runtime": status,
		})
	}
}

// handleRemoveNode removes only the Node.js toolchain managed by Fluxo.
func (s *Server) handleRemoveNode() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeSiteLifecycleMu.Lock()
		defer nodeSiteLifecycleMu.Unlock()
		runtimePackageMutationMu.Lock()
		defer runtimePackageMutationMu.Unlock()

		var nodeSites int
		if err := databaseCountNodeSites(&nodeSites); err != nil {
			http.Error(w, "Failed to check Node.js site usage", http.StatusInternalServerError)
			return
		}
		if nodeSites > 0 {
			http.Error(w, "Remove all Node.js sites before removing the Node.js toolchain", http.StatusConflict)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
		defer cancel()
		if err := nodetoolchain.Remove(ctx); err != nil {
			http.Error(w, "Node.js toolchain removal failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeNodeRuntimeJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"message": "Fluxo-managed Node.js toolchain removed successfully",
		})
	}
}

func databaseCountNodeSites(count *int) error {
	return database.DB.QueryRow("SELECT COUNT(*) FROM sites WHERE LOWER(COALESCE(app_type, '')) = 'node'").Scan(count)
}

// handleGetNodeInfo returns all Node.js toolchain versions and readiness.
func (s *Server) handleGetNodeInfo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		status := nodetoolchain.Inspect(ctx)
		writeNodeRuntimeJSON(w, http.StatusOK, status)
	}
}

func requireNodeToolchain(ctx context.Context) error {
	status := nodetoolchain.Inspect(ctx)
	if status.ToolchainReady {
		return nil
	}
	missing := strings.Join(status.Missing, ", ")
	if missing == "" {
		missing = "required components"
	}
	return &nodeToolchainUnavailableError{missing: missing}
}

type nodeToolchainUnavailableError struct {
	missing string
}

func (e *nodeToolchainUnavailableError) Error() string {
	return "Node.js toolchain is not ready (missing: " + e.missing + "). Install or repair it from Runtime > Node.js"
}
