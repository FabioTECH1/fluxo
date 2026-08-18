// Nginx site management: create, read, update, delete.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/config"
	"fluxo/internal/database"
	"fluxo/internal/safeinput"
	"fluxo/internal/services/cron"
	"fluxo/internal/services/daemon"
	"fluxo/internal/services/deploy"
	"fluxo/internal/services/git"
	"fluxo/internal/services/mysql"
	"fluxo/internal/services/nginx"
	"fluxo/internal/services/php"
	"fluxo/internal/services/postgres"
	"fluxo/internal/services/site"
	"fluxo/internal/syscmd"
)

type CreateSiteRequest struct {
	Domain             string `json:"domain"`
	PHPVersion         string `json:"php_version"`
	WebRoot            string `json:"web_root"`
	Repository         string `json:"repository"`
	Branch             string `json:"branch"`
	DeploymentStrategy string `json:"deployment_strategy"`
	AppType            string `json:"app_type"`
	AppPort            int    `json:"app_port"`
	NodePreset         string `json:"node_preset"`
	NodeMode           string `json:"node_mode"`
	PackageManager     string `json:"package_manager"`
	BuildCommand       string `json:"build_command"`
	StartCommand       string `json:"start_command"`
	StaticOutputDir    string `json:"static_output_dir"`
	DatabaseName       string `json:"database_name"`
	DatabaseUser       string `json:"database_user"`
	DatabasePassword   string `json:"database_password"`
	DBEngine           string `json:"db_engine"`
	InstallComposer    bool   `json:"install_composer"`
	GitHubAccountID    int    `json:"github_account_id"`
}

var domainRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+$`)

func isValidAppType(appType string) bool {
	switch appType {
	case "php", "laravel", "html", "node", "wordpress":
		return true
	default:
		return false
	}
}

func isValidDeploymentStrategy(strategy string) bool {
	switch strategy {
	case "standard", "zero-downtime":
		return true
	default:
		return false
	}
}

func validateDeploymentStrategyUnchanged(current, requested string) error {
	if requested != "" && requested != current {
		return fmt.Errorf("deployment strategy is fixed when the site is created and cannot be changed")
	}
	return nil
}

func validateApplicationTypeUnchanged(current, requested string) error {
	if requested != "" && requested != current {
		return fmt.Errorf("application type cannot be changed after site creation")
	}
	return nil
}

func shouldSyncRepositoryInPlace(strategy string) bool {
	return strategy != "zero-downtime"
}

func validateDeploymentCompatibility(appType, strategy, repo string, appPort int, nodeMode string) error {
	if appType == "wordpress" {
		if strategy != "standard" {
			return fmt.Errorf("WordPress sites only support standard in-place hosting")
		}
		if repo != "" {
			return fmt.Errorf("WordPress site creation does not use a Git repository")
		}
	}
	if appType == "node" {
		if site.NormalizeNodeMode(nodeMode) == "server" && !safeinput.ValidatePortNumber(appPort) {
			return fmt.Errorf("Node.js server sites require a valid app port")
		}
		if strategy == "zero-downtime" && repo == "" {
			return fmt.Errorf("Zero-downtime deployment requires a repository")
		}
	}
	if strategy == "zero-downtime" {
		if appType != "laravel" && appType != "php" && appType != "html" && appType != "node" {
			return fmt.Errorf("Zero-downtime deployment is only supported for Laravel, PHP, HTML, and Node.js sites")
		}
		if repo == "" {
			return fmt.Errorf("Zero-downtime deployment requires a repository")
		}
	}
	return nil
}

func validateSiteDatabaseCredentials(req CreateSiteRequest) error {
	databaseCapable := req.AppType == "laravel" || req.AppType == "php" || req.AppType == "wordpress"
	if req.DatabaseName != "" && !databaseCapable {
		return fmt.Errorf("database connections are not supported for this application type")
	}
	if req.DatabaseName == "" {
		if req.AppType == "wordpress" {
			return fmt.Errorf("WordPress requires a database")
		}
		if req.DatabaseUser != "" || req.DatabasePassword != "" {
			return fmt.Errorf("database credentials require a selected database")
		}
		return nil
	}
	if req.DatabaseUser == "" || req.DatabasePassword == "" {
		return fmt.Errorf("a dedicated database username and password are required")
	}
	if req.DatabaseUser == "fluxo" || req.DatabaseUser == "root" || req.DatabaseUser == "postgres" {
		return fmt.Errorf("the database control-plane account cannot be used by an application")
	}
	if (req.AppType == "laravel" || req.AppType == "php") && strings.Contains(req.DatabasePassword, "'") {
		return fmt.Errorf("database passwords for Laravel and PHP sites cannot contain a single quote")
	}
	return nil
}

func deleteIncompleteSiteRecord(id int64, domain string) error {
	if id > 0 {
		_, err := database.DB.Exec("DELETE FROM sites WHERE id = ?", id)
		return err
	}
	_, err := database.DB.Exec("DELETE FROM sites WHERE domain = ?", domain)
	return err
}

// handleGetSite returns a single site by ID.
func (s *Server) handleGetSite() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "Invalid ID", http.StatusBadRequest)
			return
		}
		var site database.Site
		err = database.DB.QueryRow(`SELECT id, domain, path, COALESCE(php_version, ''), COALESCE(repository, ''),
			COALESCE(branch, ''), COALESCE(app_type, 'php'), COALESCE(app_port, 0), COALESCE(node_preset, ''),
			COALESCE(node_mode, ''), COALESCE(package_manager, 'npm'), COALESCE(build_command, ''),
			COALESCE(start_command, ''), COALESCE(static_output_dir, ''), COALESCE(deployment_strategy, 'standard'),
			COALESCE(ssl_provider, 'none'), COALESCE(ssl_active, 0), COALESCE(web_root, '/public'),
			COALESCE(push_to_deploy, 0), COALESCE(deploy_script, ''), COALESCE(deploy_script_mode, 'legacy'),
			COALESCE(expose_env, 0), COALESCE(db_engine, ''),
			COALESCE(deletion_status, ''), COALESCE(deletion_error, ''), COALESCE(deletion_stage, ''),
			COALESCE(deletion_delete_databases, 0), COALESCE(deletion_database_ids, ''),
			COALESCE(github_account_id, 0), created_at, updated_at FROM sites WHERE id = ?`, id).Scan(
			&site.ID, &site.Domain, &site.Path, &site.PHPVersion, &site.Repository, &site.Branch, &site.AppType, &site.AppPort, &site.NodePreset, &site.NodeMode, &site.PackageManager, &site.BuildCommand, &site.StartCommand, &site.StaticOutputDir, &site.DeploymentStrategy, &site.SSLProvider, &site.SSLActive, &site.WebRoot, &site.PushToDeploy, &site.DeployScript, &site.DeployScriptMode, &site.ExposeEnv, &site.DBEngine, &site.DeletionStatus, &site.DeletionError, &site.DeletionStage, &site.DeletionDeleteDBs, &site.DeletionDatabaseIDs, &site.GithubAccountID, &site.CreatedAt, &site.UpdatedAt,
		)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(site)
	}
}

type UpdateSiteRequest struct {
	AppType            string  `json:"app_type"`
	AppPort            *int    `json:"app_port"`
	PHPVersion         string  `json:"php_version"`
	WebRoot            string  `json:"web_root"`
	DeploymentStrategy string  `json:"deployment_strategy"`
	Repository         *string `json:"repository"`
	Branch             *string `json:"branch"`
	PushToDeploy       *bool   `json:"push_to_deploy"`
	DeployScript       *string `json:"deploy_script"`
	DeployScriptMode   *string `json:"deploy_script_mode"`
	ExposeEnv          *bool   `json:"expose_env"`
	NodePreset         *string `json:"node_preset"`
	NodeMode           *string `json:"node_mode"`
	PackageManager     *string `json:"package_manager"`
	BuildCommand       *string `json:"build_command"`
	StartCommand       *string `json:"start_command"`
	StaticOutputDir    *string `json:"static_output_dir"`
}

// handleUpdateSite patches site settings and triggers nginx regen or repo sync as needed.
func (s *Server) handleUpdateSite() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "Invalid ID", http.StatusBadRequest)
			return
		}

		var req UpdateSiteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		if req.Repository != nil {
			trimmed := strings.TrimSpace(*req.Repository)
			req.Repository = &trimmed
		}
		if req.Branch != nil {
			trimmed := strings.TrimSpace(*req.Branch)
			req.Branch = &trimmed
		}
		trimStringPtr := func(v **string) {
			if *v != nil {
				trimmed := strings.TrimSpace(**v)
				*v = &trimmed
			}
		}
		trimStringPtr(&req.NodePreset)
		trimStringPtr(&req.NodeMode)
		trimStringPtr(&req.PackageManager)
		trimStringPtr(&req.BuildCommand)
		trimStringPtr(&req.StartCommand)
		trimStringPtr(&req.StaticOutputDir)
		req.WebRoot = strings.TrimSpace(req.WebRoot)

		if req.AppType != "" && !isValidAppType(req.AppType) {
			http.Error(w, "Invalid app type", http.StatusBadRequest)
			return
		}
		if req.DeploymentStrategy != "" && !isValidDeploymentStrategy(req.DeploymentStrategy) {
			http.Error(w, "Invalid deployment strategy", http.StatusBadRequest)
			return
		}
		if req.DeployScriptMode != nil && *req.DeployScriptMode != deploy.ScriptModeManaged && *req.DeployScriptMode != deploy.ScriptModeLegacy {
			http.Error(w, "Invalid deployment script mode", http.StatusBadRequest)
			return
		}
		if req.PHPVersion != "" && !safeinput.ValidatePHPVersion(req.PHPVersion) {
			http.Error(w, "Invalid PHP version", http.StatusBadRequest)
			return
		}
		if req.Repository != nil {
			if *req.Repository != "" && !safeinput.ValidateRepoFullName(*req.Repository) {
				http.Error(w, "Invalid repository", http.StatusBadRequest)
				return
			}
		}
		if req.Branch != nil && !safeinput.ValidateGitRef(*req.Branch) {
			http.Error(w, "Invalid branch", http.StatusBadRequest)
			return
		}
		if req.AppPort != nil && *req.AppPort != 0 && !safeinput.ValidatePortNumber(*req.AppPort) {
			http.Error(w, "Invalid app port", http.StatusBadRequest)
			return
		}
		if req.NodePreset != nil && *req.NodePreset != site.NormalizeNodePreset(*req.NodePreset) {
			http.Error(w, "Invalid Node.js preset", http.StatusBadRequest)
			return
		}
		if req.NodeMode != nil && *req.NodeMode != site.NormalizeNodeMode(*req.NodeMode) {
			http.Error(w, "Invalid Node.js mode", http.StatusBadRequest)
			return
		}
		if req.PackageManager != nil && *req.PackageManager != site.NormalizePackageManager(*req.PackageManager) {
			http.Error(w, "Invalid package manager", http.StatusBadRequest)
			return
		}
		if req.BuildCommand != nil && safeinput.HasControlChars(*req.BuildCommand) {
			http.Error(w, "Invalid build command", http.StatusBadRequest)
			return
		}
		if req.StartCommand != nil && safeinput.HasControlChars(*req.StartCommand) {
			http.Error(w, "Invalid start command", http.StatusBadRequest)
			return
		}

		var curDomain, curSitePath, curAppType, curStrategy, curRepo, curBranch, curNodeMode, curDeployScript, curScriptMode, curDeletionStatus string
		var curAppPort int
		var curGithubDeployKeyID int64
		var curGithubAccountID int
		if err := database.DB.QueryRow("SELECT domain, path, app_type, deployment_strategy, repository, branch, COALESCE(app_port, 0), node_mode, COALESCE(deploy_script, ''), COALESCE(deploy_script_mode, 'legacy'), COALESCE(deletion_status, ''), COALESCE(github_deploy_key_id, 0), COALESCE(github_account_id, 0) FROM sites WHERE id = ?", id).Scan(&curDomain, &curSitePath, &curAppType, &curStrategy, &curRepo, &curBranch, &curAppPort, &curNodeMode, &curDeployScript, &curScriptMode, &curDeletionStatus, &curGithubDeployKeyID, &curGithubAccountID); err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		if curDeletionStatus != "" {
			http.Error(w, "Site deletion has started; retry deletion instead of changing site settings", http.StatusConflict)
			return
		}
		if curAppType == "" {
			curAppType = "php"
		}
		if curStrategy == "" {
			curStrategy = "standard"
		}
		repositoryWillChange := req.Repository != nil && *req.Repository != curRepo
		branchWillChange := req.Branch != nil && *req.Branch != curBranch
		if repositoryWillChange || branchWillChange {
			var activeDeployments int
			if err := database.DB.QueryRow("SELECT COUNT(*) FROM deployments WHERE site_id = ? AND status IN ('pending', 'running')", id).Scan(&activeDeployments); err != nil {
				http.Error(w, "Failed to validate deployment state", http.StatusInternalServerError)
				return
			}
			if activeDeployments > 0 {
				http.Error(w, "Wait for the site's deployments to finish before changing its repository or branch", http.StatusConflict)
				return
			}
		}
		if err := validateDeploymentStrategyUnchanged(curStrategy, req.DeploymentStrategy); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if err := validateApplicationTypeUnchanged(curAppType, req.AppType); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if req.WebRoot != "" {
			if _, err := safeinput.NormalizeWebRoot(curSitePath, req.WebRoot); err != nil {
				http.Error(w, "Invalid web root", http.StatusBadRequest)
				return
			}
		}

		effectiveAppType := curAppType
		effectiveStrategy := curStrategy
		if req.DeploymentStrategy != "" {
			effectiveStrategy = req.DeploymentStrategy
		}
		effectiveRepo := curRepo
		if req.Repository != nil {
			effectiveRepo = *req.Repository
		}
		effectiveAppPort := curAppPort
		if req.AppPort != nil {
			effectiveAppPort = *req.AppPort
		}
		effectiveNodeMode := curNodeMode
		if req.NodeMode != nil {
			effectiveNodeMode = *req.NodeMode
		}
		if effectiveAppType == "node" {
			nodeSiteLifecycleMu.Lock()
			defer nodeSiteLifecycleMu.Unlock()
			effectiveNodeMode = site.NormalizeNodeMode(effectiveNodeMode)
			if effectiveNodeMode == "static" {
				effectiveAppPort = 0
			}
		}
		if err := validateDeploymentCompatibility(effectiveAppType, effectiveStrategy, effectiveRepo, effectiveAppPort, effectiveNodeMode); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		octaneEnabled := isOctaneEnabled(id) || curStrategy == "octane"
		horizonEnabled := isHorizonEnabled(id)
		if octaneEnabled && effectiveAppType != "laravel" && effectiveAppType != "php" {
			http.Error(w, "Disable Laravel Octane before changing this site's app type", http.StatusBadRequest)
			return
		}
		if octaneEnabled && effectiveStrategy == "zero-downtime" {
			http.Error(w, "Disable Laravel Octane before enabling zero-downtime deployment", http.StatusBadRequest)
			return
		}
		if horizonEnabled && effectiveAppType != "laravel" && effectiveAppType != "php" {
			http.Error(w, "Disable Laravel Horizon before changing this site's app type", http.StatusBadRequest)
			return
		}
		usesAppPort := (effectiveAppType == "node" && effectiveNodeMode == "server") ||
			((effectiveAppType == "laravel" || effectiveAppType == "php") && octaneEnabled)
		if usesAppPort && effectiveAppPort > 0 {
			var portOwnerCount int
			if err := database.DB.QueryRow("SELECT COUNT(*) FROM sites WHERE id != ? AND app_port = ?", id, effectiveAppPort).Scan(&portOwnerCount); err != nil {
				http.Error(w, "Failed to validate app port", http.StatusInternalServerError)
				return
			}
			if portOwnerCount > 0 {
				http.Error(w, "Application port is already in use by another site", http.StatusBadRequest)
				return
			}
		}
		if req.StaticOutputDir != nil {
			if _, err := safeinput.NormalizeWebRoot(curSitePath, *req.StaticOutputDir); err != nil {
				http.Error(w, "Invalid static output directory", http.StatusBadRequest)
				return
			}
		}

		regenNginx := false
		syncNodeDaemon := false
		syncOctaneDaemon := false
		syncHorizonDaemon := false
		if req.PHPVersion != "" {
			database.DB.Exec("UPDATE sites SET php_version = ? WHERE id = ?", req.PHPVersion, id)
			regenNginx = true
			if octaneEnabled {
				syncOctaneDaemon = true
			}
			if horizonEnabled {
				syncHorizonDaemon = true
			}
		}
		if req.WebRoot != "" {
			database.DB.Exec("UPDATE sites SET web_root = ? WHERE id = ?", req.WebRoot, id)
			regenNginx = true
		}
		if req.NodeMode != nil {
			if *req.NodeMode == "static" {
				database.DB.Exec("UPDATE sites SET node_mode = ?, app_port = 0 WHERE id = ?", *req.NodeMode, id)
			} else {
				database.DB.Exec("UPDATE sites SET node_mode = ? WHERE id = ?", *req.NodeMode, id)
			}
			regenNginx = true
			syncNodeDaemon = true
		}
		if req.AppPort != nil {
			appPortToSave := 0
			if effectiveAppType == "node" && effectiveNodeMode == "server" {
				appPortToSave = *req.AppPort
			} else if (effectiveAppType == "laravel" || effectiveAppType == "php") && octaneEnabled {
				appPortToSave = curAppPort
				if safeinput.ValidatePortNumber(*req.AppPort) {
					appPortToSave = *req.AppPort
					syncOctaneDaemon = true
				}
			}
			if _, err := database.DB.Exec("UPDATE sites SET app_port = ? WHERE id = ?", appPortToSave, id); err != nil {
				http.Error(w, "Failed to update app port: "+err.Error(), http.StatusInternalServerError)
				return
			}
			regenNginx = true
			syncNodeDaemon = true
		}
		if req.NodePreset != nil {
			database.DB.Exec("UPDATE sites SET node_preset = ? WHERE id = ?", *req.NodePreset, id)
		}
		if req.PackageManager != nil {
			database.DB.Exec("UPDATE sites SET package_manager = ? WHERE id = ?", *req.PackageManager, id)
		}
		if req.BuildCommand != nil {
			database.DB.Exec("UPDATE sites SET build_command = ? WHERE id = ?", *req.BuildCommand, id)
		}
		if req.StartCommand != nil {
			database.DB.Exec("UPDATE sites SET start_command = ? WHERE id = ?", *req.StartCommand, id)
			syncNodeDaemon = true
		}
		if req.StaticOutputDir != nil {
			database.DB.Exec("UPDATE sites SET static_output_dir = ? WHERE id = ?", *req.StaticOutputDir, id)
			regenNginx = true
		}

		triggerRepoSync := false
		syncReason := ""
		repoDeployAccessUpdated := false

		if req.Repository != nil && *req.Repository != curRepo {
			if *req.Repository != "" {
				if err := ensureSiteDeployKeyAccess(r.Context(), id, curRepo, *req.Repository, curGithubAccountID, curGithubDeployKeyID); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				repoDeployAccessUpdated = true
			} else if curGithubDeployKeyID > 0 && curRepo != "" {
				go revokeSiteDeployKey(id, curRepo, curGithubAccountID, curGithubDeployKeyID)
				database.DB.Exec("UPDATE sites SET github_deploy_key_id = 0 WHERE id = ?", id)
			}
			database.DB.Exec("UPDATE sites SET repository = ? WHERE id = ?", *req.Repository, id)
			triggerRepoSync = true
			syncReason = "repository"
		}
		if req.Branch != nil && *req.Branch != curBranch {
			database.DB.Exec("UPDATE sites SET branch = ? WHERE id = ?", *req.Branch, id)
			triggerRepoSync = true
			if syncReason == "repository" {
				syncReason = "repository and branch"
			} else {
				syncReason = "branch"
			}
		}
		deployScriptHandled := false
		if req.DeployScriptMode != nil && *req.DeployScriptMode != curScriptMode {
			desiredScript := curDeployScript
			if req.DeployScript != nil {
				desiredScript = *req.DeployScript
			} else if *req.DeployScriptMode == deploy.ScriptModeManaged {
				desiredScript = deploy.GenerateApplicationCommands(effectiveAppType)
			}
			if _, err := database.DB.Exec("UPDATE sites SET deploy_script_mode = ?, deploy_script = ? WHERE id = ?", *req.DeployScriptMode, desiredScript, id); err != nil {
				http.Error(w, "Failed to update deployment lifecycle", http.StatusInternalServerError)
				return
			}
			curScriptMode = *req.DeployScriptMode
			deployScriptHandled = true
		}
		if req.DeployScript != nil && !deployScriptHandled {
			if _, err := database.DB.Exec("UPDATE sites SET deploy_script = ? WHERE id = ?", *req.DeployScript, id); err != nil {
				http.Error(w, "Failed to update deployment commands", http.StatusInternalServerError)
				return
			}
			if horizonEnabled && curScriptMode == deploy.ScriptModeLegacy {
				if err := addHorizonTerminateToDeployScript(id); err != nil {
					http.Error(w, "Failed to preserve Horizon deployment hook", http.StatusInternalServerError)
					return
				}
			}
		}
		if deployScriptHandled && horizonEnabled && curScriptMode == deploy.ScriptModeLegacy {
			if err := addHorizonTerminateToDeployScript(id); err != nil {
				http.Error(w, "Failed to preserve Horizon deployment hook", http.StatusInternalServerError)
				return
			}
		}
		if req.PushToDeploy != nil {
			if *req.PushToDeploy {
				database.DB.Exec("UPDATE sites SET push_to_deploy = 1 WHERE id = ?", id)

				// Register GitHub webhook asynchronously
				var accountID int
				database.DB.QueryRow("SELECT github_account_id FROM sites WHERE id = ?", id).Scan(&accountID)

				go func(siteID, accountID int, host string) {
					var repo string
					database.DB.QueryRow("SELECT repository FROM sites WHERE id = ?", siteID).Scan(&repo)
					repo = strings.TrimSpace(repo)

					if repo != "" && safeinput.ValidateRepoFullName(repo) {
						var pat, secret string
						database.DB.QueryRow("SELECT webhook_secret FROM users LIMIT 1").Scan(&secret)
						secret = config.Decrypt(secret)

						if accountID > 0 {
							database.DB.QueryRow("SELECT token FROM github_accounts WHERE id = ?", accountID).Scan(&pat)
						} else {
							database.DB.QueryRow("SELECT token FROM github_accounts ORDER BY id ASC LIMIT 1").Scan(&pat)
						}

						if pat != "" {
							pat = config.Decrypt(pat)
							if secret == "" {
								if generated, err := safeinput.GenerateSecretHex(32); err == nil {
									secret = generated
									database.DB.Exec("UPDATE users SET webhook_secret = ?", config.Encrypt(secret))
								}
							}

							provider := git.NewGitHubProvider(pat)
							webhookURL := "https://" + host + "/api/v1/github/webhook"
							hookID, err := provider.RegisterWebhook(repo, webhookURL, secret)
							if err != nil {
								log.Printf("Failed to register webhook for site %d (%s): %v", siteID, repo, err)
							} else if hookID > 0 {
								database.DB.Exec("UPDATE sites SET github_webhook_id = ? WHERE id = ?", hookID, siteID)
								log.Printf("Webhook registered for site %d (%s)", siteID, repo)
							}
						}
					}
				}(id, accountID, r.Host)

			} else {
				database.DB.Exec("UPDATE sites SET push_to_deploy = 0, github_webhook_id = 0 WHERE id = ?", id)
			}
		}
		if req.ExposeEnv != nil {
			if *req.ExposeEnv {
				database.DB.Exec("UPDATE sites SET expose_env = 1 WHERE id = ?", id)
			} else {
				database.DB.Exec("UPDATE sites SET expose_env = 0 WHERE id = ?", id)
			}
		}

		if regenNginx {
			go regenerateNginxForSite(id)
		}
		if syncNodeDaemon || (curAppType == "node" && effectiveAppType != "node") {
			go syncNodeDaemonForSite(context.Background(), id)
		}
		if syncOctaneDaemon {
			go func(siteID int) {
				if err := syncOctaneDaemonForSite(context.Background(), siteID); err != nil {
					log.Printf("Failed to sync Octane daemon for site %d: %v", siteID, err)
				}
			}(id)
		}
		if syncHorizonDaemon {
			go func(siteID int) {
				if err := syncHorizonDaemonForSite(context.Background(), siteID); err != nil {
					log.Printf("Failed to sync Horizon daemon for site %d: %v", siteID, err)
				}
			}(id)
		}

		if triggerRepoSync {
			var currentRepo, currentBranch, currentStrategy, siteDir string
			database.DB.QueryRow("SELECT repository, branch, COALESCE(deployment_strategy, 'standard'), path FROM sites WHERE id = ?", id).Scan(&currentRepo, &currentBranch, &currentStrategy, &siteDir)
			currentRepo = strings.TrimSpace(currentRepo)
			currentBranch = strings.TrimSpace(currentBranch)

			if currentRepo != "" && currentBranch != "" {
				if !safeinput.ValidateRepoFullName(currentRepo) || !safeinput.ValidateGitRef(currentBranch) {
					log.Printf("Skipping repo sync for site %d due to invalid repository or branch configuration", id)
				} else if !shouldSyncRepositoryInPlace(currentStrategy) {
					// Never mutate the active release or clone into the non-empty ZDD
					// site root. The next deployment clones these settings into a new
					// release and activates it only after application commands pass.
					summary := syncReason + " changed to " + currentRepo + " (" + currentBranch + "); the next zero-downtime deployment will apply it"
					LogActivity(id, "repo_sync", summary)
				} else {
					repoURL := "git@github.com:" + currentRepo + ".git"
					privKeyPath := git.GetSSHKeyPath(id)

					go func(waitForDeployKey bool) {
						if waitForDeployKey {
							time.Sleep(2 * time.Second)
						}
						ctx := context.Background()
						var out string
						var err error
						if _, statErr := os.Stat(filepath.Join(siteDir, ".git")); os.IsNotExist(statErr) {
							out, err = syscmd.RunEnvAsUser(ctx, 120*time.Second, "fluxo", []string{"GIT_SSH_COMMAND=ssh -o StrictHostKeyChecking=no -i " + privKeyPath}, "git", "clone", "-b", currentBranch, repoURL, siteDir)
						} else {
							out, err = syscmd.RunEnvAsUser(ctx, 30*time.Second, "fluxo", []string{"GIT_SSH_COMMAND=ssh -o StrictHostKeyChecking=no -i " + privKeyPath}, "git", "-C", siteDir, "remote", "set-url", "origin", repoURL)
							if err == nil {
								out, err = syscmd.RunEnvAsUser(ctx, 60*time.Second, "fluxo", []string{"GIT_SSH_COMMAND=ssh -o StrictHostKeyChecking=no -i " + privKeyPath}, "git", "-C", siteDir, "fetch", "origin")
							}
							if err == nil {
								out, err = syscmd.RunEnvAsUser(ctx, 30*time.Second, "fluxo", []string{"GIT_SSH_COMMAND=ssh -o StrictHostKeyChecking=no -i " + privKeyPath}, "git", "-C", siteDir, "checkout", "-f", "-B", currentBranch, "origin/"+currentBranch)
							}
						}
						status := "success"
						failureReason := ""
						summary := syncReason + " changed to " + currentRepo + " (" + currentBranch + ")"
						commitMsg := "Git sync — " + summary
						if err != nil {
							status = "failed"
							failureReason = err.Error()
							summary = "Failed to sync " + syncReason + ": " + err.Error()
							commitMsg = summary
						}
						database.DB.Exec(`INSERT INTO deployments
							(site_id, status, output, failure_reason, commit_message, branch, trigger_source)
							VALUES (?, ?, ?, ?, ?, ?, 'repo_sync')`, id, status, out, failureReason, commitMsg, currentBranch)
						LogActivity(id, "repo_sync", summary)
					}(repoDeployAccessUpdated)
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
	}
}

func ensureSiteDeployKeyAccess(ctx context.Context, siteID int, oldRepo, newRepo string, githubAccountID int, oldDeployKeyID int64) error {
	newRepo = strings.TrimSpace(newRepo)
	oldRepo = strings.TrimSpace(oldRepo)
	if newRepo == "" {
		return nil
	}
	if !safeinput.ValidateRepoFullName(newRepo) {
		return fmt.Errorf("invalid repository")
	}

	pat := githubTokenForAccount(githubAccountID)
	if pat == "" {
		return fmt.Errorf("connect a GitHub account before changing this site's repository")
	}

	stagedKeyPath, publicKey, cleanupStagedKey, err := git.GenerateTemporarySSHKey(ctx, siteID)
	if err != nil {
		return fmt.Errorf("failed to prepare deploy key: %w", err)
	}
	defer cleanupStagedKey()

	provider := git.NewGitHubProvider(config.Decrypt(pat))
	keyID, injectErr := provider.InjectDeployKey(newRepo, publicKey)
	if injectErr != nil {
		return fmt.Errorf("failed to grant deploy access to %s: %w", newRepo, injectErr)
	}
	if err := git.ReplaceSSHKeyPair(siteID, stagedKeyPath); err != nil {
		if keyID > 0 {
			_ = provider.RemoveDeployKey(newRepo, keyID)
		}
		return fmt.Errorf("failed to activate deploy key for %s: %w", newRepo, err)
	}
	if keyID > 0 {
		database.DB.Exec("UPDATE sites SET github_deploy_key_id = ? WHERE id = ?", keyID, siteID)
	}
	if oldDeployKeyID > 0 && oldRepo != "" && oldRepo != newRepo {
		go revokeSiteDeployKey(siteID, oldRepo, githubAccountID, oldDeployKeyID)
	}
	return nil
}

func revokeSiteDeployKey(siteID int, repo string, githubAccountID int, deployKeyID int64) {
	pat := githubTokenForAccount(githubAccountID)
	if pat == "" {
		LogActivity(siteID, "warning", "Failed to remove old GitHub deploy key: account token is unavailable")
		return
	}
	if err := git.NewGitHubProvider(config.Decrypt(pat)).RemoveDeployKey(repo, deployKeyID); err != nil {
		LogActivity(siteID, "warning", fmt.Sprintf("Failed to remove old GitHub deploy key: %v", err))
	}
}

func githubTokenForAccount(accountID int) string {
	var pat string
	if accountID > 0 {
		_ = database.DB.QueryRow("SELECT token FROM github_accounts WHERE id = ?", accountID).Scan(&pat)
	} else {
		_ = database.DB.QueryRow("SELECT token FROM github_accounts ORDER BY id ASC LIMIT 1").Scan(&pat)
	}
	return pat
}

type sqliteTime struct {
	Time  time.Time
	Valid bool
}

func (st *sqliteTime) Scan(value interface{}) error {
	if value == nil {
		st.Time, st.Valid = time.Time{}, false
		return nil
	}

	switch v := value.(type) {
	case time.Time:
		st.Time, st.Valid = v, true
		return nil
	case string:
		formats := []string{
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05Z",
			"2006-01-02T15:04:05Z07:00",
			time.RFC3339,
		}
		for _, f := range formats {
			if t, err := time.Parse(f, v); err == nil {
				st.Time, st.Valid = t, true
				return nil
			}
		}
		if t, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", v); err == nil {
			st.Time, st.Valid = t, true
			return nil
		}
		return fmt.Errorf("cannot parse SQLite time %q", v)
	case []byte:
		return st.Scan(string(v))
	}
	return fmt.Errorf("unsupported type %T for sqliteTime", value)
}

type siteListItem struct {
	database.Site
	LastDeployedAt *time.Time `json:"last_deployed_at"`
}

// handleListSites returns all sites ordered by newest first.
func (s *Server) handleListSites() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := database.DB.Query(`
			SELECT
				s.id, s.domain, s.path, COALESCE(s.php_version, ''), COALESCE(s.repository, ''),
				COALESCE(s.branch, ''), COALESCE(s.app_type, 'php'), COALESCE(s.app_port, 0),
				COALESCE(s.node_preset, ''), COALESCE(s.node_mode, ''), COALESCE(s.package_manager, 'npm'),
				COALESCE(s.build_command, ''), COALESCE(s.start_command, ''), COALESCE(s.static_output_dir, ''),
				COALESCE(s.deployment_strategy, 'standard'), COALESCE(s.ssl_provider, 'none'),
				COALESCE(s.ssl_active, 0), COALESCE(s.web_root, '/public'), COALESCE(s.push_to_deploy, 0),
				COALESCE(s.deploy_script, ''), COALESCE(s.deploy_script_mode, 'legacy'), COALESCE(s.expose_env, 0), COALESCE(s.db_engine, ''),
				COALESCE(s.deletion_status, ''), COALESCE(s.deletion_error, ''),
				COALESCE(s.deletion_stage, ''), COALESCE(s.deletion_delete_databases, 0), COALESCE(s.deletion_database_ids, ''),
				COALESCE(s.github_account_id, 0), s.created_at, s.updated_at,
				(
					SELECT MAX(d.updated_at)
					FROM deployments d
					WHERE d.site_id = s.id AND d.status = 'success'
				) AS last_deployed_at
			FROM sites s
			ORDER BY s.id DESC
		`)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		sites := []siteListItem{}
		for rows.Next() {
			var item siteListItem
			var lastDeployedAt sqliteTime
			if err := rows.Scan(&item.ID, &item.Domain, &item.Path, &item.PHPVersion, &item.Repository, &item.Branch, &item.AppType, &item.AppPort, &item.NodePreset, &item.NodeMode, &item.PackageManager, &item.BuildCommand, &item.StartCommand, &item.StaticOutputDir, &item.DeploymentStrategy, &item.SSLProvider, &item.SSLActive, &item.WebRoot, &item.PushToDeploy, &item.DeployScript, &item.DeployScriptMode, &item.ExposeEnv, &item.DBEngine, &item.DeletionStatus, &item.DeletionError, &item.DeletionStage, &item.DeletionDeleteDBs, &item.DeletionDatabaseIDs, &item.GithubAccountID, &item.CreatedAt, &item.UpdatedAt, &lastDeployedAt); err != nil {
				log.Printf("Error scanning site row: %v", err)
				continue
			}
			if lastDeployedAt.Valid {
				item.LastDeployedAt = &lastDeployedAt.Time
			}
			sites = append(sites, item)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sites)
	}
}

// handleCreateSite provisions a complete site: nginx, PHP-FPM, .env, DB, and deploy key.
func (s *Server) handleCreateSite() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateSiteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))

		if !domainRegex.MatchString(req.Domain) {
			http.Error(w, "Invalid domain name", http.StatusBadRequest)
			return
		}
		if inUse, err := domainInUse(req.Domain, true); err != nil {
			http.Error(w, "Failed to validate domain", http.StatusInternalServerError)
			return
		} else if inUse {
			http.Error(w, "Domain is already attached to another site", http.StatusConflict)
			return
		}

		if req.AppType == "" {
			req.AppType = "laravel"
		}
		if !isValidAppType(req.AppType) {
			http.Error(w, "Invalid app type", http.StatusBadRequest)
			return
		}
		if req.AppType == "node" {
			nodeSiteLifecycleMu.Lock()
			defer nodeSiteLifecycleMu.Unlock()
			toolchainCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			toolchainErr := requireNodeToolchain(toolchainCtx)
			cancel()
			if toolchainErr != nil {
				http.Error(w, toolchainErr.Error(), http.StatusConflict)
				return
			}
		}

		prov := site.Resolve(req.AppType)

		if req.WebRoot == "" {
			req.WebRoot = prov.DefaultWebRoot()
		}
		if req.PHPVersion == "" {
			req.PHPVersion = "8.4"
		}
		if !safeinput.ValidatePHPVersion(req.PHPVersion) {
			http.Error(w, "Invalid PHP version", http.StatusBadRequest)
			return
		}
		if req.DeploymentStrategy == "" {
			req.DeploymentStrategy = "standard"
		}
		if !isValidDeploymentStrategy(req.DeploymentStrategy) {
			http.Error(w, "Invalid deployment strategy", http.StatusBadRequest)
			return
		}
		req.WebRoot = strings.TrimSpace(req.WebRoot)
		req.Repository = strings.TrimSpace(req.Repository)
		req.Branch = strings.TrimSpace(req.Branch)
		req.NodePreset = strings.TrimSpace(req.NodePreset)
		req.NodeMode = strings.TrimSpace(req.NodeMode)
		req.PackageManager = strings.TrimSpace(req.PackageManager)
		req.BuildCommand = strings.TrimSpace(req.BuildCommand)
		req.StartCommand = strings.TrimSpace(req.StartCommand)
		req.StaticOutputDir = strings.TrimSpace(req.StaticOutputDir)
		if req.AppType == "wordpress" {
			req.Repository = ""
			req.Branch = ""
			req.GitHubAccountID = 0
		}
		if req.AppType == "node" {
			req.NodePreset = site.NormalizeNodePreset(req.NodePreset)
			req.NodeMode = site.NormalizeNodeMode(req.NodeMode)
			req.PackageManager = site.NormalizePackageManager(req.PackageManager)
			req.StaticOutputDir = site.NormalizeStaticOutputDir(req.NodePreset, req.StaticOutputDir)
			if req.BuildCommand == "" {
				req.BuildCommand = site.DefaultNodeBuildCommand(req.NodePreset, req.PackageManager)
			}
			if req.StartCommand == "" {
				req.StartCommand = site.DefaultNodeStartCommand(req.NodePreset, req.PackageManager)
			}
			if req.NodeMode == "server" && req.AppPort == 0 {
				req.AppPort = 3000
			}
			if req.NodeMode == "static" {
				req.AppPort = 0
			}
			if safeinput.HasControlChars(req.BuildCommand) || safeinput.HasControlChars(req.StartCommand) {
				http.Error(w, "Invalid Node.js command", http.StatusBadRequest)
				return
			}
			if _, err := safeinput.NormalizeWebRoot(filepath.Join("/home/fluxo", req.Domain), req.StaticOutputDir); err != nil {
				http.Error(w, "Invalid static output directory", http.StatusBadRequest)
				return
			}
		}
		if err := validateDeploymentCompatibility(req.AppType, req.DeploymentStrategy, req.Repository, req.AppPort, req.NodeMode); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.DatabaseName != "" && !safeinput.ValidateDBIdent(req.DatabaseName) {
			http.Error(w, "Invalid database name", http.StatusBadRequest)
			return
		}
		if req.DatabaseUser != "" && !safeinput.ValidateDBIdent(req.DatabaseUser) {
			http.Error(w, "Invalid database user", http.StatusBadRequest)
			return
		}
		if safeinput.HasControlChars(req.DatabasePassword) {
			http.Error(w, "Invalid database password", http.StatusBadRequest)
			return
		}
		if req.DBEngine == "pgsql" {
			req.DBEngine = "postgres"
		}
		if req.DBEngine != "" && req.DBEngine != "mysql" && req.DBEngine != "postgres" {
			http.Error(w, "Invalid database engine", http.StatusBadRequest)
			return
		}
		var selectedDatabaseID int
		if req.DatabaseName != "" {
			var assignedSiteID int
			err := database.DB.QueryRow("SELECT id, site_id FROM databases WHERE engine = ? AND name = ?", req.DBEngine, req.DatabaseName).Scan(&selectedDatabaseID, &assignedSiteID)
			if err == sql.ErrNoRows {
				http.Error(w, "Selected database was not found", http.StatusBadRequest)
				return
			}
			if err != nil {
				http.Error(w, "Failed to validate selected database", http.StatusInternalServerError)
				return
			}
			if assignedSiteID != 0 {
				http.Error(w, "Selected database is already connected to another site", http.StatusConflict)
				return
			}
		}
		if req.AppType == "wordpress" {
			if req.DBEngine != "mysql" {
				http.Error(w, "WordPress requires MySQL or MariaDB", http.StatusBadRequest)
				return
			}
			req.Repository = ""
			req.Branch = ""
			req.GitHubAccountID = 0
			req.InstallComposer = false
			req.AppPort = 0
		}
		if repo := req.Repository; repo != "" && !safeinput.ValidateRepoFullName(repo) {
			http.Error(w, "Invalid repository", http.StatusBadRequest)
			return
		}
		if req.Branch != "" && !safeinput.ValidateGitRef(req.Branch) {
			http.Error(w, "Invalid branch", http.StatusBadRequest)
			return
		}
		if req.Branch == "" && req.AppType != "wordpress" {
			req.Branch = "main"
		}

		if err := validateSiteDatabaseCredentials(req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		dbUser := req.DatabaseUser
		dbPass := req.DatabasePassword
		if req.DatabaseName != "" {
			var accessErr error
			if req.DBEngine == "postgres" {
				accessErr = postgres.VerifyDatabaseAccess(req.DatabaseName, dbUser, dbPass)
			} else {
				accessErr = mysql.VerifyDatabaseAccess(req.DatabaseName, dbUser, dbPass)
			}
			if accessErr != nil {
				http.Error(w, "The supplied database user cannot access the selected database", http.StatusBadRequest)
				return
			}
		}
		ctx := r.Context()

		deployScript := deploy.GenerateApplicationCommands(req.AppType)

		// Save to DB first to get the ID. Remember whether this attempt owns the
		// site directory so a rollback never removes pre-existing user files.
		sitePath := filepath.Join("/home/fluxo", req.Domain)
		if _, err := safeinput.NormalizeWebRoot(sitePath, req.WebRoot); err != nil {
			http.Error(w, "Invalid web root", http.StatusBadRequest)
			return
		}
		removeSiteDirOnRollback := false
		if info, err := os.Lstat(sitePath); os.IsNotExist(err) {
			removeSiteDirOnRollback = true
		} else if err != nil {
			http.Error(w, "Failed to inspect site directory", http.StatusInternalServerError)
			return
		} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			http.Error(w, "Site path already exists and is not a directory", http.StatusConflict)
			return
		}
		provisionConfigPaths := []string{
			filepath.Join("/etc/nginx/sites-enabled", req.Domain),
			filepath.Join("/etc/nginx/sites-available", req.Domain),
		}
		if req.AppType != "node" && req.PHPVersion != "" {
			provisionConfigPaths = append(provisionConfigPaths,
				fmt.Sprintf("/etc/php/%s/fpm/pool.d/%s.conf", req.PHPVersion, req.Domain))
		}
		for _, configPath := range provisionConfigPaths {
			if _, err := os.Lstat(configPath); err == nil {
				http.Error(w, "Site configuration already exists on this server", http.StatusConflict)
				return
			} else if !os.IsNotExist(err) {
				http.Error(w, "Failed to inspect existing site configuration", http.StatusInternalServerError)
				return
			}
		}

		domainMutationMu.Lock()
		inUse, domainErr := domainInUse(req.Domain, true)
		if domainErr != nil {
			domainMutationMu.Unlock()
			http.Error(w, "Failed to validate domain", http.StatusInternalServerError)
			return
		}
		if inUse {
			domainMutationMu.Unlock()
			http.Error(w, "Domain is already attached to another site", http.StatusConflict)
			return
		}
		res, err := database.DB.Exec("INSERT INTO sites (domain, path, php_version, repository, branch, deployment_strategy, app_type, app_port, node_preset, node_mode, package_manager, build_command, start_command, static_output_dir, db_engine, deploy_script, deploy_script_mode, web_root, github_account_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", req.Domain, filepath.Join("/home/fluxo", req.Domain), req.PHPVersion, req.Repository, req.Branch, req.DeploymentStrategy, req.AppType, req.AppPort, req.NodePreset, req.NodeMode, req.PackageManager, req.BuildCommand, req.StartCommand, req.StaticOutputDir, req.DBEngine, deployScript, deploy.ScriptModeManaged, req.WebRoot, req.GitHubAccountID)
		domainMutationMu.Unlock()
		if err != nil {
			http.Error(w, "Failed to save to database: "+err.Error(), http.StatusInternalServerError)
			return
		}
		id, err := res.LastInsertId()
		if err != nil {
			message := "Failed to identify the created site"
			if cleanupErr := deleteIncompleteSiteRecord(0, req.Domain); cleanupErr != nil {
				message += "; cleanup also failed: " + cleanupErr.Error()
			}
			http.Error(w, message, http.StatusInternalServerError)
			return
		}

		if req.AppType == "wordpress" {
			result, updateErr := database.DB.Exec("UPDATE databases SET site_id = ?, username = ? WHERE id = ? AND site_id = 0", id, dbUser, selectedDatabaseID)
			if updateErr != nil {
				message := "Selected WordPress database could not be connected"
				if cleanupErr := deleteIncompleteSiteRecord(id, req.Domain); cleanupErr != nil {
					message += "; cleanup also failed: " + cleanupErr.Error()
				}
				http.Error(w, message, http.StatusConflict)
				return
			}
			rowsAffected, rowsErr := result.RowsAffected()
			if rowsErr != nil || rowsAffected != 1 {
				message := "Selected WordPress database could not be connected"
				if cleanupErr := deleteIncompleteSiteRecord(id, req.Domain); cleanupErr != nil {
					message += "; cleanup also failed: " + cleanupErr.Error()
				}
				http.Error(w, message, http.StatusConflict)
				return
			}
		} else if req.DatabaseName != "" {
			result, updateErr := database.DB.Exec("UPDATE databases SET site_id = ?, username = ? WHERE id = ? AND site_id = 0", id, dbUser, selectedDatabaseID)
			if updateErr != nil {
				message := "Selected database could not be connected"
				if cleanupErr := deleteIncompleteSiteRecord(id, req.Domain); cleanupErr != nil {
					message += "; cleanup also failed: " + cleanupErr.Error()
				}
				http.Error(w, message, http.StatusInternalServerError)
				return
			}
			if affected, rowsErr := result.RowsAffected(); rowsErr != nil || affected != 1 {
				message := "Selected database could not be connected"
				if cleanupErr := deleteIncompleteSiteRecord(id, req.Domain); cleanupErr != nil {
					message += "; cleanup also failed: " + cleanupErr.Error()
				}
				http.Error(w, message, http.StatusConflict)
				return
			}
		}

		// Generate SSH deploy key and inject to GitHub
		var privKeyPath string
		var injectedDeployKeyID int64
		if req.Repository != "" {
			LogActivity(int(id), "provision", "Generating SSH deploy key")
			keyPath, pub, err := git.GenerateSSHKey(ctx, int(id))
			if err == nil {
				privKeyPath = keyPath
				var pat string
				if req.GitHubAccountID > 0 {
					database.DB.QueryRow("SELECT token FROM github_accounts WHERE id = ?", req.GitHubAccountID).Scan(&pat)
				} else {
					database.DB.QueryRow("SELECT token FROM github_accounts ORDER BY id ASC LIMIT 1").Scan(&pat)
				}
				if pat != "" {
					pat = config.Decrypt(pat)
					provider := git.NewGitHubProvider(pat)
					if keyID, err := provider.InjectDeployKey(req.Repository, pub); err == nil {
						injectedDeployKeyID = keyID
						database.DB.Exec("UPDATE sites SET github_deploy_key_id = ? WHERE id = ?", keyID, id)
					}
				}
			} else {
				privKeyPath = git.GetSSHKeyPath(int(id))
			}
			// Wait for GitHub to register the key
			time.Sleep(2 * time.Second)
		}

		provReq := site.ProvisionRequest{
			Domain:             req.Domain,
			PHPVersion:         req.PHPVersion,
			WebRoot:            req.WebRoot,
			AppType:            req.AppType,
			AppPort:            req.AppPort,
			DatabaseName:       req.DatabaseName,
			DatabaseUser:       dbUser,
			DatabasePassword:   dbPass,
			DatabaseEngine:     req.DBEngine,
			Repository:         req.Repository,
			Branch:             req.Branch,
			SSHKeyPath:         privKeyPath,
			InstallComposer:    req.InstallComposer,
			DeploymentStrategy: req.DeploymentStrategy,
			NodePreset:         req.NodePreset,
			NodeMode:           req.NodeMode,
			PackageManager:     req.PackageManager,
			BuildCommand:       req.BuildCommand,
			StartCommand:       req.StartCommand,
			StaticOutputDir:    req.StaticOutputDir,
			SiteID:             int(id),
			ActivityLog:        LogActivity,
		}

		if err := site.Provision(ctx, provReq); err != nil {
			rollbackFailedProvision(int(id), req.Domain, req.PHPVersion, req.AppType, req.Repository,
				req.GitHubAccountID, injectedDeployKeyID, removeSiteDirOnRollback)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if req.AppType == "node" {
			if err := syncNodeDaemonForSite(ctx, int(id)); err != nil {
				rollbackFailedProvision(int(id), req.Domain, req.PHPVersion, req.AppType, req.Repository,
					req.GitHubAccountID, injectedDeployKeyID, removeSiteDirOnRollback)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		siteObj := database.Site{
			ID:                 int(id),
			Domain:             req.Domain,
			Path:               filepath.Join("/home/fluxo", req.Domain),
			PHPVersion:         req.PHPVersion,
			Repository:         req.Repository,
			Branch:             req.Branch,
			DeploymentStrategy: req.DeploymentStrategy,
			AppType:            req.AppType,
			AppPort:            req.AppPort,
			NodePreset:         req.NodePreset,
			NodeMode:           req.NodeMode,
			PackageManager:     req.PackageManager,
			BuildCommand:       req.BuildCommand,
			StartCommand:       req.StartCommand,
			StaticOutputDir:    req.StaticOutputDir,
			DBEngine:           req.DBEngine,
			DeployScript:       deployScript,
			DeployScriptMode:   deploy.ScriptModeManaged,
			WebRoot:            req.WebRoot,
			GithubAccountID:    req.GitHubAccountID,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(siteObj)

		LogActivity(int(id), "provision", "Site "+req.Domain+" was created")
	}
}

func rollbackFailedProvision(siteID int, domain, phpVersion, appType, repository string, githubAccountID int, deployKeyID int64, removeSiteDir bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if appType == "node" {
		if err := deleteNodeDaemon(ctx, siteID); err != nil {
			log.Printf("Failed to remove Node.js daemon while rolling back site %d: %v", siteID, err)
		}
	}
	if _, err := database.DB.Exec("UPDATE databases SET site_id = 0 WHERE site_id = ?", siteID); err != nil {
		log.Printf("Failed to release databases while rolling back site %d: %v", siteID, err)
	}
	if deployKeyID > 0 && repository != "" {
		var pat string
		if githubAccountID > 0 {
			_ = database.DB.QueryRow("SELECT token FROM github_accounts WHERE id = ?", githubAccountID).Scan(&pat)
		} else {
			_ = database.DB.QueryRow("SELECT token FROM github_accounts ORDER BY id ASC LIMIT 1").Scan(&pat)
		}
		if pat == "" {
			log.Printf("Failed to revoke GitHub deploy key while rolling back site %d: account token is unavailable", siteID)
		} else if err := git.NewGitHubProvider(config.Decrypt(pat)).RemoveDeployKey(repository, deployKeyID); err != nil {
			log.Printf("Failed to revoke GitHub deploy key while rolling back site %d: %v", siteID, err)
		}
	}
	sshKeyPath := git.GetSSHKeyPath(siteID)
	for _, keyPath := range []string{sshKeyPath, sshKeyPath + ".pub"} {
		if err := os.Remove(keyPath); err != nil && !os.IsNotExist(err) {
			log.Printf("Failed to remove SSH key %s while rolling back site %d: %v", keyPath, siteID, err)
		}
	}
	if err := nginx.RemoveConfigFiles(domain); err != nil {
		log.Printf("Failed to remove Nginx configuration while rolling back site %d: %v", siteID, err)
	} else if err := nginx.Reload(ctx); err != nil {
		log.Printf("Failed to reload Nginx while rolling back site %d: %v", siteID, err)
	}
	if phpVersion != "" && appType != "node" {
		if err := os.Remove(fmt.Sprintf("/etc/php/%s/fpm/pool.d/%s.conf", phpVersion, domain)); err != nil && !os.IsNotExist(err) {
			log.Printf("Failed to remove PHP-FPM pool while rolling back site %d: %v", siteID, err)
		}
		if err := php.ReloadFPM(ctx, phpVersion); err != nil {
			log.Printf("Failed to reload PHP-FPM while rolling back site %d: %v", siteID, err)
		}
	}
	if removeSiteDir {
		if err := os.RemoveAll(filepath.Join("/home/fluxo", domain)); err != nil {
			log.Printf("Failed to remove files while rolling back site %d: %v", siteID, err)
		}
	}
	if _, err := database.DB.Exec("DELETE FROM sites WHERE id = ?", siteID); err != nil {
		log.Printf("Failed to remove site record while rolling back site %d: %v", siteID, err)
	}
}

// handleDeleteSite removes a site through an idempotent, retryable workflow.
func (s *Server) handleDeleteSite() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "Invalid ID", http.StatusBadRequest)
			return
		}
		if !s.beginCertificateSiteDeletion(id) {
			http.Error(w, "Wait for the site's certificate issuance to finish before deleting it", http.StatusConflict)
			return
		}
		defer s.endCertificateSiteDeletion(id)

		requestedDeleteDatabases := false
		if raw := r.URL.Query().Get("delete_databases"); raw != "" {
			requestedDeleteDatabases, err = strconv.ParseBool(raw)
			if err != nil {
				http.Error(w, "Invalid delete_databases option", http.StatusBadRequest)
				return
			}
		}
		requestedDatabaseIDs := []int{}
		if requestedDeleteDatabases {
			requestedDatabaseIDs, err = parseExpectedDatabaseIDs(r.URL.Query().Get("database_ids"))
			if err != nil {
				http.Error(w, "Invalid database_ids option: "+err.Error(), http.StatusBadRequest)
				return
			}
		} else if r.URL.Query().Get("database_ids") != "" {
			http.Error(w, "database_ids requires delete_databases=true", http.StatusBadRequest)
			return
		}

		var domain, sitePath, phpVersion, repository, deletionStatus, storedDatabaseIDs string
		var deployKeyID, webhookID int64
		var accountID int
		var storedDeleteDatabases bool
		err = database.DB.QueryRow(`
			SELECT domain, path, COALESCE(php_version, ''), COALESCE(repository, ''),
			       COALESCE(github_deploy_key_id, 0), COALESCE(github_webhook_id, 0),
			       COALESCE(github_account_id, 0), COALESCE(deletion_status, ''),
			       COALESCE(deletion_delete_databases, 0), COALESCE(deletion_database_ids, '')
			FROM sites WHERE id = ?`, id).Scan(
			&domain, &sitePath, &phpVersion, &repository, &deployKeyID, &webhookID, &accountID,
			&deletionStatus, &storedDeleteDatabases, &storedDatabaseIDs,
		)
		if err != nil || domain == "" {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		sitePath, err = safeinput.NormalizeManagedSitePath(sitePath)
		if err != nil {
			http.Error(w, "Site path is invalid", http.StatusInternalServerError)
			return
		}
		infrastructureName := filepath.Base(sitePath)

		deleteDatabases := requestedDeleteDatabases
		expectedDatabaseIDs := requestedDatabaseIDs
		isRetry := deletionStatus != ""
		if isRetry {
			deleteDatabases = storedDeleteDatabases
			expectedDatabaseIDs, err = parseExpectedDatabaseIDs(storedDatabaseIDs)
			if err != nil {
				http.Error(w, "Stored site deletion intent is invalid", http.StatusInternalServerError)
				return
			}
		}

		var runningDeployments int
		if err := database.DB.QueryRow("SELECT COUNT(*) FROM deployments WHERE site_id = ? AND status = 'running'", id).Scan(&runningDeployments); err != nil {
			http.Error(w, "Failed to inspect active deployments", http.StatusInternalServerError)
			return
		}
		if runningDeployments > 0 {
			http.Error(w, "Wait for the site's active deployment to finish before deleting it", http.StatusConflict)
			return
		}

		if err := s.backupManager.PrepareSiteDeletion(id); err != nil {
			if strings.Contains(err.Error(), "active backup") || strings.Contains(err.Error(), "already in progress") {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			http.Error(w, "Failed to prepare site deletion", http.StatusInternalServerError)
			return
		}
		defer s.backupManager.FinishSiteDeletion(id)

		ctx := r.Context()
		domainMutationMu.Lock()
		defer domainMutationMu.Unlock()

		if _, err := database.GetCertificatesBySite(id); err != nil {
			http.Error(w, "Failed to load the site's certificates", http.StatusInternalServerError)
			return
		}

		databasePreflightErr := func() error {
			databaseMutationMu.Lock()
			defer databaseMutationMu.Unlock()
			if deleteDatabases {
				if isRetry {
					if err := validateRemainingDatabasesForDeletion(id, expectedDatabaseIDs); err != nil {
						return err
					}
				} else if err := validateAttachedDatabasesForDeletion(id, expectedDatabaseIDs); err != nil {
					return err
				}
				if err := preflightDatabaseEngines(id); err != nil {
					return err
				}
			}
			result, err := database.DB.Exec(`UPDATE sites
				SET deletion_status = 'deleting', deletion_error = '', deletion_stage = 'disabling_traffic',
				    deletion_delete_databases = ?, deletion_database_ids = ?, updated_at = CURRENT_TIMESTAMP
				WHERE id = ?`, deleteDatabases, formatDatabaseIDs(expectedDatabaseIDs), id)
			if err != nil {
				return err
			}
			if affected, err := result.RowsAffected(); err != nil {
				return err
			} else if affected != 1 {
				return sql.ErrNoRows
			}
			return nil
		}()
		if databasePreflightErr != nil {
			if errors.Is(databasePreflightErr, errAttachedDatabaseSetChanged) {
				http.Error(w, "The site's attached databases changed after deletion was confirmed", http.StatusConflict)
				return
			}
			http.Error(w, "Failed deletion preflight: "+databasePreflightErr.Error(), http.StatusServiceUnavailable)
			return
		}

		deletionStage := "disabling_traffic"
		deletionComplete := false
		setStage := func(stage string) error {
			deletionStage = stage
			result, err := database.DB.Exec("UPDATE sites SET deletion_stage = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", stage, id)
			if err != nil {
				return err
			}
			if affected, err := result.RowsAffected(); err != nil {
				return err
			} else if affected != 1 {
				return sql.ErrNoRows
			}
			return nil
		}
		defer func() {
			if deletionComplete {
				return
			}
			message := "Deletion interrupted during " + strings.ReplaceAll(deletionStage, "_", " ") + ". Retry deletion to continue."
			if _, err := database.DB.Exec(`UPDATE sites
				SET deletion_status = 'interrupted', deletion_error = ?, deletion_stage = ?, updated_at = CURRENT_TIMESTAMP
				WHERE id = ?`, message, deletionStage, id); err != nil {
				log.Printf("Failed to record interrupted deletion for site %d: %v", id, err)
			}
		}()

		if err := database.DB.QueryRow("SELECT COUNT(*) FROM deployments WHERE site_id = ? AND status = 'running'", id).Scan(&runningDeployments); err != nil {
			http.Error(w, "Failed to recheck active deployments", http.StatusInternalServerError)
			return
		}
		if runningDeployments > 0 {
			http.Error(w, "A deployment started while deletion was being prepared; retry after it finishes", http.StatusConflict)
			return
		}

		// Pending deployments must not start after the deletion marker is durable.
		if _, err := database.DB.Exec(`UPDATE deployments
			SET status = 'failed', output = 'Deployment cancelled because site deletion started.',
				failure_reason = 'Deployment cancelled because site deletion started.', updated_at = CURRENT_TIMESTAMP
			WHERE site_id = ? AND status = 'pending'`, id); err != nil {
			http.Error(w, "Failed to cancel pending deployments", http.StatusInternalServerError)
			return
		}

		LogActivity(id, "site_deletion", "Disabling site traffic")
		if _, err := nginx.DisableConfig(ctx, infrastructureName); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := setStage("process_cleanup"); err != nil {
			http.Error(w, "Failed to record deletion progress", http.StatusInternalServerError)
			return
		}
		LogActivity(id, "site_deletion", "Removing daemons")
		daemonRows, err := database.DB.Query("SELECT id FROM daemons WHERE site_id = ?", id)
		if err != nil {
			http.Error(w, "Failed to load site daemons", http.StatusInternalServerError)
			return
		}
		var daemonIDs []int
		for daemonRows.Next() {
			var daemonID int
			if err := daemonRows.Scan(&daemonID); err != nil {
				daemonRows.Close()
				http.Error(w, "Failed to read site daemons", http.StatusInternalServerError)
				return
			}
			daemonIDs = append(daemonIDs, daemonID)
		}
		if err := daemonRows.Close(); err != nil {
			http.Error(w, "Failed to finish reading site daemons", http.StatusInternalServerError)
			return
		}
		for _, daemonID := range daemonIDs {
			if err := daemon.Delete(ctx, daemonID); err != nil {
				http.Error(w, "Failed to remove a site daemon: "+err.Error(), http.StatusInternalServerError)
				return
			}
			_ = os.Remove(filepath.Join("/var/log/fluxo", fmt.Sprintf("fluxo-daemon-%d.log", daemonID)))
		}

		LogActivity(id, "site_deletion", "Removing cron jobs")
		cronRows, err := database.DB.Query("SELECT id FROM crons WHERE site_id = ?", id)
		if err != nil {
			http.Error(w, "Failed to load site cron jobs", http.StatusInternalServerError)
			return
		}
		var cronIDs []int
		for cronRows.Next() {
			var cronID int
			if err := cronRows.Scan(&cronID); err != nil {
				cronRows.Close()
				http.Error(w, "Failed to read site cron jobs", http.StatusInternalServerError)
				return
			}
			cronIDs = append(cronIDs, cronID)
		}
		if err := cronRows.Close(); err != nil {
			http.Error(w, "Failed to finish reading site cron jobs", http.StatusInternalServerError)
			return
		}
		for _, cronID := range cronIDs {
			if err := cron.Delete(cronID); err != nil && !os.IsNotExist(err) {
				http.Error(w, "Failed to remove a site cron job: "+err.Error(), http.StatusInternalServerError)
				return
			}
			_ = os.Remove(filepath.Join("/var/log/fluxo", fmt.Sprintf("cron-%d.log", cronID)))
		}

		if err := setStage("database_cleanup"); err != nil {
			http.Error(w, "Failed to record deletion progress", http.StatusInternalServerError)
			return
		}
		databaseCleanupErr := func() error {
			databaseMutationMu.Lock()
			defer databaseMutationMu.Unlock()
			if deleteDatabases {
				LogActivity(id, "site_deletion", "Permanently deleting attached databases")
				if err := validateRemainingDatabasesForDeletion(id, expectedDatabaseIDs); err != nil {
					return err
				}
				return dropDatabasesForSite(id)
			}
			LogActivity(id, "site_deletion", "Releasing attached databases")
			_, err := database.DB.Exec("UPDATE databases SET site_id = 0 WHERE site_id = ?", id)
			return err
		}()
		if databaseCleanupErr != nil {
			http.Error(w, "Failed to clean up attached databases: "+databaseCleanupErr.Error(), http.StatusServiceUnavailable)
			return
		}

		if err := setStage("configuration_cleanup"); err != nil {
			http.Error(w, "Failed to record deletion progress", http.StatusInternalServerError)
			return
		}
		if err := nginx.RemoveConfigFiles(infrastructureName); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = os.Remove(fmt.Sprintf("/var/log/nginx/%s.access.log", domain))
		_ = os.Remove(fmt.Sprintf("/var/log/nginx/%s.error.log", domain))

		if webhookID > 0 && repository != "" {
			var pat string
			if accountID > 0 {
				_ = database.DB.QueryRow("SELECT token FROM github_accounts WHERE id = ?", accountID).Scan(&pat)
			} else {
				_ = database.DB.QueryRow("SELECT token FROM github_accounts ORDER BY id ASC LIMIT 1").Scan(&pat)
			}
			if pat != "" {
				if err := git.NewGitHubProvider(config.Decrypt(pat)).RemoveWebhook(repository, webhookID); err != nil {
					LogActivity(id, "warning", fmt.Sprintf("Failed to remove GitHub webhook: %v", err))
				}
			}
		}
		if deployKeyID > 0 && repository != "" {
			var pat string
			if accountID > 0 {
				_ = database.DB.QueryRow("SELECT token FROM github_accounts WHERE id = ?", accountID).Scan(&pat)
			} else {
				_ = database.DB.QueryRow("SELECT token FROM github_accounts ORDER BY id ASC LIMIT 1").Scan(&pat)
			}
			if pat != "" {
				if err := git.NewGitHubProvider(config.Decrypt(pat)).RemoveDeployKey(repository, deployKeyID); err != nil {
					LogActivity(id, "warning", fmt.Sprintf("Failed to remove GitHub deploy key: %v", err))
				}
			}
		}

		sshKeyPath := git.GetSSHKeyPath(id)
		_ = os.Remove(sshKeyPath)
		_ = os.Remove(sshKeyPath + ".pub")
		if phpVersion != "" {
			_ = os.Remove(fmt.Sprintf("/etc/php/%s/fpm/pool.d/%s.conf", phpVersion, infrastructureName))
			_ = php.ReloadFPM(ctx, phpVersion)
		}

		if err := setStage("site_file_cleanup"); err != nil {
			http.Error(w, "Failed to record deletion progress", http.StatusInternalServerError)
			return
		}
		LogActivity(id, "site_deletion", "Removing site directory")
		if err := os.RemoveAll(sitePath); err != nil {
			http.Error(w, "Failed to remove site directory: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if err := setStage("finalization"); err != nil {
			http.Error(w, "Failed to record deletion progress", http.StatusInternalServerError)
			return
		}
		deploy.RemoveQueue(id)
		if err := s.backupManager.FinalizeSiteDeletion(id); err != nil {
			http.Error(w, "Failed to finalize site deletion: "+err.Error(), http.StatusInternalServerError)
			return
		}
		deletionComplete = true
		s.signalCertificateCleanup()

		LogActivity(id, "site_deleted", "Site "+domain+" was deleted")
		w.WriteHeader(http.StatusNoContent)
	}
}
