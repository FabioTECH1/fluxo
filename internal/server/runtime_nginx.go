package server

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"

	"fluxo/internal/services/nginx"
)

func (s *Server) handleRestartNginx() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := nginx.Reload(r.Context()); err != nil {
			http.Error(w, "Failed to restart Nginx: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleGetNginxInfo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info := map[string]interface{}{}

		if out, err := exec.LookPath("nginx"); err == nil {
			info["binary"] = out
		} else {
			info["binary"] = ""
		}

		// nginx -v outputs to stderr, capture both streams
		if out, err := exec.CommandContext(r.Context(), "nginx", "-v").CombinedOutput(); err == nil {
			info["version"] = strings.TrimSpace(string(out))
		}

		info["config_dir"] = "/etc/nginx"
		info["sites_available"] = filepath.Join("/etc/nginx", "sites-available")
		info["sites_enabled"] = filepath.Join("/etc/nginx", "sites-enabled")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	}
}
