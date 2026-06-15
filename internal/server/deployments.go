// Deployment handlers: list deployments for a site, trigger new deployment.
// Triggering a deployment is asynchronous: a deployment record is created
// with status "running", the deploy script is generated and executed in
// a goroutine (as www-data), output is streamed via WebSocket, and the
// record is updated with success/failure + full output.
package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"fluxo/internal/database"
	"fluxo/internal/services/deploy"
)

func (s *Server) handleListDeployments() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteIDStr := r.PathValue("id")
		siteID, _ := strconv.Atoi(siteIDStr)

		pageStr := r.URL.Query().Get("page")
		page, _ := strconv.Atoi(pageStr)
		if page < 1 {
			page = 1
		}
		limit := 12
		offset := (page - 1) * limit

		var total int
		database.DB.QueryRow("SELECT COUNT(*) FROM deployments WHERE site_id = ?", siteID).Scan(&total)

		rows, err := database.DB.Query("SELECT id, site_id, commit_hash, commit_message, branch, trigger_source, status, output, created_at, updated_at FROM deployments WHERE site_id = ? ORDER BY id DESC LIMIT ? OFFSET ?", siteID, limit, offset)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		deployments := []database.Deployment{}
		for rows.Next() {
			var d database.Deployment
			var commitHash, commitMessage, branch, output sql.NullString
			if err := rows.Scan(&d.ID, &d.SiteID, &commitHash, &commitMessage, &branch, &d.TriggerSource, &d.Status, &output, &d.CreatedAt, &d.UpdatedAt); err != nil {
				continue
			}
			d.CommitHash = commitHash.String
			d.CommitMessage = commitMessage.String
			d.Branch = branch.String
			if d.TriggerSource == "" {
				d.TriggerSource = "manual"
			}
			d.Output = output.String
			deployments = append(deployments, d)
		}

		response := map[string]interface{}{
			"data":         deployments,
			"current_page": page,
			"total":        total,
			"per_page":     limit,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
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

		_, err = database.DB.Exec("INSERT INTO deployments (site_id, status, trigger_source) VALUES (?, ?, ?)", siteID, "pending", "manual")
		if err != nil {
			http.Error(w, "Failed to create deployment record", http.StatusInternalServerError)
			return
		}

		deploy.Enqueue(siteID)

		w.WriteHeader(http.StatusAccepted)
	}
}
