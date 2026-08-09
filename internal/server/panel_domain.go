package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/config"
	"fluxo/internal/database"
	"fluxo/internal/services/nginx"
	"fluxo/internal/services/ssl"
)

type panelDomainRequest struct {
	Domain string `json:"domain"`
}

type panelCustomCertificateRequest struct {
	Domain      string `json:"domain"`
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"private_key"`
}

type panelCloneCertificateRequest struct {
	Domain        string `json:"domain"`
	CertificateID int    `json:"certificate_id"`
}

type panelDomainResponse struct {
	Domain                string `json:"domain"`
	URL                   string `json:"url"`
	SSLProvider           string `json:"ssl_provider"`
	SSLActive             bool   `json:"ssl_active"`
	ExpiresAt             string `json:"expires_at"`
	Status                string `json:"status"`
	StatusError           string `json:"status_error,omitempty"`
	UpdatedAt             string `json:"updated_at"`
	DirectAccessPreserved bool   `json:"direct_access_preserved"`
}

const productionPanelChallengeRoot = "/var/lib/fluxo-acme"

func normalizePanelDomain(value string) (string, error) {
	domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
	if !domainRegex.MatchString(domain) || net.ParseIP(domain) != nil {
		return "", fmt.Errorf("enter a valid hostname such as panel.example.com")
	}
	return domain, nil
}

func ensurePanelDomainAvailable(domain string) error {
	conflict, err := database.PanelDomainConflicts(domain)
	if err != nil {
		return err
	}
	if conflict {
		return fmt.Errorf("domain is already attached to a site")
	}
	return nil
}

func (s *Server) panelChallengeRoot() (string, error) {
	runtimeConfig := config.LoadConfig()
	root, err := panelChallengeRootPath(runtimeConfig.Env, s.dataDir)
	if err != nil {
		return "", fmt.Errorf("resolve panel ACME directory: %w", err)
	}
	if err := ensurePanelChallengeRoot(root); err != nil {
		return "", err
	}
	return root, nil
}

func ensurePanelChallengeRoot(root string) error {
	if err := os.MkdirAll(root, 0755); err != nil {
		return fmt.Errorf("create panel ACME directory: %w", err)
	}
	if err := os.Chmod(root, 0755); err != nil {
		return fmt.Errorf("set panel ACME directory permissions: %w", err)
	}
	return nil
}

func panelChallengeRootPath(environment, dataDir string) (string, error) {
	if environment == "prod" {
		// /var/lib/fluxo is deliberately root-only because it contains the
		// database and credentials. Nginx needs traverse access only to this
		// separate, non-secret webroot while answering ACME challenges.
		return productionPanelChallengeRoot, nil
	}
	return filepath.Abs(filepath.Join(dataDir, "acme"))
}

func (s *Server) panelProxyConfig(configured database.PanelDomainConfig) (nginx.PanelProxyConfig, error) {
	challengeRoot, err := s.panelChallengeRoot()
	if err != nil {
		return nginx.PanelProxyConfig{}, err
	}
	runtimeConfig := config.LoadConfig()
	port, err := strconv.Atoi(runtimeConfig.Port)
	if err != nil || port < 1 || port > 65535 {
		return nginx.PanelProxyConfig{}, fmt.Errorf("invalid Fluxo dashboard port")
	}
	scheme := "https"
	if os.Getenv("FLUXO_USE_HTTP") == "1" {
		scheme = "http"
	}
	return nginx.PanelProxyConfig{
		Domain: configured.Domain, CertPath: configured.CertPath, KeyPath: configured.KeyPath,
		ChallengeRoot: challengeRoot, UpstreamScheme: scheme, UpstreamPort: port,
	}, nil
}

func (s *Server) panelDomainStatus(configured database.PanelDomainConfig) panelDomainResponse {
	response := panelDomainResponse{
		Domain: configured.Domain, SSLProvider: configured.SSLProvider, UpdatedAt: configured.UpdatedAt,
		Status: "not_configured", DirectAccessPreserved: true,
	}
	if configured.Domain == "" {
		return response
	}
	response.URL = "https://" + configured.Domain
	response.Status = "error"

	inspection, err := ssl.InspectCertificateFiles(configured.CertPath, configured.KeyPath)
	if err != nil {
		response.StatusError = err.Error()
		if expiry, expiryErr := ssl.GetCertExpiry(configured.CertPath); expiryErr == nil {
			response.ExpiresAt = expiry.UTC().Format(time.RFC3339)
		}
		return response
	}
	response.ExpiresAt = inspection.Certificate.NotAfter.UTC().Format(time.RFC3339)
	if err := ssl.VerifyCertificateDomains(inspection.Certificate, []string{configured.Domain}); err != nil {
		response.StatusError = err.Error()
		return response
	}
	if active, statusError := nginx.PanelConfigStatus(configured.Domain); !active {
		response.StatusError = statusError
		return response
	}
	response.SSLActive = true
	response.Status = "active"
	return response
}

func (s *Server) handleGetPanelDomain() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		configured, err := database.GetPanelDomainConfig()
		if err != nil {
			http.Error(w, "Failed to load panel domain", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.panelDomainStatus(configured))
	}
}

func (s *Server) handlePanelLetsEncrypt() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request panelDomainRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}
		domain, err := normalizePanelDomain(request.Domain)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		s.panelDomainMu.Lock()
		defer s.panelDomainMu.Unlock()
		domainMutationMu.Lock()
		defer domainMutationMu.Unlock()
		operationCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 7*time.Minute)
		defer cancel()

		if err := ensurePanelDomainAvailable(domain); err != nil {
			panelDomainHTTPError(w, err)
			return
		}
		var email sql.NullString
		if err := database.DB.QueryRow("SELECT admin_email FROM users ORDER BY id ASC LIMIT 1").Scan(&email); err != nil || !email.Valid || strings.TrimSpace(email.String) == "" {
			http.Error(w, "Configure the admin email before issuing a Let's Encrypt certificate", http.StatusBadRequest)
			return
		}
		current, err := database.GetPanelDomainConfig()
		if err != nil {
			http.Error(w, "Failed to load current panel domain", http.StatusInternalServerError)
			return
		}
		challengeRoot, err := s.panelChallengeRoot()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var existingProxy *nginx.PanelProxyConfig
		if current.Domain != "" && panelCertificateFilesExist(current) {
			proxyConfig, configErr := s.panelProxyConfig(current)
			if configErr != nil {
				http.Error(w, configErr.Error(), http.StatusInternalServerError)
				return
			}
			existingProxy = &proxyConfig
		}
		restoreOriginal, err := nginx.InstallPanelChallenge(operationCtx, domain, challengeRoot, existingProxy)
		if err != nil {
			http.Error(w, "Failed to prepare domain validation: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if err := ssl.IssueLetsEncrypt(operationCtx, []string{domain}, challengeRoot, strings.TrimSpace(email.String)); err != nil {
			if rollbackErr := restoreOriginal(operationCtx); rollbackErr != nil {
				err = fmt.Errorf("%v (panel rollback failed: %v)", err, rollbackErr)
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		candidate := database.PanelDomainConfig{
			Domain: domain, SSLProvider: "letsencrypt",
			CertPath: "/etc/letsencrypt/live/" + domain + "/fullchain.pem",
			KeyPath:  "/etc/letsencrypt/live/" + domain + "/privkey.pem",
		}
		warning, err := s.activatePanelDomain(operationCtx, current, candidate, restoreOriginal)
		if err != nil {
			s.cleanupFailedPanelCertificate(operationCtx, current, candidate)
			http.Error(w, "Certificate issued but panel activation failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		LogActivity(0, "settings", "Panel domain connected with Let's Encrypt: "+domain)
		writePanelDomainResponse(w, s.persistedPanelDomainStatus(candidate), warning, http.StatusOK)
	}
}

func (s *Server) handlePanelCustomCertificate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request panelCustomCertificateRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}
		domain, err := normalizePanelDomain(request.Domain)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		inspection, err := ssl.InspectCertificatePEM([]byte(request.Certificate), []byte(request.PrivateKey))
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		if err := ssl.VerifyCertificateDomains(inspection.Certificate, []string{domain}); err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}

		s.panelDomainMu.Lock()
		defer s.panelDomainMu.Unlock()
		domainMutationMu.Lock()
		defer domainMutationMu.Unlock()
		operationCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), time.Minute)
		defer cancel()
		if err := ensurePanelDomainAvailable(domain); err != nil {
			panelDomainHTTPError(w, err)
			return
		}
		current, err := database.GetPanelDomainConfig()
		if err != nil {
			http.Error(w, "Failed to load current panel domain", http.StatusInternalServerError)
			return
		}
		certPath, keyPath, err := ssl.IssueCustom(domain, request.Certificate, request.PrivateKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		candidate := database.PanelDomainConfig{Domain: domain, SSLProvider: "custom", CertPath: certPath, KeyPath: keyPath}
		warning, err := s.activatePanelDomain(operationCtx, current, candidate, nil)
		if err != nil {
			s.cleanupFailedPanelCertificate(operationCtx, current, candidate)
			http.Error(w, "Failed to activate panel certificate: "+err.Error(), http.StatusInternalServerError)
			return
		}
		LogActivity(0, "settings", "Panel domain connected with a custom certificate: "+domain)
		writePanelDomainResponse(w, s.persistedPanelDomainStatus(candidate), warning, http.StatusOK)
	}
}

func (s *Server) handleListPanelCloneableCertificates() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		domain, err := normalizePanelDomain(r.URL.Query().Get("domain"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := ensurePanelDomainAvailable(domain); err != nil {
			panelDomainHTTPError(w, err)
			return
		}
		certificates, err := database.GetCloneableCertificates(0)
		if err != nil {
			http.Error(w, "Failed to load certificates", http.StatusInternalServerError)
			return
		}
		items := make([]cloneableCertificateResponse, 0)
		seen := make(map[string]struct{})
		for _, candidate := range certificates {
			inspection, inspectErr := ssl.InspectCertificateFiles(candidate.CertPath, candidate.KeyPath)
			if inspectErr != nil || ssl.VerifyCertificateDomains(inspection.Certificate, []string{domain}) != nil {
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
				ID: candidate.ID, SiteID: candidate.SiteID, SiteDomain: candidate.SiteDomain,
				Provider: candidate.Provider, Domains: names,
				ExpiresAt: inspection.Certificate.NotAfter.UTC().Format(time.RFC3339),
				Issuer:    issuer, Fingerprint: inspection.Fingerprint, Active: candidate.Active,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(items)
	}
}

func (s *Server) handlePanelCloneCertificate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request panelCloneCertificateRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.CertificateID <= 0 {
			http.Error(w, "Select a certificate to clone", http.StatusBadRequest)
			return
		}
		domain, err := normalizePanelDomain(request.Domain)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		s.panelDomainMu.Lock()
		defer s.panelDomainMu.Unlock()
		domainMutationMu.Lock()
		defer domainMutationMu.Unlock()
		operationCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), time.Minute)
		defer cancel()
		if err := ensurePanelDomainAvailable(domain); err != nil {
			panelDomainHTTPError(w, err)
			return
		}
		source, err := database.GetCloneSourceCertificate(request.CertificateID, 0)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		inspection, err := ssl.InspectCertificateFiles(source.CertPath, source.KeyPath)
		if err != nil {
			http.Error(w, "Source certificate is unavailable or invalid", http.StatusUnprocessableEntity)
			return
		}
		if err := ssl.VerifyCertificateDomains(inspection.Certificate, []string{domain}); err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		current, err := database.GetPanelDomainConfig()
		if err != nil {
			http.Error(w, "Failed to load current panel domain", http.StatusInternalServerError)
			return
		}
		certPath, keyPath, err := ssl.CloneCertificateFiles(domain, source.ID, inspection)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		candidate := database.PanelDomainConfig{
			Domain: domain, SSLProvider: "cloned", CertPath: certPath, KeyPath: keyPath,
			SourceCertificateID: source.ID,
		}
		warning, err := s.activatePanelDomain(operationCtx, current, candidate, nil)
		if err != nil {
			s.cleanupFailedPanelCertificate(operationCtx, current, candidate)
			http.Error(w, "Failed to activate cloned panel certificate: "+err.Error(), http.StatusInternalServerError)
			return
		}
		LogActivity(0, "settings", "Panel domain connected with a certificate cloned from "+source.SiteDomain)
		writePanelDomainResponse(w, s.persistedPanelDomainStatus(candidate), warning, http.StatusCreated)
	}
}

func (s *Server) activatePanelDomain(
	ctx context.Context,
	current, candidate database.PanelDomainConfig,
	restoreOriginal func(context.Context) error,
) (string, error) {
	inspection, err := ssl.InspectCertificateFiles(candidate.CertPath, candidate.KeyPath)
	if err != nil {
		if restoreOriginal != nil {
			_ = restoreOriginal(ctx)
		}
		return "", err
	}
	if err := ssl.VerifyCertificateDomains(inspection.Certificate, []string{candidate.Domain}); err != nil {
		if restoreOriginal != nil {
			_ = restoreOriginal(ctx)
		}
		return "", err
	}
	proxyConfig, err := s.panelProxyConfig(candidate)
	if err != nil {
		if restoreOriginal != nil {
			_ = restoreOriginal(ctx)
		}
		return "", err
	}
	restorePrevious, err := nginx.InstallPanelProxy(ctx, proxyConfig)
	if err != nil {
		if restoreOriginal != nil {
			if rollbackErr := restoreOriginal(ctx); rollbackErr != nil {
				return "", fmt.Errorf("%v (panel rollback failed: %v)", err, rollbackErr)
			}
		}
		return "", err
	}
	rollback := restorePrevious
	if restoreOriginal != nil {
		rollback = restoreOriginal
	}
	rollbackError := func(operationErr error) error {
		if rollbackErr := rollback(ctx); rollbackErr != nil {
			return fmt.Errorf("%v (panel rollback failed: %v)", operationErr, rollbackErr)
		}
		return operationErr
	}
	if err := verifyPanelDomainHealth(ctx, candidate.Domain, inspection.Certificate.Raw); err != nil {
		return "", rollbackError(err)
	}
	if err := database.SetPanelDomainConfig(candidate); err != nil {
		return "", rollbackError(err)
	}

	warning := ""
	if !samePanelCertificate(current, candidate) {
		if err := cleanupPanelCertificate(ctx, current); err != nil {
			warning = "The previous certificate could not be removed automatically: " + err.Error()
			log.Printf("Warning: panel domain activated but previous certificate cleanup failed: %v", err)
		}
	}
	return warning, nil
}

func verifyPanelDomainHealth(ctx context.Context, domain string, expectedCertificate []byte) error {
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(dialCtx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(dialCtx, network, "127.0.0.1:443")
		},
		// This is a loopback reachability check. Private custom CAs are
		// intentionally supported, so public-chain verification is replaced by
		// an exact match against the certificate inspected before installation.
		TLSClientConfig: &tls.Config{ //nolint:gosec
			ServerName: domain, MinVersion: tls.VersionTLS12, InsecureSkipVerify: true,
			VerifyConnection: func(state tls.ConnectionState) error {
				if len(state.PeerCertificates) == 0 || !bytes.Equal(state.PeerCertificates[0].Raw, expectedCertificate) {
					return fmt.Errorf("panel proxy served an unexpected TLS certificate")
				}
				return nil
			},
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+domain+"/api/v1/health", nil)
	if err != nil {
		return fmt.Errorf("prepare panel health check: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("panel health check failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("panel health check returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err != nil {
		return fmt.Errorf("read panel health check: %w", err)
	}
	var health map[string]string
	if err := json.Unmarshal(body, &health); err != nil || health["status"] != "ok" {
		return fmt.Errorf("panel health check returned an unexpected response")
	}
	return nil
}

func (s *Server) handleRemovePanelDomain() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.panelDomainMu.Lock()
		defer s.panelDomainMu.Unlock()
		domainMutationMu.Lock()
		defer domainMutationMu.Unlock()
		operationCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), time.Minute)
		defer cancel()

		current, err := database.GetPanelDomainConfig()
		if err != nil {
			http.Error(w, "Failed to load panel domain", http.StatusInternalServerError)
			return
		}
		if current.Domain == "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		restore, err := nginx.RemovePanelConfig(operationCtx)
		if err != nil {
			http.Error(w, "Failed to remove panel domain: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := database.ClearPanelDomainConfig(); err != nil {
			if rollbackErr := restore(operationCtx); rollbackErr != nil {
				err = fmt.Errorf("%v (panel rollback failed: %v)", err, rollbackErr)
			}
			http.Error(w, "Failed to remove panel domain: "+err.Error(), http.StatusInternalServerError)
			return
		}
		warning := ""
		if err := cleanupPanelCertificate(operationCtx, current); err != nil {
			warning = "The certificate could not be removed automatically: " + err.Error()
			log.Printf("Warning: panel domain removed but certificate cleanup failed: %v", err)
		}
		LogActivity(0, "settings", "Panel domain removed: "+current.Domain)
		writePanelDomainResponse(w, s.persistedPanelDomainStatus(database.PanelDomainConfig{}), warning, http.StatusOK)
	}
}

func (s *Server) persistedPanelDomainStatus(fallback database.PanelDomainConfig) panelDomainResponse {
	configured, err := database.GetPanelDomainConfig()
	if err != nil {
		configured = fallback
	}
	return s.panelDomainStatus(configured)
}

func cleanupPanelCertificate(ctx context.Context, configured database.PanelDomainConfig) error {
	if configured.Domain == "" || configured.CertPath == "" {
		return nil
	}
	switch configured.SSLProvider {
	case "letsencrypt":
		return ssl.DeleteLetsEncrypt(ctx, configured.CertPath)
	case "custom", "cloned":
		return ssl.RemoveManagedCertificateFiles(configured.CertPath, configured.KeyPath)
	default:
		return nil
	}
}

func (s *Server) cleanupFailedPanelCertificate(ctx context.Context, current, candidate database.PanelDomainConfig) {
	if samePanelCertificate(current, candidate) {
		return
	}
	if err := cleanupPanelCertificate(ctx, candidate); err != nil {
		log.Printf("Warning: failed to clean up unused panel certificate: %v", err)
	}
}

func samePanelCertificate(left, right database.PanelDomainConfig) bool {
	return left.CertPath != "" && left.CertPath == right.CertPath && left.KeyPath == right.KeyPath
}

func panelCertificateFilesExist(configured database.PanelDomainConfig) bool {
	if configured.CertPath == "" || configured.KeyPath == "" {
		return false
	}
	certInfo, certErr := os.Stat(configured.CertPath)
	keyInfo, keyErr := os.Stat(configured.KeyPath)
	return certErr == nil && keyErr == nil && certInfo.Mode().IsRegular() && keyInfo.Mode().IsRegular()
}

func panelDomainHTTPError(w http.ResponseWriter, err error) {
	if strings.Contains(err.Error(), "already attached") {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	http.Error(w, "Failed to validate panel domain", http.StatusInternalServerError)
}

func writePanelDomainResponse(w http.ResponseWriter, response panelDomainResponse, warning string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	payload := map[string]interface{}{
		"domain": response.Domain, "url": response.URL, "ssl_provider": response.SSLProvider,
		"ssl_active": response.SSLActive, "expires_at": response.ExpiresAt, "status": response.Status,
		"status_error": response.StatusError, "updated_at": response.UpdatedAt,
		"direct_access_preserved": response.DirectAccessPreserved,
	}
	if warning != "" {
		payload["warning"] = warning
	}
	json.NewEncoder(w).Encode(payload)
}

func (s *Server) reconcilePanelDomain(ctx context.Context) error {
	configured, err := database.GetPanelDomainConfig()
	if err != nil {
		return err
	}
	if configured.Domain == "" {
		cleanupCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		return nginx.RemoveOrphanedPanelConfig(cleanupCtx)
	}
	inspection, err := ssl.InspectCertificateFiles(configured.CertPath, configured.KeyPath)
	if err != nil {
		return err
	}
	if err := ssl.VerifyCertificateDomains(inspection.Certificate, []string{configured.Domain}); err != nil {
		return err
	}
	proxyConfig, err := s.panelProxyConfig(configured)
	if err != nil {
		return err
	}
	reconcileCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, err = nginx.InstallPanelProxy(reconcileCtx, proxyConfig)
	return err
}
