package server

import (
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
	"fluxo/internal/syscmd"
)

type wordpressConfigRequest struct {
	Content string `json:"content"`
}

func wordpressConfigDetails(siteID int) (string, string, error) {
	var sitePath, webRoot, appType, phpVersion string
	if err := database.DB.QueryRow("SELECT path, web_root, app_type, php_version FROM sites WHERE id = ?", siteID).Scan(&sitePath, &webRoot, &appType, &phpVersion); err != nil {
		return "", "", err
	}
	if appType != "wordpress" {
		return "", "", fmt.Errorf("WordPress configuration is only available for WordPress sites")
	}
	resolvedRoot, err := safeinput.NormalizeWebRoot(sitePath, webRoot)
	if err != nil {
		return "", "", err
	}
	if !safeinput.ValidatePHPVersion(phpVersion) {
		return "", "", fmt.Errorf("invalid PHP version")
	}
	return filepath.Join(resolvedRoot, "wp-config.php"), phpVersion, nil
}

func (s *Server) handleGetWordPressConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))
		configPath, _, err := wordpressConfigDetails(siteID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		content, err := os.ReadFile(configPath)
		if err != nil && !os.IsNotExist(err) {
			http.Error(w, "Failed to read WordPress configuration", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"content": string(content)})
	}
}

func (s *Server) handleUpdateWordPressConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))
		configPath, phpVersion, err := wordpressConfigDetails(siteID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, (1<<20)+(64<<10))
		var req wordpressConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		if len(req.Content) > 1<<20 || !strings.HasPrefix(strings.TrimSpace(req.Content), "<?php") {
			http.Error(w, "WordPress configuration must be a PHP file smaller than 1 MB", http.StatusBadRequest)
			return
		}

		tmp, err := os.CreateTemp(filepath.Dir(configPath), ".wp-config-*.tmp")
		if err != nil {
			http.Error(w, "Failed to prepare WordPress configuration", http.StatusInternalServerError)
			return
		}
		tmpPath := tmp.Name()
		defer os.Remove(tmpPath)
		if _, err := tmp.WriteString(req.Content); err != nil {
			tmp.Close()
			http.Error(w, "Failed to write WordPress configuration", http.StatusInternalServerError)
			return
		}
		if err := tmp.Sync(); err != nil {
			tmp.Close()
			http.Error(w, "Failed to sync WordPress configuration", http.StatusInternalServerError)
			return
		}
		if err := tmp.Close(); err != nil {
			http.Error(w, "Failed to close WordPress configuration", http.StatusInternalServerError)
			return
		}
		if err := os.Chmod(tmpPath, 0640); err != nil {
			http.Error(w, "Failed to secure WordPress configuration", http.StatusInternalServerError)
			return
		}
		lintOutput, lintErr := syscmd.Run(r.Context(), 10*time.Second, "php"+phpVersion, "-l", tmpPath)
		if lintErr != nil {
			http.Error(w, "WordPress configuration has invalid PHP syntax: "+strings.TrimSpace(lintOutput), http.StatusUnprocessableEntity)
			return
		}
		if _, err := syscmd.Run(r.Context(), 5*time.Second, "chown", "fluxo:www-data", tmpPath); err != nil {
			http.Error(w, "Failed to secure WordPress configuration ownership", http.StatusInternalServerError)
			return
		}
		if err := os.Rename(tmpPath, configPath); err != nil {
			http.Error(w, "Failed to activate WordPress configuration", http.StatusInternalServerError)
			return
		}

		LogActivity(siteID, "settings", "WordPress configuration was updated")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}
}
