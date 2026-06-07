package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

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
}

var domainRegex = regexp.MustCompile(`^[a-zA-Z0-9.-]+$`)

func (s *Server) handleListSites() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := database.DB.Query("SELECT id, domain, path, php_version, created_at, updated_at FROM sites ORDER BY id DESC")
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		sites := []database.Site{}
		for rows.Next() {
			var site database.Site
			if err := rows.Scan(&site.ID, &site.Domain, &site.Path, &site.PHPVersion, &site.CreatedAt, &site.UpdatedAt); err != nil {
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
			req.AppType = "php"
		}

		ctx := r.Context()

		if req.AppType == "node" {
			if req.AppPort == 0 {
				http.Error(w, "app_port is required for Node.js apps", http.StatusBadRequest)
				return
			}

			var existingPort int
			err := database.DB.QueryRow("SELECT app_port FROM sites WHERE app_port = ?", req.AppPort).Scan(&existingPort)
			if err == nil {
				http.Error(w, "app_port is already in use by another site", http.StatusBadRequest)
				return
			}
		}

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

		// 3. Setup Nginx
		if err := nginx.GenerateConfig(req.Domain, fullWebRoot, req.PHPVersion, req.AppType, req.AppPort, "none"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// 4. Setup PHP-FPM Pool (Only if PHP)
		if req.AppType == "php" {
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
