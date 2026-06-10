package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"fluxo/syscmd"
)

func (s *Server) handleRestartNode() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Kill all node processes; they'll be restarted by supervisor/daemon
		syscmd.Run(r.Context(), 10*time.Second, "pkill", "-x", "node")
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleInstallNode() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := exec.LookPath("node"); err == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Node.js already installed"})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()

		if _, err := syscmd.Run(ctx, 5*time.Minute, "apt-get", "update"); err != nil {
			http.Error(w, "apt-get update failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := syscmd.Run(ctx, 5*time.Minute, "apt-get", "install", "-y", "nodejs", "npm"); err != nil {
			http.Error(w, "Node.js installation failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Node.js installed successfully"})
	}
}

func (s *Server) handleRemoveNode() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()

		if _, err := syscmd.Run(ctx, 3*time.Minute, "apt-get", "purge", "-y", "nodejs", "npm"); err != nil {
			http.Error(w, "Node.js removal failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Node.js removed successfully"})
	}
}

func (s *Server) handleGetNodeInfo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info := map[string]interface{}{}

		if out, err := exec.LookPath("node"); err == nil {
			info["binary"] = out
		} else {
			info["binary"] = ""
		}

		if out, err := syscmd.Run(r.Context(), 5*time.Second, "node", "--version"); err == nil {
			info["version"] = strings.TrimSpace(out)
		} else {
			info["version"] = ""
		}

		if out, err := syscmd.Run(r.Context(), 5*time.Second, "npm", "--version"); err == nil {
			info["npm"] = strings.TrimSpace(out)
		} else {
			info["npm"] = ""
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	}
}
