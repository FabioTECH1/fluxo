package server

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"fluxo/internal/database"
	sitepkg "fluxo/internal/services/site"
	"fluxo/internal/syscmd"
)

type EnvRequest struct {
	Content              string `json:"content"`
	CacheConfigAfterSave *bool  `json:"cache_config_after_save,omitempty"`
}

// handleGetEnv reads the .env file for a site.
func (s *Server) handleGetEnv() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var sitePath string
		var cacheConfigAfterSave bool
		err := database.DB.QueryRow("SELECT path, cache_config_after_env_save FROM sites WHERE id = ?", siteID).Scan(&sitePath, &cacheConfigAfterSave)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		envPath := filepath.Join(sitePath, ".env")
		content, err := os.ReadFile(envPath)
		if err != nil {
			if os.IsNotExist(err) {
				content = []byte("")
			} else {
				http.Error(w, "Failed to read .env", http.StatusInternalServerError)
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"content": string(content), "cache_config_after_save": cacheConfigAfterSave})
	}
}

// handleUpdateEnv writes the .env file atomically with backup and ownership.
func (s *Server) handleUpdateEnv() http.HandlerFunc {
	return s.handleUpdateEnvWithCacheRunner(syscmd.RunAsUserInDir)
}

func (s *Server) handleUpdateEnvWithCacheRunner(run func(context.Context, time.Duration, string, string, string, ...string) (string, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var req EnvRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		var sitePath, appType, phpVersion, strategy string
		var cacheAfterSave bool
		err := database.DB.QueryRow("SELECT path, COALESCE(app_type, 'php'), COALESCE(php_version, '8.4'), COALESCE(deployment_strategy, 'standard'), cache_config_after_env_save FROM sites WHERE id = ?", siteID).Scan(&sitePath, &appType, &phpVersion, &strategy, &cacheAfterSave)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		if req.CacheConfigAfterSave != nil && *req.CacheConfigAfterSave && appType != "laravel" {
			http.Error(w, "Configuration caching is only supported for Laravel sites", http.StatusBadRequest)
			return
		}
		if req.CacheConfigAfterSave != nil {
			cacheAfterSave = *req.CacheConfigAfterSave
		}
		envPath := filepath.Join(sitePath, ".env")

		// Atomic write via temp file
		tmpFile, err := os.CreateTemp(sitePath, ".env.tmp.*")
		if err != nil {
			http.Error(w, "Failed to create temp file", http.StatusInternalServerError)
			return
		}

		tmpName := tmpFile.Name()
		defer os.Remove(tmpName)

		if _, err := io.WriteString(tmpFile, req.Content); err != nil {
			tmpFile.Close()
			http.Error(w, "Failed to write env", http.StatusInternalServerError)
			return
		}
		tmpFile.Close()

		if err := os.Chmod(tmpName, 0640); err != nil {
			http.Error(w, "Failed to chmod", http.StatusInternalServerError)
			return
		}

		// Backup existing .env before overwrite
		if _, err := os.Stat(envPath); err == nil {
			os.Rename(envPath, envPath+".bak")
		}

		if err := os.Rename(tmpName, envPath); err != nil {
			http.Error(w, "Failed to save .env atomically", http.StatusInternalServerError)
			return
		}

		// Set ownership of .env to fluxo:www-data
		ctx := r.Context()
		if _, err := syscmd.Run(ctx, 5*time.Second, "chown", "fluxo:www-data", envPath); err != nil {
			log.Printf("Warning: failed to chown env file: %v", err)
		}

		if req.CacheConfigAfterSave != nil {
			if _, err := database.DB.Exec("UPDATE sites SET cache_config_after_env_save = ? WHERE id = ?", *req.CacheConfigAfterSave, siteID); err != nil {
				http.Error(w, "Environment saved, but the configuration-cache preference could not be saved. Please retry.", http.StatusInternalServerError)
				return
			}
		}
		response := map[string]string{"status": "saved", "cache_status": "skipped"}
		if cacheAfterSave && appType == "laravel" {
			if phpVersion == "" {
				phpVersion = "8.4"
			}
			// An automatic save action, not a user-issued terminal command: no
			// command-history row is created. Report the result in this response.
			_, err := run(r.Context(), 2*time.Minute, "fluxo", sitepkg.ActiveSitePath(sitePath, strategy), "php"+phpVersion, "artisan", "config:cache")
			response["cache_status"] = "success"
			if err != nil {
				response["cache_status"] = "failed"
				response["cache_error"] = "The environment was saved, but configuration caching failed. Check the application's configuration and PHP dependencies, then save again to retry."
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}
}
