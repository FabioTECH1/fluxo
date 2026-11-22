package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"fluxo/internal/services/nginx"
	"fluxo/internal/syscmd"
)

var reloadNginxConfiguration = nginx.Reload

// handleRestartNginx reloads the Nginx configuration.
func (s *Server) handleRestartNginx() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := reloadNginxConfiguration(r.Context()); err != nil {
			http.Error(w, "Failed to reload Nginx: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleGetNginxInfo returns Nginx binary path, version, and config directory info.
func (s *Server) handleGetNginxInfo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info := map[string]interface{}{}

		if out, err := exec.LookPath("nginx"); err == nil {
			info["binary"] = out
		} else {
			info["binary"] = ""
		}

		// nginx -v outputs to stderr, capture both streams
		if out, err := nginxDiskVersion(r.Context(), info["binary"].(string)); err == nil {
			info["version"] = strings.TrimSpace(out)
		}

		info["version_scope"] = "on_disk_binary"
		info["running_adoption"] = "not_verified"
		info["config_dir"] = "/etc/nginx"
		info["sites_available"] = filepath.Join("/etc/nginx", "sites-available")
		info["sites_enabled"] = filepath.Join("/etc/nginx", "sites-enabled")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	}
}

func nginxDiskVersion(ctx context.Context, binary string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var output strings.Builder
	err := syscmd.RunMaintenance(ctx, "", &output, nil, binary, "-v")
	return output.String(), err
}
