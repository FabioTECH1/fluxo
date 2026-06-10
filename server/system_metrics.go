package server

import (
	"encoding/json"
	"net/http"

	"fluxo/database"
	"fluxo/services/system"
)

func (s *Server) handleGetMetrics() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m := system.GetMetrics(r.Context())
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(m)
	}
}

func (s *Server) handleGetActivity() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type ActivityItem struct {
			Type      string `json:"type"`
			Summary   string `json:"summary"`
			CreatedAt string `json:"created_at"`
		}

		items := make([]ActivityItem, 0)

		rows, err := database.DB.Query(`
			SELECT 'deployment', 'Deployment #' || id || ' for site ' || (SELECT domain FROM sites WHERE id = site_id), created_at
			FROM deployments ORDER BY created_at DESC LIMIT 20
		`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var item ActivityItem
				if err := rows.Scan(&item.Type, &item.Summary, &item.CreatedAt); err == nil {
					items = append(items, item)
				}
			}
		}

		if len(items) == 0 {
			items = []ActivityItem{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(items)
	}
}
