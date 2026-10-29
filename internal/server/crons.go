package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/safeinput"
	"fluxo/internal/services/cron"
	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/scheduler"
	sitepkg "fluxo/internal/services/site"
	"fluxo/internal/syscmd"
)

type CreateCronRequest struct {
	Name       string `json:"name"`
	Expression string `json:"expression"`
	Command    string `json:"command"`
	User       string `json:"user"`
}

func (s *Server) handleSchedulerStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(scheduler.SchedulerStatus(ctx))
	}
}

func cronConfiguration(ctx context.Context, c database.Cron) string {
	dir := ""
	if c.SiteID > 0 {
		var err error
		dir, err = siteCronWorkingDirectory(c.SiteID)
		if err != nil {
			return "unknown"
		}
	}
	user := c.User
	if user == "" {
		user = "fluxo"
	}
	prefix := ""
	if dir != "" {
		prefix = "cd " + dir + " && "
	}
	legacy := fmt.Sprintf("# Fluxo Cron ID: %d\n%s %s %s%s >> /var/log/fluxo/cron-%d.log 2>&1\n", c.ID, c.Expression, user, prefix, c.Command, c.ID)
	if registered, e := maintenancetask.Registered(ctx, c.ID); e != nil {
		return "unknown"
	} else if registered {
		contract, ok := maintenancetask.Qualify(c.ID, c.SiteID, c.Expression, c.Command, user)
		if !ok {
			contract, ok = maintenancetask.QualifyApplication(c.ID, c.SiteID, dir, c.Expression, c.Command, user)
		}
		if !ok {
			return "modified"
		}
		dbPath, e := cron.PanelDatabasePath()
		if e != nil {
			return "unknown"
		}
		return maintenancetask.ConfigurationState(ctx, contract, dbPath)
	}
	return scheduler.ConfigurationState(ctx, c.ID, legacy)
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

		type configuredCron struct {
			database.Cron
			ConfigurationState string `json:"configuration_state"`
		}
		var crons []configuredCron
		for rows.Next() {
			var c configuredCron
			if err := rows.Scan(&c.ID, &c.SiteID, &c.Name, &c.Expression, &c.Command, &c.User, &c.CreatedAt); err != nil {
				http.Error(w, "DB error", http.StatusInternalServerError)
				return
			}
			crons = append(crons, c)
		}
		if err := rows.Err(); err != nil {
			http.Error(w, "DB error", http.StatusInternalServerError)
			return
		}

		if crons == nil {
			crons = []configuredCron{}
		}

		rows.Close()
		for i := range crons {
			crons[i].ConfigurationState = cronConfiguration(r.Context(), crons[i].Cron)
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

		workingDirectory, err := siteCronWorkingDirectory(siteID)
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

		if err := cron.Create(int(id), workingDirectory, req.Expression, req.Command, req.User, siteID); err != nil {
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

func siteCronWorkingDirectory(siteID int) (string, error) {
	var sitePath, strategy, appType, appDirectory string
	if err := database.DB.QueryRow(`SELECT path, COALESCE(deployment_strategy, 'standard'),
		COALESCE(app_type, 'php'), COALESCE(app_directory, '.') FROM sites WHERE id = ?`, siteID).
		Scan(&sitePath, &strategy, &appType, &appDirectory); err != nil {
		return "", err
	}
	workingDirectory := sitepkg.ActiveSitePath(sitePath, strategy)
	if appType == "python" {
		normalized, err := sitepkg.NormalizeAppDirectory(appDirectory)
		if err != nil {
			return "", err
		}
		workingDirectory = filepath.Join(workingDirectory, normalized)
	}
	return workingDirectory, nil
}

func syncSiteCrons(siteID int) error {
	workingDirectory, err := siteCronWorkingDirectory(siteID)
	if err != nil {
		return err
	}
	rows, err := database.DB.Query("SELECT id, expression, command, user FROM crons WHERE site_id = ?", siteID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type cronRecord struct {
		id                        int
		expression, command, user string
	}
	var records []cronRecord
	for rows.Next() {
		var record cronRecord
		if err := rows.Scan(&record.id, &record.expression, &record.command, &record.user); err != nil {
			return err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, record := range records {
		if err := cron.Create(record.id, workingDirectory, record.expression, record.command, record.user, siteID); err != nil {
			return fmt.Errorf("recreate cron %d: %w", record.id, err)
		}
	}
	return nil
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

		if err := cron.Delete(cronID); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if _, err := database.DB.Exec("DELETE FROM crons WHERE id = ?", cronID); err != nil {
			http.Error(w, "Failed to delete cron record", http.StatusInternalServerError)
			return
		}
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

		var command, cronUser, expression string
		var siteID int
		err := database.DB.QueryRow("SELECT command, user, site_id, expression FROM crons WHERE id = ?", cronID).Scan(&command, &cronUser, &siteID, &expression)
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

		workingDirectory := ""
		if siteID > 0 {
			workingDirectory, err = siteCronWorkingDirectory(siteID)
			if err != nil {
				http.Error(w, "Site working directory is unavailable", http.StatusInternalServerError)
				return
			}
		}
		if qualified, ok := scheduler.Qualify(cronID, siteID, workingDirectory, expression, command, cronUser); ok {
			managed, err := cron.ManagedScheduler(r.Context(), cronID, workingDirectory, expression, command, cronUser)
			if err != nil {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			if managed {
				out, err := scheduler.Dispatch(r.Context(), cronID)
				if err != nil {
					http.Error(w, err.Error(), http.StatusConflict)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"output": out})
				return
			}
			if err = scheduler.InvalidateLegacyManual(r.Context(), qualified.Version); err != nil {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
		}

		if c, ok := maintenancetask.QualifySite(cronID, siteID, "", "", "", "", expression, command, cronUser); ok && siteID == 0 {
			dbPath, e := cron.PanelDatabasePath()
			if e != nil {
				http.Error(w, e.Error(), http.StatusInternalServerError)
				return
			}
			out, e := maintenancetask.RunNow(r.Context(), c, dbPath)
			if e != nil {
				http.Error(w, e.Error(), http.StatusConflict)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"output": out})
			return
		}
		if c, ok := maintenancetask.QualifyApplication(cronID, siteID, workingDirectory, expression, command, cronUser); ok {
			dbPath, e := cron.PanelDatabasePath()
			if e != nil {
				http.Error(w, e.Error(), http.StatusInternalServerError)
				return
			}
			out, e := maintenancetask.RunNow(r.Context(), c, dbPath)
			if e != nil {
				http.Error(w, e.Error(), http.StatusConflict)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"output": out})
			return
		}
		if managed, e := maintenancetask.Registered(r.Context(), cronID); e != nil || managed {
			http.Error(w, "Guarded scheduled-job registration needs reconciliation", http.StatusConflict)
			return
		}

		var out string
		if workingDirectory != "" {
			out, err = syscmd.RunAsUserInDir(r.Context(), 5*time.Minute, cronUser, workingDirectory, executable, args...)
		} else {
			out, err = syscmd.RunAsUser(r.Context(), 5*time.Minute, cronUser, executable, args...)
		}
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
			SiteDomain         string `json:"site_domain"`
			ConfigurationState string `json:"configuration_state"`
		}

		var crons []CronWithSite
		for rows.Next() {
			var c CronWithSite
			if err := rows.Scan(&c.ID, &c.SiteID, &c.Name, &c.Expression, &c.Command, &c.User, &c.CreatedAt, &c.SiteDomain); err != nil {
				http.Error(w, "DB error", http.StatusInternalServerError)
				return
			}
			crons = append(crons, c)
		}
		if err := rows.Err(); err != nil {
			http.Error(w, "DB error", http.StatusInternalServerError)
			return
		}
		if crons == nil {
			crons = []CronWithSite{}
		}
		// Release the SQLite cursor before querying each site's working path.
		rows.Close()
		for i := range crons {
			crons[i].ConfigurationState = cronConfiguration(r.Context(), crons[i].Cron)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(crons)
	}
}
