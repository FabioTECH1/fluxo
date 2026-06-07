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

		var domain, path, phpVersion, appType, strategy string
		var appPort sql.NullInt64
		err := database.DB.QueryRow("SELECT domain, path, php_version, app_type, app_port, deployment_strategy FROM sites WHERE id = ?", siteID).
			Scan(&domain, &path, &phpVersion, &appType, &appPort, &strategy)
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

		database.DB.Exec("UPDATE sites SET ssl_provider = 'letsencrypt' WHERE id = ?", siteID)

		port := 0
		if appPort.Valid {
			port = int(appPort.Int64)
		}

		if err := nginx.GenerateConfig(domain, webRoot, phpVersion, appType, port, "letsencrypt"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

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

		var domain, path, phpVersion, appType, strategy string
		var appPort sql.NullInt64
		var currentSSL string
		err := database.DB.QueryRow("SELECT domain, path, php_version, app_type, app_port, deployment_strategy, ssl_provider FROM sites WHERE id = ?", siteID).
			Scan(&domain, &path, &phpVersion, &appType, &appPort, &strategy, &currentSSL)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		if err := ssl.IssueCustom(domain, req.Certificate, req.PrivateKey); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		webRoot := getSiteWebRoot(path, strategy)
		port := 0
		if appPort.Valid {
			port = int(appPort.Int64)
		}

		if err := nginx.GenerateConfig(domain, webRoot, phpVersion, appType, port, "custom"); err != nil {
			// Rollback config to previous ssl_provider
			nginx.GenerateConfig(domain, webRoot, phpVersion, appType, port, currentSSL)
			http.Error(w, "Invalid Nginx Configuration: "+err.Error(), http.StatusBadRequest)
			return
		}

		database.DB.Exec("UPDATE sites SET ssl_provider = 'custom' WHERE id = ?", siteID)

		w.WriteHeader(http.StatusOK)
	}
}
