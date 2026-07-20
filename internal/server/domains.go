package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"fluxo/internal/database"
	sslservice "fluxo/internal/services/ssl"
)

var domainMutationMu sync.Mutex

type AddDomainRequest struct {
	Domain string `json:"domain"`
}

// handleListDomains returns the primary domain plus all aliases for a site.
func (s *Server) handleListDomains() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var primaryDomain string
		database.DB.QueryRow("SELECT domain FROM sites WHERE id = ?", siteID).Scan(&primaryDomain)

		type DomainItem struct {
			ID      int    `json:"id"`
			Domain  string `json:"domain"`
			Primary bool   `json:"primary"`
		}

		domains := make([]DomainItem, 0)

		if primaryDomain != "" {
			domains = append(domains, DomainItem{ID: 0, Domain: primaryDomain, Primary: true})
		}

		rows, err := database.DB.Query("SELECT id, domain FROM domain_aliases WHERE site_id = ? ORDER BY id", siteID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var d DomainItem
				if rows.Scan(&d.ID, &d.Domain) == nil {
					d.Primary = false
					domains = append(domains, d)
				}
			}
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

		inUse, err := domainInUse(req.Domain)
		if err != nil {
			http.Error(w, "Failed to validate domain", http.StatusInternalServerError)
			return
		}
		if inUse {
			http.Error(w, "Domain is already attached to another site", http.StatusConflict)
			return
		}
		if err := validateActiveCertificateHostname(siteID, req.Domain); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
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
		if err := database.DB.QueryRow("SELECT domain FROM domain_aliases WHERE id = ? AND site_id = ?", domainID, siteID).Scan(&domain); err != nil {
			http.Error(w, "Domain not found", http.StatusNotFound)
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
			_, rollbackErr := database.DB.Exec("INSERT INTO domain_aliases (id, site_id, domain) VALUES (?, ?, ?)", domainID, siteID, domain)
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

func domainInUse(domain string) (bool, error) {
	var inUse int
	err := database.DB.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM sites WHERE domain = ? COLLATE NOCASE
			UNION ALL
			SELECT 1 FROM domain_aliases WHERE domain = ? COLLATE NOCASE
		)`, domain, domain).Scan(&inUse)
	return inUse == 1, err
}

func validateActiveCertificateHostname(siteID int, domain string) error {
	cert, err := database.GetActiveCertificate(siteID)
	if err != nil {
		return fmt.Errorf("failed to inspect the active certificate")
	}
	if cert == nil {
		return nil
	}
	inspection, err := sslservice.InspectCertificateFiles(cert.CertPath, cert.KeyPath)
	if err != nil {
		return fmt.Errorf("active certificate is unavailable; deactivate it before adding this domain")
	}
	if err := sslservice.VerifyCertificateDomains(inspection.Certificate, []string{domain}); err != nil {
		return fmt.Errorf("active certificate does not cover %s; deactivate it before adding this domain", domain)
	}
	return nil
}
