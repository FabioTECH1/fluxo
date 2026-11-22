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
	backupservice "fluxo/internal/services/backup"
)

var domainMutationMu sync.Mutex

type AddDomainRequest struct {
	Domain      string `json:"domain"`
	WWWRedirect string `json:"www_redirect"`
	Wildcard    *bool  `json:"wildcard,omitempty"`
}

type UpdateDomainConfigurationRequest struct {
	ExpectedRevision string `json:"expected_revision,omitempty"`
	WWWRedirect      string `json:"www_redirect"`
	Wildcard         *bool  `json:"wildcard,omitempty"`
}

func restoreDeletedDomainAlias(id, siteID int, domain string, sslDisabled bool, wwwRedirect string) error {
	_, err := database.DB.Exec(
		"INSERT INTO domain_aliases (id, site_id, domain, ssl_disabled, www_redirect) VALUES (?, ?, ?, ?, ?)",
		id, siteID, domain, sslDisabled, wwwRedirect,
	)
	return err
}

type DomainItem struct {
	Revision          string `json:"revision"`
	Wildcard          bool   `json:"wildcard"`
	WildcardSSLActive bool   `json:"wildcard_ssl_active"`
	WildcardSSLState  string `json:"wildcard_ssl_state"`
	ID                int    `json:"id"`
	Domain            string `json:"domain"`
	Primary           bool   `json:"primary"`
	SSLActive         bool   `json:"ssl_active"`
	SSLProvider       string `json:"ssl_provider,omitempty"`
	CertificateID     int    `json:"certificate_id,omitempty"`
	SSLInherited      bool   `json:"ssl_inherited,omitempty"`
	SSLDisabled       bool   `json:"ssl_disabled,omitempty"`
	WWWRedirect       string `json:"www_redirect"`
}

// handleListDomains returns the primary domain plus all aliases for a site.
func (s *Server) handleListDomains() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var primaryDomain, primaryWWWRedirect string
		database.DB.QueryRow("SELECT domain, COALESCE(www_redirect, 'none') FROM sites WHERE id = ?", siteID).Scan(&primaryDomain, &primaryWWWRedirect)

		domains := make([]DomainItem, 0)

		if primaryDomain != "" {
			domains = append(domains, DomainItem{ID: 0, Domain: primaryDomain, Primary: true, WWWRedirect: primaryWWWRedirect})
		}

		rows, err := database.DB.Query("SELECT id, domain, ssl_disabled, COALESCE(www_redirect, 'none') FROM domain_aliases WHERE site_id = ? ORDER BY id", siteID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var d DomainItem
				if rows.Scan(&d.ID, &d.Domain, &d.SSLDisabled, &d.WWWRedirect) == nil {
					d.Primary = false
					domains = append(domains, d)
				}
			}
		}
		if err := applyDomainSSLState(siteID, domains); err != nil {
			http.Error(w, "Failed to load domain SSL state", http.StatusInternalServerError)
			return
		}

		if e := wildcardDomainState(siteID, domains); e != nil {
			networkHTTPError(w, e)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(domains)
	}
}

// handleAddDomain atomically persists a canonical parent and explicit wildcard
// setting only after validated activation. Root/alias certificate state remains
// independent from wildcard coverage and DNS remains operator-managed.
func (s *Server) handleAddDomain() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, e := parseSiteID(r)
		if e != nil {
			phpJSON(w, 400, map[string]string{"message": e.Error()})
			return
		}
		var req AddDomainRequest
		if !decodePHPRequest(w, r, &req) {
			return
		}
		req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))
		wild := req.Wildcard != nil && *req.Wildcard
		if strings.HasPrefix(req.Domain, "*.") {
			wild = true
			req.Domain = strings.TrimPrefix(req.Domain, "*.")
		}
		if !domainRegex.MatchString(req.Domain) {
			phpJSON(w, 400, map[string]string{"message": "Invalid domain format"})
			return
		}
		behavior, e := normalizeWWWRedirect(req.Domain, req.WWWRedirect, defaultWWWRedirectForDomain(req.Domain))
		if e != nil {
			phpJSON(w, 400, map[string]string{"message": e.Error()})
			return
		}
		if !s.reserveNetworkMutation(w, siteID) {
			return
		}
		defer s.endCertificateSiteMutation(siteID)

		domainMutationMu.Lock()
		defer domainMutationMu.Unlock()
		siteNginxMutationMu.Lock()
		defer siteNginxMutationMu.Unlock()
		if used, e := domainInUse(req.Domain, false); e != nil || used {
			networkHTTPError(w, fmt.Errorf("Domain is unavailable"))
			return
		}
		if e = ensureWWWRouteAvailable(siteID, -1, req.Domain, behavior); e != nil {
			networkHTTPError(w, e)
			return
		}
		state, e := database.GetSiteNetwork(siteID)
		if e != nil {
			networkHTTPError(w, e)
			return
		}
		if wild {
			if e = validateWildcardOwner(siteID, req.Domain, state); e != nil {
				networkHTTPError(w, e)
				return
			}
		}
		setWildcard(&state, req.Domain, wild)
		var newID int64
		options := siteRenderOptions{MetadataOnly: !wild, ExtraDomains: []networkExtraDomain{{Domain: req.Domain, WWWRedirect: behavior}}}
		e = applyNetworkChangeLocked(r.Context(), siteID, state, options, func(tx *sql.Tx) error {
			res, err := tx.Exec("INSERT INTO domain_aliases(site_id,domain,www_redirect) VALUES(?,?,?)", siteID, req.Domain, behavior)
			if err == nil {
				newID, err = res.LastInsertId()
			}
			return err
		})
		if e != nil {
			networkHTTPError(w, e)
			return
		}
		phpJSON(w, 200, map[string]any{"id": newID, "site_id": siteID, "domain": req.Domain, "www_redirect": behavior, "wildcard": wild})
	}
}
func (s *Server) handleUpdateDomainConfiguration() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, e := parseSiteID(r)
		if e != nil {
			phpJSON(w, 400, map[string]string{"message": e.Error()})
			return
		}
		domainID, e := strconv.Atoi(r.PathValue("domain_id"))
		if e != nil || domainID < 0 {
			phpJSON(w, 400, map[string]string{"message": "Invalid domain ID"})
			return
		}
		var req UpdateDomainConfigurationRequest
		if !decodePHPRequest(w, r, &req) {
			return
		}
		if !s.reserveNetworkMutation(w, siteID) {
			return
		}
		defer s.endCertificateSiteMutation(siteID)

		domainMutationMu.Lock()
		defer domainMutationMu.Unlock()
		siteNginxMutationMu.Lock()
		defer siteNginxMutationMu.Unlock()
		var domain, previous string
		if domainID == 0 {
			e = database.DB.QueryRow("SELECT domain,COALESCE(www_redirect,'none') FROM sites WHERE id=?", siteID).Scan(&domain, &previous)
		} else {
			e = database.DB.QueryRow("SELECT domain,COALESCE(www_redirect,'none') FROM domain_aliases WHERE id=? AND site_id=?", domainID, siteID).Scan(&domain, &previous)
		}
		if e != nil {
			networkHTTPError(w, e)
			return
		}
		behavior, e := normalizeWWWRedirect(domain, req.WWWRedirect, wwwRedirectNone)
		if e != nil {
			phpJSON(w, 400, map[string]string{"message": e.Error()})
			return
		}
		if e = ensureWWWRouteAvailable(siteID, domainID, domain, behavior); e != nil {
			networkHTTPError(w, e)
			return
		}
		if behavior != previous {
			if e = validateWWWRedirectCertificateCoverage(siteID, domain, behavior); e != nil {
				networkHTTPError(w, e)
				return
			}
		}
		state, e := database.GetSiteNetwork(siteID)
		if e != nil {
			networkHTTPError(w, e)
			return
		}
		wild := false
		for _, parent := range state.Wildcards {
			if parent == domain {
				wild = true
			}
		}
		override, overrideErr := database.GetSiteVhostOverride(siteID)
		if overrideErr != nil {
			networkHTTPError(w, overrideErr)
			return
		}
		if req.Wildcard != nil && req.ExpectedRevision == "" {
			phpJSON(w, 400, map[string]string{"message": "expected_revision is required for wildcard configuration"})
			return
		}
		if req.ExpectedRevision != "" && req.ExpectedRevision != domainSettingsRevision(domain, previous, wild, override != nil) {
			networkHTTPError(w, errors.New("Domain settings changed; refresh before saving"))
			return
		}
		if req.Wildcard != nil {
			wild = *req.Wildcard
		}
		if wild {
			if e = validateWildcardOwner(siteID, domain, state); e != nil {
				networkHTTPError(w, e)
				return
			}
		}
		setWildcard(&state, domain, wild)
		e = applyNetworkChangeLocked(r.Context(), siteID, state, siteRenderOptions{MetadataOnly: !wild, WWW: map[string]string{domain: behavior}}, func(tx *sql.Tx) error {
			if domainID == 0 {
				_, err := tx.Exec("UPDATE sites SET www_redirect=?,updated_at=CURRENT_TIMESTAMP WHERE id=?", behavior, siteID)
				return err
			}
			_, err := tx.Exec("UPDATE domain_aliases SET www_redirect=? WHERE id=? AND site_id=?", behavior, domainID, siteID)
			return err
		})
		if e != nil {
			networkHTTPError(w, e)
			return
		}
		LogActivity(siteID, "domain", "Domain routing updated")
		phpJSON(w, 200, map[string]any{"domain": domain, "www_redirect": behavior, "wildcard": wild})
	}
}

// Domain deletion removes routing through the same serialized, recoverable
// managed activation. A custom vhost remains operator-owned and untouched.
func (s *Server) handleDeleteDomain() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, e := parseSiteID(r)
		if e != nil {
			phpJSON(w, 400, map[string]string{"message": e.Error()})
			return
		}
		id, e := strconv.Atoi(r.PathValue("domain_id"))
		if e != nil || id <= 0 {
			phpJSON(w, 400, map[string]string{"message": "Invalid domain ID"})
			return
		}
		if !s.reserveNetworkMutation(w, siteID) {
			return
		}
		defer s.endCertificateSiteMutation(siteID)

		domainMutationMu.Lock()
		defer domainMutationMu.Unlock()
		siteNginxMutationMu.Lock()
		defer siteNginxMutationMu.Unlock()
		var domain string
		if e = database.DB.QueryRow("SELECT domain FROM domain_aliases WHERE id=? AND site_id=?", id, siteID).Scan(&domain); e != nil {
			networkHTTPError(w, e)
			return
		}
		state, e := database.GetSiteNetwork(siteID)
		if e != nil {
			networkHTTPError(w, e)
			return
		}
		setWildcard(&state, domain, false)
		e = applyNetworkChangeLocked(r.Context(), siteID, state, siteRenderOptions{MetadataOnly: true, RemoveDomains: map[string]bool{domain: true}}, func(tx *sql.Tx) error {
			if _, e := tx.Exec("DELETE FROM certificate_domain_bindings WHERE site_id=? AND domain=?", siteID, domain); e != nil {
				return e
			}
			_, e := tx.Exec("DELETE FROM domain_aliases WHERE id=? AND site_id=?", id, siteID)
			return e
		})
		if e != nil {
			networkHTTPError(w, e)
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
			SELECT 1 FROM sites WHERE COALESCE(www_redirect, 'none') != 'none' AND ('www.' || domain) = ? COLLATE NOCASE
			UNION ALL
			SELECT 1 FROM domain_aliases WHERE COALESCE(www_redirect, 'none') != 'none' AND ('www.' || domain) = ? COLLATE NOCASE
			UNION ALL
			SELECT 1 FROM sites WHERE ? AND path = ? COLLATE NOCASE
			UNION ALL
			SELECT 1 FROM panel_domain WHERE domain != '' AND domain = ? COLLATE NOCASE
		)`, domain, domain, domain, domain, reserveInfrastructureName, filepath.Join(safeinput.ManagedSitesRoot, domain), domain).Scan(&inUse)
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
			if strings.Contains(err.Error(), "active backup") || strings.Contains(err.Error(), "already in progress") ||
				errors.Is(err, backupservice.ErrDatabaseOperationInProgress) {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			http.Error(w, "Failed to prepare the primary domain change", http.StatusInternalServerError)
			return
		}
		defer s.backupManager.FinishSiteMutation(siteID)

		domainMutationMu.Lock()
		defer domainMutationMu.Unlock()
		if e := pendingNetwork(siteID); e != nil {
			networkHTTPError(w, e)
			return
		}

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
	domain      string
	disabled    bool
	wwwRedirect string
	binding     *database.CertificateDomainBinding
}

func planPrimaryDomainCertificates(siteID, domainID int) (primaryDomainCertificatePlan, error) {
	return planPrimaryDomainCertificatesWithCoverage(siteID, domainID, certificateCoversHostname)
}

func planPrimaryDomainCertificatesWithCoverage(
	siteID, domainID int,
	covers func(database.Certificate, string) bool,
) (primaryDomainCertificatePlan, error) {
	var primary, primaryWWWRedirect, selectedDomain, selectedWWWRedirect string
	var selectedDisabled bool
	if err := database.DB.QueryRow(
		"SELECT domain, COALESCE(www_redirect, 'none') FROM sites WHERE id = ?", siteID,
	).Scan(&primary, &primaryWWWRedirect); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return primaryDomainCertificatePlan{}, errDomainNotFound
		}
		return primaryDomainCertificatePlan{}, err
	}
	if err := database.DB.QueryRow(
		"SELECT domain, ssl_disabled, COALESCE(www_redirect, 'none') FROM domain_aliases WHERE id = ? AND site_id = ?", domainID, siteID,
	).Scan(&selectedDomain, &selectedDisabled, &selectedWWWRedirect); err != nil {
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
	coversConfiguredDomain := func(cert database.Certificate, domain, behavior string) bool {
		for _, hostname := range configuredDomainHostnames(domain, behavior) {
			if !covers(cert, hostname) {
				return false
			}
		}
		return true
	}
	if !selectedDisabled {
		if binding, ok := bindingByDomain[strings.ToLower(selectedDomain)]; ok {
			newActiveID = binding.CertificateID
			newActiveCert, err = database.GetCertificate(newActiveID, siteID)
			if err != nil || !coversConfiguredDomain(*newActiveCert, selectedDomain, selectedWWWRedirect) {
				return primaryDomainCertificatePlan{}, errDomainCertificateInvalid
			}
		} else if activeCert != nil && coversConfiguredDomain(*activeCert, selectedDomain, selectedWWWRedirect) {
			newActiveID = activeCert.ID
			newActiveCert = activeCert
		}
	}

	aliases := []aliasCertificateState{{domain: primary, wwwRedirect: primaryWWWRedirect}}
	rows, err := database.DB.Query(
		"SELECT domain, ssl_disabled, COALESCE(www_redirect, 'none') FROM domain_aliases WHERE site_id = ? AND id != ? ORDER BY id", siteID, domainID,
	)
	if err != nil {
		return primaryDomainCertificatePlan{}, err
	}
	for rows.Next() {
		var alias aliasCertificateState
		if err := rows.Scan(&alias.domain, &alias.disabled, &alias.wwwRedirect); err != nil {
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
		newCovers := newActiveCert != nil && coversConfiguredDomain(*newActiveCert, alias.domain, alias.wwwRedirect)
		if alias.binding != nil {
			if alias.binding.Origin == database.CertificateBindingOriginPreserved && newCovers {
				mutations = append(mutations, database.CertificateDomainBindingMutation{Domain: alias.domain})
			}
			continue
		}
		oldCovers := activeCert != nil && coversConfiguredDomain(*activeCert, alias.domain, alias.wwwRedirect)
		if oldCovers && !newCovers {
			mutations = append(mutations, database.CertificateDomainBindingMutation{
				Domain: alias.domain, CertificateID: activeCert.ID, Origin: database.CertificateBindingOriginPreserved,
			})
		}
	}

	return primaryDomainCertificatePlan{activeCertificateID: newActiveID, mutations: mutations}, nil
}
