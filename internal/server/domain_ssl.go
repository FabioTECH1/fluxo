package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/ssl"
)

func (s *Server) handleDomainLetsEncrypt() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, err := strconv.Atoi(r.PathValue("id"))
		if err != nil || siteID <= 0 {
			http.Error(w, "Invalid site ID", http.StatusBadRequest)
			return
		}
		domainID, err := strconv.Atoi(r.PathValue("domain_id"))
		if err != nil || domainID <= 0 {
			http.Error(w, "Invalid domain ID", http.StatusBadRequest)
			return
		}
		if !s.beginCertificateIssuance(siteID) {
			http.Error(w, "Another site or certificate operation is in progress", http.StatusConflict)
			return
		}
		defer s.endCertificateIssuance(siteID)

		domain, behavior, err := aliasDomainConfiguration(siteID, domainID)
		if err != nil {
			http.Error(w, "Domain not found", http.StatusNotFound)
			return
		}

		var path, strategy, webRoot, appType, nodePreset, nodeMode, staticOutputDir, pythonPreset, appDirectory string
		err = database.DB.QueryRow(`
			SELECT path, deployment_strategy, web_root, app_type, node_preset, node_mode, static_output_dir,
			       COALESCE(python_preset, ''), COALESCE(app_directory, '.')
			FROM sites WHERE id = ?`, siteID).Scan(
			&path, &strategy, &webRoot, &appType, &nodePreset, &nodeMode, &staticOutputDir, &pythonPreset, &appDirectory,
		)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		var email sql.NullString
		if err := database.DB.QueryRow("SELECT admin_email FROM users LIMIT 1").Scan(&email); err != nil || !email.Valid || email.String == "" {
			http.Error(w, "Admin email not configured in settings", http.StatusBadRequest)
			return
		}

		webRootFull, err := getSiteApplicationWebRoot(path, webRoot, strategy, appType, nodePreset, nodeMode, staticOutputDir, pythonPreset, appDirectory)
		if err != nil {
			http.Error(w, "Invalid web root", http.StatusBadRequest)
			return
		}

		if err := ssl.IssueLetsEncrypt(r.Context(), configuredDomainHostnames(domain, behavior), webRootFull, email.String); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		certPath := "/etc/letsencrypt/live/" + domain + "/fullchain.pem"
		keyPath := "/etc/letsencrypt/live/" + domain + "/privkey.pem"
		expiresAt := ""
		if expiry, err := ssl.GetCertExpiry(certPath); err == nil {
			expiresAt = expiry.Format(time.RFC3339)
		}

		// Certbot may take several minutes. Only serialize the short state change
		// and verify that the alias still exists after issuance completes.
		domainMutationMu.Lock()
		defer domainMutationMu.Unlock()
		currentDomain, currentBehavior, aliasErr := aliasDomainConfiguration(siteID, domainID)
		targetStillAttached := aliasErr == nil && strings.EqualFold(currentDomain, domain) && currentBehavior == behavior

		certID, err := database.CreateCertificate(siteID, domain, "letsencrypt", certPath, keyPath, expiresAt)
		if err != nil {
			http.Error(w, "Failed to save certificate record", http.StatusInternalServerError)
			return
		}

		active := true
		activationError := ""
		if !targetStillAttached {
			active = false
			activationError = "Domain was removed while the certificate was being issued"
		} else if safeToKeep, err := activateCertificateForDomainLocked(siteID, domain, int(certID)); err != nil {
			if !safeToKeep {
				http.Error(w, "Certificate issued but activation failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
			active = false
			activationError = err.Error()
		}

		LogActivity(siteID, "settings", "Let's Encrypt certificate issued for "+domain)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":               certID,
			"domain":           domain,
			"provider":         "letsencrypt",
			"active":           active,
			"activation_error": activationError,
		})
	}
}

func (s *Server) handleActivateDomainCert() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))
		domainID, _ := strconv.Atoi(r.PathValue("domain_id"))
		certID, _ := strconv.Atoi(r.PathValue("certId"))
		if siteID <= 0 || domainID <= 0 || certID <= 0 {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		domainMutationMu.Lock()
		defer domainMutationMu.Unlock()

		domain, err := aliasDomain(siteID, domainID)
		if err != nil {
			http.Error(w, "Domain not found", http.StatusNotFound)
			return
		}
		if _, err := activateCertificateForDomainLocked(siteID, domain, certID); err != nil {
			status := http.StatusInternalServerError
			var validationErr *certificateActivationValidationError
			if errors.As(err, &validationErr) {
				status = http.StatusUnprocessableEntity
			}
			http.Error(w, "Failed to activate certificate: "+err.Error(), status)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

func (s *Server) handleDeactivateDomainCert() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))
		domainID, _ := strconv.Atoi(r.PathValue("domain_id"))
		if siteID <= 0 || domainID <= 0 {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		domainMutationMu.Lock()
		defer domainMutationMu.Unlock()

		domain, err := aliasDomain(siteID, domainID)
		if err != nil {
			http.Error(w, "Domain not found", http.StatusNotFound)
			return
		}
		previous, err := database.GetCertificateDomainBinding(siteID, domain)
		if err != nil {
			http.Error(w, "Failed to inspect domain SSL", http.StatusInternalServerError)
			return
		}
		previousDisabled, err := database.IsDomainSSLDisabled(siteID, domain)
		if err != nil {
			http.Error(w, "Failed to inspect domain SSL preference", http.StatusInternalServerError)
			return
		}
		if err := database.SetDomainSSLDisabled(siteID, domain, true); err != nil {
			http.Error(w, "Failed to deactivate domain SSL", http.StatusInternalServerError)
			return
		}
		if err := regenerateNginxForSiteWithError(siteID); err != nil {
			rollbackErr := database.SetDomainSSLDisabled(siteID, domain, previousDisabled)
			if rollbackErr == nil && previous != nil {
				rollbackErr = database.SetCertificateDomainBindingWithOrigin(
					siteID, domain, previous.CertificateID, previous.Origin,
				)
			}
			if rollbackErr == nil {
				rollbackErr = regenerateNginxForSiteWithError(siteID)
			}
			if rollbackErr != nil {
				http.Error(w, fmt.Sprintf("Failed to deactivate domain SSL: %v (rollback failed: %v)", err, rollbackErr), http.StatusInternalServerError)
				return
			}
			http.Error(w, "Failed to deactivate domain SSL: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func aliasDomain(siteID, domainID int) (string, error) {
	var domain string
	err := database.DB.QueryRow(
		"SELECT domain FROM domain_aliases WHERE id = ? AND site_id = ?", domainID, siteID,
	).Scan(&domain)
	return domain, err
}

type certificateActivationValidationError struct {
	err error
}

func (e *certificateActivationValidationError) Error() string { return e.err.Error() }
func (e *certificateActivationValidationError) Unwrap() error { return e.err }

var errWWWRedirectCertificateCoverage = errors.New("the active SSL certificate does not cover the requested WWW behavior")

func certificateAssignedToDomain(siteID int, domain string) (*database.Certificate, error) {
	var primaryDomain string
	if err := database.DB.QueryRow("SELECT domain FROM sites WHERE id = ?", siteID).Scan(&primaryDomain); err != nil {
		return nil, err
	}
	if strings.EqualFold(primaryDomain, domain) {
		return database.GetActiveCertificate(siteID)
	}

	disabled, err := database.IsDomainSSLDisabled(siteID, domain)
	if err != nil {
		return nil, err
	}
	if disabled {
		return nil, nil
	}
	binding, err := database.GetCertificateDomainBinding(siteID, domain)
	if err != nil {
		return nil, err
	}
	if binding != nil {
		return &database.Certificate{
			ID:       binding.CertificateID,
			SiteID:   binding.SiteID,
			Domain:   binding.Domain,
			Provider: binding.Provider,
			CertPath: binding.CertPath,
			KeyPath:  binding.KeyPath,
			Active:   true,
		}, nil
	}
	return database.GetActiveCertificate(siteID)
}

func validateWWWRedirectCertificateCoverage(siteID int, domain, behavior string) error {
	return validateWWWRedirectCertificateCoverageWith(siteID, domain, behavior, certificateCoversHostnames)
}

func validateWWWRedirectCertificateCoverageWith(
	siteID int,
	domain, behavior string,
	covers func(database.Certificate, []string) bool,
) error {
	certificate, err := certificateAssignedToDomain(siteID, domain)
	if err != nil || certificate == nil {
		return err
	}
	required := configuredDomainHostnames(domain, behavior)
	if covers(*certificate, required) {
		return nil
	}
	return fmt.Errorf("%w: deactivate SSL first, change the WWW behavior, then install a certificate covering %s",
		errWWWRedirectCertificateCoverage, strings.Join(required, " and "))
}

func domainHasActiveCertificate(siteID int, domain string) (bool, error) {
	behavior, err := domainWWWRedirect(siteID, domain)
	if err != nil {
		return false, err
	}
	requiredHostnames := configuredDomainHostnames(domain, behavior)
	certificate, err := certificateAssignedToDomain(siteID, domain)
	if err != nil {
		return false, err
	}
	if certificate == nil {
		return false, nil
	}
	return certificateCoversHostnames(*certificate, requiredHostnames), nil
}

// activateCertificateForDomainIfNoneActiveLocked leaves an existing explicit
// or inherited certificate in place.
func activateCertificateForDomainIfNoneActiveLocked(siteID int, domain string, certID int) (activated bool, safeToDiscard bool, err error) {
	active, err := domainHasActiveCertificate(siteID, domain)
	if err != nil {
		return false, true, err
	}
	if active {
		return false, true, nil
	}
	safeToDiscard, err = activateCertificateForDomainLocked(siteID, domain, certID)
	return err == nil, safeToDiscard, err
}

// activateCertificateForDomainLocked requires domainMutationMu to be held by the caller.
func activateCertificateForDomainLocked(siteID int, domain string, certID int) (bool, error) {
	cert, err := database.GetCertificate(certID, siteID)
	if err != nil {
		return true, &certificateActivationValidationError{err: err}
	}
	inspection, err := ssl.InspectCertificateFiles(cert.CertPath, cert.KeyPath)
	if err != nil {
		return true, err
	}
	behavior, err := domainWWWRedirect(siteID, domain)
	if err != nil {
		return true, &certificateActivationValidationError{err: err}
	}
	if err := ssl.VerifyCertificateDomains(inspection.Certificate, configuredDomainHostnames(domain, behavior)); err != nil {
		return true, &certificateActivationValidationError{err: err}
	}

	previous, err := database.GetCertificateDomainBinding(siteID, domain)
	if err != nil {
		return true, err
	}
	previousDisabled, err := database.IsDomainSSLDisabled(siteID, domain)
	if err != nil {
		return true, err
	}
	if err := database.SetCertificateDomainBinding(siteID, domain, certID); err != nil {
		return true, err
	}
	if err := regenerateNginxForSiteWithError(siteID); err != nil {
		var rollbackErr error
		if previous != nil {
			rollbackErr = database.SetCertificateDomainBindingWithOrigin(
				siteID, domain, previous.CertificateID, previous.Origin,
			)
		} else {
			rollbackErr = database.DeleteCertificateDomainBinding(siteID, domain)
			if rollbackErr == nil {
				rollbackErr = database.SetDomainSSLDisabled(siteID, domain, previousDisabled)
			}
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
