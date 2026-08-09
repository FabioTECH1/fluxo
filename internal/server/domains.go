package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"fluxo/internal/database"
	"fluxo/internal/safeinput"
)

var domainMutationMu sync.Mutex

type AddDomainRequest struct {
	Domain string `json:"domain"`
}

type DomainItem struct {
	ID            int    `json:"id"`
	Domain        string `json:"domain"`
	Primary       bool   `json:"primary"`
	SSLActive     bool   `json:"ssl_active"`
	SSLProvider   string `json:"ssl_provider,omitempty"`
	CertificateID int    `json:"certificate_id,omitempty"`
	SSLInherited  bool   `json:"ssl_inherited,omitempty"`
	SSLDisabled   bool   `json:"ssl_disabled,omitempty"`
}

// handleListDomains returns the primary domain plus all aliases for a site.
func (s *Server) handleListDomains() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var primaryDomain string
		database.DB.QueryRow("SELECT domain FROM sites WHERE id = ?", siteID).Scan(&primaryDomain)

		domains := make([]DomainItem, 0)

		if primaryDomain != "" {
			domains = append(domains, DomainItem{ID: 0, Domain: primaryDomain, Primary: true})
		}

		rows, err := database.DB.Query("SELECT id, domain, ssl_disabled FROM domain_aliases WHERE site_id = ? ORDER BY id", siteID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var d DomainItem
				if rows.Scan(&d.ID, &d.Domain, &d.SSLDisabled) == nil {
					d.Primary = false
					domains = append(domains, d)
				}
			}
		}
		if err := applyDomainSSLState(siteID, domains); err != nil {
			http.Error(w, "Failed to load domain SSL state", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(domains)
	}
}

// handleAddDomain adds an alias domain to a site and regenerates nginx config.
func (s *Server) handleAddDomain() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var req AddDomainRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Domain) == "" {
			http.Error(w, "Invalid domain", http.StatusBadRequest)
			return
		}
		req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))

		if !domainRegex.MatchString(req.Domain) {
			http.Error(w, "Invalid domain format", http.StatusBadRequest)
			return
		}

		domainMutationMu.Lock()
		defer domainMutationMu.Unlock()

		var siteExists int
		if err := database.DB.QueryRow("SELECT COUNT(*) FROM sites WHERE id = ?", siteID).Scan(&siteExists); err != nil {
			http.Error(w, "Failed to validate site", http.StatusInternalServerError)
			return
		}
		if siteExists != 1 {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		inUse, err := domainInUse(req.Domain, false)
		if err != nil {
			http.Error(w, "Failed to validate domain", http.StatusInternalServerError)
			return
		}
		if inUse {
			http.Error(w, "Domain is already attached to another site", http.StatusConflict)
			return
		}
		res, err := database.DB.Exec("INSERT INTO domain_aliases (site_id, domain) VALUES (?, ?)", siteID, req.Domain)
		if err != nil {
			http.Error(w, "Failed to add domain", http.StatusInternalServerError)
			return
		}

		id, err := res.LastInsertId()
		if err != nil {
			database.DB.Exec("DELETE FROM domain_aliases WHERE site_id = ? AND domain = ?", siteID, req.Domain)
			http.Error(w, "Failed to identify the added domain", http.StatusInternalServerError)
			return
		}

		if err := regenerateNginxForSiteWithError(siteID); err != nil {
			deleteResult, deleteErr := database.DB.Exec("DELETE FROM domain_aliases WHERE id = ? AND site_id = ?", id, siteID)
			if deleteErr == nil {
				if rows, rowsErr := deleteResult.RowsAffected(); rowsErr != nil || rows != 1 {
					deleteErr = fmt.Errorf("failed to remove the alias record")
				}
			}
			rollbackErr := deleteErr
			if rollbackErr == nil {
				rollbackErr = regenerateNginxForSiteWithError(siteID)
			}
			if rollbackErr != nil {
				http.Error(w, fmt.Sprintf("Failed to configure domain: %v (rollback failed: %v)", err, rollbackErr), http.StatusInternalServerError)
				return
			}
			http.Error(w, "Failed to configure domain: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      id,
			"site_id": siteID,
			"domain":  req.Domain,
		})
	}
}

// handleDeleteDomain removes an alias domain and regenerates nginx config.
func (s *Server) handleDeleteDomain() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))
		domainID, _ := strconv.Atoi(r.PathValue("domain_id"))

		domainMutationMu.Lock()
		defer domainMutationMu.Unlock()

		var domain string
		var sslDisabled bool
		if err := database.DB.QueryRow(
			"SELECT domain, ssl_disabled FROM domain_aliases WHERE id = ? AND site_id = ?", domainID, siteID,
		).Scan(&domain, &sslDisabled); err != nil {
			http.Error(w, "Domain not found", http.StatusNotFound)
			return
		}
		previousBinding, err := database.GetCertificateDomainBinding(siteID, domain)
		if err != nil {
			http.Error(w, "Failed to inspect domain SSL", http.StatusInternalServerError)
			return
		}
		res, err := database.DB.Exec("DELETE FROM domain_aliases WHERE id = ? AND site_id = ?", domainID, siteID)
		if err != nil {
			http.Error(w, "Failed to delete domain", http.StatusInternalServerError)
			return
		}
		if rows, _ := res.RowsAffected(); rows == 0 {
			http.Error(w, "Domain not found", http.StatusNotFound)
			return
		}

		if err := regenerateNginxForSiteWithError(siteID); err != nil {
			_, rollbackErr := database.DB.Exec(
				"INSERT INTO domain_aliases (id, site_id, domain, ssl_disabled) VALUES (?, ?, ?, ?)",
				domainID, siteID, domain, sslDisabled,
			)
			if rollbackErr == nil && previousBinding != nil {
				rollbackErr = database.SetCertificateDomainBindingWithOrigin(
					siteID, domain, previousBinding.CertificateID, previousBinding.Origin,
				)
			}
			if rollbackErr == nil {
				rollbackErr = regenerateNginxForSiteWithError(siteID)
			}
			if rollbackErr != nil {
				http.Error(w, fmt.Sprintf("Failed to remove domain: %v (rollback failed: %v)", err, rollbackErr), http.StatusInternalServerError)
				return
			}
			http.Error(w, "Failed to remove domain: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func domainInUse(domain string, reserveInfrastructureName bool) (bool, error) {
	var inUse int
	err := database.DB.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM sites WHERE domain = ? COLLATE NOCASE
			UNION ALL
			SELECT 1 FROM domain_aliases WHERE domain = ? COLLATE NOCASE
			UNION ALL
			SELECT 1 FROM sites WHERE ? AND path = ? COLLATE NOCASE
			UNION ALL
			SELECT 1 FROM panel_domain WHERE domain != '' AND domain = ? COLLATE NOCASE
		)`, domain, domain, reserveInfrastructureName, filepath.Join(safeinput.ManagedSitesRoot, domain), domain).Scan(&inUse)
	return inUse == 1, err
}

// handlePromoteDomain makes an alias the public primary domain while retaining
// the existing site path as its stable infrastructure identity.
func (s *Server) handlePromoteDomain() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, siteErr := strconv.Atoi(r.PathValue("id"))
		domainID, domainErr := strconv.Atoi(r.PathValue("domain_id"))
		if siteErr != nil || domainErr != nil || siteID <= 0 || domainID <= 0 {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		if !s.beginCertificateSiteMutation(siteID) {
			http.Error(w, "Wait for the site's active certificate operation to finish", http.StatusConflict)
			return
		}
		defer s.endCertificateSiteMutation(siteID)
		if err := s.backupManager.PrepareSiteMutation(siteID); err != nil {
			if strings.Contains(err.Error(), "active backup") || strings.Contains(err.Error(), "already in progress") {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			http.Error(w, "Failed to prepare the primary domain change", http.StatusInternalServerError)
			return
		}
		defer s.backupManager.FinishSiteMutation(siteID)

		domainMutationMu.Lock()
		defer domainMutationMu.Unlock()

		var activeWork int
		if err := database.DB.QueryRow(`
			SELECT
				(SELECT COUNT(*) FROM deployments WHERE site_id = ? AND status IN ('pending', 'running'))`,
			siteID,
		).Scan(&activeWork); err != nil {
			http.Error(w, "Failed to inspect active site operations", http.StatusInternalServerError)
			return
		}
		if activeWork > 0 {
			http.Error(w, "Wait for the site's deployments to finish before changing its primary domain", http.StatusConflict)
			return
		}

		plan, err := planPrimaryDomainCertificates(siteID, domainID)
		if err != nil {
			status := http.StatusInternalServerError
			if err == errDomainNotFound {
				status = http.StatusNotFound
			} else if err == errDomainCertificateInvalid {
				status = http.StatusUnprocessableEntity
			}
			http.Error(w, err.Error(), status)
			return
		}

		snapshot, err := database.PromoteDomainAlias(siteID, domainID, plan.activeCertificateID, plan.mutations)
		if err != nil {
			http.Error(w, "Failed to change primary domain: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := regenerateNginxForSiteWithError(siteID); err != nil {
			rollbackErr := database.RestorePrimaryDomain(snapshot)
			if rollbackErr == nil {
				rollbackErr = regenerateNginxForSiteWithError(siteID)
			}
			if rollbackErr != nil {
				http.Error(w, fmt.Sprintf("Failed to change primary domain: %v (rollback failed: %v)", err, rollbackErr), http.StatusInternalServerError)
				return
			}
			http.Error(w, "Failed to change primary domain: "+err.Error(), http.StatusInternalServerError)
			return
		}

		LogActivity(siteID, "domain", "Primary domain changed from "+snapshot.OldPrimary+" to "+snapshot.NewPrimary)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"primary_domain":          snapshot.NewPrimary,
			"previous_primary_domain": snapshot.OldPrimary,
			"site_path_changed":       false,
			"warnings": []string{
				"Review application URLs, cookies, OAuth callbacks, and custom commands that contain the previous domain.",
			},
		})
	}
}

var (
	errDomainNotFound           = errors.New("domain alias not found")
	errDomainCertificateInvalid = errors.New("the alias certificate is unavailable or no longer covers this domain")
)

type primaryDomainCertificatePlan struct {
	activeCertificateID int
	mutations           []database.CertificateDomainBindingMutation
}

type aliasCertificateState struct {
	domain   string
	disabled bool
	binding  *database.CertificateDomainBinding
}

func planPrimaryDomainCertificates(siteID, domainID int) (primaryDomainCertificatePlan, error) {
	return planPrimaryDomainCertificatesWithCoverage(siteID, domainID, certificateCoversHostname)
}

func planPrimaryDomainCertificatesWithCoverage(
	siteID, domainID int,
	covers func(database.Certificate, string) bool,
) (primaryDomainCertificatePlan, error) {
	var primary, selectedDomain string
	var selectedDisabled bool
	if err := database.DB.QueryRow("SELECT domain FROM sites WHERE id = ?", siteID).Scan(&primary); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return primaryDomainCertificatePlan{}, errDomainNotFound
		}
		return primaryDomainCertificatePlan{}, err
	}
	if err := database.DB.QueryRow(
		"SELECT domain, ssl_disabled FROM domain_aliases WHERE id = ? AND site_id = ?", domainID, siteID,
	).Scan(&selectedDomain, &selectedDisabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return primaryDomainCertificatePlan{}, errDomainNotFound
		}
		return primaryDomainCertificatePlan{}, err
	}

	bindings, err := database.GetCertificateDomainBindings(siteID)
	if err != nil {
		return primaryDomainCertificatePlan{}, err
	}
	bindingByDomain := make(map[string]database.CertificateDomainBinding, len(bindings))
	for _, binding := range bindings {
		bindingByDomain[strings.ToLower(binding.Domain)] = binding
	}

	activeCert, err := database.GetActiveCertificate(siteID)
	if err != nil {
		return primaryDomainCertificatePlan{}, err
	}
	newActiveID := 0
	var newActiveCert *database.Certificate
	if !selectedDisabled {
		if binding, ok := bindingByDomain[strings.ToLower(selectedDomain)]; ok {
			newActiveID = binding.CertificateID
			newActiveCert, err = database.GetCertificate(newActiveID, siteID)
			if err != nil || !covers(*newActiveCert, selectedDomain) {
				return primaryDomainCertificatePlan{}, errDomainCertificateInvalid
			}
		} else if activeCert != nil && covers(*activeCert, selectedDomain) {
			newActiveID = activeCert.ID
			newActiveCert = activeCert
		}
	}

	aliases := []aliasCertificateState{{domain: primary}}
	rows, err := database.DB.Query(
		"SELECT domain, ssl_disabled FROM domain_aliases WHERE site_id = ? AND id != ? ORDER BY id", siteID, domainID,
	)
	if err != nil {
		return primaryDomainCertificatePlan{}, err
	}
	for rows.Next() {
		var alias aliasCertificateState
		if err := rows.Scan(&alias.domain, &alias.disabled); err != nil {
			rows.Close()
			return primaryDomainCertificatePlan{}, err
		}
		if binding, ok := bindingByDomain[strings.ToLower(alias.domain)]; ok {
			bindingCopy := binding
			alias.binding = &bindingCopy
		}
		aliases = append(aliases, alias)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return primaryDomainCertificatePlan{}, err
	}
	rows.Close()

	mutations := make([]database.CertificateDomainBindingMutation, 0)
	for _, alias := range aliases {
		if alias.disabled {
			continue
		}
		newCovers := newActiveCert != nil && covers(*newActiveCert, alias.domain)
		if alias.binding != nil {
			if alias.binding.Origin == database.CertificateBindingOriginPreserved && newCovers {
				mutations = append(mutations, database.CertificateDomainBindingMutation{Domain: alias.domain})
			}
			continue
		}
		oldCovers := activeCert != nil && covers(*activeCert, alias.domain)
		if oldCovers && !newCovers {
			mutations = append(mutations, database.CertificateDomainBindingMutation{
				Domain: alias.domain, CertificateID: activeCert.ID, Origin: database.CertificateBindingOriginPreserved,
			})
		}
	}

	return primaryDomainCertificatePlan{activeCertificateID: newActiveID, mutations: mutations}, nil
}
