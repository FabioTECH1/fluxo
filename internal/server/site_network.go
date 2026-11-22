package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"fluxo/internal/database"
	"fluxo/internal/services/nginx"
	"github.com/google/uuid"
)

var ensureSiteAuthFile = nginx.EnsureAuthFile
var installNetworkVhost = nginx.InstallNetworkConfigNamedTransactional
var renderNetworkVhost = renderManagedNetworkVhost
var errNetworkPending = errors.New("A network change was interrupted; restore the last confirmed settings in Network before making changes")

type networkProtectionResponse struct {
	Enabled  bool   `json:"enabled"`
	Username string `json:"username"`
}
type siteNetworkResponse struct {
	Managed    bool                      `json:"managed"`
	Revision   string                    `json:"revision"`
	Pending    bool                      `json:"pending"`
	Protection networkProtectionResponse `json:"protection"`
	Redirects  []database.SiteRedirect   `json:"redirects"`
	Wildcards  []string                  `json:"wildcards"`
}

func networkRevision(state database.SiteNetworkSettings, custom bool, pending string) string {
	b, _ := json.Marshal(state)
	sum := sha256.Sum256(append(b, []byte(fmt.Sprint(custom)+pending)...))
	return hex.EncodeToString(sum[:])
}
func configuredSiteDomains(siteID int) (map[string]bool, error) {
	rows, e := database.DB.Query("SELECT domain FROM sites WHERE id=? UNION ALL SELECT domain FROM domain_aliases WHERE site_id=?", siteID, siteID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	m := map[string]bool{}
	for rows.Next() {
		var d string
		if e = rows.Scan(&d); e != nil {
			return nil, e
		}
		m[d] = true
	}
	return m, rows.Err()
}
func activeWildcards(siteID int, state database.SiteNetworkSettings) ([]string, error) {
	domains, e := configuredSiteDomains(siteID)
	if e != nil {
		return nil, e
	}
	out := []string{}
	for _, d := range state.Wildcards {
		if domains[d] {
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out, nil
}
func loadNetworkResponse(siteID int) (siteNetworkResponse, error) {
	state, e := database.GetSiteNetwork(siteID)
	if e != nil {
		return siteNetworkResponse{}, e
	}
	if e = nginx.ValidateNetwork(state); e != nil {
		return siteNetworkResponse{}, e
	}
	domains, e := configuredSiteDomains(siteID)
	if e != nil {
		return siteNetworkResponse{}, e
	}
	if len(domains) == 0 {
		return siteNetworkResponse{}, sql.ErrNoRows
	}
	override, e := database.GetSiteVhostOverride(siteID)
	if e != nil {
		return siteNetworkResponse{}, e
	}
	pending, e := database.PendingSiteNetwork(siteID)
	if e != nil {
		return siteNetworkResponse{}, e
	}
	operation := ""
	if pending != nil {
		operation = pending.ID
	}
	wild, e := activeWildcards(siteID, state)
	if e != nil {
		return siteNetworkResponse{}, e
	}
	response := siteNetworkResponse{Managed: override == nil, Pending: pending != nil, Revision: networkRevision(state, override != nil, operation), Protection: networkProtectionResponse{Enabled: state.Enabled && override == nil && pending == nil, Username: state.Username}, Redirects: state.Redirects, Wildcards: wild}
	if response.Redirects == nil {
		response.Redirects = []database.SiteRedirect{}
	}
	if override != nil {
		response.Wildcards = []string{}
	}
	return response, nil
}
func pendingNetwork(siteID int) error {
	op, e := database.PendingSiteNetwork(siteID)
	if e != nil {
		return e
	}
	if op != nil {
		return errNetworkPending
	}
	return nil
}
func activeNetworkFeatures(siteID int) (bool, error) {
	s, e := database.GetSiteNetwork(siteID)
	if e != nil {
		return false, e
	}
	w, e := activeWildcards(siteID, s)
	return s.Enabled || len(s.Redirects) > 0 || len(w) > 0, e
}
func networkHTTPError(w http.ResponseWriter, e error) {
	code := 409
	if errors.Is(e, sql.ErrNoRows) {
		code = 404
	}
	phpJSON(w, code, map[string]string{"message": e.Error()})
}
func writeNetworkResponse(w http.ResponseWriter, siteID int) {
	v, e := loadNetworkResponse(siteID)
	if e != nil {
		networkHTTPError(w, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	phpJSON(w, 200, v)
}

type networkMutationRequest struct {
	ExpectedRevision string `json:"expected_revision"`
	Enabled          *bool  `json:"enabled,omitempty"`
	Username         string `json:"username,omitempty"`
	Password         string `json:"password,omitempty"`
	Source           string `json:"source,omitempty"`
	Destination      string `json:"destination,omitempty"`
	Status           int    `json:"status,omitempty"`
}

func (s *Server) siteNetworkRoutes() {
	s.mux.HandleFunc("GET /api/v1/sites/{id}/network", func(w http.ResponseWriter, r *http.Request) {
		id, e := parseSiteID(r)
		if e != nil {
			phpJSON(w, 400, map[string]string{"message": e.Error()})
			return
		}
		siteNginxMutationMu.Lock()
		defer siteNginxMutationMu.Unlock()
		writeNetworkResponse(w, id)
	})
	for _, route := range []string{"PUT /api/v1/sites/{id}/network/protection", "POST /api/v1/sites/{id}/network/redirects", "PUT /api/v1/sites/{id}/network/redirects/{redirect_id}", "DELETE /api/v1/sites/{id}/network/redirects/{redirect_id}"} {
		s.mux.HandleFunc(route, s.handleSiteNetworkMutation())
	}
	s.mux.HandleFunc("POST /api/v1/sites/{id}/network/recover", s.handleRecoverSiteNetwork())
}

// Existing certificate-site reservation excludes issuance/clone/delete from
// discovery through config activation, before acquiring the Nginx locks.
func (s *Server) reserveNetworkMutation(w http.ResponseWriter, id int) bool {
	if !s.beginCertificateSiteMutation(id) {
		networkHTTPError(w, errors.New("Wait for the site's certificate or domain operation to finish"))
		return false
	}
	return true
}
func (s *Server) handleSiteNetworkMutation() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, e := parseSiteID(r)
		if e != nil {
			phpJSON(w, 400, map[string]string{"message": e.Error()})
			return
		}
		var req networkMutationRequest
		if !decodeNetworkMutation(w, r, &req) {
			return
		}
		if req.ExpectedRevision == "" {
			phpJSON(w, 400, map[string]string{"message": "expected_revision is required"})
			return
		}
		if !s.reserveNetworkMutation(w, id) {
			return
		}
		defer s.endCertificateSiteMutation(id)

		domainMutationMu.Lock()
		defer domainMutationMu.Unlock()
		siteNginxMutationMu.Lock()
		defer siteNginxMutationMu.Unlock()
		current, e := loadNetworkResponse(id)
		if e != nil {
			networkHTTPError(w, e)
			return
		}
		if !current.Managed {
			networkHTTPError(w, errors.New("Restore Fluxo Default in Nginx Vhost before using managed network controls; operator-written rules remain your responsibility"))
			return
		}
		if current.Pending {
			networkHTTPError(w, errNetworkPending)
			return
		}
		if current.Revision != req.ExpectedRevision {
			networkHTTPError(w, errors.New("Network settings changed; refresh before saving"))
			return
		}
		state, e := database.GetSiteNetwork(id)
		if e != nil {
			networkHTTPError(w, e)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/protection") {
			if req.Enabled == nil {
				phpJSON(w, 400, map[string]string{"message": "enabled must be a boolean"})
				return
			}
			if *req.Enabled {
				if e = nginx.ValidateProtection(req.Username, req.Password); e != nil {
					phpJSON(w, 400, map[string]string{"message": e.Error()})
					return
				}
				state.PasswordHash, e = nginx.HashProtection(req.Password)
				req.Password = ""
				if e != nil {
					networkHTTPError(w, errors.New("Could not prepare credentials"))
					return
				}
				state.Username = req.Username
			}
			state.Enabled = *req.Enabled
		} else {
			rid := r.PathValue("redirect_id")
			index := -1
			for i, v := range state.Redirects {
				if v.ID == rid {
					index = i
				}
			}
			if rid != "" && index < 0 {
				networkHTTPError(w, sql.ErrNoRows)
				return
			}
			if r.Method == http.MethodDelete {
				state.Redirects = append(state.Redirects[:index], state.Redirects[index+1:]...)
			} else {
				if e = nginx.ValidateRedirect(req.Source, req.Destination, req.Status); e != nil {
					phpJSON(w, 400, map[string]string{"message": e.Error()})
					return
				}
				row := database.SiteRedirect{ID: rid, Source: req.Source, Destination: req.Destination, Status: req.Status}
				if row.ID == "" {
					row.ID = uuid.NewString()
					state.Redirects = append(state.Redirects, row)
				} else {
					state.Redirects[index] = row
				}
			}
		}
		if e = applyNetworkChangeLocked(r.Context(), id, state, siteRenderOptions{}, nil); e != nil {
			networkHTTPError(w, e)
			return
		}
		LogActivity(id, "settings", "Managed network settings updated")
		writeNetworkResponse(w, id)
	}
}

// The Nginx activation is preceded by durable evidence containing only the last
// confirmed generated configuration. Application state changes and completion
// are one SQLite transaction. An interrupted operation never silently replays;
// explicit recovery reinstalls the last confirmed configuration.
func applyNetworkChangeLocked(ctx context.Context, id int, next database.SiteNetworkSettings, options siteRenderOptions, persist func(*sql.Tx) error) error {
	if e := nginx.ValidateNetwork(next); e != nil {
		return e
	}
	if e := pendingNetwork(id); e != nil {
		return e
	}
	current, e := loadSiteVhostState(id)
	if e != nil {
		return e
	}
	if current.Customized {
		if !options.MetadataOnly {
			return errors.New("Restore Fluxo Default before changing managed network settings")
		}
		tx, err := database.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err = database.SaveSiteNetworkTx(tx, id, next); err == nil && persist != nil {
			err = persist(tx)
		}
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	if e = ensureSiteAuthFile(id, next); e != nil {
		return e
	}
	options.Network = &next
	rendered, e := renderNetworkVhost(id, options)
	if e != nil {
		return e
	}
	response, e := loadNetworkResponse(id)
	if e != nil {
		return e
	}
	operation := uuid.NewString()
	if e = database.BeginSiteNetworkOperation(operation, id, current.Config, response.Revision); e != nil {
		return e
	}
	restore := func(cause error) error {
		if er := installNetworkVhost(context.Background(), rendered.ConfigName, current.Config); er != nil {
			return fmt.Errorf("%w; rollback needs review in Network: %v", cause, er)
		}
		if er := database.FinishSiteNetworkOperation(operation, "rolled_back"); er != nil {
			return errors.Join(cause, er)
		}
		return cause
	}
	if e = installNetworkVhost(ctx, rendered.ConfigName, rendered.Content); e != nil {
		return restore(e)
	}
	tx, e := database.DB.BeginTx(ctx, nil)
	if e != nil {
		return restore(e)
	}
	e = database.SaveSiteNetworkTx(tx, id, next)
	if e == nil && persist != nil {
		e = persist(tx)
	}
	if e == nil {
		var result sql.Result
		result, e = tx.Exec("UPDATE site_network_operations SET phase='committed' WHERE id=? AND phase='pending'", operation)
		if e == nil {
			var n int64
			n, e = result.RowsAffected()
			if e == nil && n != 1 {
				e = errors.New("Network operation changed during completion")
			}
		}
	}
	if e != nil {
		tx.Rollback()
		return restore(e)
	}
	if e = tx.Commit(); e != nil {
		tx.Rollback()
		return restore(e)
	}
	return nil
}
func (s *Server) handleRecoverSiteNetwork() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, e := parseSiteID(r)
		if e != nil {
			phpJSON(w, 400, map[string]string{"message": e.Error()})
			return
		}
		var req struct {
			ExpectedRevision string `json:"expected_revision"`
		}
		if !decodePHPRequest(w, r, &req) {
			return
		}
		if !s.reserveNetworkMutation(w, id) {
			return
		}
		defer s.endCertificateSiteMutation(id)

		domainMutationMu.Lock()
		defer domainMutationMu.Unlock()
		siteNginxMutationMu.Lock()
		defer siteNginxMutationMu.Unlock()
		v, e := loadNetworkResponse(id)
		if e != nil {
			networkHTTPError(w, e)
			return
		}
		if !v.Managed || req.ExpectedRevision != v.Revision {
			networkHTTPError(w, errors.New("Refresh the network state before restoring"))
			return
		}
		op, e := database.PendingSiteNetwork(id)
		if e != nil {
			networkHTTPError(w, e)
			return
		}
		if op == nil {
			writeNetworkResponse(w, id)
			return
		}
		name, e := siteVhostConfigName(id)
		if e != nil {
			networkHTTPError(w, e)
			return
		}
		if e = installNetworkVhost(r.Context(), name, op.PreviousConfig); e == nil {
			e = database.FinishSiteNetworkOperation(op.ID, "rolled_back")
		}
		if e != nil {
			networkHTTPError(w, e)
			return
		}
		LogActivity(id, "settings", "Interrupted network change restored to last confirmed configuration")
		writeNetworkResponse(w, id)
	}
}

// read-only admission for other mutations; recovery and GET remain available.
func (s *Server) networkMutationGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" || !strings.HasPrefix(r.URL.Path, "/api/v1/sites/") || strings.HasSuffix(r.URL.Path, "/network/recover") {
			next.ServeHTTP(w, r)
			return
		}
		raw := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/sites/"), "/")[0]
		id, e := strconv.Atoi(raw)
		if e == nil && id > 0 {
			if e = pendingNetwork(id); e != nil {
				networkHTTPError(w, e)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func decodeNetworkMutation(w http.ResponseWriter, r *http.Request, req *networkMutationRequest) bool {
	if strings.HasSuffix(r.URL.Path, "/protection") {
		var v struct {
			ExpectedRevision string `json:"expected_revision"`
			Enabled          *bool  `json:"enabled"`
			Username         string `json:"username"`
			Password         string `json:"password"`
		}
		if !decodePHPRequest(w, r, &v) {
			return false
		}
		req.ExpectedRevision = v.ExpectedRevision
		req.Enabled = v.Enabled
		req.Username = v.Username
		req.Password = v.Password
		return true
	}
	if r.Method == http.MethodDelete {
		var v struct {
			ExpectedRevision string `json:"expected_revision"`
		}
		if !decodePHPRequest(w, r, &v) {
			return false
		}
		req.ExpectedRevision = v.ExpectedRevision
		return true
	}
	var v struct {
		ExpectedRevision string `json:"expected_revision"`
		Source           string `json:"source"`
		Destination      string `json:"destination"`
		Status           int    `json:"status"`
	}
	if !decodePHPRequest(w, r, &v) {
		return false
	}
	req.ExpectedRevision = v.ExpectedRevision
	req.Source = v.Source
	req.Destination = v.Destination
	req.Status = v.Status
	return true
}
