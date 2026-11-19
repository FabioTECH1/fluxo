package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"fluxo/internal/services/traffic"
)

var errTrafficPending = errors.New("traffic collection is still applying; wait for the current change")
var errTrafficSiteMissing = errors.New("site is unavailable for traffic collection")

func trafficResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func trafficToggle(r *http.Request) (bool, error) {
	if t := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(t), "application/json") {
		return false, errors.New("use Content-Type: application/json")
	}
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1025))
	if err != nil || len(raw) > 1024 {
		return false, errors.New("traffic settings body exceeds 1024 bytes")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return false, errors.New("provide exactly one boolean enabled field")
	}
	if input.Enabled == nil {
		return false, errors.New("enabled is required and must be boolean")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return false, errors.New("unexpected trailing JSON")
	}
	return *input.Enabled, nil
}

func trafficError(w http.ResponseWriter, err error) {
	code := http.StatusServiceUnavailable
	if errors.Is(err, errTrafficPending) {
		code = http.StatusConflict
	}
	if errors.Is(err, errTrafficSiteMissing) {
		code = http.StatusNotFound
	}
	if strings.Contains(err.Error(), "at most") {
		code = http.StatusBadRequest
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, err.Error(), code)
}

func (s *Server) handleTrafficAnalytics() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			state, err := s.traffic.Status(r.Context())
			if err != nil {
				trafficError(w, err)
				return
			}
			trafficResponse(w, 200, state)
			return
		}
		enabled, err := trafficToggle(r)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		state, err := s.traffic.change(r.Context(), 0, enabled)
		if err != nil {
			trafficError(w, err)
			return
		}
		code := 200
		if state.Pending {
			code = 202
		}
		trafficResponse(w, code, state)
	}
}

func (s *Server) trafficSiteID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "Invalid site ID", 400)
		return 0, false
	}
	sites, err := s.traffic.listSites(r.Context())
	if err != nil {
		trafficError(w, errors.New("unable to verify traffic site"))
		return 0, false
	}
	for _, site := range sites {
		if site.ID == id {
			return id, true
		}
	}
	trafficError(w, errTrafficSiteMissing)
	return 0, false
}

func (s *Server) handleSiteTrafficAnalytics() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := s.trafficSiteID(w, r)
		if !ok {
			return
		}
		if r.Method == http.MethodGet {
			period := traffic.Range(r.URL.Query().Get("range"))
			if period == "" {
				period = traffic.RangeToday
			}
			if period != traffic.RangeToday && period != traffic.Range7Days && period != traffic.Range30Days {
				http.Error(w, "range must be today, 7d or 30d", 400)
				return
			}
			report, err := s.traffic.Report(r.Context(), id, period)
			if err != nil {
				trafficError(w, err)
				return
			}
			trafficResponse(w, 200, report)
			return
		}
		if r.Method == http.MethodDelete {
			if err := s.traffic.DeleteHistory(r.Context(), id); err != nil {
				trafficError(w, err)
				return
			}
			state, err := s.traffic.Status(r.Context())
			if err != nil {
				trafficError(w, err)
				return
			}
			for _, site := range state.Sites {
				if site.SiteID == id {
					trafficResponse(w, 200, map[string]any{"deleted": true, "site": site})
					return
				}
			}
			trafficError(w, errTrafficSiteMissing)
			return
		}
		enabled, err := trafficToggle(r)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		state, err := s.traffic.change(r.Context(), id, enabled)
		if err != nil {
			trafficError(w, err)
			return
		}
		for _, site := range state.Sites {
			if site.SiteID == id {
				code := 200
				if state.Pending {
					code = 202
				}
				trafficResponse(w, code, site)
				return
			}
		}
		trafficError(w, errTrafficSiteMissing)
	}
}
