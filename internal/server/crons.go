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
	"fluxo/internal/safeinput"
	"fluxo/internal/services/cron"
	sitepkg "fluxo/internal/services/site"
	"fluxo/internal/syscmd"
)

type CreateCronRequest struct {
	Name       string `json:"name"`
	Expression string `json:"expression"`
	Command    string `json:"command"`
	User       string `json:"user"`
}

// handleListCrons returns all cron jobs for a site.
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

// handleCreateCron creates a cron job for a site and registers it with the system cron.
func (s *Server) handleCreateCron() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var req CreateCronRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}
		if !safeinput.ValidateCronExpression(req.Expression) || safeinput.HasControlChars(req.Command) || safeinput.HasControlChars(req.Name) {
			http.Error(w, "Invalid cron fields", http.StatusBadRequest)
			return
		}

		var sitePath, deploymentStrategy string
		err := database.DB.QueryRow("SELECT path, deployment_strategy FROM sites WHERE id = ?", siteID).Scan(&sitePath, &deploymentStrategy)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		if req.User == "" {
			req.User = "fluxo"
		}
		if !safeinput.ValidateCronUser(req.User, false) {
			http.Error(w, "Invalid cron user", http.StatusBadRequest)
			return
		}
		req.Command = resolveArtisanCronCommand(req.Command, siteID)
		res, err := database.DB.Exec("INSERT INTO crons (site_id, name, expression, command, user) VALUES (?, ?, ?, ?, ?)", siteID, req.Name, req.Expression, req.Command, req.User)
		if err != nil {
			http.Error(w, "Failed to insert", http.StatusInternalServerError)
			return
		}

		id, _ := res.LastInsertId()

		if err := cron.Create(int(id), sitepkg.ActiveSitePath(sitePath, deploymentStrategy), req.Expression, req.Command, req.User); err != nil {
			database.DB.Exec("DELETE FROM crons WHERE id = ?", id)
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

// handleDeleteCron removes a cron job from the system and database.
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

// handleCreateGlobalCron creates a cron job not tied to any site.
func (s *Server) handleCreateGlobalCron() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateCronRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}
		if !safeinput.ValidateCronExpression(req.Expression) || safeinput.HasControlChars(req.Command) || safeinput.HasControlChars(req.Name) {
			http.Error(w, "Invalid cron fields", http.StatusBadRequest)
			return
		}
		if req.User == "" {
			req.User = "fluxo"
		}
		if !safeinput.ValidateCronUser(req.User, true) {
			http.Error(w, "Invalid cron user", http.StatusBadRequest)
			return
		}

		res, err := database.DB.Exec("INSERT INTO crons (site_id, name, expression, command, user) VALUES (?, ?, ?, ?, ?)", 0, req.Name, req.Expression, req.Command, req.User)
		if err != nil {
			http.Error(w, "Failed to insert", http.StatusInternalServerError)
			return
		}

		id, _ := res.LastInsertId()

		if err := cron.Create(int(id), "", req.Expression, req.Command, req.User); err != nil {
			database.DB.Exec("DELETE FROM crons WHERE id = ?", id)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{"id": id})
	}
}

// handleRunCron manually executes a cron job's command.
func (s *Server) handleRunCron() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cronID, _ := strconv.Atoi(r.PathValue("cron_id"))

		var command, cronUser string
		err := database.DB.QueryRow("SELECT command, user FROM crons WHERE id = ?", cronID).Scan(&command, &cronUser)
		if err != nil {
			http.Error(w, "Cron not found", http.StatusNotFound)
			return
		}

		if cronUser == "" {
			cronUser = "fluxo"
		}
		if !safeinput.ValidateCronUser(cronUser, true) {
			http.Error(w, "Invalid cron user", http.StatusBadRequest)
			return
		}

		var executable string
		var args []string

		// Commands with shell operators (&&, ||, |, ;) need sh -c.
		if strings.ContainsAny(command, "&|;") {
			executable = "sh"
			args = []string{"-c", command}
		} else {
			parts := strings.Fields(command)
			if len(parts) == 0 {
				http.Error(w, "Invalid command", http.StatusBadRequest)
				return
			}
			executable = parts[0]
			args = parts[1:]
		}

		out, err := syscmd.RunAsUser(r.Context(), 5*time.Minute, cronUser, executable, args...)
		if err != nil {
			http.Error(w, "Command failed: "+err.Error()+out, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"output": out})
	}
}

// handleGetCronLogs returns the last 100 lines of a cron job's log file.
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

// handleListAllCrons returns all cron jobs across all sites.
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
