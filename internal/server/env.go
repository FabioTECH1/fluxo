package server

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"fluxo/database"
	"fluxo/syscmd"
)

type EnvRequest struct {
	Content string `json:"content"`
}

func (s *Server) handleGetEnv() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var domain string
		err := database.DB.QueryRow("SELECT domain FROM sites WHERE id = ?", siteID).Scan(&domain)
		if err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}

		envPath := filepath.Join("/home/fluxo", domain, ".env")
		content, err := os.ReadFile(envPath)
		if err != nil {
			if os.IsNotExist(err) {
				content = []byte("") // Return empty if not exists
			} else {
				http.Error(w, "Failed to read .env", http.StatusInternalServerError)
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"content": string(content)})
	}
}

func (s *Server) handleUpdateEnv() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))

		var req EnvRequest
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

		envPath := filepath.Join("/home/fluxo", domain, ".env")

		// Atomic write
		tmpFile, err := os.CreateTemp(filepath.Join("/home/fluxo", domain), ".env.tmp.*")
		if err != nil {
			http.Error(w, "Failed to create temp file", http.StatusInternalServerError)
			return
		}

		tmpName := tmpFile.Name()
		defer os.Remove(tmpName) // Clean up if rename fails

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

		// Backup existing
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

		w.WriteHeader(http.StatusNoContent)
	}
}
