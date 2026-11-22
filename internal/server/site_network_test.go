package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"fluxo/internal/database"
	"fluxo/internal/services/nginx"
	"golang.org/x/crypto/bcrypt"
)

const networkTestConfig = `server {
 listen 80;
 server_name example.com;
 root /srv/public;
 location ^~ /.well-known/acme-challenge/ { allow all; }
 location / { try_files $uri /index.php; }
}`

type networkHarness struct {
	id       int
	s        *Server
	live     string
	failNext bool
	failAll  bool
	installs int
}

func setupNetworkAPI(t *testing.T) *networkHarness {
	t.Helper()
	id := setupSiteVhostTest(t)
	if _, e := database.DB.Exec(database.SiteNetworkSchema); e != nil {
		t.Fatal(e)
	}
	oldRender, oldInstall, oldAuth := renderNetworkVhost, installNetworkVhost, ensureSiteAuthFile
	h := &networkHarness{id: id, s: NewServer(nil, t.TempDir(), false), live: networkTestConfig}
	renderNetworkVhost = func(siteID int, o siteRenderOptions) (renderedSiteVhost, error) {
		state, e := database.GetSiteNetwork(siteID)
		if e != nil {
			return renderedSiteVhost{}, e
		}
		if o.Network != nil {
			state = *o.Network
		}
		config := networkTestConfig
		for _, d := range state.Wildcards {
			part, e := nginx.RenderWildcard(d, "example.com", "/srv/public", "8.4", "example.com", "php", 0, "", "")
			if e != nil {
				return renderedSiteVhost{}, e
			}
			config += part
		}
		for _, d := range o.ExtraDomains {
			config += "\n# Proposed canonical domain " + d.Domain + "\n"
		}
		config, e = nginx.ApplyNetworkSettings(config, siteID, state)
		return renderedSiteVhost{ConfigName: "example.com", Content: config}, e
	}
	renderSiteVhostDefault = func(siteID int) (renderedSiteVhost, error) { return renderNetworkVhost(siteID, siteRenderOptions{}) }
	installNetworkVhost = func(_ context.Context, _ string, raw string) error {
		h.installs++
		if h.failAll {
			return errors.New("reload failed")
		}
		if h.failNext {
			h.failNext = false
			return errors.New("nginx test failed")
		}
		h.live = raw
		return nil
	}
	installSiteVhost = func(ctx context.Context, n, c string) error { return installNetworkVhost(ctx, n, c) }
	ensureSiteAuthFile = func(int, database.SiteNetworkSettings) error { return nil }
	t.Cleanup(func() { renderNetworkVhost = oldRender; installNetworkVhost = oldInstall; ensureSiteAuthFile = oldAuth })
	return h
}
func (h *networkHarness) request(method, path string, body any) *httptest.ResponseRecorder {
	r := vhostRequest(method, fmt.Sprintf("/api/v1/sites/%d%s", h.id, path), h.id, body)
	w := httptest.NewRecorder()
	h.s.mux.ServeHTTP(w, r)
	return w
}
func (h *networkHarness) state(t *testing.T) siteNetworkResponse {
	t.Helper()
	w := h.request("GET", "/network", nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var s siteNetworkResponse
	if e := json.Unmarshal(w.Body.Bytes(), &s); e != nil {
		t.Fatal(e)
	}
	return s
}
func TestSiteNetworkProtectionPrivacyAndCustomConflict(t *testing.T) {
	h := setupNetworkAPI(t)
	state := h.state(t)
	w := h.request("PUT", "/network/protection", map[string]any{"expected_revision": state.Revision, "enabled": true, "username": "staging", "password": "a-private-test-password"})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "a-private-test-password") || strings.Contains(w.Body.String(), "password_hash") {
		t.Fatal("returned saved credentials")
	}
	saved, e := database.GetSiteNetwork(h.id)
	if e != nil || !saved.Enabled || bcrypt.CompareHashAndPassword([]byte(saved.PasswordHash), []byte("a-private-test-password")) != nil {
		t.Fatal("invalid credential storage", e)
	}
	current, e := loadSiteVhostState(h.id)
	if e != nil {
		t.Fatal(e)
	}
	w = h.request("PUT", "/vhost", updateSiteVhostRequest{Config: "server { server_name custom.test; return 200; }", ExpectedRevision: current.Revision})
	if w.Code != 409 {
		t.Fatal("allowed active settings to become custom", w.Code, w.Body.String())
	}
	if override, _ := database.GetSiteVhostOverride(h.id); override != nil {
		t.Fatal("persisted forbidden custom override")
	}
	state = h.state(t)
	w = h.request("PUT", "/network/protection", map[string]any{"expected_revision": state.Revision, "enabled": false})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	current, _ = loadSiteVhostState(h.id)
	w = h.request("PUT", "/vhost", updateSiteVhostRequest{Config: "server { server_name custom.test; return 200; }", ExpectedRevision: current.Revision})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	state = h.state(t)
	if state.Managed || state.Protection.Enabled {
		t.Fatal("claimed operator vhost was managed")
	}
	w = h.request("PUT", "/network/protection", map[string]any{"expected_revision": state.Revision, "enabled": true, "username": "staging", "password": "another-private-test"})
	if w.Code != 409 {
		t.Fatal("bypassed managed-only guard", w.Code)
	}
	current, _ = loadSiteVhostState(h.id)
	w = h.request("POST", "/vhost/restore", restoreSiteVhostRequest{ExpectedRevision: current.Revision})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	state = h.state(t)
	if !state.Managed || state.Protection.Enabled {
		t.Fatal("restoring default reactivated disabled protection")
	}
}
func TestSiteNetworkRedirectCRUDAndStaleRefusal(t *testing.T) {
	h := setupNetworkAPI(t)
	initial := h.state(t)
	w := h.request("POST", "/network/redirects", map[string]any{"expected_revision": initial.Revision, "source": "/old", "destination": "/new?q=1", "status": 302})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	state := h.state(t)
	if len(state.Redirects) != 1 || state.Redirects[0].Status != 302 || !strings.Contains(h.live, `return 302 "/new?q=1"`) {
		t.Fatal(state, h.live)
	}
	w = h.request("POST", "/network/redirects", map[string]any{"expected_revision": initial.Revision, "source": "/another", "destination": "/after", "status": 301})
	if w.Code != 409 {
		t.Fatal("lost-update guard missing", w.Code)
	}
	rule := state.Redirects[0]
	w = h.request("PUT", "/network/redirects/"+rule.ID, map[string]any{"expected_revision": state.Revision, "source": "/before", "destination": "https://example.org/after", "status": 301})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	state = h.state(t)
	w = h.request("DELETE", "/network/redirects/"+rule.ID, map[string]any{"expected_revision": state.Revision})
	if w.Code != 200 || len(h.state(t).Redirects) != 0 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestSiteNetworkActivationAndPersistenceRollback(t *testing.T) {
	for _, kind := range []string{"nginx-test", "database-write", "rollback-unconfirmed"} {
		t.Run(kind, func(t *testing.T) {
			h := setupNetworkAPI(t)
			state := h.state(t)
			switch kind {
			case "nginx-test":
				h.failNext = true
			case "database-write":
				_, e := database.DB.Exec("CREATE TRIGGER refuse_network BEFORE INSERT ON site_network_settings BEGIN SELECT RAISE(FAIL,'simulated write failure'); END")
				if e != nil {
					t.Fatal(e)
				}
			case "rollback-unconfirmed":
				h.failAll = true
			}
			w := h.request("POST", "/network/redirects", map[string]any{"expected_revision": state.Revision, "source": "/old", "destination": "/new", "status": 301})
			if w.Code == 200 {
				t.Fatal("claimed failed activation")
			}
			saved, _ := database.GetSiteNetwork(h.id)
			if len(saved.Redirects) != 0 {
				t.Fatal("persisted failed mutation")
			}
			after := h.state(t)
			if kind == "rollback-unconfirmed" {
				if !after.Pending || after.Protection.Enabled {
					t.Fatal("hid uncertain operation")
				}
				h.failAll = false
				w = h.request("POST", "/network/recover", map[string]any{"expected_revision": after.Revision})
				if w.Code != 200 || h.state(t).Pending {
					t.Fatal(w.Code, w.Body.String())
				}
			} else if after.Pending {
				t.Fatal("confirmed rollback stayed pending")
			}
			var operations int
			database.DB.QueryRow("SELECT count(*) FROM site_network_operations").Scan(&operations)
			if operations != 1 {
				t.Fatal("erased operation evidence")
			}
		})
	}
}
func TestSiteNetworkPendingBlocksOtherMutationsAndExplicitRecovery(t *testing.T) {
	h := setupNetworkAPI(t)
	if e := database.BeginSiteNetworkOperation("interrupted", h.id, networkTestConfig, "old-revision"); e != nil {
		t.Fatal(e)
	}
	state := h.state(t)
	if !state.Pending {
		t.Fatal("did not report interruption")
	}
	called := 0
	handler := h.s.networkMutationGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++; w.WriteHeader(204) }))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/sites/%d", h.id), nil))
	if w.Code != 409 || called != 0 {
		t.Fatal("deleted site during interrupted change")
	}
	w = h.request("POST", "/network/recover", map[string]any{"expected_revision": "stale"})
	if w.Code != 409 {
		t.Fatal("recovered stale request")
	}
	w = h.request("POST", "/network/recover", map[string]any{"expected_revision": state.Revision})
	if w.Code != 200 || h.state(t).Pending || h.live != networkTestConfig {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestSiteNetworkConcurrentCustomAndProtectionMutations(t *testing.T) {
	h := setupNetworkAPI(t)
	state := h.state(t)
	vhost, _ := loadSiteVhostState(h.id)
	var a, b *httptest.ResponseRecorder
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		a = h.request("PUT", "/network/protection", map[string]any{"expected_revision": state.Revision, "enabled": true, "username": "staging", "password": "a-private-password"})
	}()
	go func() {
		defer wg.Done()
		<-start
		b = h.request("PUT", "/vhost", updateSiteVhostRequest{Config: "server { server_name custom.test; return 200; }", ExpectedRevision: vhost.Revision})
	}()
	close(start)
	wg.Wait()
	if (a.Code == 200) == (b.Code == 200) {
		t.Fatal("both or neither conflicting mutations applied", a.Code, b.Code)
	}
	s, _ := database.GetSiteNetwork(h.id)
	custom, _ := database.GetSiteVhostOverride(h.id)
	if custom != nil && s.Enabled {
		t.Fatal("raced into custom plus active protection")
	}
}
func TestSiteNetworkWildcardCanonicalizationOwnershipAndCertificatePending(t *testing.T) {
	h := setupNetworkAPI(t)
	w := h.request("POST", "/domains", map[string]any{"domain": "*.Alias.TEST", "www_redirect": "none", "wildcard": true})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		ID     int    `json:"id"`
		Domain string `json:"domain"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	if result.Domain != "alias.test" {
		t.Fatal("stored duplicate wildcard representation")
	}
	state := h.state(t)
	if len(state.Wildcards) != 1 || state.Wildcards[0] != "alias.test" || !strings.Contains(h.live, "ssl_reject_handshake on;") {
		t.Fatal(state)
	}
	w = h.request("GET", "/domains", nil)
	var domains []DomainItem
	json.Unmarshal(w.Body.Bytes(), &domains)
	var domain DomainItem
	for _, d := range domains {
		if d.ID == result.ID {
			domain = d
		}
	}
	if !domain.Wildcard || domain.WildcardSSLActive || domain.WildcardSSLState != "pending" {
		t.Fatal("invented certificate coverage", domain)
	}
	w = h.request("PUT", fmt.Sprintf("/domains/%d", result.ID), map[string]any{"expected_revision": "stale", "www_redirect": "none", "wildcard": false})
	if w.Code != 409 {
		t.Fatal("domain lost-write guard missing", w.Code)
	}
	if _, e := database.DB.Exec("INSERT INTO sites(id,domain,path)VALUES(99,'other.test','/home/fluxo/other.test')"); e != nil {
		t.Fatal(e)
	}
	other := &networkHarness{id: 99, s: h.s}
	w = other.request("POST", "/domains", map[string]any{"domain": "sub.alias.test", "www_redirect": "none", "wildcard": true})
	if w.Code != 409 {
		t.Fatal("overlapping wildcard accepted", w.Code, w.Body.String())
	}
	w = other.request("POST", "/domains", map[string]any{"domain": "exact.alias.test", "www_redirect": "none", "wildcard": false})
	if w.Code != 200 {
		t.Fatal("explicit exact site lost priority", w.Code, w.Body.String())
	}
	w = h.request("DELETE", fmt.Sprintf("/domains/%d", result.ID), nil)
	if w.Code != 204 || len(h.state(t).Wildcards) != 0 {
		t.Fatal("wildcard deletion retained activation", w.Code, w.Body.String())
	}
}
func TestSiteNetworkAuthenticationAndStrictPayload(t *testing.T) {
	h := setupNetworkAPI(t)
	w := httptest.NewRecorder()
	h.s.ServeHTTP(w, vhostRequest("PUT", fmt.Sprintf("/api/v1/sites/%d/network/protection", h.id), h.id, map[string]any{"enabled": true}))
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	state := h.state(t)
	w = h.request("PUT", "/network/protection", map[string]any{"expected_revision": state.Revision, "enabled": false, "source": "/ignored"})
	if w.Code != 400 || h.installs != 0 {
		t.Fatal("accepted irrelevant payload", w.Code)
	}
	w = h.request("POST", "/network/redirects", map[string]any{"expected_revision": state.Revision, "source": "/.well-known/acme-challenge/token", "destination": "/new", "status": 301})
	if w.Code != 400 || h.installs != 0 {
		t.Fatal("broke ACME exception", w.Code)
	}
}
