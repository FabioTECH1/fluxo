// Nginx site management handlers: create, read, update, delete.
// Creating a site provisions the full stack: directory structure,
// Nginx config, PHP-FPM pool (for PHP/Laravel), .env file (with
// database credentials if provided), GitHub deploy key, and SQLite
// record.
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
	"time"

	"fluxo/internal/config"
	"fluxo/internal/database"
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
	DatabaseName       string `json:"database_name"`
	DatabaseUser       string `json:"database_user"`
	DatabasePassword   string `json:"database_password"`
	DBEngine           string `json:"db_engine"`
}

var domainRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+$`)

func (s *Server) handleGetSite() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "Invalid ID", http.StatusBadRequest)
			return
		}

		var site database.Site
		err = database.DB.QueryRow("SELECT id, domain, path, php_version, repository, branch, app_type, app_port, deployment_strategy, ssl_provider, ssl_active, web_root, push_to_deploy, deploy_script, expose_env, db_engine, created_at, updated_at FROM sites WHERE id = ?", id).Scan(
			&site.ID, &site.Domain, &site.Path, &site.PHPVersion, &site.Repository, &site.Branch, &site.AppType, &site.AppPort, &site.DeploymentStrategy, &site.SSLProvider, &site.SSLActive, &site.WebRoot, &site.PushToDeploy, &site.DeployScript, &site.ExposeEnv, &site.DBEngine, &site.CreatedAt, &site.UpdatedAt,
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
	AppType      string  `json:"app_type"`
	PHPVersion   string  `json:"php_version"`
	WebRoot      string  `json:"web_root"`
	Repository   *string `json:"repository"`
	Branch       *string `json:"branch"`
	PushToDeploy *bool   `json:"push_to_deploy"`
	DeployScript string  `json:"deploy_script"`
	ExposeEnv    *bool   `json:"expose_env"`
}

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

		regenNginx := false
		if req.AppType != "" {
			database.DB.Exec("UPDATE sites SET app_type = ? WHERE id = ?", req.AppType, id)
			regenNginx = true
		}
		if req.PHPVersion != "" {
			database.DB.Exec("UPDATE sites SET php_version = ? WHERE id = ?", req.PHPVersion, id)
			regenNginx = true
		}
		if req.WebRoot != "" {
			database.DB.Exec("UPDATE sites SET web_root = ? WHERE id = ?", req.WebRoot, id)
			regenNginx = true
		}

		triggerRepoSync := false
		syncReason := ""
		// Compare with current values to avoid unnecessary syncs
		var curRepo, curBranch string
		database.DB.QueryRow("SELECT repository, branch FROM sites WHERE id = ?", id).Scan(&curRepo, &curBranch)

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

				// Setup Github Webhook asynchronously
				go func(siteID int, host string) {
					var repo string
					database.DB.QueryRow("SELECT repository FROM sites WHERE id = ?", siteID).Scan(&repo)

					if repo != "" {
						var pat, secret string
						database.DB.QueryRow("SELECT github_pat, webhook_secret FROM users LIMIT 1").Scan(&pat, &secret)

						if pat != "" {
							pat = config.Decrypt(pat)
							if secret == "" {
								secret = fmt.Sprintf("fluxo-%d", time.Now().UnixNano()) // Simple secret
								database.DB.Exec("UPDATE users SET webhook_secret = ?", secret)
							}

							provider := git.NewGitHubProvider(pat)
							webhookURL := "https://" + host + "/api/v1/github/webhook"
							provider.RegisterWebhook(repo, webhookURL, secret)
						}
					}
				}(id, r.Host)

			} else {
				database.DB.Exec("UPDATE sites SET push_to_deploy = 0 WHERE id = ?", id)
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

			if currentRepo != "" && currentBranch != "" {
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

		w.WriteHeader(http.StatusOK)
	}
}

func (s *Server) handleListSites() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := database.DB.Query("SELECT id, domain, path, php_version, repository, branch, app_type, app_port, deployment_strategy, ssl_provider, ssl_active, web_root, push_to_deploy, deploy_script, expose_env, db_engine, created_at, updated_at FROM sites ORDER BY id DESC")
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		sites := []database.Site{}
		for rows.Next() {
			var site database.Site
			if err := rows.Scan(&site.ID, &site.Domain, &site.Path, &site.PHPVersion, &site.Repository, &site.Branch, &site.AppType, &site.AppPort, &site.DeploymentStrategy, &site.SSLProvider, &site.SSLActive, &site.WebRoot, &site.PushToDeploy, &site.DeployScript, &site.ExposeEnv, &site.DBEngine, &site.CreatedAt, &site.UpdatedAt); err != nil {
				continue
			}
			sites = append(sites, site)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sites)
	}
}

// handleCreateSite provisions a complete site from a single API call:
//  1. Ensure nginx config directories exist
//  2. Verify the requested PHP-FPM version is installed
//  3. Create /var/www/{domain} directory structure + default index file
//  4. Generate .env for PHP/Laravel apps (optionally with DB credentials)
//  5. Write and symlink nginx virtual host config
//  6. Write PHP-FPM pool config (PHP/Laravel only)
//  7. Insert site record in SQLite
//  8. Generate SSH deploy key and inject it to GitHub (if repo provided)
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

		prov := site.Resolve(req.AppType)

		if req.WebRoot == "" {
			req.WebRoot = prov.DefaultWebRoot()
		}
		if req.PHPVersion == "" {
			req.PHPVersion = "8.4"
		}
		if req.DeploymentStrategy == "" {
			req.DeploymentStrategy = "standard"
		}
		if req.Branch == "" {
			req.Branch = "main"
		}

		ctx := r.Context()

		deployScript := prov.DefaultDeployScript(req.Domain, req.Branch, req.PHPVersion)

		// 6. Save to DB first to get the ID
		res, err := database.DB.Exec("INSERT INTO sites (domain, path, php_version, repository, branch, deployment_strategy, app_type, app_port, db_engine, deploy_script, web_root) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", req.Domain, filepath.Join("/home/fluxo", req.Domain), req.PHPVersion, req.Repository, req.Branch, req.DeploymentStrategy, req.AppType, req.AppPort, req.DBEngine, deployScript, req.WebRoot)
		if err != nil {
			http.Error(w, "Failed to save to database", http.StatusInternalServerError)
			return
		}
		id, _ := res.LastInsertId()

		if req.DatabaseName != "" {
			database.DB.Exec("UPDATE databases SET site_id = ? WHERE name = ?", id, req.DatabaseName)
		}

		// 7. Git Integration: Generate Key and Inject
		var privKeyPath string
		if req.Repository != "" {
			keyPath, pub, err := git.GenerateSSHKey(ctx, int(id))
			if err == nil {
				privKeyPath = keyPath
				var pat string
				database.DB.QueryRow("SELECT github_pat FROM users LIMIT 1").Scan(&pat)
				if pat != "" {
					pat = config.Decrypt(pat)
					provider := git.NewGitHubProvider(pat)
					provider.InjectDeployKey(req.Repository, pub)
				}
			} else {
				privKeyPath = git.GetSSHKeyPath(int(id))
			}
			// Wait 2 seconds for GitHub to register the key
			time.Sleep(2 * time.Second)
		}

		// 8. Provision the site (includes cloning if repository exists)
		// If no explicit DB credentials are provided, use the fluxo admin account.
		dbUser := req.DatabaseUser
		dbPass := req.DatabasePassword
		if req.DatabaseName != "" && dbUser == "" {
			dbUser = "fluxo"
			var encPass string
			database.DB.QueryRow("SELECT fluxo_db_password FROM users LIMIT 1").Scan(&encPass)
			dbPass = config.Decrypt(encPass)
		}
		provReq := site.ProvisionRequest{
			Domain:           req.Domain,
			PHPVersion:       req.PHPVersion,
			WebRoot:          req.WebRoot,
			AppType:          req.AppType,
			AppPort:          req.AppPort,
			DatabaseName:     req.DatabaseName,
			DatabaseUser:     dbUser,
			DatabasePassword: dbPass,
			DatabaseEngine:   req.DBEngine,
			Repository:       req.Repository,
			Branch:           req.Branch,
			SSHKeyPath:       privKeyPath,
		}

		if err := site.Provision(ctx, provReq); err != nil {
			// Clean up the site row if provisioning fails
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
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(siteObj)

		LogActivity(int(id), "site_created", "Site "+req.Domain+" was created")
	}
}

func (s *Server) handleDeleteSite() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "Invalid ID", http.StatusBadRequest)
			return
		}

		var domain, phpVersion string
		database.DB.QueryRow("SELECT domain, php_version FROM sites WHERE id = ?", id).Scan(&domain, &phpVersion)

		if domain != "" {
			ctx := r.Context()
			// 1. Delete Nginx config files
			os.Remove(filepath.Join("/etc/nginx/sites-enabled", domain))
			os.Remove(filepath.Join("/etc/nginx/sites-available", domain))
			nginx.Reload(ctx)

			// 2. Delete PHP FPM pool config
			if phpVersion != "" {
				poolPath := fmt.Sprintf("/etc/php/%s/fpm/pool.d/%s.conf", phpVersion, domain)
				os.Remove(poolPath)
				php.ReloadFPM(ctx, phpVersion)
			}

			// 3. Delete SSL custom certificates directory
			os.RemoveAll(filepath.Join("/etc/nginx/ssl", domain))

			// 4. Delete databases
			rows, errDb := database.DB.Query("SELECT engine, name, username FROM databases WHERE site_id = ?", id)
			if errDb == nil {
				type dbItem struct {
					engine, name, username string
				}
				var dbsToDrop []dbItem
				for rows.Next() {
					var item dbItem
					if rows.Scan(&item.engine, &item.name, &item.username) == nil {
						dbsToDrop = append(dbsToDrop, item)
					}
				}
				rows.Close()

				for _, item := range dbsToDrop {
					if item.engine == "mysql" {
						if errDrop := mysql.DeleteDatabase(item.name, item.username); errDrop != nil {
							log.Printf("[site delete] Failed to delete MySQL database %s for site %d: %v", item.name, id, errDrop)
							LogActivity(id, "warning", fmt.Sprintf("Failed to delete MySQL database %s: %v", item.name, errDrop))
						}
					} else if item.engine == "postgres" {
						if errDrop := postgres.DeleteDatabase(item.name, item.username); errDrop != nil {
							log.Printf("[site delete] Failed to delete PostgreSQL database %s for site %d: %v", item.name, id, errDrop)
							LogActivity(id, "warning", fmt.Sprintf("Failed to delete PostgreSQL database %s: %v", item.name, errDrop))
						}
					}
				}
			}

			// 5. Delete site directory files recursively
			os.RemoveAll(filepath.Join("/home/fluxo", domain))
		}

		database.DB.Exec("DELETE FROM sites WHERE id = ?", id)
		LogActivity(id, "site_deleted", "Site "+domain+" was deleted")
		w.WriteHeader(http.StatusNoContent)
	}
}
