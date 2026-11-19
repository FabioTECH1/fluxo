package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"fluxo/internal/services/traffic"
)

const maxTrafficAPISettingsBody = 16 << 10

func (m *trafficControl) APISettings(ctx context.Context, id int64) (traffic.APISettingsState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.loadLocked(); err != nil {
		return traffic.APISettingsState{}, err
	}
	if err := m.ensureCollectorLocked(); err != nil {
		return traffic.APISettingsState{}, err
	}
	return m.store.APISettings(ctx, id)
}

func (m *trafficControl) SetAPISettings(ctx context.Context, id int64, settings traffic.APISettings) (traffic.APISettingsState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.loadLocked(); err != nil {
		return traffic.APISettingsState{}, err
	}
	if m.state.Pending != nil {
		return traffic.APISettingsState{}, errTrafficPending
	}
	if err := m.ensureCollectorLocked(); err != nil {
		return traffic.APISettingsState{}, err
	}
	if err := m.pruneAPISettingsLocked(ctx, nil); err != nil {
		return traffic.APISettingsState{}, err
	}
	return m.collector.SetAPISettings(ctx, id, settings)
}

// Only an authoritative, successful panel-site listing can reclaim orphan
// preferences. Retained rules and aggregates, and disabled living sites, stay
// intact. This is a bounded metadata cleanup, never a request-log scan.
func (m *trafficControl) pruneAPISettingsLocked(ctx context.Context, sites []trafficSiteInfo) error {
	if m.store == nil {
		return nil
	}
	if sites == nil {
		var err error
		sites, err = m.listSites(ctx)
		if err != nil {
			return err
		}
	}
	ids := make([]int64, len(sites))
	for i, site := range sites {
		ids[i] = site.ID
	}
	return m.store.PruneAPISettings(ctx, ids)
}

func (s *Server) handleTrafficAPISettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		id, ok := s.trafficSiteID(w, r)
		if !ok {
			return
		}
		if len(r.URL.Query()) != 0 {
			http.Error(w, "API traffic settings accept no query parameters", http.StatusBadRequest)
			return
		}
		if r.Method == http.MethodGet {
			settings, err := s.traffic.APISettings(r.Context(), id)
			if err != nil {
				trafficError(w, err)
				return
			}
			trafficResponse(w, http.StatusOK, settings)
			return
		}
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			http.Error(w, "use Content-Type: application/json", http.StatusUnsupportedMediaType)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, maxTrafficAPISettingsBody+1))
		if err != nil {
			http.Error(w, "could not read API traffic settings", http.StatusBadRequest)
			return
		}
		if len(raw) > maxTrafficAPISettingsBody {
			http.Error(w, "API traffic settings body exceeds 16384 bytes", http.StatusRequestEntityTooLarge)
			return
		}
		var input struct {
			Prefixes *[]string `json:"prefixes"`
			Routes   *[]string `json:"routes"`
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || input.Prefixes == nil || input.Routes == nil || decoder.Decode(new(any)) != io.EOF {
			http.Error(w, "provide prefixes and routes string arrays without additional fields", http.StatusBadRequest)
			return
		}
		settings := traffic.APISettings{Prefixes: *input.Prefixes, Routes: *input.Routes}
		if err := traffic.ValidateAPISettings(settings); err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		result, err := s.traffic.SetAPISettings(r.Context(), id, settings)
		if err != nil {
			if errors.Is(err, traffic.ErrAPISettingsCapacity) {
				http.Error(w, err.Error(), http.StatusConflict)
			} else {
				trafficError(w, err)
			}
			return
		}
		trafficResponse(w, http.StatusOK, result)
	}
}
