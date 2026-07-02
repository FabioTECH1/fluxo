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
	"fluxo/internal/services/daemon"
	"fluxo/internal/syscmd"
)

type CreateDaemonRequest struct {
	Name       string `json:"name"`
	Command    string `json:"command"`
	Directory  string `json:"directory"`
	User       string `json:"user"`
	Instances  int    `json:"instances"`
	StartSec   int    `json:"start_seconds"`
	StopSec    int    `json:"stop_seconds"`
	StopSignal string `json:"stop_signal"`
}

// handleListDaemons returns all daemons for a site with live status.
func (s *Server) handleListDaemons() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		rows, err := database.DB.Query("SELECT id, site_id, name, command, directory, user, instances, status, start_seconds, stop_seconds, stop_signal FROM daemons WHERE site_id = ?", siteID)
		if err != nil {
			http.Error(w, "DB error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var daemons = make([]database.Daemon, 0)
		for rows.Next() {
			var d database.Daemon
			rows.Scan(&d.ID, &d.SiteID, &d.Name, &d.Command, &d.Directory, &d.User, &d.Instances, &d.Status, &d.StartSeconds, &d.StopSeconds, &d.StopSignal)

			// Refresh status from systemd
			d.Status = strings.TrimSpace(daemon.Status(r.Context(), d.ID))

			daemons = append(daemons, d)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(daemons)
	}
}

// createDaemonCommon inserts a daemon record into the database.
func createDaemonCommon(siteID int, req CreateDaemonRequest) (int64, error) {
	instances := req.Instances
	if instances <= 0 {
		instances = 1
	}
	if req.StopSignal == "" {
		req.StopSignal = "SIGTERM"
	}
	if !safeinput.ValidateCronUser(req.User, false) {
		return 0, fmt.Errorf("invalid daemon user")
	}
	if safeinput.HasControlChars(req.Command) || safeinput.HasControlChars(req.Directory) || safeinput.HasControlChars(req.StopSignal) {
		return 0, fmt.Errorf("invalid daemon fields")
	}
	res, err := database.DB.Exec(
		"INSERT INTO daemons (site_id, name, command, directory, user, instances, start_seconds, stop_seconds, stop_signal) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		siteID, req.Name, req.Command, req.Directory, req.User, instances, req.StartSec, req.StopSec, req.StopSignal,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// handleCreateDaemon creates and starts a systemd daemon for a site.
func (s *Server) handleCreateDaemon() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var req CreateDaemonRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}
		if req.User == "" {
			req.User = "fluxo"
		}

		req.Command = resolveArtisanCommand(req.Command, siteID)

		id, err := createDaemonCommon(siteID, req)
		if err != nil {
			http.Error(w, "Failed to insert", http.StatusInternalServerError)
			return
		}

		if err := daemon.GenerateServiceFile(int(id), req.Command, req.Directory, req.User, req.StartSec, req.StopSec, req.StopSignal); err != nil {
			database.DB.Exec("DELETE FROM daemons WHERE id = ?", id)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := daemon.EnableAndStart(r.Context(), int(id)); err != nil {
			database.DB.Exec("DELETE FROM daemons WHERE id = ?", id)
			http.Error(w, "Failed to start daemon: "+err.Error(), http.StatusInternalServerError)
			return
		}

		database.DB.Exec("UPDATE daemons SET status = 'active' WHERE id = ?", id)

		label := req.Name
		if label == "" {
			label = req.Command
		}
		LogActivity(siteID, "daemon_created", "Daemon \""+label+"\" was created")

		w.WriteHeader(http.StatusCreated)
	}
}

// handleDeleteDaemon stops and removes a daemon.
func (s *Server) handleDeleteDaemon() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		daemonID, _ := strconv.Atoi(r.PathValue("daemon_id"))

		var name, command string
		database.DB.QueryRow("SELECT COALESCE(name,''), command FROM daemons WHERE id = ?", daemonID).Scan(&name, &command)
		label := name
		if label == "" {
			label = command
		}

		daemon.Delete(r.Context(), daemonID)
		database.DB.Exec("DELETE FROM daemons WHERE id = ?", daemonID)

		LogActivity(0, "daemon_deleted", "Daemon \""+label+"\" was deleted")
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleRestartDaemon restarts a daemon via systemctl.
func (s *Server) handleRestartDaemon() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		daemonID, _ := strconv.Atoi(r.PathValue("daemon_id"))

		daemon.Restart(r.Context(), daemonID)

		w.WriteHeader(http.StatusNoContent)
	}
}

// handleStartDaemon starts a daemon via systemctl.
func (s *Server) handleStartDaemon() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		daemonID, _ := strconv.Atoi(r.PathValue("daemon_id"))

		if err := daemon.Start(r.Context(), daemonID); err != nil {
			http.Error(w, "Failed to start daemon: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// handleStopDaemon stops a daemon via systemctl.
func (s *Server) handleStopDaemon() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		daemonID, _ := strconv.Atoi(r.PathValue("daemon_id"))

		if err := daemon.Stop(r.Context(), daemonID); err != nil {
			http.Error(w, "Failed to stop daemon: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// handleGetDaemonLogs returns the last 100 lines of a daemon's log file.
func (s *Server) handleGetDaemonLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		daemonID, _ := strconv.Atoi(r.PathValue("daemon_id"))

		logPath := fmt.Sprintf("/var/log/fluxo/fluxo-daemon-%d.log", daemonID)

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

// handleCreateGlobalDaemon creates a standalone daemon not tied to a site.
func (s *Server) handleCreateGlobalDaemon() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateDaemonRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}
		if req.User == "" {
			req.User = "fluxo"
		}

		// Create with site_id = 0 (standalone, no site)
		id, err := createDaemonCommon(0, req)
		if err != nil {
			http.Error(w, "Failed to insert", http.StatusInternalServerError)
			return
		}

		if err := daemon.GenerateServiceFile(int(id), req.Command, req.Directory, req.User, req.StartSec, req.StopSec, req.StopSignal); err != nil {
			database.DB.Exec("DELETE FROM daemons WHERE id = ?", id)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := daemon.EnableAndStart(r.Context(), int(id)); err != nil {
			database.DB.Exec("DELETE FROM daemons WHERE id = ?", id)
			http.Error(w, "Failed to start daemon: "+err.Error(), http.StatusInternalServerError)
			return
		}

		database.DB.Exec("UPDATE daemons SET status = 'active' WHERE id = ?", id)

		var d database.Daemon
		database.DB.QueryRow("SELECT id, site_id, name, command, directory, user, instances, status, start_seconds, stop_seconds, stop_signal, created_at, updated_at FROM daemons WHERE id = ?", id).Scan(
			&d.ID, &d.SiteID, &d.Name, &d.Command, &d.Directory, &d.User, &d.Instances, &d.Status, &d.StartSeconds, &d.StopSeconds, &d.StopSignal, &d.CreatedAt, &d.UpdatedAt,
		)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(d)
	}
}

// handleListAllDaemons returns all daemons across all sites.
func (s *Server) handleListAllDaemons() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := database.DB.Query(`
			SELECT d.id, d.site_id, d.name, d.command, d.directory, d.user, d.instances, d.status, d.start_seconds, d.stop_seconds, d.stop_signal, COALESCE(s.domain, '')
			FROM daemons d LEFT JOIN sites s ON d.site_id = s.id
		`)
		if err != nil {
			http.Error(w, "DB error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		type DaemonWithSite struct {
			database.Daemon
			SiteDomain string `json:"site_domain"`
		}

		var daemons []DaemonWithSite
		for rows.Next() {
			var d DaemonWithSite
			rows.Scan(&d.ID, &d.SiteID, &d.Name, &d.Command, &d.Directory, &d.User, &d.Instances, &d.Status, &d.StartSeconds, &d.StopSeconds, &d.StopSignal, &d.SiteDomain)
			d.Status = daemon.Status(r.Context(), d.ID)
			daemons = append(daemons, d)
		}
		if daemons == nil {
			daemons = []DaemonWithSite{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(daemons)
	}
}
