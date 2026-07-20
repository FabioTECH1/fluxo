package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/safeinput"
	"fluxo/internal/services/nginx"
	sitepkg "fluxo/internal/services/site"
	"fluxo/internal/services/ssl"
)

type CustomSSLRequest struct {
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"private_key"`
}

type cloneCertificateRequest struct {
	CertificateID int `json:"certificate_id"`
}

type cloneableCertificateResponse struct {
	ID          int      `json:"id"`
	SiteID      int      `json:"site_id"`
	SiteDomain  string   `json:"site_domain"`
	Provider    string   `json:"provider"`
	Domains     []string `json:"domains"`
	ExpiresAt   string   `json:"expires_at"`
	Issuer      string   `json:"issuer"`
	Fingerprint string   `json:"fingerprint"`
	Active      bool     `json:"active"`
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

		var domain, path, strategy, webRoot, appType, nodePreset, nodeMode, staticOutputDir string
		err := database.DB.QueryRow("SELECT domain, path, deployment_strategy, web_root, app_type, node_preset, node_mode, static_output_dir FROM sites WHERE id = ?", siteID).
			Scan(&domain, &path, &strategy, &webRoot, &appType, &nodePreset, &nodeMode, &staticOutputDir)
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

		if appType == "node" && nodeMode == "static" {
			webRoot = sitepkg.NormalizeStaticOutputDir(nodePreset, staticOutputDir)
		}
		webRootFull, err := getSiteWebRoot(path, webRoot, strategy)
		if err != nil {
			http.Error(w, "Invalid web root", http.StatusBadRequest)
			return
		}

		_, domains, err := siteCertificateDomains(siteID)
		if err != nil {
			http.Error(w, "Failed to load site domains", http.StatusInternalServerError)
			return
		}
		if err := ssl.IssueLetsEncrypt(r.Context(), domains, webRootFull, email.String); err != nil {
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

		domain, domains, err := siteCertificateDomains(siteID)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		inspection, err := ssl.InspectCertificatePEM([]byte(req.Certificate), []byte(req.PrivateKey))
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		if err := ssl.VerifyCertificateDomains(inspection.Certificate, domains); err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}

		certPath, keyPath, err := ssl.IssueCustom(domain, req.Certificate, req.PrivateKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		expiresAt := inspection.Certificate.NotAfter.Format(time.RFC3339)

		id, err := database.CreateCertificate(siteID, domain, "custom", certPath, keyPath, expiresAt)
		if err != nil {
			_ = ssl.RemoveManagedCertificateFiles(certPath, keyPath)
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
		for i := range certs {
			expiresAt, err := ssl.GetCertExpiry(certs[i].CertPath)
			if err != nil {
				continue
			}
			actualExpiry := expiresAt.UTC().Format(time.RFC3339)
			if actualExpiry == certs[i].ExpiresAt {
				continue
			}
			certs[i].ExpiresAt = actualExpiry
			if err := database.UpdateCertificateExpiry(certs[i].ID, actualExpiry); err != nil {
				log.Printf("Failed to refresh expiry for certificate %d: %v", certs[i].ID, err)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(certs)
	}
}

// handleListCloneableCertificates returns only valid certificates that cover every hostname on the target site.
func (s *Server) handleListCloneableCertificates() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, err := strconv.Atoi(r.PathValue("id"))
		if err != nil || siteID <= 0 {
			http.Error(w, "Invalid site ID", http.StatusBadRequest)
			return
		}
		_, domains, err := siteCertificateDomains(siteID)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		certs, err := database.GetCloneableCertificates(siteID)
		if err != nil {
			http.Error(w, "Failed to load certificates", http.StatusInternalServerError)
			return
		}

		items := make([]cloneableCertificateResponse, 0)
		seen := make(map[string]struct{})
		for _, candidate := range certs {
			inspection, err := ssl.InspectCertificateFiles(candidate.CertPath, candidate.KeyPath)
			if err != nil || ssl.VerifyCertificateDomains(inspection.Certificate, domains) != nil {
				continue
			}
			if _, duplicate := seen[inspection.Fingerprint]; duplicate {
				continue
			}
			seen[inspection.Fingerprint] = struct{}{}

			names := append([]string(nil), inspection.Certificate.DNSNames...)
			sort.Strings(names)
			issuer := inspection.Certificate.Issuer.CommonName
			if issuer == "" && len(inspection.Certificate.Issuer.Organization) > 0 {
				issuer = strings.Join(inspection.Certificate.Issuer.Organization, ", ")
			}
			items = append(items, cloneableCertificateResponse{
				ID:          candidate.ID,
				SiteID:      candidate.SiteID,
				SiteDomain:  candidate.SiteDomain,
				Provider:    candidate.Provider,
				Domains:     names,
				ExpiresAt:   inspection.Certificate.NotAfter.Format(time.RFC3339),
				Issuer:      issuer,
				Fingerprint: inspection.Fingerprint,
				Active:      candidate.Active,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(items)
	}
}

// handleCloneCertificate copies a compatible certificate into target-owned storage.
func (s *Server) handleCloneCertificate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, err := strconv.Atoi(r.PathValue("id"))
		if err != nil || siteID <= 0 {
			http.Error(w, "Invalid site ID", http.StatusBadRequest)
			return
		}
		var req cloneCertificateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CertificateID <= 0 {
			http.Error(w, "Select a certificate to clone", http.StatusBadRequest)
			return
		}

		targetDomain, domains, err := siteCertificateDomains(siteID)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		source, err := database.GetCloneSourceCertificate(req.CertificateID, siteID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		inspection, err := ssl.InspectCertificateFiles(source.CertPath, source.KeyPath)
		if err != nil {
			http.Error(w, "Source certificate is unavailable or invalid", http.StatusUnprocessableEntity)
			return
		}
		if err := ssl.VerifyCertificateDomains(inspection.Certificate, domains); err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}

		certPath, keyPath, err := ssl.CloneCertificateFiles(targetDomain, source.ID, inspection)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		expiresAt := inspection.Certificate.NotAfter.Format(time.RFC3339)
		certID, err := database.CreateClonedCertificate(siteID, targetDomain, certPath, keyPath, expiresAt, source.ID)
		if err != nil {
			ssl.RemoveClonedCertificateFiles(certPath)
			http.Error(w, "Failed to save cloned certificate", http.StatusInternalServerError)
			return
		}

		active := false
		activeCert, activeErr := database.GetActiveCertificate(siteID)
		if activeErr != nil {
			removeClonedCertificateRecord(siteID, int(certID), certPath)
			http.Error(w, "Failed to inspect active certificate", http.StatusInternalServerError)
			return
		}
		if activeCert == nil {
			safeToDiscard, err := activateCertificateForSite(siteID, int(certID))
			if err != nil {
				if safeToDiscard {
					removeClonedCertificateRecord(siteID, int(certID), certPath)
				}
				http.Error(w, "Failed to activate cloned certificate: "+err.Error(), http.StatusInternalServerError)
				return
			}
			active = true
		}

		LogActivity(siteID, "settings", "SSL certificate cloned from "+source.SiteDomain)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":                    certID,
			"provider":              "cloned",
			"active":                active,
			"source_certificate_id": source.ID,
			"source_site":           source.SiteDomain,
		})
	}
}

func removeClonedCertificateRecord(siteID, certID int, certPath string) {
	if _, _, err := database.DeleteCertificate(certID, siteID); err == nil {
		ssl.RemoveClonedCertificateFiles(certPath)
	}
}

func siteCertificateDomains(siteID int) (string, []string, error) {
	var primary string
	if err := database.DB.QueryRow("SELECT domain FROM sites WHERE id = ?", siteID).Scan(&primary); err != nil {
		return "", nil, err
	}
	domains := []string{primary}
	rows, err := database.DB.Query("SELECT domain FROM domain_aliases WHERE site_id = ? ORDER BY id", siteID)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var alias string
		if err := rows.Scan(&alias); err == nil && strings.TrimSpace(alias) != "" {
			domains = append(domains, alias)
		}
	}
	return primary, domains, rows.Err()
}

// handleActivateCert activates a specific certificate and regenerates nginx config.
func (s *Server) handleActivateCert() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))
		certID, _ := strconv.Atoi(r.PathValue("certId"))

		if _, err := activateCertificateForSite(siteID, certID); err != nil {
			http.Error(w, "Failed to activate certificate: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

// handleDeactivateCert deactivates a certificate and regenerates nginx config without SSL.
func (s *Server) handleDeactivateCert() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))
		certID, _ := strconv.Atoi(r.PathValue("certId"))

		domainMutationMu.Lock()
		defer domainMutationMu.Unlock()

		previous, err := database.GetActiveCertificate(siteID)
		if err != nil {
			http.Error(w, "Failed to inspect active certificate", http.StatusInternalServerError)
			return
		}
		if err := database.DeactivateCertificate(certID, siteID); err != nil {
			http.Error(w, "Failed to deactivate certificate: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := regenerateNginxForSiteWithError(siteID); err != nil {
			var rollbackErr error
			if previous != nil {
				rollbackErr = database.ActivateCertificate(previous.ID, siteID)
				if rollbackErr == nil {
					rollbackErr = regenerateNginxForSiteWithError(siteID)
				}
			}
			if rollbackErr != nil {
				http.Error(w, fmt.Sprintf("Failed to deactivate certificate: %v (rollback failed: %v)", err, rollbackErr), http.StatusInternalServerError)
				return
			}
			http.Error(w, "Failed to deactivate certificate: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

// handleDeleteCert removes a certificate and its files from disk.
func (s *Server) handleDeleteCert() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))
		certID, _ := strconv.Atoi(r.PathValue("certId"))

		domainMutationMu.Lock()
		defer domainMutationMu.Unlock()

		cert, err := database.GetCertificate(certID, siteID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if cert.Active {
			http.Error(w, "deactivate the certificate before deleting it", http.StatusConflict)
			return
		}

		var references int
		if err := database.DB.QueryRow(
			"SELECT COUNT(*) FROM certificates WHERE id != ? AND (cert_path IN (?, ?) OR key_path IN (?, ?))",
			cert.ID, cert.CertPath, cert.KeyPath, cert.CertPath, cert.KeyPath,
		).Scan(&references); err != nil {
			http.Error(w, "Failed to inspect certificate references", http.StatusInternalServerError)
			return
		}
		if references == 0 {
			switch cert.Provider {
			case "letsencrypt":
				if err := ssl.DeleteLetsEncrypt(r.Context(), cert.CertPath); err != nil {
					http.Error(w, "Failed to delete certificate: "+err.Error(), http.StatusInternalServerError)
					return
				}
			case "custom", "cloned":
				if err := ssl.RemoveManagedCertificateFiles(cert.CertPath, cert.KeyPath); err != nil {
					http.Error(w, "Failed to delete certificate files: "+err.Error(), http.StatusInternalServerError)
					return
				}
			default:
				http.Error(w, "Unsupported certificate provider", http.StatusConflict)
				return
			}
		}

		if _, _, err := database.DeleteCertificate(certID, siteID); err != nil {
			http.Error(w, "Failed to delete certificate record: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

// activateCertificateForSite returns whether a failed activation was fully rolled back and is safe to discard.
func activateCertificateForSite(siteID, certID int) (bool, error) {
	domainMutationMu.Lock()
	defer domainMutationMu.Unlock()

	cert, err := database.GetCertificate(certID, siteID)
	if err != nil {
		return true, err
	}
	_, domains, err := siteCertificateDomains(siteID)
	if err != nil {
		return true, fmt.Errorf("site not found")
	}
	inspection, err := ssl.InspectCertificateFiles(cert.CertPath, cert.KeyPath)
	if err != nil {
		return true, err
	}
	if err := ssl.VerifyCertificateDomains(inspection.Certificate, domains); err != nil {
		return true, err
	}

	previous, err := database.GetActiveCertificate(siteID)
	if err != nil {
		return true, err
	}
	if err := database.ActivateCertificate(certID, siteID); err != nil {
		return true, err
	}
	if err := regenerateNginxForSiteWithError(siteID); err != nil {
		var rollbackErr error
		if previous != nil {
			rollbackErr = database.ActivateCertificate(previous.ID, siteID)
		} else {
			rollbackErr = database.DeactivateCertificate(certID, siteID)
		}
		if rollbackErr == nil {
			rollbackErr = regenerateNginxForSiteWithError(siteID)
		}
		if rollbackErr != nil {
			return false, fmt.Errorf("%w (rollback failed: %v)", err, rollbackErr)
		}
		return true, err
	}
	return true, nil
}

// regenerateNginxForSite regenerates nginx config for the site using the active certificate (if any).
func regenerateNginxForSite(siteID int) {
	if err := regenerateNginxForSiteWithError(siteID); err != nil {
		log.Printf("Failed to regenerate nginx for site %d: %v", siteID, err)
	}
}

func regenerateNginxForSiteWithError(siteID int) error {
	var domain, path, phpVersion, appType, webRoot, strategy, nodePreset, nodeMode, staticOutputDir string
	var appPort sql.NullInt64

	err := database.DB.QueryRow(
		"SELECT domain, path, php_version, app_type, app_port, web_root, deployment_strategy, node_preset, node_mode, static_output_dir FROM sites WHERE id = ?", siteID,
	).Scan(&domain, &path, &phpVersion, &appType, &appPort, &webRoot, &strategy, &nodePreset, &nodeMode, &staticOutputDir)
	if err != nil {
		return err
	}

	port := 0
	if appPort.Valid {
		port = int(appPort.Int64)
	}

	fullWebRoot, err := getSiteWebRoot(path, webRoot, strategy)
	if err != nil {
		return fmt.Errorf("invalid web root: %w", err)
	}
	nginxAppType := appType
	if appType == "node" && nodeMode == "static" {
		nginxAppType = "html"
		staticOutputDir = sitepkg.NormalizeStaticOutputDir(nodePreset, staticOutputDir)
		fullWebRoot, err = getSiteWebRoot(path, staticOutputDir, strategy)
		if err != nil {
			return fmt.Errorf("invalid static output directory: %w", err)
		}
	}
	if (appType == "laravel" || appType == "php") && isOctaneEnabled(siteID) {
		if port <= 0 {
			return fmt.Errorf("octane app port is not configured")
		}
		nginxAppType = "node"
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

	return nginx.GenerateConfig(domain, fullWebRoot, phpVersion, nginxAppType, port, certPath, keyPath, aliases...)
}
