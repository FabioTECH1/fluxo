package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	DomainID    int    `json:"domain_id"`
}

type cloneCertificateRequest struct {
	CertificateID int `json:"certificate_id"`
	DomainID      int `json:"domain_id"`
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

// getSiteApplicationWebRoot resolves the directory Nginx actually serves for
// an application. Certificate challenges must use the same directory or
// Certbot will write tokens somewhere the active vhost cannot read them.
func getSiteApplicationWebRoot(path, webRoot, strategy, appType, nodePreset, nodeMode, staticOutputDir, pythonPreset, appDirectory string) (string, error) {
	if appType == "node" && nodeMode == "static" {
		webRoot = sitepkg.NormalizeStaticOutputDir(nodePreset, staticOutputDir)
	}
	if appType == "python" && sitepkg.NormalizePythonPreset(pythonPreset) == "django" {
		normalized, err := sitepkg.NormalizeAppDirectory(appDirectory)
		if err != nil {
			return "", err
		}
		return filepath.Join(sitepkg.ActiveSitePath(path, strategy), normalized), nil
	}
	return getSiteWebRoot(path, webRoot, strategy)
}

// handleLetsEncrypt issues a Let's Encrypt certificate and creates a certificate record.
func (s *Server) handleLetsEncrypt() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))
		if siteID <= 0 {
			http.Error(w, "Invalid site ID", http.StatusBadRequest)
			return
		}
		if !s.beginCertificateIssuance(siteID) {
			http.Error(w, "Another site or certificate operation is in progress", http.StatusConflict)
			return
		}
		defer s.endCertificateIssuance(siteID)

		var domain, path, strategy, webRoot, appType, nodePreset, nodeMode, staticOutputDir, pythonPreset, appDirectory string
		err := database.DB.QueryRow(`SELECT domain, path, deployment_strategy, web_root, app_type, node_preset,
			node_mode, static_output_dir, COALESCE(python_preset, ''), COALESCE(app_directory, '.')
			FROM sites WHERE id = ?`, siteID).
			Scan(&domain, &path, &strategy, &webRoot, &appType, &nodePreset, &nodeMode, &staticOutputDir, &pythonPreset, &appDirectory)
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

		webRootFull, err := getSiteApplicationWebRoot(path, webRoot, strategy, appType, nodePreset, nodeMode, staticOutputDir, pythonPreset, appDirectory)
		if err != nil {
			http.Error(w, "Invalid web root", http.StatusBadRequest)
			return
		}

		domains, err := siteTLSHostnames(siteID)
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
		active := true
		activationError := ""
		if safeToKeep, err := activateCertificateForSite(siteID, int(id)); err != nil {
			if !safeToKeep {
				http.Error(w, "Certificate issued but activation failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
			active = false
			activationError = err.Error()
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":               id,
			"provider":         "letsencrypt",
			"active":           active,
			"activation_error": activationError,
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
		if req.DomainID < 0 {
			http.Error(w, "Invalid domain ID", http.StatusBadRequest)
			return
		}
		if req.DomainID > 0 {
			s.installCustomSSLForAlias(w, siteID, req)
			return
		}

		var domain, behavior string
		err := database.DB.QueryRow(
			"SELECT domain, COALESCE(www_redirect, 'none') FROM sites WHERE id = ?", siteID,
		).Scan(&domain, &behavior)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		inspection, err := ssl.InspectCertificatePEM([]byte(req.Certificate), []byte(req.PrivateKey))
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		if err := ssl.VerifyCertificateDomains(inspection.Certificate, configuredDomainHostnames(domain, behavior)); err != nil {
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

		active, safeToDiscard, err := activateCertificateForSiteIfNoneActive(siteID, int(id))
		if err != nil {
			if safeToDiscard {
				if _, _, deleteErr := database.DeleteCertificate(int(id), siteID); deleteErr == nil {
					_ = ssl.RemoveManagedCertificateFiles(certPath, keyPath)
				}
			}
			http.Error(w, "Failed to activate custom certificate: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":       id,
			"provider": "custom",
			"active":   active,
		})
	}
}

func (s *Server) installCustomSSLForAlias(w http.ResponseWriter, siteID int, req CustomSSLRequest) {
	domainMutationMu.Lock()
	defer domainMutationMu.Unlock()

	domain, behavior, err := aliasDomainConfiguration(siteID, req.DomainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}
	inspection, err := ssl.InspectCertificatePEM([]byte(req.Certificate), []byte(req.PrivateKey))
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if err := ssl.VerifyCertificateDomains(inspection.Certificate, configuredDomainHostnames(domain, behavior)); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	certPath, keyPath, err := ssl.IssueCustom(domain, req.Certificate, req.PrivateKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	certID, err := database.CreateCertificate(
		siteID, domain, "custom", certPath, keyPath, inspection.Certificate.NotAfter.Format(time.RFC3339),
	)
	if err != nil {
		_ = ssl.RemoveManagedCertificateFiles(certPath, keyPath)
		http.Error(w, "Failed to save certificate record", http.StatusInternalServerError)
		return
	}

	active, safeToDiscard, err := activateCertificateForDomainIfNoneActiveLocked(siteID, domain, int(certID))
	if err != nil {
		if safeToDiscard {
			if _, _, deleteErr := database.DeleteCertificate(int(certID), siteID); deleteErr == nil {
				_ = ssl.RemoveManagedCertificateFiles(certPath, keyPath)
			}
		}
		http.Error(w, "Failed to activate custom certificate: "+err.Error(), http.StatusInternalServerError)
		return
	}

	action := "installed for "
	if active {
		action = "installed and activated for "
	}
	LogActivity(siteID, "settings", "Custom SSL certificate "+action+domain)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":       certID,
		"domain":   domain,
		"provider": "custom",
		"active":   active,
	})
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
		_, siteDomains, err := siteCertificateDomains(siteID)
		if err != nil {
			http.Error(w, "Failed to load site domains", http.StatusInternalServerError)
			return
		}
		for i := range certs {
			inspection, err := ssl.InspectCertificateFiles(certs[i].CertPath, certs[i].KeyPath)
			if err != nil {
				continue
			}
			for _, domain := range siteDomains {
				behavior, behaviorErr := domainWWWRedirect(siteID, domain)
				if behaviorErr == nil && ssl.VerifyCertificateDomains(inspection.Certificate, configuredDomainHostnames(domain, behavior)) == nil {
					certs[i].CoveredDomains = append(certs[i].CoveredDomains, domain)
				}
			}
		}
		bindings, err := database.GetCertificateDomainBindings(siteID)
		if err != nil {
			http.Error(w, "Failed to load certificate assignments", http.StatusInternalServerError)
			return
		}
		for _, binding := range bindings {
			for i := range certs {
				if certs[i].ID == binding.CertificateID {
					certs[i].ActiveDomains = append(certs[i].ActiveDomains, binding.Domain)
					break
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(certs)
	}
}

// handleListCloneableCertificates returns valid custom certificates for the selected site hostname.
func (s *Server) handleListCloneableCertificates() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, err := strconv.Atoi(r.PathValue("id"))
		if err != nil || siteID <= 0 {
			http.Error(w, "Invalid site ID", http.StatusBadRequest)
			return
		}
		domainID := 0
		if rawDomainID := r.URL.Query().Get("domain_id"); rawDomainID != "" {
			domainID, err = strconv.Atoi(rawDomainID)
			if err != nil || domainID <= 0 {
				http.Error(w, "Invalid domain ID", http.StatusBadRequest)
				return
			}
		}
		_, targetHostnames, err := certificateTargetHostnames(siteID, domainID)
		if err != nil {
			http.Error(w, "Domain not found", http.StatusNotFound)
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
			if err != nil || ssl.VerifyCertificateDomains(inspection.Certificate, targetHostnames) != nil {
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
		if req.DomainID < 0 {
			http.Error(w, "Invalid domain ID", http.StatusBadRequest)
			return
		}
		if req.DomainID > 0 {
			domainMutationMu.Lock()
			defer domainMutationMu.Unlock()
		}

		targetDomain, targetHostnames, err := certificateTargetHostnames(siteID, req.DomainID)
		if err != nil {
			http.Error(w, "Domain not found", http.StatusNotFound)
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
		if err := ssl.VerifyCertificateDomains(inspection.Certificate, targetHostnames); err != nil {
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
		safeToDiscard := true
		if req.DomainID > 0 {
			active, safeToDiscard, err = activateCertificateForDomainIfNoneActiveLocked(siteID, targetDomain, int(certID))
		} else {
			active, safeToDiscard, err = activateCertificateForSiteIfNoneActive(siteID, int(certID))
		}
		if err != nil {
			if safeToDiscard {
				removeClonedCertificateRecord(siteID, int(certID), certPath)
			}
			http.Error(w, "Failed to activate cloned certificate: "+err.Error(), http.StatusInternalServerError)
			return
		}

		LogActivity(siteID, "settings", "SSL certificate cloned from "+source.SiteDomain)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":                    certID,
			"provider":              "cloned",
			"domain":                targetDomain,
			"active":                active,
			"source_certificate_id": source.ID,
			"source_site":           source.SiteDomain,
		})
	}
}

func certificateTargetHostnames(siteID, domainID int) (string, []string, error) {
	if domainID > 0 {
		domain, behavior, err := aliasDomainConfiguration(siteID, domainID)
		return domain, configuredDomainHostnames(domain, behavior), err
	}
	var primary, behavior string
	err := database.DB.QueryRow(
		"SELECT domain, COALESCE(www_redirect, 'none') FROM sites WHERE id = ?", siteID,
	).Scan(&primary, &behavior)
	return primary, configuredDomainHostnames(primary, behavior), err
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
	for rows.Next() {
		var alias string
		if err := rows.Scan(&alias); err == nil && strings.TrimSpace(alias) != "" {
			domains = append(domains, alias)
		}
	}
	return primary, domains, rows.Err()
}

func siteTLSHostnames(siteID int) ([]string, error) {
	var primary, behavior string
	if err := database.DB.QueryRow(
		"SELECT domain, COALESCE(www_redirect, 'none') FROM sites WHERE id = ?", siteID,
	).Scan(&primary, &behavior); err != nil {
		return nil, err
	}
	hosts := configuredDomainHostnames(primary, behavior)
	rows, err := database.DB.Query(
		"SELECT domain, COALESCE(www_redirect, 'none') FROM domain_aliases WHERE site_id = ? ORDER BY id", siteID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var alias, aliasBehavior string
		if err := rows.Scan(&alias, &aliasBehavior); err != nil {
			return nil, err
		}
		hosts = append(hosts, configuredDomainHostnames(alias, aliasBehavior)...)
	}
	return hosts, rows.Err()
}

func aliasDomainConfiguration(siteID, domainID int) (string, string, error) {
	var domain, behavior string
	err := database.DB.QueryRow(
		"SELECT domain, COALESCE(www_redirect, 'none') FROM domain_aliases WHERE id = ? AND site_id = ?",
		domainID, siteID,
	).Scan(&domain, &behavior)
	return domain, behavior, err
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
		bindings, err := database.GetCertificateDomainBindings(siteID)
		if err != nil {
			http.Error(w, "Failed to inspect certificate assignments", http.StatusInternalServerError)
			return
		}
		for _, binding := range bindings {
			if binding.CertificateID == cert.ID {
				http.Error(w, "deactivate the certificate for its domains before deleting it", http.StatusConflict)
				return
			}
		}

		var references int
		if err := database.DB.QueryRow(
			`SELECT COUNT(*) FROM certificates
			 WHERE id != ? AND (
				(? != '' AND (cert_path = ? OR key_path = ?)) OR
				(? != '' AND (cert_path = ? OR key_path = ?))
			)`,
			cert.ID,
			cert.CertPath, cert.CertPath, cert.CertPath,
			cert.KeyPath, cert.KeyPath, cert.KeyPath,
		).Scan(&references); err != nil {
			http.Error(w, "Failed to inspect certificate references", http.StatusInternalServerError)
			return
		}
		if references == 0 {
			if err := cleanupCertificateStorage(r.Context(), *cert); err != nil {
				http.Error(w, "Failed to delete certificate: "+err.Error(), http.StatusInternalServerError)
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

func cleanupCertificateStorage(ctx context.Context, cert database.Certificate) error {
	switch cert.Provider {
	case "letsencrypt":
		return ssl.DeleteLetsEncrypt(ctx, cert.CertPath)
	case "custom", "cloned":
		return ssl.RemoveManagedCertificateFiles(cert.CertPath, cert.KeyPath)
	default:
		return fmt.Errorf("unsupported certificate provider %q", cert.Provider)
	}
}

// activateCertificateForSite returns whether a failed activation was fully rolled back and is safe to discard.
func activateCertificateForSite(siteID, certID int) (bool, error) {
	domainMutationMu.Lock()
	defer domainMutationMu.Unlock()

	return activateCertificateForSiteLocked(siteID, certID)
}

// activateCertificateForSiteIfNoneActive activates a certificate only when the
// site still has no active certificate at the moment the mutation lock is held.
func activateCertificateForSiteIfNoneActive(siteID, certID int) (activated bool, safeToDiscard bool, err error) {
	domainMutationMu.Lock()
	defer domainMutationMu.Unlock()

	activeCert, err := database.GetActiveCertificate(siteID)
	if err != nil {
		return false, true, err
	}
	if activeCert != nil {
		return false, true, nil
	}
	safeToDiscard, err = activateCertificateForSiteLocked(siteID, certID)
	return err == nil, safeToDiscard, err
}

// activateCertificateForSiteLocked requires domainMutationMu to be held by the caller.
func activateCertificateForSiteLocked(siteID, certID int) (bool, error) {
	cert, err := database.GetCertificate(certID, siteID)
	if err != nil {
		return true, err
	}
	primary, domains, err := siteCertificateDomains(siteID)
	if err != nil {
		return true, fmt.Errorf("site not found")
	}
	inspection, err := ssl.InspectCertificateFiles(cert.CertPath, cert.KeyPath)
	if err != nil {
		return true, err
	}
	primaryBehavior, err := domainWWWRedirect(siteID, primary)
	if err != nil {
		return true, err
	}
	if err := ssl.VerifyCertificateDomains(inspection.Certificate, configuredDomainHostnames(primary, primaryBehavior)); err != nil {
		return true, err
	}

	previous, err := database.GetActiveCertificate(siteID)
	if err != nil {
		return true, err
	}

	bindings, err := database.GetCertificateDomainBindings(siteID)
	if err != nil {
		return true, err
	}
	bindingByDomain := make(map[string]database.CertificateDomainBinding, len(bindings))
	for _, binding := range bindings {
		bindingByDomain[strings.ToLower(binding.Domain)] = binding
	}
	bindingChanges := make([]certificateBindingChange, 0)
	for _, alias := range domains[1:] {
		disabled, err := database.IsDomainSSLDisabled(siteID, alias)
		if err != nil {
			return true, err
		}
		if disabled {
			continue
		}
		key := strings.ToLower(alias)
		binding, bound := bindingByDomain[key]
		behavior, err := domainWWWRedirect(siteID, alias)
		if err != nil {
			return true, err
		}
		requiredHostnames := configuredDomainHostnames(alias, behavior)
		newCovers := ssl.VerifyCertificateDomains(inspection.Certificate, requiredHostnames) == nil
		var bindingRef *database.CertificateDomainBinding
		if bound {
			bindingRef = &binding
		}
		oldCovers := false
		if !bound && previous != nil && previous.ID != cert.ID {
			oldCovers = certificateCoversHostnames(*previous, requiredHostnames)
		}
		switch certificateBindingActionFor(bindingRef, oldCovers, newCovers) {
		case certificateBindingRelease:
			previousBinding := binding
			bindingChanges = append(bindingChanges, certificateBindingChange{
				domain: alias, previous: &previousBinding,
			})
		case certificateBindingPreserve:
			bindingChanges = append(bindingChanges, certificateBindingChange{
				domain:        alias,
				certificateID: previous.ID,
				origin:        database.CertificateBindingOriginPreserved,
			})
		}
	}
	if err := database.SetActiveCertificateWithBindings(
		siteID, certID, certificateBindingMutations(bindingChanges, false),
	); err != nil {
		return true, err
	}
	if err := regenerateNginxForSiteWithError(siteID); err != nil {
		previousID := 0
		if previous != nil {
			previousID = previous.ID
		}
		rollbackErr := database.SetActiveCertificateWithBindings(
			siteID, previousID, certificateBindingMutations(bindingChanges, true),
		)
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

type certificateBindingChange struct {
	domain        string
	certificateID int
	origin        string
	previous      *database.CertificateDomainBinding
}

type certificateBindingAction uint8

const (
	certificateBindingKeep certificateBindingAction = iota
	certificateBindingPreserve
	certificateBindingRelease
)

func certificateBindingActionFor(binding *database.CertificateDomainBinding, oldCovers, newCovers bool) certificateBindingAction {
	if binding != nil {
		if binding.Origin == database.CertificateBindingOriginPreserved && newCovers {
			return certificateBindingRelease
		}
		return certificateBindingKeep
	}
	if oldCovers && !newCovers {
		return certificateBindingPreserve
	}
	return certificateBindingKeep
}

func certificateBindingMutations(changes []certificateBindingChange, rollback bool) []database.CertificateDomainBindingMutation {
	mutations := make([]database.CertificateDomainBindingMutation, 0, len(changes))
	for _, change := range changes {
		mutation := database.CertificateDomainBindingMutation{
			Domain: change.domain, CertificateID: change.certificateID, Origin: change.origin,
		}
		if rollback {
			mutation.CertificateID = 0
			mutation.Origin = ""
			if change.previous != nil {
				mutation.CertificateID = change.previous.CertificateID
				mutation.Origin = change.previous.Origin
			}
		}
		mutations = append(mutations, mutation)
	}
	return mutations
}

// regenerateNginxForSite regenerates nginx config for the site using the active certificate (if any).
func regenerateNginxForSite(siteID int) {
	if err := regenerateNginxForSiteWithError(siteID); err != nil {
		log.Printf("Failed to regenerate nginx for site %d: %v", siteID, err)
	}
}

var (
	siteNginxMutationMu         sync.Mutex
	renderRegeneratedSiteVhost  = renderManagedNginxForSite
	installRegeneratedSiteVhost = nginx.InstallConfigNamed
)

type renderedSiteVhost struct {
	ConfigName string
	Content    string
}

func regenerateNginxForSiteWithError(siteID int) error {
	siteNginxMutationMu.Lock()
	defer siteNginxMutationMu.Unlock()

	managed, err := renderRegeneratedSiteVhost(siteID)
	if err != nil {
		return err
	}
	content := managed.Content
	override, err := database.GetSiteVhostOverride(siteID)
	if err != nil {
		return err
	}
	if override != nil {
		content = override.Config
	}
	return installRegeneratedSiteVhost(context.Background(), managed.ConfigName, content)
}

func renderManagedNginxForSite(siteID int) (renderedSiteVhost, error) {
	var domain, path, phpVersion, appType, webRoot, strategy, nodePreset, nodeMode, staticOutputDir, pythonPreset, appDirectory, primaryWWWRedirect string
	var appPort sql.NullInt64

	err := database.DB.QueryRow(
		`SELECT domain, path, php_version, app_type, app_port, web_root, deployment_strategy,
			node_preset, node_mode, static_output_dir, COALESCE(python_preset, ''), COALESCE(app_directory, '.'),
			COALESCE(www_redirect, 'none') FROM sites WHERE id = ?`, siteID,
	).Scan(&domain, &path, &phpVersion, &appType, &appPort, &webRoot, &strategy, &nodePreset, &nodeMode, &staticOutputDir, &pythonPreset, &appDirectory, &primaryWWWRedirect)
	if err != nil {
		return renderedSiteVhost{}, err
	}

	port := 0
	if appPort.Valid {
		port = int(appPort.Int64)
	}

	fullWebRoot, err := getSiteApplicationWebRoot(path, webRoot, strategy, appType, nodePreset, nodeMode, staticOutputDir, pythonPreset, appDirectory)
	if err != nil {
		return renderedSiteVhost{}, fmt.Errorf("invalid web root: %w", err)
	}
	nginxAppType := appType
	if appType == "node" && nodeMode == "static" {
		nginxAppType = "html"
	}
	if appType == "python" && sitepkg.NormalizePythonPreset(pythonPreset) == "django" {
		nginxAppType = "python-django"
	}
	if (appType == "laravel" || appType == "php") && isOctaneEnabled(siteID) {
		if port <= 0 {
			return renderedSiteVhost{}, fmt.Errorf("octane app port is not configured")
		}
		nginxAppType = "node"
	}

	type aliasSSLState struct {
		domain      string
		disabled    bool
		wwwRedirect string
	}
	var aliases []aliasSSLState
	rows, err := database.DB.Query("SELECT domain, ssl_disabled, COALESCE(www_redirect, 'none') FROM domain_aliases WHERE site_id = ?", siteID)
	if err != nil {
		return renderedSiteVhost{}, fmt.Errorf("failed to load domain aliases: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var alias aliasSSLState
		if err := rows.Scan(&alias.domain, &alias.disabled, &alias.wwwRedirect); err != nil {
			rows.Close()
			return renderedSiteVhost{}, fmt.Errorf("failed to read a domain alias: %w", err)
		}
		aliases = append(aliases, alias)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return renderedSiteVhost{}, fmt.Errorf("failed to iterate domain aliases: %w", err)
	}
	if err := rows.Close(); err != nil {
		return renderedSiteVhost{}, fmt.Errorf("failed to close domain aliases: %w", err)
	}

	activeCert, err := database.GetActiveCertificate(siteID)
	if err != nil {
		return renderedSiteVhost{}, fmt.Errorf("failed to load the active certificate: %w", err)
	}
	bindings, err := database.GetCertificateDomainBindings(siteID)
	if err != nil {
		return renderedSiteVhost{}, fmt.Errorf("failed to load domain certificate assignments: %w", err)
	}
	bindingByDomain := make(map[string]database.CertificateDomainBinding, len(bindings))
	for _, binding := range bindings {
		bindingByDomain[strings.ToLower(binding.Domain)] = binding
	}

	hosts := make([]nginx.HostCertificate, 0, (len(aliases)+1)*2)
	appendConfiguredHosts := func(configuredDomain, behavior string, cert *database.Certificate) {
		applicationHost, redirectHost, redirectTarget := configuredDomainRouting(configuredDomain, behavior)
		appendHost := func(hostname, target string) {
			if hostname == "" {
				return
			}
			host := nginx.HostCertificate{Domain: hostname, RedirectTo: target}
			if cert != nil && certificateCoversHostname(*cert, hostname) {
				host.CertPath = cert.CertPath
				host.KeyPath = cert.KeyPath
			}
			hosts = append(hosts, host)
		}
		appendHost(applicationHost, "")
		appendHost(redirectHost, redirectTarget)
	}
	appendConfiguredHosts(domain, primaryWWWRedirect, activeCert)

	for _, alias := range aliases {
		var candidate *database.Certificate
		if alias.disabled {
			appendConfiguredHosts(alias.domain, alias.wwwRedirect, nil)
			continue
		}
		if binding, ok := bindingByDomain[strings.ToLower(alias.domain)]; ok {
			candidate = &database.Certificate{CertPath: binding.CertPath, KeyPath: binding.KeyPath}
		} else if activeCert != nil {
			candidate = activeCert
		}
		appendConfiguredHosts(alias.domain, alias.wwwRedirect, candidate)
	}

	infrastructureName := filepath.Base(filepath.Clean(path))
	content, err := nginx.RenderConfigWithHostsNamed(
		infrastructureName, infrastructureName, domain,
		fullWebRoot, phpVersion, nginxAppType, port, hosts,
	)
	if err != nil {
		return renderedSiteVhost{}, err
	}
	return renderedSiteVhost{ConfigName: infrastructureName, Content: content}, nil
}

func certificateCoversHostname(cert database.Certificate, hostname string) bool {
	return certificateCoversHostnames(cert, []string{hostname})
}

func certificateCoversHostnames(cert database.Certificate, hostnames []string) bool {
	inspection, err := ssl.InspectCertificateFiles(cert.CertPath, cert.KeyPath)
	if err != nil {
		return false
	}
	return ssl.VerifyCertificateDomains(inspection.Certificate, hostnames) == nil
}

func applyDomainSSLState(siteID int, domains []DomainItem) error {
	activeCert, err := database.GetActiveCertificate(siteID)
	if err != nil {
		return err
	}
	bindings, err := database.GetCertificateDomainBindings(siteID)
	if err != nil {
		return err
	}
	bindingByDomain := make(map[string]database.CertificateDomainBinding, len(bindings))
	for _, binding := range bindings {
		bindingByDomain[strings.ToLower(binding.Domain)] = binding
	}

	for i := range domains {
		if domains[i].Primary {
			if activeCert != nil && certificateCoversHostnames(*activeCert, configuredDomainHostnames(domains[i].Domain, domains[i].WWWRedirect)) {
				domains[i].SSLActive = true
				domains[i].SSLProvider = activeCert.Provider
				domains[i].CertificateID = activeCert.ID
			}
			continue
		}
		if domains[i].SSLDisabled {
			continue
		}
		if binding, ok := bindingByDomain[strings.ToLower(domains[i].Domain)]; ok {
			cert := database.Certificate{CertPath: binding.CertPath, KeyPath: binding.KeyPath}
			if certificateCoversHostnames(cert, configuredDomainHostnames(domains[i].Domain, domains[i].WWWRedirect)) {
				domains[i].SSLActive = true
				domains[i].SSLProvider = binding.Provider
				domains[i].CertificateID = binding.CertificateID
			}
			continue
		}
		if activeCert != nil && certificateCoversHostnames(*activeCert, configuredDomainHostnames(domains[i].Domain, domains[i].WWWRedirect)) {
			domains[i].SSLActive = true
			domains[i].SSLProvider = activeCert.Provider
			domains[i].CertificateID = activeCert.ID
			domains[i].SSLInherited = true
		}
	}
	return nil
}
