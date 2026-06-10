package server

import (
	"fmt"
	"net/http"
	"time"

	"fluxo/syscmd"
)

// systemdAction returns an http.HandlerFunc that performs a systemctl action
// (start, stop, restart, reload) on a target service name.
func (s *Server) systemdAction(service, action, label string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := syscmd.Run(r.Context(), 10*time.Second, "systemctl", action, service); err != nil {
			http.Error(w, fmt.Sprintf("Failed to %s %s: %s", action, label, err.Error()), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
