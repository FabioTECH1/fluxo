package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fluxo/internal/database"
	"math/big"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWildcardPanelExactRouteRequiredWithoutBlanketParentRefusal(t *testing.T) {
	for _, v := range []struct {
		parent, panel    string
		enabled, allowed bool
	}{
		{"example.com", "panel.example.com", true, true},
		{"example.com", "PANEL.Example.COM.", true, true},
		{"example.com", "panel.example.com", false, false},
		{"example.com", "example.com", true, false},
		{"example.com", "other.test", false, true},
		{"example.com", "", false, true},
	} {
		t.Run(v.panel, func(t *testing.T) {
			e := wildcardPanelRoute(v.parent, v.panel, func(name string) (bool, string) {
				if name != "panel.example.com" {
					t.Fatal(name)
				}
				return v.enabled, "missing/unmanaged"
			})
			if (e == nil) != v.allowed {
				t.Fatal(e, v)
			}
		})
	}
}
func TestPanelMoveRemovalRefuseWildcardFallthroughAndCustomClaim(t *testing.T) {
	h := setupNetworkAPI(t)
	state, e := database.GetSiteNetwork(h.id)
	if e != nil {
		t.Fatal(e)
	}
	state.Wildcards = []string{"example.com"}
	if e = savePolishNetwork(h.id, state); e != nil {
		t.Fatal(e)
	}
	for _, next := range []string{"", "new.other.test"} {
		if e = guardPanelWildcardDeparture("panel.example.com", next); e == nil {
			t.Fatal("allowed reserved fallback", next)
		}
	}
	if e = guardPanelWildcardDeparture("panel.example.com", "panel.example.com"); e != nil {
		t.Fatal(e)
	}
	if e = guardPanelWildcardDeparture("elsewhere.test", "new.other.test"); e != nil {
		t.Fatal(e)
	}
	if _, e = database.DB.Exec("UPDATE panel_domain SET domain='panel.example.com' WHERE id=1"); e != nil {
		t.Fatal(e)
	}
	if e = validateCustomNetworkClaims(h.id, "server { server_name PANEL.example.com; }"); e == nil {
		t.Fatal("custom exact panel collision")
	}
	state.Wildcards = []string{}
	if e = savePolishNetwork(h.id, state); e != nil {
		t.Fatal(e)
	}
	if e = guardPanelWildcardDeparture("panel.example.com", ""); e != nil {
		t.Fatal("disabled scope still blocks", e)
	}
}
func TestVisibleActivityKeepsAuditRowsAndChangeFailuresInBothScopes(t *testing.T) {
	setupSiteVhostTest(t)
	kinds := []struct {
		typ, summary string
		visible      bool
	}{
		{"php_update", "check PHP 8.4: failed — diagnostic", false},
		{"php_update", "repository_review PHP : succeeded — review", false},
		{"php_update", "apply PHP 8.4: failed — recovery", true},
		{"node_update", "Node.js repair completed.", true},
		{"login_failed", "Failed login attempt", true},
		{"settings", "Edited configuration", true},
	}
	for _, v := range kinds {
		LogActivity(1, v.typ, v.summary)
	}
	s := NewServer(nil, t.TempDir(), false)
	for _, suffix := range []string{"", "?site_id=1"} {
		w := httptest.NewRecorder()
		s.handleGetActivity()(w, httptest.NewRequest("GET", "/api/v1/system/activity"+suffix, nil))
		var got struct {
			Items []struct{ Summary string }
			Total int
		}
		if e := json.Unmarshal(w.Body.Bytes(), &got); e != nil {
			t.Fatal(e)
		}
		if got.Total != 4 || len(got.Items) != 4 {
			t.Fatal(w.Body.String())
		}
		for _, i := range got.Items {
			if strings.HasPrefix(i.Summary, "check PHP") || strings.HasPrefix(i.Summary, "repository_review PHP") {
				t.Fatal(i)
			}
		}
	}
	var n int
	database.DB.QueryRow("SELECT count(*) FROM activity").Scan(&n)
	if n != 6 {
		t.Fatal("deleted audit history", n)
	}
}

func savePolishNetwork(id int, state database.SiteNetworkSettings) error {
	tx, err := database.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = database.SaveSiteNetworkTx(tx, id, state); err != nil {
		return err
	}
	return tx.Commit()
}

func TestCoveredPanelHTTPRemovalAndMoveReturnConflictBeforeAnyMutation(t *testing.T) {
	oldIssue, oldRemove := issuePanelCustomCertificate, removePanelDomainConfig
	t.Cleanup(func() { issuePanelCustomCertificate = oldIssue; removePanelDomainConfig = oldRemove })
	effects := 0
	issuePanelCustomCertificate = func(string, string, string) (string, string, error) {
		effects++
		return "", "", errors.New("unexpected certificate mutation")
	}
	removePanelDomainConfig = func(context.Context) (func(context.Context) error, error) {
		effects++
		return nil, errors.New("unexpected Nginx mutation")
	}
	h := setupNetworkAPI(t)
	state, e := database.GetSiteNetwork(h.id)
	if e != nil {
		t.Fatal(e)
	}
	state.Wildcards = []string{"example.com"}
	if e = savePolishNetwork(h.id, state); e != nil {
		t.Fatal(e)
	}
	if _, e = database.DB.Exec("UPDATE panel_domain SET domain='panel.example.com',ssl_provider='custom',cert_path='/original.crt',key_path='/original.key' WHERE id=1"); e != nil {
		t.Fatal(e)
	}
	pub, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	cert, e := x509.CreateCertificate(rand.Reader, &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"new.example.net"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}, &x509.Certificate{SerialNumber: big.NewInt(1)}, pub, key)
	if e != nil {
		t.Fatal(e)
	}
	keyDER, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		t.Fatal(e)
	}
	payload, e := json.Marshal(panelCustomCertificateRequest{Domain: "new.example.net", Certificate: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert})), PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))})
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range []struct{ method, path, body string }{
		{"DELETE", "/api/v1/settings/panel-domain", ""},
		{"POST", "/api/v1/settings/panel-domain/custom", string(payload)},
	} {
		w := httptest.NewRecorder()
		h.s.mux.ServeHTTP(w, httptest.NewRequest(v.method, v.path, strings.NewReader(v.body)))
		if w.Code != 409 || !strings.Contains(w.Body.String(), "Disable wildcard") {
			t.Fatal(w.Code, w.Body.String())
		}
		panel, e := database.GetPanelDomainConfig()
		if e != nil || panel.Domain != "panel.example.com" || panel.CertPath != "/original.crt" || h.installs != 0 || effects != 0 {
			t.Fatal(panel, h.installs, e)
		}
	}
}
func TestNewPanelExactCustomClaimIsRejectedButManagedWildcardAllowsReservation(t *testing.T) {
	h := setupNetworkAPI(t)
	state, e := database.GetSiteNetwork(h.id)
	if e != nil {
		t.Fatal(e)
	}
	state.Wildcards = []string{"example.com"}
	if e = savePolishNetwork(h.id, state); e != nil {
		t.Fatal(e)
	}
	if e = ensurePanelDomainAvailable("panel.example.com"); e != nil {
		t.Fatal("initial panel must be able to connect under existing wildcard", e)
	}
	if e = database.SaveSiteVhostOverride(h.id, "server { listen 443 ssl; server_name PANEL.EXAMPLE.COM.; }"); e != nil {
		t.Fatal(e)
	}
	if e = ensurePanelDomainAvailable("panel.example.com"); e == nil {
		t.Fatal("custom exact-host collision allowed")
	}
}

func TestPanelRoutingRefusesUnfinishedNetworkRatherThanInferringProposedNames(t *testing.T) {
	h := setupNetworkAPI(t)
	if e := database.BeginSiteNetworkOperation("pending-polish", h.id, networkTestConfig, "observed-revision"); e != nil {
		t.Fatal(e)
	}
	if e := ensurePanelDomainAvailable("new.example.net"); e == nil || !errors.Is(e, errPanelRoutingConflict) {
		t.Fatal("pending routing accepted", e)
	}
	if e := database.FinishSiteNetworkOperation("pending-polish", "rolled_back"); e != nil {
		t.Fatal(e)
	}
	if e := ensurePanelDomainAvailable("new.example.net"); e != nil {
		t.Fatal("confirmed recovery did not restore availability", e)
	}
}
