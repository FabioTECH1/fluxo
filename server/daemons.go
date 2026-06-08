package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"fluxo/database"
	"fluxo/services/daemon"
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

func (s *Server) handleListDaemons() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		rows, err := database.DB.Query("SELECT id, site_id, name, command, directory, user, instances, status, start_seconds, stop_seconds, stop_signal FROM daemons WHERE site_id = ?", siteID)
		if err != nil {
			http.Error(w, "DB error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var daemons []database.Daemon
		for rows.Next() {
			var d database.Daemon
			rows.Scan(&d.ID, &d.SiteID, &d.Name, &d.Command, &d.Directory, &d.User, &d.Instances, &d.Status, &d.StartSeconds, &d.StopSeconds, &d.StopSignal)

			// Refresh status
			d.Status = strings.TrimSpace(daemon.Status(r.Context(), d.ID))

			daemons = append(daemons, d)
		}

		// Update statuses in db implicitly if desired, but returning live is better
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(daemons)
	}
}

func createDaemonCommon(siteID int, req CreateDaemonRequest) (int64, error) {
	instances := req.Instances
	if instances <= 0 {
		instances = 1
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

func (s *Server) handleCreateDaemon() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var req CreateDaemonRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		id, err := createDaemonCommon(siteID, req)
		if err != nil {
			http.Error(w, "Failed to insert", http.StatusInternalServerError)
			return
		}

		daemon.GenerateServiceFile(int(id), req.Command, req.Directory, req.User, req.StartSec, req.StopSec, req.StopSignal)
		daemon.EnableAndStart(r.Context(), int(id))

		database.DB.Exec("UPDATE daemons SET status = 'active' WHERE id = ?", id)

		w.WriteHeader(http.StatusCreated)
	}
}

func (s *Server) handleDeleteDaemon() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		daemonID, _ := strconv.Atoi(r.PathValue("daemon_id"))

		daemon.Delete(r.Context(), daemonID)
		database.DB.Exec("DELETE FROM daemons WHERE id = ?", daemonID)

		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleRestartDaemon() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		daemonID, _ := strconv.Atoi(r.PathValue("daemon_id"))

		daemon.Restart(r.Context(), daemonID)

		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleCreateGlobalDaemon() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateDaemonRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		// Create with site_id = 0 (standalone, no site)
		id, err := createDaemonCommon(0, req)
		if err != nil {
			http.Error(w, "Failed to insert", http.StatusInternalServerError)
			return
		}

		daemon.GenerateServiceFile(int(id), req.Command, req.Directory, req.User, req.StartSec, req.StopSec, req.StopSignal)
		daemon.EnableAndStart(r.Context(), int(id))

		database.DB.Exec("UPDATE daemons SET status = 'active' WHERE id = ?", id)

		var d database.Daemon
		database.DB.QueryRow("SELECT id, site_id, name, command, directory, user, instances, status, start_seconds, stop_seconds, stop_signal, created_at, updated_at FROM daemons WHERE id = ?", id).Scan(
			&d.ID, &d.SiteID, &d.Name, &d.Command, &d.Directory, &d.User, &d.Instances, &d.Status, &d.StartSeconds, &d.StopSeconds, &d.StopSignal, &d.CreatedAt, &d.UpdatedAt,
		)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(d)
	}
}

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
