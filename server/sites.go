package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"fluxo/database"
	"fluxo/services/git"
	"fluxo/services/nginx"
	"fluxo/services/php"
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
}

var domainRegex = regexp.MustCompile(`^[a-zA-Z0-9.-]+$`)

func (s *Server) handleGetSite() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "Invalid ID", http.StatusBadRequest)
			return
		}

		var site database.Site
		err = database.DB.QueryRow("SELECT id, domain, path, php_version, repository, branch, app_type, app_port, deployment_strategy, ssl_provider, ssl_active, web_root, push_to_deploy, deploy_script, expose_env, created_at, updated_at FROM sites WHERE id = ?", id).Scan(
			&site.ID, &site.Domain, &site.Path, &site.PHPVersion, &site.Repository, &site.Branch, &site.AppType, &site.AppPort, &site.DeploymentStrategy, &site.SSLProvider, &site.SSLActive, &site.WebRoot, &site.PushToDeploy, &site.DeployScript, &site.ExposeEnv, &site.CreatedAt, &site.UpdatedAt,
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
	AppType            string `json:"app_type"`
	PHPVersion         string `json:"php_version"`
	WebRoot            string `json:"web_root"`
	Repository         string `json:"repository"`
	Branch             string `json:"branch"`
	PushToDeploy       *bool  `json:"push_to_deploy"`
	DeployScript       string `json:"deploy_script"`
	ExposeEnv          *bool  `json:"expose_env"`
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

		if req.AppType != "" {
			database.DB.Exec("UPDATE sites SET app_type = ? WHERE id = ?", req.AppType, id)
		}
		if req.PHPVersion != "" {
			database.DB.Exec("UPDATE sites SET php_version = ? WHERE id = ?", req.PHPVersion, id)
		}
		if req.WebRoot != "" {
			database.DB.Exec("UPDATE sites SET web_root = ? WHERE id = ?", req.WebRoot, id)
		}
		if req.Repository != "" {
			database.DB.Exec("UPDATE sites SET repository = ? WHERE id = ?", req.Repository, id)
		}
		if req.Branch != "" {
			database.DB.Exec("UPDATE sites SET branch = ? WHERE id = ?", req.Branch, id)
		}
		if req.DeployScript != "" {
			database.DB.Exec("UPDATE sites SET deploy_script = ? WHERE id = ?", req.DeployScript, id)
		}
		if req.PushToDeploy != nil {
			if *req.PushToDeploy {
				database.DB.Exec("UPDATE sites SET push_to_deploy = 1 WHERE id = ?", id)
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

		w.WriteHeader(http.StatusOK)
	}
}

func (s *Server) handleListSites() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := database.DB.Query("SELECT id, domain, path, php_version, repository, branch, app_type, app_port, deployment_strategy, ssl_provider, ssl_active, created_at, updated_at FROM sites ORDER BY id DESC")
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		sites := []database.Site{}
		for rows.Next() {
			var site database.Site
			if err := rows.Scan(&site.ID, &site.Domain, &site.Path, &site.PHPVersion, &site.Repository, &site.Branch, &site.AppType, &site.AppPort, &site.DeploymentStrategy, &site.SSLProvider, &site.SSLActive, &site.CreatedAt, &site.UpdatedAt); err != nil {
				continue
			}
			sites = append(sites, site)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sites)
	}
}

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

		if req.WebRoot == "" {
			req.WebRoot = "/public"
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
		if req.AppType == "" {
			req.AppType = "laravel"
		}

		ctx := r.Context()

		// 1. Ensure Nginx dirs exist
		nginx.EnsureDirs()

		// 2. Check PHP FPM
		if err := php.EnsureFPMExists(ctx, req.PHPVersion); err != nil {
			http.Error(w, fmt.Sprintf("PHP Version %s not installed or not running", req.PHPVersion), http.StatusBadRequest)
			return
		}

		// 3. Create Web Directory
		cleanWebRoot := filepath.Clean(req.WebRoot)
		fullWebRoot := filepath.Join("/var/www", req.Domain, cleanWebRoot)
		if err := os.MkdirAll(fullWebRoot, 0755); err != nil {
			http.Error(w, fmt.Sprintf("Failed to create web root: %v", err), http.StatusInternalServerError)
			return
		}

		indexPath := filepath.Join(fullWebRoot, "index.php")
		if _, err := os.Stat(indexPath); os.IsNotExist(err) {
			os.WriteFile(indexPath, []byte("<?php\necho 'Fluxo Site: ' . htmlspecialchars($_SERVER['HTTP_HOST']);\n"), 0644)
		}

		// 3.5. Provision .env for PHP/Laravel apps with database
		siteDir := filepath.Join("/var/www", req.Domain)
		if req.DatabaseName != "" && (req.AppType == "laravel" || req.AppType == "php") {
			dbUser := req.DatabaseUser
			if dbUser == "" {
				dbUser = "fluxo"
			}
			dbPass := req.DatabasePassword
			if dbPass == "" {
				dbPass = "secret"
			}

			envPath := filepath.Join(siteDir, ".env")
			envExample := filepath.Join(siteDir, ".env.example")

			var envContent string
			if data, err := os.ReadFile(envExample); err == nil {
				envContent = string(data)
			} else {
				envContent = `APP_NAME=Fluxo
				APP_ENV=production
				APP_KEY=
				APP_DEBUG=false
				APP_URL=http://` + req.Domain + `
				LOG_CHANNEL=stack
				DB_CONNECTION=mysql
				DB_HOST=127.0.0.1
				DB_PORT=3306
				DB_DATABASE=` + req.DatabaseName + `
				DB_USERNAME=` + dbUser + `
				DB_PASSWORD=` + dbPass + ``
			}

			// Replace values regardless of source
			replacements := map[string]string{
				"APP_NAME":    "Fluxo",
				"APP_ENV":     "production",
				"APP_DEBUG":   "false",
				"APP_URL":     "http://" + req.Domain,
				"DB_DATABASE": req.DatabaseName,
				"DB_USERNAME": dbUser,
				"DB_PASSWORD": dbPass,
			}
			for key, val := range replacements {
				envContent = strings.ReplaceAll(envContent, key+"=", key+"="+val+"\n"+key+"=")
				// Ensure single assignment via sed-like approach: if line starts with KEY=, replace the value
			}

			// Simple line-by-line replacement
			lines := strings.Split(envContent, "\n")
			for i, line := range lines {
				for key, val := range replacements {
					if strings.HasPrefix(line, key+"=") {
						lines[i] = key + "=" + val
					}
				}
			}
			envContent = strings.Join(lines, "\n")

			os.WriteFile(envPath, []byte(envContent), 0644)

			// Generate APP_KEY if artisan exists
			artisanPath := filepath.Join(siteDir, "artisan")
			if _, err := os.Stat(artisanPath); err == nil {
				exec.Command("php", "artisan", "key:generate", "--force").Run()
			}
		}

		// 4. Setup Nginx
		if err := nginx.GenerateConfig(req.Domain, fullWebRoot, req.PHPVersion, req.AppType, req.AppPort, "none"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// 5. Setup PHP-FPM Pool (Only for PHP/Laravel)
		if req.AppType == "php" || req.AppType == "laravel" {
			if err := php.GeneratePoolConfig(ctx, req.Domain, req.PHPVersion); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		// 6. Save to DB
		res, err := database.DB.Exec("INSERT INTO sites (domain, path, php_version, repository, branch, deployment_strategy, app_type, app_port) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", req.Domain, filepath.Join("/var/www", req.Domain), req.PHPVersion, req.Repository, req.Branch, req.DeploymentStrategy, req.AppType, req.AppPort)
		if err != nil {
			http.Error(w, "Failed to save to database", http.StatusInternalServerError)
			return
		}
		id, _ := res.LastInsertId()

		// 7. Git Integration: Generate Key and Inject
		if req.Repository != "" {
			_, pub, err := git.GenerateSSHKey(ctx, int(id))
			if err == nil {
				var pat string
				database.DB.QueryRow("SELECT github_pat FROM users LIMIT 1").Scan(&pat)
				if pat != "" {
					provider := git.NewGitHubProvider(pat)
					provider.InjectDeployKey(req.Repository, pub)
				}
			}
		}

		site := database.Site{
			ID:                 int(id),
			Domain:             req.Domain,
			Path:               filepath.Join("/var/www", req.Domain),
			PHPVersion:         req.PHPVersion,
			Repository:         req.Repository,
			Branch:             req.Branch,
			DeploymentStrategy: req.DeploymentStrategy,
			AppType:            req.AppType,
			AppPort:            req.AppPort,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(site)
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

		// Note: in a real app, delete files, symlinks, and reload services.
		database.DB.Exec("DELETE FROM sites WHERE id = ?", id)
		w.WriteHeader(http.StatusNoContent)
	}
}
