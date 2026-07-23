package database

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitDBAddsBindingOriginToExistingDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	legacyDB, err := sql.Open("sqlite", sqliteConnectionString(dbPath))
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	if _, err := legacyDB.Exec(`
		CREATE TABLE domain_aliases (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			site_id INTEGER NOT NULL,
			domain TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE certificate_domain_bindings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			site_id INTEGER NOT NULL,
			domain TEXT NOT NULL,
			certificate_id INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(site_id, domain)
		)`); err != nil {
		_ = legacyDB.Close()
		t.Fatalf("create legacy binding table: %v", err)
	}
	_ = legacyDB.Close()

	if err := InitDB(dbPath); err != nil {
		t.Fatalf("upgrade database: %v", err)
	}
	t.Cleanup(func() { _ = DB.Close() })

	rows, err := DB.Query("PRAGMA table_info(certificate_domain_bindings)")
	if err != nil {
		t.Fatalf("inspect binding table: %v", err)
	}
	defer rows.Close()
	foundOrigin := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("read binding column: %v", err)
		}
		if name == "origin" {
			foundOrigin = true
		}
	}
	if !foundOrigin {
		t.Fatal("expected origin column after database upgrade")
	}
	aliasRows, err := DB.Query("PRAGMA table_info(domain_aliases)")
	if err != nil {
		t.Fatalf("inspect domain alias table: %v", err)
	}
	defer aliasRows.Close()
	foundSSLDisabled := false
	for aliasRows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := aliasRows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("read domain alias column: %v", err)
		}
		if name == "ssl_disabled" {
			foundSSLDisabled = true
		}
	}
	if !foundSSLDisabled {
		t.Fatal("expected ssl_disabled column after database upgrade")
	}
}

func TestCertificateDomainBindingLifecycle(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { _ = DB.Close() })

	siteResult, err := DB.Exec("INSERT INTO sites (domain, path) VALUES (?, ?)", "example.com", "/srv/example.com")
	if err != nil {
		t.Fatalf("create site: %v", err)
	}
	siteID64, err := siteResult.LastInsertId()
	if err != nil {
		t.Fatalf("read site ID: %v", err)
	}
	siteID := int(siteID64)
	if _, err := DB.Exec("INSERT INTO domain_aliases (site_id, domain) VALUES (?, ?)", siteID, "app.example.com"); err != nil {
		t.Fatalf("create alias: %v", err)
	}

	certID64, err := CreateCertificate(siteID, "app.example.com", "custom", "/tmp/cert.pem", "/tmp/key.pem", "")
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certID := int(certID64)
	if err := SetCertificateDomainBinding(siteID, "app.example.com", certID); err != nil {
		t.Fatalf("bind certificate: %v", err)
	}

	bindings, err := GetCertificateDomainBindings(siteID)
	if err != nil || len(bindings) != 1 || bindings[0].CertificateID != certID {
		t.Fatalf("unexpected bindings: %#v, %v", bindings, err)
	}
	if bindings[0].Origin != CertificateBindingOriginManual {
		t.Fatalf("expected manual binding origin, got %q", bindings[0].Origin)
	}
	if err := SetDomainSSLDisabled(siteID, "app.example.com", true); err != nil {
		t.Fatalf("disable domain SSL: %v", err)
	}
	if binding, err := GetCertificateDomainBinding(siteID, "app.example.com"); err != nil || binding != nil {
		t.Fatalf("expected disabling SSL to remove the binding: %#v, %v", binding, err)
	}
	if disabled, err := IsDomainSSLDisabled(siteID, "app.example.com"); err != nil || !disabled {
		t.Fatalf("expected explicit disabled state: %v, %v", disabled, err)
	}
	if err := SetCertificateDomainBinding(siteID, "app.example.com", certID); err != nil {
		t.Fatalf("reactivate domain certificate: %v", err)
	}
	if disabled, err := IsDomainSSLDisabled(siteID, "app.example.com"); err != nil || disabled {
		t.Fatalf("expected certificate activation to clear disabled state: %v, %v", disabled, err)
	}
	if err := SetCertificateDomainBindingWithOrigin(
		siteID, "app.example.com", certID, CertificateBindingOriginPreserved,
	); err != nil {
		t.Fatalf("preserve certificate binding: %v", err)
	}
	binding, err := GetCertificateDomainBinding(siteID, "app.example.com")
	if err != nil || binding == nil || binding.Origin != CertificateBindingOriginPreserved {
		t.Fatalf("expected preserved binding origin: %#v, %v", binding, err)
	}
	if _, _, err := DeleteCertificate(certID, siteID); err == nil || !strings.Contains(err.Error(), "domains") {
		t.Fatalf("expected bound certificate deletion to fail, got %v", err)
	}

	if _, err := DB.Exec("DELETE FROM domain_aliases WHERE site_id = ? AND domain = ?", siteID, "app.example.com"); err != nil {
		t.Fatalf("delete alias: %v", err)
	}
	bindings, err = GetCertificateDomainBindings(siteID)
	if err != nil || len(bindings) != 0 {
		t.Fatalf("expected alias deletion to remove binding: %#v, %v", bindings, err)
	}
	if _, _, err := DeleteCertificate(certID, siteID); err != nil {
		t.Fatalf("delete unbound certificate: %v", err)
	}
}

func TestSetActiveCertificateWithBindingsIsAtomic(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { _ = DB.Close() })

	siteResult, err := DB.Exec(
		"INSERT INTO sites (domain, path) VALUES (?, ?)", "example.com", "/srv/example.com",
	)
	if err != nil {
		t.Fatalf("create site: %v", err)
	}
	siteID64, _ := siteResult.LastInsertId()
	siteID := int(siteID64)
	if _, err := DB.Exec(
		"INSERT INTO domain_aliases (site_id, domain) VALUES (?, ?)", siteID, "app.example.com",
	); err != nil {
		t.Fatalf("create alias: %v", err)
	}

	firstID64, err := CreateCertificate(siteID, "example.com", "custom", "/tmp/first.pem", "/tmp/first.key", "")
	if err != nil {
		t.Fatalf("create first certificate: %v", err)
	}
	secondID64, err := CreateCertificate(siteID, "example.com", "custom", "/tmp/second.pem", "/tmp/second.key", "")
	if err != nil {
		t.Fatalf("create second certificate: %v", err)
	}
	firstID, secondID := int(firstID64), int(secondID64)

	if err := SetActiveCertificateWithBindings(siteID, firstID, []CertificateDomainBindingMutation{{
		Domain: "app.example.com", CertificateID: firstID, Origin: CertificateBindingOriginPreserved,
	}}); err != nil {
		t.Fatalf("set initial certificate state: %v", err)
	}
	if err := SetActiveCertificateWithBindings(siteID, secondID, []CertificateDomainBindingMutation{{
		Domain: "app.example.com", CertificateID: secondID, Origin: "invalid",
	}}); err == nil {
		t.Fatal("expected invalid binding mutation to fail")
	}

	active, err := GetActiveCertificate(siteID)
	if err != nil || active == nil || active.ID != firstID {
		t.Fatalf("expected original active certificate after rollback: %#v, %v", active, err)
	}
	binding, err := GetCertificateDomainBinding(siteID, "app.example.com")
	if err != nil || binding == nil || binding.CertificateID != firstID || binding.Origin != CertificateBindingOriginPreserved {
		t.Fatalf("expected original binding after rollback: %#v, %v", binding, err)
	}
}
