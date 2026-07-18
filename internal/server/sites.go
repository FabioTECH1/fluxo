// Nginx site management: create, read, update, delete.
package server

import (
	"context"
	"encoding/json"
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
	"fluxo/internal/services/nginx"
	"fluxo/internal/services/php"
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
	case "php", "laravel", "html", "node":
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

func validateDeploymentCompatibility(appType, strategy, repo string, appPort int, nodeMode string) error {
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
		err = database.DB.QueryRow("SELECT id, domain, path, php_version, repository, branch, app_type, COALESCE(app_port, 0), node_preset, node_mode, package_manager, build_command, start_command, static_output_dir, deployment_strategy, ssl_provider, ssl_active, web_root, push_to_deploy, deploy_script, expose_env, db_engine, github_account_id, created_at, updated_at FROM sites WHERE id = ?", id).Scan(
			&site.ID, &site.Domain, &site.Path, &site.PHPVersion, &site.Repository, &site.Branch, &site.AppType, &site.AppPort, &site.NodePreset, &site.NodeMode, &site.PackageManager, &site.BuildCommand, &site.StartCommand, &site.StaticOutputDir, &site.DeploymentStrategy, &site.SSLProvider, &site.SSLActive, &site.WebRoot, &site.PushToDeploy, &site.DeployScript, &site.ExposeEnv, &site.DBEngine, &site.GithubAccountID, &site.CreatedAt, &site.UpdatedAt,
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
	DeployScript       string  `json:"deploy_script"`
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

		var curDomain, curAppType, curStrategy, curRepo, curBranch, curNodeMode string
		var curAppPort int
		if err := database.DB.QueryRow("SELECT domain, app_type, deployment_strategy, repository, branch, COALESCE(app_port, 0), node_mode FROM sites WHERE id = ?", id).Scan(&curDomain, &curAppType, &curStrategy, &curRepo, &curBranch, &curAppPort, &curNodeMode); err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		if curAppType == "" {
			curAppType = "php"
		}
		if curStrategy == "" {
			curStrategy = "standard"
		}
		if req.WebRoot != "" {
			if _, err := safeinput.NormalizeWebRoot(filepath.Join("/home/fluxo", curDomain), req.WebRoot); err != nil {
				http.Error(w, "Invalid web root", http.StatusBadRequest)
				return
			}
		}

		effectiveAppType := curAppType
		if req.AppType != "" {
			effectiveAppType = req.AppType
		}
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
		if octaneEnabled && effectiveAppType != "laravel" {
			http.Error(w, "Disable Laravel Octane before changing this site's app type", http.StatusBadRequest)
			return
		}
		if octaneEnabled && effectiveStrategy == "zero-downtime" {
			http.Error(w, "Disable Laravel Octane before enabling zero-downtime deployment", http.StatusBadRequest)
			return
		}
		if effectiveAppType == "node" && effectiveAppPort > 0 {
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
			if _, err := safeinput.NormalizeWebRoot(filepath.Join("/home/fluxo", curDomain), *req.StaticOutputDir); err != nil {
				http.Error(w, "Invalid static output directory", http.StatusBadRequest)
				return
			}
		}

		regenNginx := false
		syncNodeDaemon := false
		syncOctaneDaemon := false
		if req.AppType != "" {
			if req.AppType == "node" || (req.AppType == "laravel" && octaneEnabled) {
				database.DB.Exec("UPDATE sites SET app_type = ? WHERE id = ?", req.AppType, id)
			} else {
				database.DB.Exec("UPDATE sites SET app_type = ?, app_port = 0 WHERE id = ?", req.AppType, id)
			}
			regenNginx = true
			syncNodeDaemon = true
		}
		if req.PHPVersion != "" {
			database.DB.Exec("UPDATE sites SET php_version = ? WHERE id = ?", req.PHPVersion, id)
			regenNginx = true
			if octaneEnabled {
				syncOctaneDaemon = true
			}
		}
		if req.DeploymentStrategy != "" {
			database.DB.Exec("UPDATE sites SET deployment_strategy = ? WHERE id = ?", req.DeploymentStrategy, id)
			regenNginx = true
			syncNodeDaemon = true
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
			} else if effectiveAppType == "laravel" && octaneEnabled {
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

		if req.Repository != nil && *req.Repository != curRepo {
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
		if req.DeployScript != "" {
			database.DB.Exec("UPDATE sites SET deploy_script = ? WHERE id = ?", req.DeployScript, id)
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

		if triggerRepoSync {
			var domain, currentRepo, currentBranch string
			database.DB.QueryRow("SELECT domain, repository, branch FROM sites WHERE id = ?", id).Scan(&domain, &currentRepo, &currentBranch)
			currentRepo = strings.TrimSpace(currentRepo)
			currentBranch = strings.TrimSpace(currentBranch)

			if currentRepo != "" && currentBranch != "" {
				if !safeinput.ValidateRepoFullName(currentRepo) || !safeinput.ValidateGitRef(currentBranch) {
					log.Printf("Skipping repo sync for site %d due to invalid repository or branch configuration", id)
				} else {
					siteDir := "/home/fluxo/" + domain
					repoURL := "git@github.com:" + currentRepo + ".git"
					privKeyPath := git.GetSSHKeyPath(id)

					go func() {
						ctx := context.Background()
						var out string
						var err error
						if _, statErr := os.Stat(filepath.Join(siteDir, ".git")); os.IsNotExist(statErr) {
							out, err = syscmd.RunEnvAsUser(ctx, 120*time.Second, "fluxo", []string{"GIT_SSH_COMMAND=ssh -o StrictHostKeyChecking=no -i " + privKeyPath}, "git", "clone", "-b", currentBranch, repoURL, siteDir)
						} else {
							syscmd.RunEnvAsUser(ctx, 30*time.Second, "fluxo", []string{"GIT_SSH_COMMAND=ssh -o StrictHostKeyChecking=no -i " + privKeyPath}, "git", "-C", siteDir, "remote", "set-url", "origin", repoURL)
							out, err = syscmd.RunEnvAsUser(ctx, 60*time.Second, "fluxo", []string{"GIT_SSH_COMMAND=ssh -o StrictHostKeyChecking=no -i " + privKeyPath}, "git", "-C", siteDir, "fetch", "origin")
							if err == nil {
								out, err = syscmd.RunEnvAsUser(ctx, 30*time.Second, "fluxo", []string{"GIT_SSH_COMMAND=ssh -o StrictHostKeyChecking=no -i " + privKeyPath}, "git", "-C", siteDir, "checkout", "-f", "-B", currentBranch, "origin/"+currentBranch)
							}
						}
						status := "success"
						summary := syncReason + " changed to " + currentRepo + " (" + currentBranch + ")"
						commitMsg := "Git sync — " + summary
						if err != nil {
							status = "failed"
							summary = "Failed to sync " + syncReason + ": " + err.Error()
							commitMsg = summary
						}
						database.DB.Exec("INSERT INTO deployments (site_id, status, output, commit_message, branch) VALUES (?, ?, ?, ?, ?)", id, status, out, commitMsg, currentBranch)
						LogActivity(id, "repo_sync", summary)
					}()
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
	}
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
				s.id, s.domain, s.path, s.php_version, s.repository, s.branch, s.app_type,
				COALESCE(s.app_port, 0), s.node_preset, s.node_mode, s.package_manager,
				s.build_command, s.start_command, s.static_output_dir, s.deployment_strategy,
				s.ssl_provider, s.ssl_active, s.web_root, s.push_to_deploy, s.deploy_script,
				s.expose_env, s.db_engine, s.github_account_id, s.created_at, s.updated_at,
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
			if err := rows.Scan(&item.ID, &item.Domain, &item.Path, &item.PHPVersion, &item.Repository, &item.Branch, &item.AppType, &item.AppPort, &item.NodePreset, &item.NodeMode, &item.PackageManager, &item.BuildCommand, &item.StartCommand, &item.StaticOutputDir, &item.DeploymentStrategy, &item.SSLProvider, &item.SSLActive, &item.WebRoot, &item.PushToDeploy, &item.DeployScript, &item.ExposeEnv, &item.DBEngine, &item.GithubAccountID, &item.CreatedAt, &item.UpdatedAt, &lastDeployedAt); err != nil {
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

		if !domainRegex.MatchString(req.Domain) {
			http.Error(w, "Invalid domain name", http.StatusBadRequest)
			return
		}

		if req.AppType == "" {
			req.AppType = "laravel"
		}
		if !isValidAppType(req.AppType) {
			http.Error(w, "Invalid app type", http.StatusBadRequest)
			return
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
		if req.DBEngine != "" && req.DBEngine != "mysql" && req.DBEngine != "postgres" && req.DBEngine != "pgsql" {
			http.Error(w, "Invalid database engine", http.StatusBadRequest)
			return
		}
		if repo := req.Repository; repo != "" && !safeinput.ValidateRepoFullName(repo) {
			http.Error(w, "Invalid repository", http.StatusBadRequest)
			return
		}
		if req.Branch != "" && !safeinput.ValidateGitRef(req.Branch) {
			http.Error(w, "Invalid branch", http.StatusBadRequest)
			return
		}
		if req.Branch == "" {
			req.Branch = "main"
		}

		ctx := r.Context()

		var deployScript string
		if req.AppType == "node" {
			deployScript = deploy.GenerateNodeDeployScript(req.DeploymentStrategy)
		} else if req.DeploymentStrategy == "zero-downtime" {
			deployScript = deploy.GenerateDeployScript("zero-downtime", req.AppType)
		} else {
			deployScript = prov.DefaultDeployScript(req.Domain, req.Branch, req.PHPVersion)
		}

		// Save to DB first to get the ID
		if _, err := safeinput.NormalizeWebRoot(filepath.Join("/home/fluxo", req.Domain), req.WebRoot); err != nil {
			http.Error(w, "Invalid web root", http.StatusBadRequest)
			return
		}

		res, err := database.DB.Exec("INSERT INTO sites (domain, path, php_version, repository, branch, deployment_strategy, app_type, app_port, node_preset, node_mode, package_manager, build_command, start_command, static_output_dir, db_engine, deploy_script, web_root, github_account_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", req.Domain, filepath.Join("/home/fluxo", req.Domain), req.PHPVersion, req.Repository, req.Branch, req.DeploymentStrategy, req.AppType, req.AppPort, req.NodePreset, req.NodeMode, req.PackageManager, req.BuildCommand, req.StartCommand, req.StaticOutputDir, req.DBEngine, deployScript, req.WebRoot, req.GitHubAccountID)
		if err != nil {
			http.Error(w, "Failed to save to database: "+err.Error(), http.StatusInternalServerError)
			return
		}
		id, _ := res.LastInsertId()

		if req.DatabaseName != "" {
			database.DB.Exec("UPDATE databases SET site_id = ? WHERE name = ? AND engine = ?", id, req.DatabaseName, req.DBEngine)
		}

		// Generate SSH deploy key and inject to GitHub
		var privKeyPath string
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
						database.DB.Exec("UPDATE sites SET github_deploy_key_id = ? WHERE id = ?", keyID, id)
					}
				}
			} else {
				privKeyPath = git.GetSSHKeyPath(int(id))
			}
			// Wait for GitHub to register the key
			time.Sleep(2 * time.Second)
		}

		// Provision the site; fall back to fluxo admin DB credentials if none provided
		dbUser := req.DatabaseUser
		dbPass := req.DatabasePassword
		if req.DatabaseName != "" && dbUser == "" {
			dbUser = "fluxo"
			var encPass string
			database.DB.QueryRow("SELECT fluxo_db_password FROM users LIMIT 1").Scan(&encPass)
			dbPass = config.Decrypt(encPass)
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
			database.DB.Exec("DELETE FROM sites WHERE id = ?", id)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if req.AppType == "node" {
			if err := syncNodeDaemonForSite(ctx, int(id)); err != nil {
				database.DB.Exec("DELETE FROM sites WHERE id = ?", id)
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
			WebRoot:            req.WebRoot,
			GithubAccountID:    req.GitHubAccountID,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(siteObj)

		LogActivity(int(id), "provision", "Site "+req.Domain+" was created")
	}
}

// handleDeleteSite removes site config, databases, SSL certs, daemons, crons, SSH keys, and files.
func (s *Server) handleDeleteSite() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "Invalid ID", http.StatusBadRequest)
			return
		}

		var domain, phpVersion, repository string
		var deployKeyID, webhookID int64
		var accountID int
		err = database.DB.QueryRow("SELECT domain, php_version, repository, github_deploy_key_id, github_webhook_id, github_account_id FROM sites WHERE id = ?", id).Scan(&domain, &phpVersion, &repository, &deployKeyID, &webhookID, &accountID)
		if err != nil || domain == "" {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		// Backup history remains downloadable, but schedules must stop before files and databases are removed.
		if err := s.backupManager.PrepareSiteDeletion(id); err != nil {
			if strings.Contains(err.Error(), "active backup") {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			http.Error(w, "Failed to remove the site's backup plans", http.StatusInternalServerError)
			return
		}
		defer s.backupManager.FinishSiteDeletion(id)

		ctx := r.Context()

		// 1. Removing databases — unlink, don't drop (reusable)
		LogActivity(id, "site_deletion", "Removing database")
		database.DB.Exec("UPDATE databases SET site_id = 0 WHERE site_id = ?", id)

		// 2. Removing daemons
		LogActivity(id, "site_deletion", "Removing daemons")
		daemonRows, _ := database.DB.Query("SELECT id FROM daemons WHERE site_id = ?", id)
		if daemonRows != nil {
			type dID struct{ id int }
			var dIDs []dID
			for daemonRows.Next() {
				var d dID
				if daemonRows.Scan(&d.id) == nil {
					dIDs = append(dIDs, d)
				}
			}
			daemonRows.Close()
			for _, d := range dIDs {
				daemon.Delete(ctx, d.id)
				os.Remove(filepath.Join("/var/log/fluxo", fmt.Sprintf("fluxo-daemon-%d.log", d.id)))
			}
		}

		// 3. Removing cron jobs
		LogActivity(id, "site_deletion", "Removing cron jobs")
		cronRows, _ := database.DB.Query("SELECT id FROM crons WHERE site_id = ?", id)
		if cronRows != nil {
			type cID struct{ id int }
			var cIDs []cID
			for cronRows.Next() {
				var c cID
				if cronRows.Scan(&c.id) == nil {
					cIDs = append(cIDs, c)
				}
			}
			cronRows.Close()
			for _, c := range cIDs {
				cron.Delete(c.id)
				os.Remove(filepath.Join("/var/log/fluxo", fmt.Sprintf("cron-%d.log", c.id)))
			}
		}

		// 4. Removing GitHub webhook
		if webhookID > 0 && repository != "" {
			LogActivity(id, "site_deletion", "Removing GitHub webhook")
			var pat string
			if accountID > 0 {
				database.DB.QueryRow("SELECT token FROM github_accounts WHERE id = ?", accountID).Scan(&pat)
			} else {
				database.DB.QueryRow("SELECT token FROM github_accounts ORDER BY id ASC LIMIT 1").Scan(&pat)
			}
			if pat != "" {
				pat = config.Decrypt(pat)
				provider := git.NewGitHubProvider(pat)
				if err := provider.RemoveWebhook(repository, webhookID); err != nil {
					LogActivity(id, "warning", fmt.Sprintf("Failed to remove GitHub webhook: %v", err))
				}
			}
		}

		// 5. Removing GitHub deploy key
		if deployKeyID > 0 && repository != "" {
			LogActivity(id, "site_deletion", "Removing GitHub deploy key")
			var pat string
			if accountID > 0 {
				database.DB.QueryRow("SELECT token FROM github_accounts WHERE id = ?", accountID).Scan(&pat)
			} else {
				database.DB.QueryRow("SELECT token FROM github_accounts ORDER BY id ASC LIMIT 1").Scan(&pat)
			}
			if pat != "" {
				pat = config.Decrypt(pat)
				provider := git.NewGitHubProvider(pat)
				if err := provider.RemoveDeployKey(repository, deployKeyID); err != nil {
					LogActivity(id, "warning", fmt.Sprintf("Failed to remove GitHub deploy key: %v", err))
				}
			}
		}

		// 6. Removing SSH deploy key
		LogActivity(id, "site_deletion", "Removing SSH deploy key")
		sshKeyPath := git.GetSSHKeyPath(id)
		os.Remove(sshKeyPath)
		os.Remove(sshKeyPath + ".pub")

		// 7. Removing Let's Encrypt certificates
		LogActivity(id, "site_deletion", "Removing SSL certificates")
		syscmd.Run(ctx, 30*time.Second, "certbot", "delete", "--cert-name", domain, "--non-interactive")
		os.RemoveAll(filepath.Join("/etc/nginx/ssl", domain))

		// 8. Removing Nginx
		LogActivity(id, "site_deletion", "Removing Nginx site ("+domain+")")
		os.Remove(filepath.Join("/etc/nginx/sites-enabled", domain))
		os.Remove(filepath.Join("/etc/nginx/sites-available", domain))
		os.Remove(fmt.Sprintf("/var/log/nginx/%s.access.log", domain))
		os.Remove(fmt.Sprintf("/var/log/nginx/%s.error.log", domain))
		nginx.Reload(ctx)

		// 9. Removing PHP-FPM pool
		if phpVersion != "" {
			LogActivity(id, "site_deletion", "Removing PHP-FPM pool")
			poolPath := fmt.Sprintf("/etc/php/%s/fpm/pool.d/%s.conf", phpVersion, domain)
			os.Remove(poolPath)
			php.ReloadFPM(ctx, phpVersion)
		}

		// 10. Removing site directory
		LogActivity(id, "site_deletion", "Removing site directory")
		os.RemoveAll(filepath.Join("/home/fluxo", domain))

		// 11. Finalizing
		LogActivity(id, "site_deletion", "Finalizing site configuration")
		deploy.RemoveQueue(id)
		database.DB.Exec("DELETE FROM sites WHERE id = ?", id)

		LogActivity(id, "site_deleted", "Site "+domain+" was deleted")
		w.WriteHeader(http.StatusNoContent)
	}
}
