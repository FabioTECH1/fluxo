// Deployment handlers for listing, triggering, and rolling back site deployments.
package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"fluxo/internal/database"
	"fluxo/internal/services/deploy"
)

const deploymentSelectColumns = `id, site_id, commit_hash, commit_message, commit_author, branch,
	trigger_source, target_commit_hash, status, output, failure_reason, created_at, updated_at`

type deploymentScanner interface {
	Scan(dest ...any) error
}

func scanDeployment(scanner deploymentScanner) (database.Deployment, error) {
	var d database.Deployment
	var commitHash, commitMessage, commitAuthor, branch, output, targetCommitHash, failureReason sql.NullString
	if err := scanner.Scan(
		&d.ID, &d.SiteID, &commitHash, &commitMessage, &commitAuthor, &branch,
		&d.TriggerSource, &targetCommitHash, &d.Status, &output, &failureReason,
		&d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		return d, err
	}

	d.CommitHash = commitHash.String
	d.CommitMessage = commitMessage.String
	d.CommitAuthor = commitAuthor.String
	// Older deployment rows stored the author as part of "message by author".
	if d.CommitAuthor == "" {
		if separator := strings.LastIndex(d.CommitMessage, " by "); separator > 0 && separator+4 < len(d.CommitMessage) {
			d.CommitAuthor = d.CommitMessage[separator+4:]
			d.CommitMessage = d.CommitMessage[:separator]
		}
	}
	d.Branch = branch.String
	d.TargetCommitHash = targetCommitHash.String
	if d.TriggerSource == "" {
		d.TriggerSource = "manual"
	}
	d.Output = output.String
	d.FailureReason = failureReason.String
	return d, nil
}

func unresolvedDeploymentFailure(siteID int) (*database.Deployment, error) {
	row := database.DB.QueryRow(`SELECT `+deploymentSelectColumns+`
		FROM deployments
		WHERE site_id = ?
		AND id = (
			SELECT id FROM deployments
			WHERE site_id = ? AND status IN ('success', 'failed')
				AND COALESCE(trigger_source, 'manual') != 'repo_sync'
			ORDER BY id DESC LIMIT 1
		)
		AND status = 'failed'
		AND failure_dismissed_at IS NULL`, siteID, siteID)
	d, err := scanDeployment(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// handleRollbackDeployment enqueues a rollback deployment that checks out the target deployment's commit.
func (s *Server) handleRollbackDeployment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteIDStr := r.PathValue("id")
		siteID, err := strconv.Atoi(siteIDStr)
		if err != nil {
			http.Error(w, "Invalid site ID", http.StatusBadRequest)
			return
		}

		depIDStr := r.PathValue("depId")
		depID, err := strconv.Atoi(depIDStr)
		if err != nil {
			http.Error(w, "Invalid deployment ID", http.StatusBadRequest)
			return
		}

		// Fetch the target deployment and verify it belongs to this site
		var commitHash, targetBranch sql.NullString
		var status string
		err = database.DB.QueryRow("SELECT status, commit_hash, branch FROM deployments WHERE id = ? AND site_id = ?", depID, siteID).Scan(&status, &commitHash, &targetBranch)
		if err != nil {
			http.Error(w, "Deployment not found", http.StatusNotFound)
			return
		}

		if status != "success" {
			http.Error(w, "Can only rollback a successful deployment", http.StatusUnprocessableEntity)
			return
		}

		if commitHash.String == "" {
			http.Error(w, "Deployment has no commit hash to rollback to", http.StatusUnprocessableEntity)
			return
		}

		branch := targetBranch.String

		domainMutationMu.Lock()
		result, err := database.DB.Exec(`INSERT INTO deployments
			(site_id, status, trigger_source, target_commit_hash, branch)
			SELECT ?, 'pending', 'rollback', ?, ?
			WHERE EXISTS (SELECT 1 FROM sites WHERE id = ? AND COALESCE(deletion_status, '') = '')`,
			siteID, commitHash.String, branch, siteID)
		domainMutationMu.Unlock()
		if err != nil {
			http.Error(w, "Failed to create rollback deployment record", http.StatusInternalServerError)
			return
		}
		if affected, err := result.RowsAffected(); err != nil || affected != 1 {
			http.Error(w, "Site deletion has started", http.StatusConflict)
			return
		}

		deploy.Enqueue(siteID)

		w.WriteHeader(http.StatusAccepted)
	}
}

// handleListDeployments returns paginated deployments for a site.
func (s *Server) handleListDeployments() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteIDStr := r.PathValue("id")
		siteID, err := strconv.Atoi(siteIDStr)
		if err != nil || siteID < 1 {
			http.Error(w, "Invalid site ID", http.StatusBadRequest)
			return
		}

		pageStr := r.URL.Query().Get("page")
		page, _ := strconv.Atoi(pageStr)
		if page < 1 {
			page = 1
		}
		limit := 12
		offset := (page - 1) * limit

		var total int
		if err := database.DB.QueryRow("SELECT COUNT(*) FROM deployments WHERE site_id = ?", siteID).Scan(&total); err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		rows, err := database.DB.Query("SELECT "+deploymentSelectColumns+" FROM deployments WHERE site_id = ? ORDER BY id DESC LIMIT ? OFFSET ?", siteID, limit, offset)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		deployments := []database.Deployment{}
		for rows.Next() {
			d, err := scanDeployment(rows)
			if err != nil {
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
			deployments = append(deployments, d)
		}
		if err := rows.Err(); err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if err := rows.Close(); err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		unresolvedFailure, err := unresolvedDeploymentFailure(siteID)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		response := map[string]interface{}{
			"data":               deployments,
			"current_page":       page,
			"total":              total,
			"per_page":           limit,
			"unresolved_failure": unresolvedFailure,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// handleTriggerDeployment enqueues an async deployment for a site.
func (s *Server) handleTriggerDeployment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteIDStr := r.PathValue("id")
		siteID, err := strconv.Atoi(siteIDStr)
		if err != nil {
			http.Error(w, "Invalid ID", http.StatusBadRequest)
			return
		}
		var appType, deployScript string
		if err := database.DB.QueryRow("SELECT app_type, COALESCE(deploy_script, '') FROM sites WHERE id = ?", siteID).Scan(&appType, &deployScript); err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		if appType == "wordpress" && strings.TrimSpace(deployScript) == "" {
			http.Error(w, "Configure a deployment script before deploying this WordPress site", http.StatusUnprocessableEntity)
			return
		}

		domainMutationMu.Lock()
		result, err := database.DB.Exec(`INSERT INTO deployments (site_id, status, trigger_source)
			SELECT ?, 'pending', 'manual'
			WHERE EXISTS (SELECT 1 FROM sites WHERE id = ? AND COALESCE(deletion_status, '') = '')`, siteID, siteID)
		domainMutationMu.Unlock()
		if err != nil {
			http.Error(w, "Failed to create deployment record", http.StatusInternalServerError)
			return
		}
		if affected, err := result.RowsAffected(); err != nil || affected != 1 {
			http.Error(w, "Site deletion has started", http.StatusConflict)
			return
		}
		deploymentID, err := result.LastInsertId()
		if err != nil {
			http.Error(w, "Failed to read deployment record", http.StatusInternalServerError)
			return
		}

		deploy.Enqueue(siteID)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"deployment_id": deploymentID,
			"status":        "pending",
		})
	}
}

// handleDismissDeploymentFailure acknowledges the site's latest completed failure.
func (s *Server) handleDismissDeploymentFailure() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, err := strconv.Atoi(r.PathValue("id"))
		if err != nil || siteID < 1 {
			http.Error(w, "Invalid site ID", http.StatusBadRequest)
			return
		}
		deploymentID, err := strconv.Atoi(r.PathValue("depId"))
		if err != nil || deploymentID < 1 {
			http.Error(w, "Invalid deployment ID", http.StatusBadRequest)
			return
		}

		var targetStatus, targetSource string
		err = database.DB.QueryRow(`SELECT status, COALESCE(trigger_source, 'manual')
			FROM deployments WHERE id = ? AND site_id = ?`, deploymentID, siteID).Scan(&targetStatus, &targetSource)
		if err == sql.ErrNoRows {
			http.Error(w, "Deployment not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if targetStatus != "failed" {
			http.Error(w, "Deployment did not fail", http.StatusUnprocessableEntity)
			return
		}
		if targetSource == "repo_sync" {
			http.Error(w, "Repository sync failures are not deployment incidents", http.StatusUnprocessableEntity)
			return
		}

		result, err := database.DB.Exec(`UPDATE deployments
			SET failure_dismissed_at = CURRENT_TIMESTAMP
			WHERE site_id = ? AND status = 'failed' AND id <= ? AND failure_dismissed_at IS NULL
				AND COALESCE(trigger_source, 'manual') != 'repo_sync'
				AND ? = (
					SELECT id FROM deployments
					WHERE site_id = ? AND status IN ('success', 'failed')
						AND COALESCE(trigger_source, 'manual') != 'repo_sync'
					ORDER BY id DESC LIMIT 1
				)`, siteID, deploymentID, deploymentID, siteID)
		if err != nil {
			http.Error(w, "Failed to dismiss deployment failure", http.StatusInternalServerError)
			return
		}
		affected, err := result.RowsAffected()
		if err != nil {
			http.Error(w, "Failed to confirm deployment dismissal", http.StatusInternalServerError)
			return
		}
		if affected > 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		currentFailure, err := unresolvedDeploymentFailure(siteID)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if currentFailure == nil {
			// The target was already dismissed or a successful retry resolved it.
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if currentFailure.ID != deploymentID {
			http.Error(w, "A newer deployment failure is now active", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to dismiss deployment failure", http.StatusInternalServerError)
	}
}
