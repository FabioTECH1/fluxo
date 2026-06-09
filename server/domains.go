package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"fluxo/database"
)

type AddDomainRequest struct {
	Domain string `json:"domain"`
}

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

func (s *Server) handleAddDomain() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var req AddDomainRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Domain) == "" {
			http.Error(w, "Invalid domain", http.StatusBadRequest)
			return
		}

		res, err := database.DB.Exec("INSERT INTO domain_aliases (site_id, domain) VALUES (?, ?)", siteID, req.Domain)
		if err != nil {
			http.Error(w, "Failed to add domain", http.StatusInternalServerError)
			return
		}

		id, _ := res.LastInsertId()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      id,
			"site_id": siteID,
			"domain":  req.Domain,
		})
	}
}

func (s *Server) handleDeleteDomain() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))
		domainID, _ := strconv.Atoi(r.PathValue("domain_id"))

		res, err := database.DB.Exec("DELETE FROM domain_aliases WHERE id = ? AND site_id = ?", domainID, siteID)
		if err != nil {
			http.Error(w, "Failed to delete domain", http.StatusInternalServerError)
			return
		}
		if rows, _ := res.RowsAffected(); rows == 0 {
			http.Error(w, "Domain not found", http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
