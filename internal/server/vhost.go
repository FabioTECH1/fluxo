package server

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/nginx"
)

const (
	maxSiteVhostSize        = 256 << 10
	maxSiteVhostRequestSize = (maxSiteVhostSize * 6) + (64 << 10)
)

var (
	renderSiteVhostDefault = renderManagedNginxForSite
	installSiteVhost       = nginx.InstallConfigNamedTransactional
)

type siteVhostResponse struct {
	Config     string `json:"config"`
	Customized bool   `json:"customized"`
	Revision   string `json:"revision"`
	Path       string `json:"path"`
	UpdatedAt  string `json:"updated_at,omitempty"`
}

type updateSiteVhostRequest struct {
	Config           string `json:"config"`
	ExpectedRevision string `json:"expected_revision"`
}

type restoreSiteVhostRequest struct {
	ExpectedRevision string `json:"expected_revision"`
}

func siteVhostRevision(config string, customized bool) string {
	mode := "managed\x00"
	if customized {
		mode = "custom\x00"
	}
	digest := sha256.Sum256([]byte(mode + config))
	return hex.EncodeToString(digest[:])
}

func normalizeSiteVhost(config string) (string, error) {
	config = strings.ReplaceAll(config, "\r\n", "\n")
	config = strings.ReplaceAll(config, "\r", "\n")
	if strings.TrimSpace(config) == "" {
		return "", fmt.Errorf("vhost configuration cannot be empty")
	}
	if strings.ContainsRune(config, '\x00') {
		return "", fmt.Errorf("vhost configuration cannot contain null bytes")
	}
	if !strings.HasSuffix(config, "\n") {
		config += "\n"
	}
	if len(config) > maxSiteVhostSize {
		return "", fmt.Errorf("vhost configuration cannot exceed %d KiB", maxSiteVhostSize>>10)
	}
	return config, nil
}

func decodeVhostPayload(w http.ResponseWriter, r *http.Request, target any) error {
	// JSON can expand a valid configuration when characters are escaped. Keep the
	// wire limit bounded while enforcing the exact 256 KiB limit after decoding.
	r.Body = http.MaxBytesReader(w, r.Body, maxSiteVhostRequestSize)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("request must contain one JSON object")
		}
		return err
	}
	return nil
}

func siteVhostConfigName(siteID int) (string, error) {
	var sitePath string
	if err := database.DB.QueryRow("SELECT path FROM sites WHERE id = ?", siteID).Scan(&sitePath); err != nil {
		return "", err
	}
	name := filepath.Base(filepath.Clean(sitePath))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "", fmt.Errorf("site has an invalid infrastructure name")
	}
	return name, nil
}

func loadSiteVhostState(siteID int) (siteVhostResponse, error) {
	configName, err := siteVhostConfigName(siteID)
	if err != nil {
		return siteVhostResponse{}, err
	}
	override, err := database.GetSiteVhostOverride(siteID)
	if err != nil {
		return siteVhostResponse{}, err
	}
	if override != nil {
		return siteVhostResponse{
			Config:     override.Config,
			Customized: true,
			Revision:   siteVhostRevision(override.Config, true),
			Path:       filepath.Join("/etc/nginx/sites-available", configName),
			UpdatedAt:  override.UpdatedAt.UTC().Format(time.RFC3339),
		}, nil
	}

	managed, err := renderSiteVhostDefault(siteID)
	if err != nil {
		return siteVhostResponse{}, err
	}
	return siteVhostResponse{
		Config:     managed.Content,
		Customized: false,
		Revision:   siteVhostRevision(managed.Content, false),
		Path:       filepath.Join("/etc/nginx/sites-available", managed.ConfigName),
	}, nil
}

func parseSiteID(r *http.Request) (int, error) {
	siteID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || siteID <= 0 {
		return 0, fmt.Errorf("invalid site ID")
	}
	return siteID, nil
}

func writeSiteVhostResponse(w http.ResponseWriter, response siteVhostResponse) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func writeSiteVhostLoadError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "Site not found", http.StatusNotFound)
		return
	}
	http.Error(w, "Failed to load vhost: "+err.Error(), http.StatusInternalServerError)
}

// handleGetSiteVhost returns either the durable override or Fluxo's current generated default.
func (s *Server) handleGetSiteVhost() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, err := parseSiteID(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		siteNginxMutationMu.Lock()
		defer siteNginxMutationMu.Unlock()
		response, err := loadSiteVhostState(siteID)
		if err != nil {
			writeSiteVhostLoadError(w, err)
			return
		}
		writeSiteVhostResponse(w, response)
	}
}

// handleUpdateSiteVhost validates and activates a custom vhost before persisting it.
func (s *Server) handleUpdateSiteVhost() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, err := parseSiteID(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var request updateSiteVhostRequest
		if err := decodeVhostPayload(w, r, &request); err != nil {
			http.Error(w, "Invalid payload: "+err.Error(), http.StatusBadRequest)
			return
		}
		request.Config, err = normalizeSiteVhost(request.Config)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(request.ExpectedRevision) == "" {
			http.Error(w, "expected_revision is required", http.StatusBadRequest)
			return
		}

		siteNginxMutationMu.Lock()
		defer siteNginxMutationMu.Unlock()
		current, err := loadSiteVhostState(siteID)
		if err != nil {
			writeSiteVhostLoadError(w, err)
			return
		}
		if request.ExpectedRevision != current.Revision {
			http.Error(w, "The vhost changed after it was loaded. Refresh it before saving.", http.StatusConflict)
			return
		}
		if request.Config == current.Config {
			writeSiteVhostResponse(w, current)
			return
		}

		configName := filepath.Base(current.Path)
		if err := installSiteVhost(r.Context(), configName, request.Config); err != nil {
			http.Error(w, "Vhost was not saved: "+err.Error(), http.StatusUnprocessableEntity)
			return
		}
		if err := database.SaveSiteVhostOverride(siteID, request.Config); err != nil {
			rollbackErr := installSiteVhost(r.Context(), configName, current.Config)
			if rollbackErr != nil {
				http.Error(w, fmt.Sprintf("Failed to persist vhost: %v (failed to restore the previous working vhost: %v)", err, rollbackErr), http.StatusInternalServerError)
				return
			}
			http.Error(w, "Failed to persist vhost; the previous working vhost was restored", http.StatusInternalServerError)
			return
		}

		saved, err := loadSiteVhostState(siteID)
		if err != nil {
			http.Error(w, "Vhost was activated but its saved state could not be reloaded", http.StatusInternalServerError)
			return
		}
		LogActivity(siteID, "settings", "Custom Nginx vhost saved")
		writeSiteVhostResponse(w, saved)
	}
}

// handleRestoreSiteVhost installs the current generated default and removes the override.
func (s *Server) handleRestoreSiteVhost() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, err := parseSiteID(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var request restoreSiteVhostRequest
		if err := decodeVhostPayload(w, r, &request); err != nil {
			http.Error(w, "Invalid payload: "+err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(request.ExpectedRevision) == "" {
			http.Error(w, "expected_revision is required", http.StatusBadRequest)
			return
		}

		siteNginxMutationMu.Lock()
		defer siteNginxMutationMu.Unlock()
		current, err := loadSiteVhostState(siteID)
		if err != nil {
			writeSiteVhostLoadError(w, err)
			return
		}
		if request.ExpectedRevision != current.Revision {
			http.Error(w, "The vhost changed after it was loaded. Refresh it before restoring.", http.StatusConflict)
			return
		}
		if !current.Customized {
			writeSiteVhostResponse(w, current)
			return
		}

		managed, err := renderSiteVhostDefault(siteID)
		if err != nil {
			http.Error(w, "Failed to generate Fluxo's default vhost: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := installSiteVhost(r.Context(), managed.ConfigName, managed.Content); err != nil {
			http.Error(w, "Default vhost was not restored: "+err.Error(), http.StatusUnprocessableEntity)
			return
		}
		if err := database.DeleteSiteVhostOverride(siteID); err != nil {
			rollbackErr := installSiteVhost(r.Context(), managed.ConfigName, current.Config)
			if rollbackErr != nil {
				http.Error(w, fmt.Sprintf("Failed to persist the restore: %v (failed to reactivate the custom vhost: %v)", err, rollbackErr), http.StatusInternalServerError)
				return
			}
			http.Error(w, "Failed to persist the restore; the custom vhost was reactivated", http.StatusInternalServerError)
			return
		}

		response := siteVhostResponse{
			Config:     managed.Content,
			Customized: false,
			Revision:   siteVhostRevision(managed.Content, false),
			Path:       filepath.Join("/etc/nginx/sites-available", managed.ConfigName),
		}
		LogActivity(siteID, "settings", "Nginx vhost restored to Fluxo default")
		writeSiteVhostResponse(w, response)
	}
}
