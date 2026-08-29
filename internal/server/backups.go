package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"fluxo/internal/config"
	"fluxo/internal/database"
	backupservice "fluxo/internal/services/backup"
)

type backupDestinationRequest struct {
	Name            string `json:"name"`
	Provider        string `json:"provider"`
	Bucket          string `json:"bucket"`
	Region          string `json:"region"`
	AccountID       string `json:"account_id"`
	Jurisdiction    string `json:"jurisdiction"`
	Prefix          string `json:"prefix"`
	AccessKey       string `json:"access_key"`
	SecretKey       string `json:"secret_key"`
	UseInstanceRole bool   `json:"use_instance_role"`
	IsDefault       bool   `json:"is_default"`
}

type backupPlanRequest struct {
	Name               string `json:"name"`
	SiteID             int    `json:"site_id"`
	DestinationID      int    `json:"destination_id"`
	IncludeFiles       bool   `json:"include_files"`
	DatabaseIDs        []int  `json:"database_ids"`
	Schedule           string `json:"schedule"`
	BackupHour         int    `json:"backup_hour"`
	RetentionProfile   string `json:"retention_profile"`
	Enabled            bool   `json:"enabled"`
	EncryptionEnabled  *bool  `json:"encryption_enabled"`
	EncryptionPassword string `json:"encryption_password"`
}

func validateBackupEncryptionPassword(password string) error {
	length := len([]rune(password))
	if length < 12 || length > 256 {
		return errors.New("backup encryption password must be between 12 and 256 characters")
	}
	if strings.IndexFunc(password, unicode.IsControl) >= 0 {
		return errors.New("backup encryption password cannot contain control characters")
	}
	return nil
}

func applyBackupPlanEncryption(plan *database.BackupPlan, request backupPlanRequest, existing *database.BackupPlan) error {
	if request.EncryptionEnabled == nil {
		if request.EncryptionPassword != "" {
			return errors.New("enable backup encryption before providing a password")
		}
		if existing != nil {
			plan.EncryptionEnabled = existing.EncryptionEnabled
			plan.EncryptionPassword = existing.EncryptionPassword
		}
		return nil
	}

	if !*request.EncryptionEnabled {
		if request.EncryptionPassword != "" {
			return errors.New("enable backup encryption before providing a password")
		}
		plan.EncryptionEnabled = false
		plan.EncryptionPassword = ""
		return nil
	}

	plan.EncryptionEnabled = true
	if request.EncryptionPassword == "" {
		if existing != nil && existing.EncryptionEnabled && existing.EncryptionPassword != "" {
			plan.EncryptionPassword = existing.EncryptionPassword
			return nil
		}
		return errors.New("backup encryption password is required")
	}
	if err := validateBackupEncryptionPassword(request.EncryptionPassword); err != nil {
		return err
	}
	encrypted, err := config.EncryptSecret(request.EncryptionPassword)
	if err != nil {
		return errors.New("failed to protect the backup encryption password")
	}
	plan.EncryptionPassword = encrypted
	return nil
}

func backupDestinationPrefix(requested, fallback string) string {
	prefix := strings.Trim(strings.TrimSpace(requested), "/")
	if prefix == "" {
		return fallback
	}
	return prefix
}

func (s *Server) handleListBackupDestinations() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		destinations, err := database.ListBackupDestinations()
		if err != nil {
			http.Error(w, "Failed to load backup destinations", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, destinations)
	}
}

func (s *Server) handleCreateBackupDestination() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		var request backupDestinationRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}
		installationID, err := database.InstallationID()
		if err != nil {
			http.Error(w, "Failed to load the server identity", http.StatusInternalServerError)
			return
		}
		destination := database.BackupDestination{
			Name: strings.TrimSpace(request.Name), Provider: strings.ToLower(strings.TrimSpace(request.Provider)),
			Bucket: strings.TrimSpace(request.Bucket), Region: strings.TrimSpace(request.Region),
			AccountID: strings.TrimSpace(request.AccountID), Jurisdiction: strings.ToLower(strings.TrimSpace(request.Jurisdiction)),
			Prefix:   backupDestinationPrefix(request.Prefix, "fluxo-backups"),
			ServerID: installationID, AccessKey: strings.TrimSpace(request.AccessKey), SecretKey: request.SecretKey,
			UseInstanceRole: request.UseInstanceRole, IsDefault: request.IsDefault,
		}
		if destination.Jurisdiction == "" {
			destination.Jurisdiction = "default"
		}
		if destination.Provider == "r2" {
			destination.Region = ""
			destination.UseInstanceRole = false
		} else if destination.Provider == "s3" {
			destination.AccountID = ""
			destination.Jurisdiction = "default"
		}
		if destination.UseInstanceRole {
			destination.AccessKey = ""
			destination.SecretKey = ""
		}
		if err := backupservice.ValidateDestination(destination); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var count int
		_ = database.DB.QueryRow("SELECT COUNT(*) FROM backup_destinations").Scan(&count)
		if count == 0 {
			destination.IsDefault = true
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		if err := s.backupManager.TestDestination(ctx, destination); err != nil {
			http.Error(w, "Destination test failed: "+redactDestinationError(err, destination), http.StatusBadRequest)
			return
		}
		if !destination.UseInstanceRole {
			encryptedAccessKey, err := config.EncryptSecret(destination.AccessKey)
			var encryptedSecretKey string
			if err == nil {
				encryptedSecretKey, err = config.EncryptSecret(destination.SecretKey)
			}
			if err != nil {
				http.Error(w, "Failed to encrypt destination credentials", http.StatusInternalServerError)
				return
			}
			destination.AccessKey = encryptedAccessKey
			destination.SecretKey = encryptedSecretKey
		}
		if err := database.CreateBackupDestination(&destination); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				http.Error(w, "A backup destination with this name already exists", http.StatusConflict)
				return
			}
			http.Error(w, "Failed to save backup destination", http.StatusInternalServerError)
			return
		}
		created, _ := database.GetBackupDestination(destination.ID)
		LogActivityWithUser(0, "backup_destination_created", "Backup destination "+destination.Name+" was connected", usernameFromContext(r.Context()), getClientIP(r))
		writeJSON(w, http.StatusCreated, created)
	}
}

func (s *Server) handleUpdateBackupDestination() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt(r, "id")
		if err != nil {
			http.Error(w, "Invalid destination ID", http.StatusBadRequest)
			return
		}
		existing, err := database.GetBackupDestination(id)
		if err != nil {
			http.Error(w, "Backup destination not found", http.StatusNotFound)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		var request backupDestinationRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}
		destination := existing
		destination.Name = strings.TrimSpace(request.Name)
		destination.Prefix = backupDestinationPrefix(request.Prefix, existing.Prefix)
		destination.IsDefault = request.IsDefault
		destination.UseInstanceRole = request.UseInstanceRole
		destination.AccessKey = strings.TrimSpace(request.AccessKey)
		destination.SecretKey = request.SecretKey
		if destination.UseInstanceRole {
			destination.AccessKey = ""
			destination.SecretKey = ""
		}
		if err := backupservice.ValidateDestination(destination); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		if err := s.backupManager.TestDestination(ctx, destination); err != nil {
			http.Error(w, "Destination test failed: "+redactDestinationError(err, destination), http.StatusBadRequest)
			return
		}
		if !destination.UseInstanceRole {
			encryptedAccessKey, err := config.EncryptSecret(destination.AccessKey)
			var encryptedSecretKey string
			if err == nil {
				encryptedSecretKey, err = config.EncryptSecret(destination.SecretKey)
			}
			if err != nil {
				http.Error(w, "Failed to encrypt destination credentials", http.StatusInternalServerError)
				return
			}
			destination.AccessKey = encryptedAccessKey
			destination.SecretKey = encryptedSecretKey
		}
		if err := s.backupManager.UpdateDestination(destination); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				http.Error(w, "A backup destination with this name already exists", http.StatusConflict)
				return
			}
			http.Error(w, "Failed to update backup destination", http.StatusInternalServerError)
			return
		}
		updated, _ := database.GetBackupDestination(id)
		LogActivityWithUser(0, "backup_destination_updated", "Backup destination "+destination.Name+" was updated", usernameFromContext(r.Context()), getClientIP(r))
		writeJSON(w, http.StatusOK, updated)
	}
}

func (s *Server) handleTestBackupDestination() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt(r, "id")
		if err != nil {
			http.Error(w, "Invalid destination ID", http.StatusBadRequest)
			return
		}
		destination, err := database.GetBackupDestination(id)
		if err != nil {
			http.Error(w, "Backup destination not found", http.StatusNotFound)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		if err := s.backupManager.TestDestination(ctx, destination); err != nil {
			http.Error(w, "Destination test failed: "+redactDestinationError(err, destination), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func (s *Server) handleDeleteBackupDestination() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt(r, "id")
		if err != nil {
			http.Error(w, "Invalid destination ID", http.StatusBadRequest)
			return
		}
		destination, _ := database.GetBackupDestination(id)
		if err := s.backupManager.DeleteDestination(id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "Backup destination not found", http.StatusNotFound)
				return
			}
			if strings.Contains(err.Error(), "still used") {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			http.Error(w, "Failed to delete backup destination", http.StatusInternalServerError)
			return
		}
		LogActivityWithUser(0, "backup_destination_deleted", "Backup destination "+destination.Name+" was removed", usernameFromContext(r.Context()), getClientIP(r))
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleListBackupPlans() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		plans, err := database.ListBackupPlans()
		if err != nil {
			http.Error(w, "Failed to load backup plans", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"plans": plans, "timezone": time.Now().Location().String()})
	}
}

func (s *Server) handleCreateBackupPlan() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		var request backupPlanRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}
		plan, err := validateBackupPlanRequest(request)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := applyBackupPlanEncryption(&plan, request, nil); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.backupManager.CreatePlan(&plan); err != nil {
			if strings.Contains(err.Error(), "being deleted") {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			http.Error(w, "Failed to create backup plan", http.StatusInternalServerError)
			return
		}
		created, _ := database.GetBackupPlan(plan.ID)
		LogActivityWithUser(plan.SiteID, "backup_plan_created", "Backup plan "+plan.Name+" was created", usernameFromContext(r.Context()), getClientIP(r))
		writeJSON(w, http.StatusCreated, created)
	}
}

func (s *Server) handleUpdateBackupPlan() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt(r, "id")
		if err != nil {
			http.Error(w, "Invalid plan ID", http.StatusBadRequest)
			return
		}
		existing, err := database.GetBackupPlan(id)
		if err != nil {
			http.Error(w, "Backup plan not found", http.StatusNotFound)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		var request backupPlanRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}
		plan, err := validateBackupPlanRequest(request)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		plan.ID = id
		if err := applyBackupPlanEncryption(&plan, request, &existing); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.backupManager.UpdatePlan(plan); err != nil {
			if strings.Contains(err.Error(), "queued or running") {
				http.Error(w, "Wait for the active backup to finish before editing this plan", http.StatusConflict)
				return
			}
			http.Error(w, "Failed to update backup plan", http.StatusInternalServerError)
			return
		}
		updated, _ := database.GetBackupPlan(id)
		writeJSON(w, http.StatusOK, updated)
	}
}

func (s *Server) handleDeleteBackupPlan() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt(r, "id")
		if err != nil {
			http.Error(w, "Invalid plan ID", http.StatusBadRequest)
			return
		}
		plan, _ := database.GetBackupPlan(id)
		if err := s.backupManager.DeletePlan(id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "Backup plan not found", http.StatusNotFound)
				return
			}
			if strings.Contains(err.Error(), "queued or running") {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			http.Error(w, "Failed to delete backup plan", http.StatusInternalServerError)
			return
		}
		LogActivityWithUser(plan.SiteID, "backup_plan_deleted", "Backup plan "+plan.Name+" was deleted", usernameFromContext(r.Context()), getClientIP(r))
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleRunBackupPlan() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt(r, "id")
		if err != nil {
			http.Error(w, "Invalid plan ID", http.StatusBadRequest)
			return
		}
		run, err := s.backupManager.EnqueuePlan(id, "manual")
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "Backup plan not found", http.StatusNotFound)
				return
			}
			if strings.Contains(err.Error(), "already has") {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			http.Error(w, "Failed to queue backup", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusAccepted, run)
	}
}

func (s *Server) handleListBackupRuns() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		runs, err := database.ListBackupRuns(limit)
		if err != nil {
			http.Error(w, "Failed to load backup history", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, runs)
	}
}

func (s *Server) handleCreateBackupDownload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		artifactID, err := pathInt(r, "artifact_id")
		if err != nil {
			http.Error(w, "Invalid artifact ID", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		url, err := s.backupManager.PresignArtifact(ctx, r.PathValue("id"), artifactID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "Backup artifact not found", http.StatusNotFound)
				return
			}
			http.Error(w, "Failed to create download link", http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"url": url, "expires_in": "5m"})
	}
}

func (s *Server) handleDeleteBackupRun() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		if err := s.backupManager.DeleteRun(ctx, r.PathValue("id")); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "Backup run not found", http.StatusNotFound)
				return
			}
			if strings.Contains(err.Error(), "cannot be deleted") {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			http.Error(w, "Failed to delete the remote backup", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func validateBackupPlanRequest(request backupPlanRequest) (database.BackupPlan, error) {
	plan := database.BackupPlan{
		Name: strings.TrimSpace(request.Name), SiteID: request.SiteID, DestinationID: request.DestinationID,
		IncludeFiles: request.IncludeFiles, Schedule: request.Schedule, BackupHour: request.BackupHour,
		RetentionProfile: request.RetentionProfile, Enabled: request.Enabled,
	}
	if len(plan.Name) < 1 || len(plan.Name) > 80 || strings.IndexFunc(plan.Name, unicode.IsControl) >= 0 {
		return plan, errors.New("plan name must be between 1 and 80 characters")
	}
	if plan.SiteID < 1 {
		return plan, errors.New("site is required")
	}
	if err := database.DB.QueryRow("SELECT domain FROM sites WHERE id = ?", plan.SiteID).Scan(&plan.SiteDomain); err != nil {
		return plan, errors.New("site not found")
	}
	if _, err := database.GetBackupDestination(plan.DestinationID); err != nil {
		return plan, errors.New("backup destination not found")
	}
	switch plan.Schedule {
	case "manual", "every_6_hours", "every_12_hours", "daily", "weekly":
	default:
		return plan, errors.New("invalid backup schedule")
	}
	if plan.BackupHour < 0 || plan.BackupHour > 23 {
		return plan, errors.New("backup hour must be between 0 and 23")
	}
	switch plan.RetentionProfile {
	case "minimal", "recommended", "extended":
	default:
		return plan, errors.New("invalid retention profile")
	}
	seen := make(map[int]bool)
	for _, databaseID := range request.DatabaseIDs {
		if databaseID < 1 || seen[databaseID] {
			continue
		}
		var siteID int
		if err := database.DB.QueryRow("SELECT site_id FROM databases WHERE id = ?", databaseID).Scan(&siteID); err != nil || siteID != plan.SiteID {
			return plan, fmt.Errorf("database %d is not linked to the selected site", databaseID)
		}
		seen[databaseID] = true
		plan.DatabaseIDs = append(plan.DatabaseIDs, databaseID)
	}
	if !plan.IncludeFiles && len(plan.DatabaseIDs) == 0 {
		return plan, errors.New("select site files, at least one database, or both")
	}
	if plan.Enabled && plan.Schedule != "manual" {
		next := backupservice.NextRunAt(plan.Schedule, plan.BackupHour, time.Now())
		plan.NextRunAt = &next
	}
	return plan, nil
}

func pathInt(r *http.Request, name string) (int, error) {
	return strconv.Atoi(r.PathValue(name))
}

func redactDestinationError(err error, destination database.BackupDestination) string {
	message := err.Error()
	for _, secret := range []string{destination.AccessKey, destination.SecretKey, config.Decrypt(destination.AccessKey), config.Decrypt(destination.SecretKey)} {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	if len(message) > 1000 {
		message = message[:1000]
	}
	return message
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
