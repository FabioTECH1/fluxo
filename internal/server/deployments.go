// Deployment handlers: list deployments for a site, trigger new deployment.
// Triggering a deployment is asynchronous: a deployment record is created
// with status "running", the deploy script is generated and executed in
// a goroutine (as www-data), output is streamed via WebSocket, and the
// record is updated with success/failure + full output.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"fluxo/internal/config"
	"fluxo/internal/database"
	"fluxo/internal/services/deploy"
	"fluxo/internal/services/git"
	"fluxo/internal/syscmd"
	"time"
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

		res, err := database.DB.Exec("INSERT INTO deployments (site_id, status) VALUES (?, ?)", siteID, "running")
		if err != nil {
			http.Error(w, "Failed to create deployment record", http.StatusInternalServerError)
			return
		}
		deployID, _ := res.LastInsertId()

		var strategy, domain, repo, branch, phpVer, appType, deployScript string
		err = database.DB.QueryRow("SELECT deployment_strategy, domain, repository, branch, php_version, app_type, deploy_script FROM sites WHERE id = ?", siteID).Scan(&strategy, &domain, &repo, &branch, &phpVer, &appType, &deployScript)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		var dbName, dbUser, dbEngine, dbConn, dbPort string
		database.DB.QueryRow("SELECT name, username, engine FROM databases WHERE site_id = ? LIMIT 1", siteID).Scan(&dbName, &dbUser, &dbEngine)

		var dbPass string
		database.DB.QueryRow("SELECT fluxo_db_password FROM users LIMIT 1").Scan(&dbPass)
		dbPass = config.Decrypt(dbPass)

		if dbEngine == "postgres" || dbEngine == "pgsql" {
			dbConn = "pgsql"
			dbPort = "5432"
		} else {
			dbConn = "mysql"
			dbPort = "3306"
		}

		var script string
		if deployScript != "" {
			script = deployScript
		} else {
			script = deploy.GenerateDeployScript(strategy)
		}

		go func() {
			privKeyPath := git.GetSSHKeyPath(siteID)
			repoURL := "git@github.com:" + repo + ".git"

			envMap := map[string]string{
				"FLUXO_PHP_VERSION": phpVer,
				"FLUXO_PHP":         "php" + phpVer,
				"FLUXO_COMPOSER":    "php" + phpVer + " /usr/local/bin/composer",
				"FLUXO_SITE_PATH":   "/home/fluxo/" + domain,
				"FLUXO_BRANCH":      branch,
				"FLUXO_REPO":        repoURL,
				"FLUXO_DOMAIN":      domain,
				"FLUXO_DB_CONN":     dbConn,
				"FLUXO_DB_PORT":     dbPort,
				"FLUXO_DB_NAME":     dbName,
				"FLUXO_DB_USER":     dbUser,
				"FLUXO_DB_PASS":     dbPass,
			}

			// Append under-the-hood final commands
			if (appType == "php" || appType == "laravel") && phpVer != "" {
				script += "\n\nsudo systemctl reload php$FLUXO_PHP_VERSION-fpm\n"
			}
			script += "\necho \"Deployment complete.\"\n"

			output, err := deploy.RunScript(context.Background(), siteID, script, privKeyPath, envMap, GlobalHub)

			// Fetch latest commit metadata
			var commitHash, commitMessage string
			commitLog, _ := syscmd.RunEnvAsUser(context.Background(), 5*time.Second, "fluxo", []string{"HOME=/home/fluxo"}, "git", "-C", "/home/fluxo/"+domain, "log", "-1", "--format=%H|%s|%an")
			parts := strings.SplitN(strings.TrimSpace(commitLog), "|", 3)
			if len(parts) == 3 {
				commitHash = parts[0]
				commitMessage = parts[1] + " by " + parts[2]
			} else {
				commitHash = strings.TrimSpace(commitLog)
			}

			status := "success"
			if err != nil {
				status = "failed"
			}

			// For manual deployments from the UI, trigger_source is "manual"
			database.DB.Exec("UPDATE deployments SET status = ?, output = ?, commit_hash = ?, commit_message = ?, branch = ?, trigger_source = 'manual' WHERE id = ?", status, output, commitHash, commitMessage, branch, deployID)

			triggerLabel := "manual"
			if r.Header.Get("X-GitHub-Event") != "" {
				triggerLabel = "GitHub push"
			}
			LogActivity(siteID, "deployment", "Deployment #"+strconv.FormatInt(deployID, 10)+" "+status+" — triggered by "+triggerLabel)
		}()

		w.WriteHeader(http.StatusAccepted)
	}
}
