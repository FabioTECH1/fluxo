package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/safeinput"
	sitepkg "fluxo/internal/services/site"
	"fluxo/internal/syscmd"
)

type ExecuteCommandRequest struct {
	Command string `json:"command"`
	Stream  bool   `json:"stream"`
}

var commandANSIRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// handleListCommands returns paginated commands executed for a site.
func (s *Server) handleListCommands() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, err := parseCommandPathID(r, "id", "site")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		page := 1
		if p := r.URL.Query().Get("page"); p != "" {
			parsed, err := strconv.Atoi(p)
			if err != nil || parsed <= 0 {
				http.Error(w, "Invalid page", http.StatusBadRequest)
				return
			}
			page = parsed
		}
		const perPage = 10
		offset := (page - 1) * perPage

		total := 0
		if err := database.DB.QueryRow("SELECT COUNT(*) FROM commands WHERE site_id = ?", siteID).Scan(&total); err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		rows, err := database.DB.Query("SELECT id, site_id, command, status, output, created_at FROM commands WHERE site_id = ? ORDER BY id DESC LIMIT ? OFFSET ?", siteID, perPage, offset)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		commands := make([]database.Command, 0)
		for rows.Next() {
			var c database.Command
			if err := rows.Scan(&c.ID, &c.SiteID, &c.Command, &c.Status, &c.Output, &c.CreatedAt); err != nil {
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
			commands = append(commands, c)
		}
		if err := rows.Err(); err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data":     commands,
			"total":    total,
			"page":     page,
			"per_page": perPage,
		})
	}
}

// handleGetCommand returns a single command history entry for a site.
func (s *Server) handleGetCommand() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, err := parseCommandPathID(r, "id", "site")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		commandID, err := parseCommandPathID(r, "command_id", "command")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var command database.Command
		err = database.DB.QueryRow(
			"SELECT id, site_id, command, status, output, created_at FROM commands WHERE id = ? AND site_id = ?",
			commandID, siteID,
		).Scan(&command.ID, &command.SiteID, &command.Command, &command.Status, &command.Output, &command.CreatedAt)
		if err != nil {
			http.Error(w, "Command not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(command)
	}
}

// handleDeleteCommand removes a command history entry for a site.
func (s *Server) handleDeleteCommand() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, err := parseCommandPathID(r, "id", "site")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		commandID, err := parseCommandPathID(r, "command_id", "command")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		result, err := database.DB.Exec("DELETE FROM commands WHERE id = ? AND site_id = ?", commandID, siteID)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if affected, err := result.RowsAffected(); err != nil || affected != 1 {
			http.Error(w, "Command not found", http.StatusNotFound)
			return
		}

		GlobalHub.ClearCommandLog(siteID, int64(commandID))
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleExecuteCommand runs a command in the site directory as the fluxo user.
func (s *Server) handleExecuteCommand() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, err := parseCommandPathID(r, "id", "site")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var req ExecuteCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Command == "" {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		if safeinput.HasControlChars(req.Command) {
			http.Error(w, "Invalid command", http.StatusBadRequest)
			return
		}

		var sitePath, webRoot, appType, deploymentStrategy string
		err = database.DB.QueryRow("SELECT path, web_root, app_type, COALESCE(deployment_strategy, 'standard') FROM sites WHERE id = ?", siteID).Scan(&sitePath, &webRoot, &appType, &deploymentStrategy)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		workingDir := sitepkg.ActiveSitePath(sitePath, deploymentStrategy)

		resolved := req.Command
		if appType == "wordpress" {
			resolvedRoot, err := safeinput.NormalizeWebRoot(sitePath, webRoot)
			if err != nil {
				http.Error(w, "Invalid WordPress web root", http.StatusInternalServerError)
				return
			}
			resolved = appendWPCLIPath(resolved, resolvedRoot)
		} else {
			resolved = resolveArtisanCommand(resolved, siteID)
		}
		parts := strings.Fields(resolved)
		if len(parts) == 0 {
			http.Error(w, "Invalid command", http.StatusBadRequest)
			return
		}

		executable := parts[0]
		args := parts[1:]

		if req.Stream {
			command, err := s.startStreamingCommand(siteID, workingDir, resolved, executable, args)
			if err != nil {
				http.Error(w, "Failed to start command", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(command)
			return
		}

		output, execErr := syscmd.RunAsUserInDir(r.Context(), 2*time.Minute, "fluxo", workingDir, executable, args...)

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

		id, err := res.LastInsertId()
		if err != nil || id <= 0 {
			http.Error(w, "Failed to save command", http.StatusInternalServerError)
			return
		}

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

func (s *Server) startStreamingCommand(siteID int, workingDir, resolved, executable string, args []string) (database.Command, error) {
	result, err := database.DB.Exec(
		"INSERT INTO commands (site_id, command, status, output) VALUES (?, ?, ?, ?)",
		siteID, resolved, "running", "",
	)
	if err != nil {
		return database.Command{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return database.Command{}, fmt.Errorf("get command insert id: %w", err)
	}
	if id <= 0 {
		return database.Command{}, fmt.Errorf("get command insert id: invalid id %d", id)
	}
	commandID := int(id)
	command := database.Command{
		ID:        commandID,
		SiteID:    siteID,
		Command:   resolved,
		Status:    "running",
		Output:    "",
		CreatedAt: time.Now(),
	}

	GlobalHub.ClearCommandLog(siteID, id)
	go s.runStreamingCommand(context.Background(), siteID, id, workingDir, resolved, executable, args)

	return command, nil
}

func (s *Server) runStreamingCommand(ctx context.Context, siteID int, commandID int64, workingDir, resolved, executable string, args []string) {
	writer := &commandStreamWriter{siteID: siteID, commandID: commandID}
	GlobalHub.BroadcastCommandLog(siteID, commandID, fmt.Sprintf("Running command: %s\n\n", resolved))

	_, execErr := syscmd.RunAsUserInDirStreaming(ctx, 2*time.Minute, "fluxo", workingDir, writer, executable, args...)

	status := "success"
	finalOutput := writer.FullLog()
	if execErr != nil {
		status = "failed"
		failureMessage := fmt.Sprintf("\nCommand failed: %v\n", execErr)
		if strings.TrimSpace(finalOutput) == "" {
			finalOutput = execErr.Error()
		} else {
			finalOutput += failureMessage
		}
		GlobalHub.BroadcastCommandLog(siteID, commandID, failureMessage)
	} else {
		GlobalHub.BroadcastCommandLog(siteID, commandID, "\nCommand completed successfully.\n")
	}

	if _, err := database.DB.Exec(
		"UPDATE commands SET status = ?, output = ? WHERE id = ? AND site_id = ?",
		status, finalOutput, commandID, siteID,
	); err != nil {
		GlobalHub.BroadcastCommandLog(siteID, commandID, fmt.Sprintf("\nFailed to save command output: %v\n", err))
	}
	time.AfterFunc(10*time.Minute, func() {
		GlobalHub.ClearCommandLog(siteID, commandID)
	})
}

type commandStreamWriter struct {
	siteID    int
	commandID int64
	mu        sync.Mutex
	fullLog   string
}

func (w *commandStreamWriter) Write(p []byte) (int, error) {
	str := commandANSIRe.ReplaceAllString(string(p), "")
	if str == "" {
		return len(p), nil
	}
	w.mu.Lock()
	w.fullLog += str
	w.mu.Unlock()
	GlobalHub.BroadcastCommandLog(w.siteID, w.commandID, str)
	return len(p), nil
}

func (w *commandStreamWriter) FullLog() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.fullLog
}

func parseCommandPathID(r *http.Request, name, label string) (int, error) {
	id, err := strconv.Atoi(r.PathValue(name))
	if err != nil || id <= 0 {
		return 0, &commandPathIDError{label: label}
	}
	return id, nil
}

type commandPathIDError struct {
	label string
}

func (e *commandPathIDError) Error() string {
	return "Invalid " + e.label + " ID"
}

func appendWPCLIPath(command, webRoot string) string {
	parts := strings.Fields(command)
	if len(parts) == 0 || parts[0] != "wp" {
		return command
	}
	for _, part := range parts[1:] {
		if part == "--path" || strings.HasPrefix(part, "--path=") {
			return command
		}
	}
	return command + " --path=" + webRoot
}
