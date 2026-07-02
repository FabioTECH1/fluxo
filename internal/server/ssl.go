package server

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/safeinput"
	"fluxo/internal/services/nginx"
	"fluxo/internal/services/ssl"
)

type CustomSSLRequest struct {
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"private_key"`
}

func getSiteWebRoot(path, webRoot, strategy string) (string, error) {
	resolved, err := safeinput.NormalizeWebRoot(path, webRoot)
	if err != nil {
		return "", err
	}

	if strategy == "zero-downtime" {
		rel, err := filepath.Rel(path, resolved)
		if err != nil {
			return "", err
		}
		return filepath.Join(path, "current", rel), nil
	}

	return resolved, nil
}

// handleLetsEncrypt issues a Let's Encrypt certificate and creates a certificate record.
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

		webRootFull, err := getSiteWebRoot(path, webRoot, strategy)
		if err != nil {
			http.Error(w, "Invalid web root", http.StatusBadRequest)
			return
		}

		if err := ssl.IssueLetsEncrypt(r.Context(), domain, webRootFull, email.String); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		certPath := "/etc/letsencrypt/live/" + domain + "/fullchain.pem"
		keyPath := "/etc/letsencrypt/live/" + domain + "/privkey.pem"

		expiresAt := ""
		if exp, err := ssl.GetCertExpiry(certPath); err == nil {
			expiresAt = exp.Format(time.RFC3339)
		}

		id, err := database.CreateCertificate(siteID, domain, "letsencrypt", certPath, keyPath, expiresAt)
		if err != nil {
			http.Error(w, "Failed to save certificate record", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":       id,
			"provider": "letsencrypt",
		})
	}
}

// handleCustomSSL installs a user-provided certificate and creates a certificate record.
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

		certPath := "/etc/nginx/ssl/" + domain + "/server.crt"
		keyPath := "/etc/nginx/ssl/" + domain + "/server.key"

		expiresAt := ""
		if exp, err := ssl.ParseCertExpiryFromPEM(req.Certificate); err == nil {
			expiresAt = exp.Format(time.RFC3339)
		}

		id, err := database.CreateCertificate(siteID, domain, "custom", certPath, keyPath, expiresAt)
		if err != nil {
			http.Error(w, "Failed to save certificate record", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":       id,
			"provider": "custom",
		})
	}
}

// handleListCertificates returns all certificates for a site.
func (s *Server) handleListCertificates() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		certs, err := database.GetCertificatesBySite(siteID)
		if err != nil {
			http.Error(w, "Failed to load certificates", http.StatusInternalServerError)
			return
		}
		if certs == nil {
			certs = []database.Certificate{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(certs)
	}
}

// handleActivateCert activates a specific certificate and regenerates nginx config.
func (s *Server) handleActivateCert() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))
		certID, _ := strconv.Atoi(r.PathValue("certId"))

		if err := database.ActivateCertificate(certID, siteID); err != nil {
			http.Error(w, "Failed to activate certificate: "+err.Error(), http.StatusInternalServerError)
			return
		}

		regenerateNginxForSite(siteID)

		w.WriteHeader(http.StatusOK)
	}
}

// handleDeactivateCert deactivates a certificate and regenerates nginx config without SSL.
func (s *Server) handleDeactivateCert() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))
		certID, _ := strconv.Atoi(r.PathValue("certId"))

		if err := database.DeactivateCertificate(certID, siteID); err != nil {
			http.Error(w, "Failed to deactivate certificate", http.StatusInternalServerError)
			return
		}

		regenerateNginxForSite(siteID)

		w.WriteHeader(http.StatusOK)
	}
}

// handleDeleteCert removes a certificate and its files from disk.
func (s *Server) handleDeleteCert() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))
		certID, _ := strconv.Atoi(r.PathValue("certId"))

		certPath, keyPath, err := database.DeleteCertificate(certID, siteID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		os.Remove(certPath)
		os.Remove(keyPath)
		if len(certPath) > 16 && certPath[:16] == "/etc/nginx/ssl/" {
			sslDir := filepath.Dir(certPath)
			os.RemoveAll(sslDir)
		}

		regenerateNginxForSite(siteID)

		w.WriteHeader(http.StatusOK)
	}
}

// regenerateNginxForSite regenerates nginx config for the site using the active certificate (if any).
func regenerateNginxForSite(siteID int) {
	var domain, path, phpVersion, appType, webRoot, strategy string
	var appPort sql.NullInt64

	err := database.DB.QueryRow(
		"SELECT domain, path, php_version, app_type, app_port, web_root, deployment_strategy FROM sites WHERE id = ?", siteID,
	).Scan(&domain, &path, &phpVersion, &appType, &appPort, &webRoot, &strategy)
	if err != nil {
		return
	}

	port := 0
	if appPort.Valid {
		port = int(appPort.Int64)
	}

	fullWebRoot, err := getSiteWebRoot(path, webRoot, strategy)
	if err != nil {
		log.Printf("Skipping nginx regeneration for site %d due to invalid web root: %v", siteID, err)
		return
	}

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

	certPath := ""
	keyPath := ""
	activeCert, _ := database.GetActiveCertificate(siteID)
	if activeCert != nil {
		certPath = activeCert.CertPath
		keyPath = activeCert.KeyPath
	}

	nginx.GenerateConfig(domain, fullWebRoot, phpVersion, appType, port, certPath, keyPath, aliases...)
}
