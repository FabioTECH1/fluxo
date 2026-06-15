package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/cron"
	"fluxo/internal/syscmd"
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
		req.Command = resolveArtisanCronCommand(req.Command, siteID)
		res, err := database.DB.Exec("INSERT INTO crons (site_id, name, expression, command, user) VALUES (?, ?, ?, ?, ?)", siteID, req.Name, req.Expression, req.Command, req.User)
		if err != nil {
			http.Error(w, "Failed to insert", http.StatusInternalServerError)
			return
		}

		id, _ := res.LastInsertId()

		if err := cron.Create(int(id), domain, req.Expression, req.Command, req.User); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		label := req.Name
		if label == "" {
			label = req.Command
		}
		LogActivity(siteID, "cron_created", "Cron \""+label+"\" was created")

		w.WriteHeader(http.StatusCreated)
	}
}

func (s *Server) handleDeleteCron() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cronID, _ := strconv.Atoi(r.PathValue("cron_id"))

		var name, command string
		database.DB.QueryRow("SELECT COALESCE(name,''), command FROM crons WHERE id = ?", cronID).Scan(&name, &command)
		label := name
		if label == "" {
			label = command
		}

		cron.Delete(cronID)
		database.DB.Exec("DELETE FROM crons WHERE id = ?", cronID)
		LogActivity(0, "cron_deleted", "Cron \""+label+"\" was deleted")

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

		if err := cron.Create(int(id), "", req.Expression, req.Command, req.User); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{"id": id})
	}
}

func (s *Server) handleRunCron() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cronID, _ := strconv.Atoi(r.PathValue("cron_id"))

		var command, cronUser string
		err := database.DB.QueryRow("SELECT command, user FROM crons WHERE id = ?", cronID).Scan(&command, &cronUser)
		if err != nil {
			http.Error(w, "Cron not found", http.StatusNotFound)
			return
		}

		parts := strings.Fields(command)
		if len(parts) == 0 {
			http.Error(w, "Invalid command", http.StatusBadRequest)
			return
		}

		if cronUser == "" {
			cronUser = "fluxo"
		}

		executable := parts[0]
		args := parts[1:]
		out, err := syscmd.RunAsUser(r.Context(), 5*time.Minute, cronUser, executable, args...)
		if err != nil {
			http.Error(w, "Command failed: "+err.Error()+out, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"output": out})
	}
}

func (s *Server) handleGetCronLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cronID, _ := strconv.Atoi(r.PathValue("cron_id"))

		logPath := fmt.Sprintf("/var/log/fluxo/cron-%d.log", cronID)

		if _, err := os.Stat(logPath); os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"lines": []string{},
				"total": 0,
			})
			return
		}

		out, err := syscmd.Run(r.Context(), 5*time.Second, "tail", "-n", "100", logPath)
		if err != nil {
			out = ""
		}

		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) == 1 && lines[0] == "" {
			lines = []string{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"lines": lines,
			"total": len(lines),
		})
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
