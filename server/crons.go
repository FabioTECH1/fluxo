package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"fluxo/database"
	"fluxo/services/cron"
)

type CreateCronRequest struct {
	Name       string `json:"name"`
	Expression string `json:"expression"`
	Command    string `json:"command"`
	User       string `json:"user"`
}

func (s *Server) handleListCrons() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		rows, err := database.DB.Query("SELECT id, site_id, name, expression, command, user, created_at FROM crons WHERE site_id = ?", siteID)
		if err != nil {
			http.Error(w, "DB error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var crons []database.Cron
		for rows.Next() {
			var c database.Cron
			rows.Scan(&c.ID, &c.SiteID, &c.Name, &c.Expression, &c.Command, &c.User, &c.CreatedAt)
			crons = append(crons, c)
		}

		if crons == nil {
			crons = []database.Cron{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(crons)
	}
}

func (s *Server) handleCreateCron() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var req CreateCronRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		var domain string
		err := database.DB.QueryRow("SELECT domain FROM sites WHERE id = ?", siteID).Scan(&domain)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		if req.User == "" {
			req.User = "fluxo"
		}
		res, err := database.DB.Exec("INSERT INTO crons (site_id, name, expression, command, user) VALUES (?, ?, ?, ?, ?)", siteID, req.Name, req.Expression, req.Command, req.User)
		if err != nil {
			http.Error(w, "Failed to insert", http.StatusInternalServerError)
			return
		}

		id, _ := res.LastInsertId()

		if err := cron.Create(int(id), domain, req.Expression, req.Command); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
	}
}

func (s *Server) handleDeleteCron() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cronID, _ := strconv.Atoi(r.PathValue("cron_id"))

		cron.Delete(cronID)
		database.DB.Exec("DELETE FROM crons WHERE id = ?", cronID)

		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleCreateGlobalCron() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateCronRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}
		if req.User == "" {
			req.User = "fluxo"
		}

		res, err := database.DB.Exec("INSERT INTO crons (site_id, name, expression, command, user) VALUES (?, ?, ?, ?, ?)", 0, req.Name, req.Expression, req.Command, req.User)
		if err != nil {
			http.Error(w, "Failed to insert", http.StatusInternalServerError)
			return
		}

		id, _ := res.LastInsertId()

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{"id": id})
	}
}

func (s *Server) handleListAllCrons() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := database.DB.Query(`
			SELECT c.id, c.site_id, c.name, c.expression, c.command, c.user, c.created_at, COALESCE(s.domain, '')
			FROM crons c LEFT JOIN sites s ON c.site_id = s.id
			ORDER BY c.created_at DESC
		`)
		if err != nil {
			http.Error(w, "DB error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		type CronWithSite struct {
			database.Cron
			SiteDomain string `json:"site_domain"`
		}

		var crons []CronWithSite
		for rows.Next() {
			var c CronWithSite
			rows.Scan(&c.ID, &c.SiteID, &c.Name, &c.Expression, &c.Command, &c.User, &c.CreatedAt, &c.SiteDomain)
			crons = append(crons, c)
		}
		if crons == nil {
			crons = []CronWithSite{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(crons)
	}
}
