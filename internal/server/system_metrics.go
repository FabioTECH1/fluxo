package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"fluxo/internal/database"
	"fluxo/internal/services/nginx"
	"fluxo/internal/services/system"
)

// handleGetMetrics returns current CPU, memory, and disk usage.
func (s *Server) handleGetMetrics() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m := system.GetMetrics(r.Context())
		m.NginxGuardActive, m.NginxGuardError = nginx.DefaultServerStatus()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(m)
	}
}

// handleGetActivity returns paginated activity feed with optional site filter.
func (s *Server) handleGetActivity() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type ActivityItem struct {
			ID        int    `json:"id"`
			SiteID    int    `json:"site_id"`
			Type      string `json:"type"`
			Summary   string `json:"summary"`
			Username  string `json:"username"`
			IPAddress string `json:"ip_address"`
			CreatedAt string `json:"created_at"`
		}

		q := r.URL.Query()
		limitStr := q.Get("limit")
		offsetStr := q.Get("offset")
		siteIDStr := q.Get("site_id")

		limit := 50
		if limitStr != "" {
			if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 100 {
				limit = v
			}
		}

		offset := 0
		if offsetStr != "" {
			if v, err := strconv.Atoi(offsetStr); err == nil && v >= 0 {
				offset = v
			}
		}

		items := make([]ActivityItem, 0)

		if siteIDStr != "" {
			siteID, err := strconv.Atoi(siteIDStr)
			if err == nil {
				rows, err := database.DB.Query(
					"SELECT id, site_id, type, summary, username, ip_address, created_at FROM activity WHERE site_id = ? ORDER BY id DESC LIMIT ? OFFSET ?",
					siteID, limit, offset,
				)
				if err == nil {
					defer rows.Close()
					for rows.Next() {
						var item ActivityItem
						if err := rows.Scan(&item.ID, &item.SiteID, &item.Type, &item.Summary, &item.Username, &item.IPAddress, &item.CreatedAt); err == nil {
							items = append(items, item)
						}
					}
				}
			}
		} else {
			rows, err := database.DB.Query(
				"SELECT id, site_id, type, summary, username, ip_address, created_at FROM activity ORDER BY id DESC LIMIT ? OFFSET ?",
				limit, offset,
			)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var item ActivityItem
					if err := rows.Scan(&item.ID, &item.SiteID, &item.Type, &item.Summary, &item.Username, &item.IPAddress, &item.CreatedAt); err == nil {
						items = append(items, item)
					}
				}
			}
		}

		if len(items) == 0 {
			items = []ActivityItem{}
		}

		// Total count for pagination
		total := 0
		countQuery := "SELECT COUNT(*) FROM activity"
		if siteIDStr != "" {
			if siteID, err := strconv.Atoi(siteIDStr); err == nil {
				database.DB.QueryRow("SELECT COUNT(*) FROM activity WHERE site_id = ?", siteID).Scan(&total)
			}
		} else {
			database.DB.QueryRow(countQuery).Scan(&total)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"items":  items,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		})
	}
}

// LogActivity inserts an activity record. Safe to call from goroutines.
func LogActivity(siteID int, typ, summary string) {
	database.DB.Exec("INSERT INTO activity (site_id, type, summary) VALUES (?, ?, ?)", siteID, typ, summary)
}

// LogActivityWithUser inserts an activity record with actor identity.
func LogActivityWithUser(siteID int, typ, summary, username, ipAddress string) {
	database.DB.Exec("INSERT INTO activity (site_id, type, summary, username, ip_address) VALUES (?, ?, ?, ?, ?)", siteID, typ, summary, username, ipAddress)
}
