package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"

	"fluxo/database"
	"fluxo/services/nginx"
	"fluxo/services/ssl"
)

type CustomSSLRequest struct {
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"private_key"`
}

func getSiteWebRoot(path, strategy string) string {
	if strategy == "zero-downtime" {
		return filepath.Join(path, "current", "public")
	}
	return filepath.Join(path, "public")
}

func (s *Server) handleLetsEncrypt() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var domain, path, strategy string
		err := database.DB.QueryRow("SELECT domain, path, deployment_strategy FROM sites WHERE id = ?", siteID).
			Scan(&domain, &path, &strategy)
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

		webRoot := getSiteWebRoot(path, strategy)

		if err := ssl.IssueLetsEncrypt(r.Context(), domain, webRoot, email.String); err != nil {
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

		var domain, path, phpVersion, appType, strategy, sslProvider string
		var appPort sql.NullInt64
		err := database.DB.QueryRow("SELECT domain, path, php_version, app_type, app_port, deployment_strategy, ssl_provider FROM sites WHERE id = ?", siteID).
			Scan(&domain, &path, &phpVersion, &appType, &appPort, &strategy, &sslProvider)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		if sslProvider == "" || sslProvider == "none" {
			http.Error(w, "No SSL certificate installed", http.StatusBadRequest)
			return
		}

		webRoot := getSiteWebRoot(path, strategy)
		port := 0
		if appPort.Valid {
			port = int(appPort.Int64)
		}

		if err := nginx.GenerateConfig(domain, webRoot, phpVersion, appType, port, sslProvider); err != nil {
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

		var domain, path, phpVersion, appType, strategy string
		var appPort sql.NullInt64
		err := database.DB.QueryRow("SELECT domain, path, php_version, app_type, app_port, deployment_strategy FROM sites WHERE id = ?", siteID).
			Scan(&domain, &path, &phpVersion, &appType, &appPort, &strategy)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		webRoot := getSiteWebRoot(path, strategy)
		port := 0
		if appPort.Valid {
			port = int(appPort.Int64)
		}

		if err := nginx.GenerateConfig(domain, webRoot, phpVersion, appType, port, "none"); err != nil {
			http.Error(w, "Failed to deactivate SSL: "+err.Error(), http.StatusInternalServerError)
			return
		}

		database.DB.Exec("UPDATE sites SET ssl_active = 0 WHERE id = ?", siteID)

		w.WriteHeader(http.StatusOK)
	}
}
