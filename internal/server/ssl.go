package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"

	"fluxo/internal/database"
	"fluxo/internal/services/nginx"
	"fluxo/internal/services/ssl"
)

type CustomSSLRequest struct {
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"private_key"`
}

func getSiteWebRoot(path, webRoot, strategy string) string {
	if strategy == "zero-downtime" {
		return filepath.Join(path, "current", webRoot)
	}
	return filepath.Join(path, webRoot)
}

func (s *Server) handleLetsEncrypt() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var domain, path, strategy, webRoot string
		err := database.DB.QueryRow("SELECT domain, path, deployment_strategy, web_root FROM sites WHERE id = ?", siteID).
			Scan(&domain, &path, &strategy, &webRoot)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		var email sql.NullString
		err = database.DB.QueryRow("SELECT admin_email FROM users LIMIT 1").Scan(&email)
		if err != nil || !email.Valid || email.String == "" {
			http.Error(w, "Admin email not configured in settings", http.StatusBadRequest)
			return
		}

		webRootFull := getSiteWebRoot(path, webRoot, strategy)

		if err := ssl.IssueLetsEncrypt(r.Context(), domain, webRootFull, email.String); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		database.DB.Exec("UPDATE sites SET ssl_provider = 'letsencrypt', ssl_active = 0 WHERE id = ?", siteID)

		w.WriteHeader(http.StatusOK)
	}
}

func (s *Server) handleCustomSSL() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var req CustomSSLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		var domain string
		err := database.DB.QueryRow("SELECT domain FROM sites WHERE id = ?", siteID).
			Scan(&domain)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		if err := ssl.IssueCustom(domain, req.Certificate, req.PrivateKey); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		database.DB.Exec("UPDATE sites SET ssl_provider = 'custom', ssl_active = 0 WHERE id = ?", siteID)

		w.WriteHeader(http.StatusOK)
	}
}

func (s *Server) handleActivateSSL() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var domain, path, phpVersion, appType, strategy, sslProvider, webRoot string
		var appPort sql.NullInt64
		err := database.DB.QueryRow("SELECT domain, path, php_version, app_type, app_port, deployment_strategy, ssl_provider, web_root FROM sites WHERE id = ?", siteID).
			Scan(&domain, &path, &phpVersion, &appType, &appPort, &strategy, &sslProvider, &webRoot)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		if sslProvider == "" || sslProvider == "none" {
			http.Error(w, "No SSL certificate installed", http.StatusBadRequest)
			return
		}

		webRootFull := getSiteWebRoot(path, webRoot, strategy)
		port := 0
		if appPort.Valid {
			port = int(appPort.Int64)
		}

		if err := nginx.GenerateConfig(domain, webRootFull, phpVersion, appType, port, sslProvider); err != nil {
			http.Error(w, "Failed to activate SSL: "+err.Error(), http.StatusInternalServerError)
			return
		}

		database.DB.Exec("UPDATE sites SET ssl_active = 1 WHERE id = ?", siteID)

		w.WriteHeader(http.StatusOK)
	}
}

func (s *Server) handleDeactivateSSL() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var domain, path, phpVersion, appType, strategy, webRoot string
		var appPort sql.NullInt64
		err := database.DB.QueryRow("SELECT domain, path, php_version, app_type, app_port, deployment_strategy, web_root FROM sites WHERE id = ?", siteID).
			Scan(&domain, &path, &phpVersion, &appType, &appPort, &strategy, &webRoot)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		webRootFull := getSiteWebRoot(path, webRoot, strategy)
		port := 0
		if appPort.Valid {
			port = int(appPort.Int64)
		}

		if err := nginx.GenerateConfig(domain, webRootFull, phpVersion, appType, port, "none"); err != nil {
			http.Error(w, "Failed to deactivate SSL: "+err.Error(), http.StatusInternalServerError)
			return
		}

		database.DB.Exec("UPDATE sites SET ssl_active = 0 WHERE id = ?", siteID)

		w.WriteHeader(http.StatusOK)
	}
}

// regenerateNginxForSite fetches the site details and domain aliases,
// then regenerates the nginx config. Safe to call from goroutines.
func regenerateNginxForSite(siteID int) {
	var domain, path, phpVersion, appType, webRoot, strategy, sslProvider string
	var appPort sql.NullInt64
	var sslActive int

	err := database.DB.QueryRow(
		"SELECT domain, path, php_version, app_type, app_port, web_root, deployment_strategy, ssl_provider, ssl_active FROM sites WHERE id = ?", siteID,
	).Scan(&domain, &path, &phpVersion, &appType, &appPort, &webRoot, &strategy, &sslProvider, &sslActive)
	if err != nil {
		return
	}

	port := 0
	if appPort.Valid {
		port = int(appPort.Int64)
	}

	fullWebRoot := getSiteWebRoot(path, webRoot, strategy)

	// Fetch domain aliases
	var aliases []string
	rows, err := database.DB.Query("SELECT domain FROM domain_aliases WHERE site_id = ?", siteID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var alias string
			if rows.Scan(&alias) == nil {
				aliases = append(aliases, alias)
			}
		}
	}

	activeProvider := "none"
	if sslActive == 1 && sslProvider != "" && sslProvider != "none" {
		activeProvider = sslProvider
	}

	nginx.GenerateConfig(domain, fullWebRoot, phpVersion, appType, port, activeProvider, aliases...)
}
