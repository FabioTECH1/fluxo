package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"fluxo/internal/database"
	"fluxo/internal/services/filemanager"
)

const (
	fileTextRequestLimit   = 8 << 20
	fileUploadRequestLimit = filemanager.MaxUploadBytes
)

type siteFileContext struct {
	ID                 int
	Domain             string
	DeploymentStrategy string
	Manager            *filemanager.Manager
}

func loadSiteFileContext(idValue string) (siteFileContext, error) {
	id, err := strconv.Atoi(idValue)
	if err != nil || id <= 0 {
		return siteFileContext{}, filemanager.ErrInvalidPath
	}
	var domain, sitePath, strategy string
	err = database.DB.QueryRow(`SELECT domain, path, COALESCE(deployment_strategy, 'standard') FROM sites WHERE id = ?`, id).
		Scan(&domain, &sitePath, &strategy)
	if err != nil {
		return siteFileContext{}, err
	}

	cleanRoot := filepath.Clean(sitePath)
	relative, err := filepath.Rel("/home/fluxo", cleanRoot)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return siteFileContext{}, fmt.Errorf("site root is outside /home/fluxo")
	}
	manager, err := filemanager.New(cleanRoot)
	if err != nil {
		return siteFileContext{}, err
	}
	return siteFileContext{ID: id, Domain: domain, DeploymentStrategy: strategy, Manager: manager}, nil
}

func handleFileManagerError(w http.ResponseWriter, err error) {
	var maxBytesError *http.MaxBytesError
	switch {
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, filemanager.ErrNotFound):
		http.Error(w, "File, directory, or site not found", http.StatusNotFound)
	case errors.Is(err, filemanager.ErrInvalidPath):
		http.Error(w, "Invalid site-relative path", http.StatusBadRequest)
	case errors.Is(err, filemanager.ErrUnsafePath):
		http.Error(w, "That symlink cannot be followed outside the site", http.StatusForbidden)
	case errors.Is(err, filemanager.ErrConflict):
		http.Error(w, "The file changed or the destination already exists", http.StatusConflict)
	case errors.Is(err, filemanager.ErrTooLarge), errors.As(err, &maxBytesError):
		http.Error(w, "File exceeds the allowed size", http.StatusRequestEntityTooLarge)
	case errors.Is(err, filemanager.ErrNotText):
		http.Error(w, "Only UTF-8 text files can be edited", http.StatusUnsupportedMediaType)
	case errors.Is(err, filemanager.ErrTooManyEntries):
		http.Error(w, "Directory has too many entries to display", http.StatusUnprocessableEntity)
	case errors.Is(err, filemanager.ErrDirectoryNotEmpty):
		http.Error(w, "Directory must be empty before it can be deleted", http.StatusConflict)
	case errors.Is(err, filemanager.ErrNotRegular):
		http.Error(w, "Path is not a regular file", http.StatusUnprocessableEntity)
	case errors.Is(err, filemanager.ErrNotDirectory):
		http.Error(w, "Path is not a directory", http.StatusUnprocessableEntity)
	default:
		log.Printf("file manager error: %v", err)
		http.Error(w, "File operation failed", http.StatusInternalServerError)
	}
}

func decodeFileJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return false
	}
	return true
}

func (s *Server) handleListSiteFiles() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, err := loadSiteFileContext(r.PathValue("id"))
		if err != nil {
			handleFileManagerError(w, err)
			return
		}
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		listing, err := ctx.Manager.List(r.URL.Query().Get("path"), r.URL.Query().Get("hidden") == "true", offset, limit)
		if err != nil {
			handleFileManagerError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			filemanager.Listing
			SiteDomain         string `json:"site_domain"`
			DeploymentStrategy string `json:"deployment_strategy"`
		}{Listing: listing, SiteDomain: ctx.Domain, DeploymentStrategy: ctx.DeploymentStrategy})
	}
}

func (s *Server) handleReadSiteFile() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, err := loadSiteFileContext(r.PathValue("id"))
		if err != nil {
			handleFileManagerError(w, err)
			return
		}
		file, err := ctx.Manager.ReadText(r.URL.Query().Get("path"))
		if err != nil {
			handleFileManagerError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(file)
	}
}

func (s *Server) handleWriteSiteFile() http.HandlerFunc {
	type request struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		SHA256  string `json:"sha256"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var req request
		if !decodeFileJSON(w, r, &req) {
			return
		}
		ctx, err := loadSiteFileContext(r.PathValue("id"))
		if err != nil {
			handleFileManagerError(w, err)
			return
		}
		if err := ctx.Manager.WriteText(req.Path, req.Content, req.SHA256); err != nil {
			handleFileManagerError(w, err)
			return
		}
		normalized, _ := filemanager.NormalizePath(req.Path)
		LogActivityWithUser(ctx.ID, "file_edited", fmt.Sprintf("Edited file %q", normalized), usernameFromContext(r.Context()), getClientIP(r))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
	}
}

func (s *Server) handleCreateSiteFileEntry() http.HandlerFunc {
	type request struct {
		Path string `json:"path"`
		Type string `json:"type"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var req request
		if !decodeFileJSON(w, r, &req) {
			return
		}
		ctx, err := loadSiteFileContext(r.PathValue("id"))
		if err != nil {
			handleFileManagerError(w, err)
			return
		}
		if err := ctx.Manager.Create(req.Path, req.Type); err != nil {
			handleFileManagerError(w, err)
			return
		}
		normalized, _ := filemanager.NormalizePath(req.Path)
		LogActivityWithUser(ctx.ID, "file_created", fmt.Sprintf("Created %s %q", req.Type, normalized), usernameFromContext(r.Context()), getClientIP(r))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"path": normalized})
	}
}

func (s *Server) handleMoveSiteFileEntry() http.HandlerFunc {
	type request struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var req request
		if !decodeFileJSON(w, r, &req) {
			return
		}
		ctx, err := loadSiteFileContext(r.PathValue("id"))
		if err != nil {
			handleFileManagerError(w, err)
			return
		}
		if err := ctx.Manager.Move(req.Source, req.Destination); err != nil {
			handleFileManagerError(w, err)
			return
		}
		source, _ := filemanager.NormalizePath(req.Source)
		destination, _ := filemanager.NormalizePath(req.Destination)
		LogActivityWithUser(ctx.ID, "file_moved", fmt.Sprintf("Moved %q to %q", source, destination), usernameFromContext(r.Context()), getClientIP(r))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"path": destination})
	}
}

func (s *Server) handleDeleteSiteFileEntry() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, err := loadSiteFileContext(r.PathValue("id"))
		if err != nil {
			handleFileManagerError(w, err)
			return
		}
		requestedPath := r.URL.Query().Get("path")
		if err := ctx.Manager.Delete(requestedPath); err != nil {
			handleFileManagerError(w, err)
			return
		}
		normalized, _ := filemanager.NormalizePath(requestedPath)
		LogActivityWithUser(ctx.ID, "file_deleted", fmt.Sprintf("Deleted %q", normalized), usernameFromContext(r.Context()), getClientIP(r))
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleUploadSiteFile() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > filemanager.MaxUploadBytes {
			handleFileManagerError(w, filemanager.ErrTooLarge)
			return
		}
		ctx, err := loadSiteFileContext(r.PathValue("id"))
		if err != nil {
			handleFileManagerError(w, err)
			return
		}
		directory := r.URL.Query().Get("path")
		name := r.URL.Query().Get("name")
		overwrite := r.URL.Query().Get("overwrite") == "true"
		if err := ctx.Manager.Upload(directory, name, r.Body, overwrite); err != nil {
			handleFileManagerError(w, err)
			return
		}
		normalizedDirectory, _ := filemanager.NormalizePath(directory)
		uploadedPath := name
		if normalizedDirectory != "." {
			uploadedPath = path.Join(normalizedDirectory, name)
		}
		LogActivityWithUser(ctx.ID, "file_uploaded", fmt.Sprintf("Uploaded file %q", uploadedPath), usernameFromContext(r.Context()), getClientIP(r))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"path": uploadedPath})
	}
}

func (s *Server) handleDownloadSiteFile() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, err := loadSiteFileContext(r.PathValue("id"))
		if err != nil {
			handleFileManagerError(w, err)
			return
		}
		requestedPath, err := filemanager.NormalizePath(r.URL.Query().Get("path"))
		if err != nil || requestedPath == "." {
			handleFileManagerError(w, filemanager.ErrInvalidPath)
			return
		}
		file, info, err := ctx.Manager.OpenDownload(requestedPath)
		if err != nil {
			handleFileManagerError(w, err)
			return
		}
		defer file.Close()
		disposition := mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(requestedPath)})
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Disposition", disposition)
		http.ServeContent(w, r, path.Base(requestedPath), info.ModTime(), file)
	}
}
