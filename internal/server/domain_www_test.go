package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"fluxo/internal/database"
)

func TestNormalizeWWWRedirectDefaultsNewDomains(t *testing.T) {
	behavior, err := normalizeWWWRedirect("example.com", "", wwwRedirectFrom)
	if err != nil || behavior != wwwRedirectFrom {
		t.Fatalf("default behavior = %q, %v", behavior, err)
	}
}

func TestNormalizeWWWRedirectRejectsNestedWWW(t *testing.T) {
	if _, err := normalizeWWWRedirect("www.example.com", wwwRedirectFrom, wwwRedirectFrom); err == nil {
		t.Fatal("expected a www-prefixed domain to reject an additional redirect")
	}
	behavior, err := normalizeWWWRedirect("www.example.com", "", wwwRedirectFrom)
	if err != nil || behavior != wwwRedirectNone {
		t.Fatalf("www-prefixed exact-host behavior = %q, %v", behavior, err)
	}
}

func TestConfiguredDomainRouting(t *testing.T) {
	tests := []struct {
		behavior                    string
		application, source, target string
	}{
		{wwwRedirectNone, "example.com", "", ""},
		{wwwRedirectFrom, "example.com", "www.example.com", "example.com"},
		{wwwRedirectTo, "www.example.com", "example.com", "www.example.com"},
	}
	for _, test := range tests {
		application, source, target := configuredDomainRouting("example.com", test.behavior)
		if application != test.application || source != test.source || target != test.target {
			t.Fatalf("routing for %s = (%q, %q, %q)", test.behavior, application, source, target)
		}
	}
}

func TestGeneratedWWWHostnameIsReserved(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { _ = database.DB.Close() })
	if _, err := database.DB.Exec(
		"INSERT INTO sites (domain, path, www_redirect) VALUES (?, ?, ?)",
		"example.com", "/home/fluxo/example.com", wwwRedirectFrom,
	); err != nil {
		t.Fatalf("create site: %v", err)
	}
	inUse, err := domainInUse("www.example.com", false)
	if err != nil || !inUse {
		t.Fatalf("generated www hostname reservation = %v, %v", inUse, err)
	}
}

func TestWWWRedirectRejectsExplicitHostnameConflict(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { _ = database.DB.Close() })
	result, err := database.DB.Exec(
		"INSERT INTO sites (domain, path) VALUES (?, ?)",
		"example.com", "/home/fluxo/example.com",
	)
	if err != nil {
		t.Fatalf("create site: %v", err)
	}
	siteID, _ := result.LastInsertId()
	if _, err := database.DB.Exec(
		"INSERT INTO domain_aliases (site_id, domain) VALUES (?, ?)", siteID, "www.example.com",
	); err != nil {
		t.Fatalf("create explicit www alias: %v", err)
	}
	if err := ensureWWWRouteAvailable(int(siteID), 0, "example.com", wwwRedirectFrom); err == nil {
		t.Fatal("expected the generated www hostname to conflict with the explicit alias")
	}
}

func TestWWWRedirectRequiresExistingCertificateCoverage(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { _ = database.DB.Close() })
	result, err := database.DB.Exec(
		"INSERT INTO sites (domain, path, www_redirect) VALUES (?, ?, ?)",
		"example.com", "/home/fluxo/example.com", wwwRedirectNone,
	)
	if err != nil {
		t.Fatal(err)
	}
	siteID64, _ := result.LastInsertId()
	siteID := int(siteID64)
	certificateID, err := database.CreateCertificate(
		siteID, "example.com", "custom",
		"/etc/nginx/ssl/example.com/server.crt", "/etc/nginx/ssl/example.com/server.key", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SetActiveCertificateWithBindings(siteID, int(certificateID), nil); err != nil {
		t.Fatal(err)
	}
	if err := validateWWWRedirectCertificateCoverageWith(siteID, "example.com", wwwRedirectNone, func(_ database.Certificate, hostnames []string) bool {
		return len(hostnames) == 1 && hostnames[0] == "example.com"
	}); err != nil {
		t.Fatalf("exact-host behavior rejected: %v", err)
	}
	if err := validateWWWRedirectCertificateCoverageWith(siteID, "example.com", wwwRedirectFrom, func(_ database.Certificate, _ []string) bool {
		return false
	}); !errors.Is(err, errWWWRedirectCertificateCoverage) {
		t.Fatalf("uncovered WWW behavior error = %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/sites/1/domains/0", strings.NewReader(`{"www_redirect":"from_www"}`))
	request.SetPathValue("id", strconv.FormatInt(siteID64, 10))
	request.SetPathValue("domain_id", "0")
	response := httptest.NewRecorder()
	(&Server{}).handleUpdateDomainConfiguration().ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("update status = %d, body = %s", response.Code, response.Body.String())
	}
	var behavior string
	if err := database.DB.QueryRow("SELECT www_redirect FROM sites WHERE id = ?", siteID).Scan(&behavior); err != nil {
		t.Fatal(err)
	}
	if behavior != wwwRedirectNone {
		t.Fatalf("rejected update changed behavior to %q", behavior)
	}
}

func TestWWWRedirectAcceptsCertificateCoveringBothHostnames(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { _ = database.DB.Close() })
	result, err := database.DB.Exec(
		"INSERT INTO sites (domain, path, www_redirect) VALUES (?, ?, ?)",
		"example.com", "/home/fluxo/example.com", wwwRedirectNone,
	)
	if err != nil {
		t.Fatal(err)
	}
	siteID64, _ := result.LastInsertId()
	siteID := int(siteID64)
	certificateID, err := database.CreateCertificate(
		siteID, "example.com", "custom",
		"/etc/nginx/ssl/example.com/server.crt", "/etc/nginx/ssl/example.com/server.key", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SetActiveCertificateWithBindings(siteID, int(certificateID), nil); err != nil {
		t.Fatal(err)
	}
	if err := validateWWWRedirectCertificateCoverageWith(siteID, "example.com", wwwRedirectTo, func(_ database.Certificate, hostnames []string) bool {
		return len(hostnames) == 2 && hostnames[0] == "example.com" && hostnames[1] == "www.example.com"
	}); err != nil {
		t.Fatalf("covered WWW behavior rejected: %v", err)
	}
}

func TestRestoreDeletedDomainAliasPreservesWWWBehavior(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { _ = database.DB.Close() })
	result, err := database.DB.Exec("INSERT INTO sites (domain, path) VALUES (?, ?)", "example.com", "/home/fluxo/example.com")
	if err != nil {
		t.Fatal(err)
	}
	siteID64, _ := result.LastInsertId()
	alias, err := database.DB.Exec(
		"INSERT INTO domain_aliases (site_id, domain, ssl_disabled, www_redirect) VALUES (?, ?, ?, ?)",
		siteID64, "alias.example.com", true, wwwRedirectTo,
	)
	if err != nil {
		t.Fatal(err)
	}
	aliasID64, _ := alias.LastInsertId()
	if _, err := database.DB.Exec("DELETE FROM domain_aliases WHERE id = ?", aliasID64); err != nil {
		t.Fatal(err)
	}
	if err := restoreDeletedDomainAlias(int(aliasID64), int(siteID64), "alias.example.com", true, wwwRedirectTo); err != nil {
		t.Fatal(err)
	}
	var disabled bool
	var behavior string
	if err := database.DB.QueryRow(
		"SELECT ssl_disabled, www_redirect FROM domain_aliases WHERE id = ?", aliasID64,
	).Scan(&disabled, &behavior); err != nil {
		t.Fatal(err)
	}
	if !disabled || behavior != wwwRedirectTo {
		t.Fatalf("restored alias = disabled:%v behavior:%q", disabled, behavior)
	}
}
