package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/backup"
)

type databaseExport struct {
	ID         string    `json:"id"`
	DatabaseID int       `json:"database_id"`
	Status     string    `json:"status"`
	Filename   string    `json:"filename,omitempty"`
	Error      string    `json:"error,omitempty"`
	ExpiresAt  time.Time `json:"expires_at"`
	path       string
	dir        string
}

type databaseExportManager struct {
	sync.Mutex
	jobs map[string]*databaseExport
}

func (s *Server) handleCreateDatabaseExport() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		id, err := pathInt(r, "id")
		if err != nil || id <= 0 {
			http.Error(w, "Invalid database ID", 400)
			return
		}
		if err := s.backupManager.BeginDatabaseExport(id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "Database not found", http.StatusNotFound)
				return
			}
			if errors.Is(err, backup.ErrDatabaseOperationInProgress) {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			http.Error(w, "Failed to prepare database export", http.StatusInternalServerError)
			return
		}
		releaseDatabase := true
		defer func() {
			if releaseDatabase {
				s.backupManager.FinishDatabaseExport(id)
			}
		}()
		var item database.Database
		err = database.DB.QueryRow("SELECT id, name, engine FROM databases WHERE id = ?", id).Scan(&item.ID, &item.Name, &item.Engine)
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Database not found", 404)
			return
		}
		if err != nil {
			http.Error(w, "Failed to load database", 500)
			return
		}
		m := &s.databaseExports
		m.Lock()
		defer m.Unlock()
		if m.jobs == nil {
			m.jobs = make(map[string]*databaseExport)
		}
		for _, job := range m.jobs {
			if job.Status == "preparing" {
				http.Error(w, "Another database export is preparing; try again when it finishes", 409)
				return
			}
		}
		// Bound retained disk usage even when clients never download completed exports.
		if len(m.jobs) >= 4 {
			http.Error(w, "Export limit reached; wait for earlier exports to expire", 429)
			return
		}
		root := filepath.Join(s.dataDir, "database-exports")
		if err := os.MkdirAll(root, 0700); err != nil {
			http.Error(w, "Cannot prepare export storage", 500)
			return
		}
		dir, err := os.MkdirTemp(root, "export-")
		if err != nil {
			http.Error(w, "Cannot prepare export storage", 500)
			return
		}
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			os.RemoveAll(dir)
			http.Error(w, "Cannot create export", 500)
			return
		}
		job := &databaseExport{ID: hex.EncodeToString(token[:]), DatabaseID: id, Status: "preparing", dir: dir, ExpiresAt: time.Now().Add(30 * time.Minute)}
		m.jobs[job.ID] = job
		releaseDatabase = false
		go func() {
			defer s.backupManager.FinishDatabaseExport(id)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			path, dumpErr := backup.ExportDatabase(ctx, item, dir)
			m.Lock()
			defer m.Unlock()
			job.ExpiresAt = time.Now().Add(10 * time.Minute)
			if dumpErr != nil {
				log.Printf("Database export %s failed: %v", job.ID, dumpErr)
				job.Status = "failed"
				job.Error = "Export failed. Check available disk space and database tools, or use Backups for larger databases."
				os.RemoveAll(dir)
				return
			}
			job.Status, job.path, job.Filename = "ready", path, filepath.Base(path)
		}()
		writeJSON(w, http.StatusAccepted, job)
	}
}

func (s *Server) handleDatabaseExport(download bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil || id <= 0 {
			http.Error(w, "Invalid database ID", 400)
			return
		}
		m := &s.databaseExports
		m.Lock()
		job := m.jobs[r.PathValue("export_id")]
		if job == nil || job.DatabaseID != id || time.Now().After(job.ExpiresAt) {
			m.Unlock()
			http.Error(w, "Export expired or not found; prepare a new download", 404)
			return
		}
		snapshot := *job
		if !download {
			m.Unlock()
			writeJSON(w, 200, snapshot)
			return
		}
		if snapshot.Status != "ready" {
			m.Unlock()
			http.Error(w, "Export is not ready", 409)
			return
		}
		file, err := os.Open(snapshot.path)
		m.Unlock()
		if err != nil {
			http.Error(w, "Export file unavailable", 404)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			http.Error(w, "Export file unavailable", 500)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": snapshot.Filename}))
		http.ServeContent(w, r, snapshot.Filename, info.ModTime(), file)
	}
}

func (s *Server) databaseExportCleanupLoop(ctx context.Context) {
	cleanup := func() {
		m := &s.databaseExports
		m.Lock()
		defer m.Unlock()
		for id, job := range m.jobs {
			if job.Status != "preparing" && time.Now().After(job.ExpiresAt) {
				if err := os.RemoveAll(job.dir); err == nil {
					delete(m.jobs, id)
				}
			}
		}
		// Recover temporary files left by a daemon crash, without touching current jobs.
		root := filepath.Join(s.dataDir, "database-exports")
		entries, _ := os.ReadDir(root)
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "export-") {
				continue
			}
			path := filepath.Join(root, entry.Name())
			active := false
			for _, job := range m.jobs {
				if job.dir == path {
					active = true
					break
				}
			}
			info, err := entry.Info()
			if !active && err == nil && time.Since(info.ModTime()) > 45*time.Minute {
				_ = os.RemoveAll(path)
			}
		}
	}
	cleanup()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanup()
		}
	}
}
