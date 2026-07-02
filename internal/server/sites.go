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
	case "standard", "zero-downtime", "octane":
		return true
	default:
		return false
	}
}

func validateDeploymentCompatibility(appType, strategy, repo string, appPort int) error {
	if appType == "node" && !safeinput.ValidatePortNumber(appPort) {
		return fmt.Errorf("Node sites require a valid app port")
	}
	if strategy == "zero-downtime" {
		if appType != "laravel" && appType != "php" {
			return fmt.Errorf("Zero-downtime deployment is only supported for Laravel and PHP sites")
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
		err = database.DB.QueryRow("SELECT id, domain, path, php_version, repository, branch, app_type, app_port, deployment_strategy, ssl_provider, ssl_active, web_root, push_to_deploy, deploy_script, expose_env, db_engine, github_account_id, created_at, updated_at FROM sites WHERE id = ?", id).Scan(
			&site.ID, &site.Domain, &site.Path, &site.PHPVersion, &site.Repository, &site.Branch, &site.AppType, &site.AppPort, &site.DeploymentStrategy, &site.SSLProvider, &site.SSLActive, &site.WebRoot, &site.PushToDeploy, &site.DeployScript, &site.ExposeEnv, &site.DBEngine, &site.GithubAccountID, &site.CreatedAt, &site.UpdatedAt,
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
	PHPVersion         string  `json:"php_version"`
	WebRoot            string  `json:"web_root"`
	DeploymentStrategy string  `json:"deployment_strategy"`
	Repository         *string `json:"repository"`
	Branch             *string `json:"branch"`
	PushToDeploy       *bool   `json:"push_to_deploy"`
	DeployScript       string  `json:"deploy_script"`
	ExposeEnv          *bool   `json:"expose_env"`
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

		var curDomain, curAppType, curStrategy, curRepo, curBranch string
		var curAppPort int
		if err := database.DB.QueryRow("SELECT domain, app_type, deployment_strategy, repository, branch, app_port FROM sites WHERE id = ?", id).Scan(&curDomain, &curAppType, &curStrategy, &curRepo, &curBranch, &curAppPort); err != nil {
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
		if err := validateDeploymentCompatibility(effectiveAppType, effectiveStrategy, effectiveRepo, curAppPort); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		regenNginx := false
		if req.AppType != "" {
			database.DB.Exec("UPDATE sites SET app_type = ? WHERE id = ?", req.AppType, id)
			regenNginx = true
		}
		if req.PHPVersion != "" {
			database.DB.Exec("UPDATE sites SET php_version = ? WHERE id = ?", req.PHPVersion, id)
			regenNginx = true
		}
		if req.DeploymentStrategy != "" {
			database.DB.Exec("UPDATE sites SET deployment_strategy = ? WHERE id = ?", req.DeploymentStrategy, id)
			regenNginx = true
		}
		if req.WebRoot != "" {
			database.DB.Exec("UPDATE sites SET web_root = ? WHERE id = ?", req.WebRoot, id)
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

// handleListSites returns all sites ordered by newest first.
func (s *Server) handleListSites() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := database.DB.Query("SELECT id, domain, path, php_version, repository, branch, app_type, app_port, deployment_strategy, ssl_provider, ssl_active, web_root, push_to_deploy, deploy_script, expose_env, db_engine, github_account_id, created_at, updated_at FROM sites ORDER BY id DESC")
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		sites := []database.Site{}
		for rows.Next() {
			var site database.Site
			if err := rows.Scan(&site.ID, &site.Domain, &site.Path, &site.PHPVersion, &site.Repository, &site.Branch, &site.AppType, &site.AppPort, &site.DeploymentStrategy, &site.SSLProvider, &site.SSLActive, &site.WebRoot, &site.PushToDeploy, &site.DeployScript, &site.ExposeEnv, &site.DBEngine, &site.GithubAccountID, &site.CreatedAt, &site.UpdatedAt); err != nil {
				continue
			}
			sites = append(sites, site)
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
		if err := validateDeploymentCompatibility(req.AppType, req.DeploymentStrategy, req.Repository, req.AppPort); err != nil {
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
		if req.DeploymentStrategy == "zero-downtime" {
			deployScript = deploy.GenerateDeployScript("zero-downtime", req.AppType)
		} else {
			deployScript = prov.DefaultDeployScript(req.Domain, req.Branch, req.PHPVersion)
		}

		// Save to DB first to get the ID
		if _, err := safeinput.NormalizeWebRoot(filepath.Join("/home/fluxo", req.Domain), req.WebRoot); err != nil {
			http.Error(w, "Invalid web root", http.StatusBadRequest)
			return
		}

		res, err := database.DB.Exec("INSERT INTO sites (domain, path, php_version, repository, branch, deployment_strategy, app_type, app_port, db_engine, deploy_script, web_root, github_account_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", req.Domain, filepath.Join("/home/fluxo", req.Domain), req.PHPVersion, req.Repository, req.Branch, req.DeploymentStrategy, req.AppType, req.AppPort, req.DBEngine, deployScript, req.WebRoot, req.GitHubAccountID)
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
			SiteID:             int(id),
			ActivityLog:        LogActivity,
		}

		if err := site.Provision(ctx, provReq); err != nil {
			database.DB.Exec("DELETE FROM sites WHERE id = ?", id)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
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
