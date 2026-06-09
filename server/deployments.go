// Deployment handlers: list deployments for a site, trigger new deployment.
// Triggering a deployment is asynchronous: a deployment record is created
// with status "running", the deploy script is generated and executed in
// a goroutine (as www-data), output is streamed via WebSocket, and the
// record is updated with success/failure + full output.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"fluxo/database"
	"fluxo/services/deploy"
)

func (s *Server) handleListDeployments() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteIDStr := r.PathValue("id")
		siteID, _ := strconv.Atoi(siteIDStr)

		rows, err := database.DB.Query("SELECT id, site_id, commit_hash, status, output, created_at, updated_at FROM deployments WHERE site_id = ? ORDER BY id DESC", siteID)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		deployments := []database.Deployment{}
		for rows.Next() {
			var d database.Deployment
			if err := rows.Scan(&d.ID, &d.SiteID, &d.CommitHash, &d.Status, &d.Output, &d.CreatedAt, &d.UpdatedAt); err != nil {
				continue
			}
			deployments = append(deployments, d)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(deployments)
	}
}

func (s *Server) handleTriggerDeployment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteIDStr := r.PathValue("id")
		siteID, err := strconv.Atoi(siteIDStr)
		if err != nil {
			http.Error(w, "Invalid ID", http.StatusBadRequest)
			return
		}

		res, err := database.DB.Exec("INSERT INTO deployments (site_id, status) VALUES (?, ?)", siteID, "running")
		if err != nil {
			http.Error(w, "Failed to create deployment record", http.StatusInternalServerError)
			return
		}
		deployID, _ := res.LastInsertId()

		var strategy, domain, repo, branch, phpVer, appType string
		err = database.DB.QueryRow("SELECT deployment_strategy, domain, repository, branch, php_version, app_type FROM sites WHERE id = ?", siteID).Scan(&strategy, &domain, &repo, &branch, &phpVer, &appType)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		script := deploy.GenerateDeployScript(strategy, domain, repo, branch, phpVer, appType)

		go func() {
			privKeyPath := fmt.Sprintf("/root/.ssh/fluxo_site_%d_ed25519", siteID)

			output, err := deploy.RunScript(r.Context(), siteID, script, privKeyPath, GlobalHub)

			status := "success"
			if err != nil {
				status = "failed"
			}

			database.DB.Exec("UPDATE deployments SET status = ?, output = ? WHERE id = ?", status, output, deployID)
		}()

		w.WriteHeader(http.StatusAccepted)
	}
}
