package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/safeinput"
	"fluxo/internal/syscmd"
)

type ExecuteCommandRequest struct {
	Command string `json:"command"`
}

// handleListCommands returns the last 50 commands executed for a site.
func (s *Server) handleListCommands() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		rows, err := database.DB.Query("SELECT id, site_id, command, status, output, created_at FROM commands WHERE site_id = ? ORDER BY id DESC LIMIT 50", siteID)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		commands := make([]database.Command, 0)
		for rows.Next() {
			var c database.Command
			if err := rows.Scan(&c.ID, &c.SiteID, &c.Command, &c.Status, &c.Output, &c.CreatedAt); err != nil {
				continue
			}
			commands = append(commands, c)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(commands)
	}
}

// handleExecuteCommand runs a command in the site directory as the fluxo user.
func (s *Server) handleExecuteCommand() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var req ExecuteCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Command == "" {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		if safeinput.HasControlChars(req.Command) {
			http.Error(w, "Invalid command", http.StatusBadRequest)
			return
		}

		var sitePath string
		err := database.DB.QueryRow("SELECT path FROM sites WHERE id = ?", siteID).Scan(&sitePath)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		resolved := resolveArtisanCommand(req.Command, siteID)
		parts := strings.Fields(resolved)
		if len(parts) == 0 {
			http.Error(w, "Invalid command", http.StatusBadRequest)
			return
		}

		executable := parts[0]
		args := parts[1:]

		output, execErr := syscmd.RunAsUserInDir(r.Context(), 2*time.Minute, "fluxo", sitePath, executable, args...)

		status := "success"
		finalOutput := output
		if execErr != nil {
			status = "failed"
			finalOutput = execErr.Error()
		}

		res, err := database.DB.Exec(
			"INSERT INTO commands (site_id, command, status, output) VALUES (?, ?, ?, ?)",
			siteID, resolved, status, finalOutput,
		)
		if err != nil {
			http.Error(w, "Failed to save command", http.StatusInternalServerError)
			return
		}

		id, _ := res.LastInsertId()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(database.Command{
			ID:        int(id),
			SiteID:    siteID,
			Command:   resolved,
			Status:    status,
			Output:    finalOutput,
			CreatedAt: time.Now(),
		})
	}
}
