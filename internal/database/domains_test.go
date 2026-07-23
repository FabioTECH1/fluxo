package database

import (
	"path/filepath"
	"testing"
)

func TestPromoteDomainAliasAndRestore(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { _ = DB.Close() })

	siteResult, err := DB.Exec(
		"INSERT INTO sites (domain, path) VALUES (?, ?)", "old.example.com", "/home/fluxo/old.example.com",
	)
	if err != nil {
		t.Fatalf("create site: %v", err)
	}
	siteID64, _ := siteResult.LastInsertId()
	siteID := int(siteID64)
	selectedResult, err := DB.Exec(
		"INSERT INTO domain_aliases (site_id, domain) VALUES (?, ?)", siteID, "new.example.com",
	)
	if err != nil {
		t.Fatalf("create selected alias: %v", err)
	}
	selectedID64, _ := selectedResult.LastInsertId()
	selectedID := int(selectedID64)
	if _, err := DB.Exec(
		"INSERT INTO domain_aliases (site_id, domain) VALUES (?, ?)", siteID, "other.example.com",
	); err != nil {
		t.Fatalf("create other alias: %v", err)
	}

	oldCertID64, err := CreateCertificate(siteID, "old.example.com", "letsencrypt", "/tmp/old.pem", "/tmp/old.key", "")
	if err != nil {
		t.Fatalf("create old certificate: %v", err)
	}
	newCertID64, err := CreateCertificate(siteID, "new.example.com", "custom", "/tmp/new.pem", "/tmp/new.key", "")
	if err != nil {
		t.Fatalf("create new certificate: %v", err)
	}
	oldCertID, newCertID := int(oldCertID64), int(newCertID64)
	if err := SetActiveCertificateWithBindings(siteID, oldCertID, []CertificateDomainBindingMutation{
		{Domain: "new.example.com", CertificateID: newCertID, Origin: CertificateBindingOriginManual},
		{Domain: "other.example.com", CertificateID: oldCertID, Origin: CertificateBindingOriginPreserved},
	}); err != nil {
		t.Fatalf("set initial certificate state: %v", err)
	}

	snapshot, err := PromoteDomainAlias(siteID, selectedID, newCertID, []CertificateDomainBindingMutation{
		{Domain: "old.example.com", CertificateID: oldCertID, Origin: CertificateBindingOriginPreserved},
		{Domain: "other.example.com"},
	})
	if err != nil {
		t.Fatalf("promote alias: %v", err)
	}

	var primary, sitePath string
	if err := DB.QueryRow("SELECT domain, path FROM sites WHERE id = ?", siteID).Scan(&primary, &sitePath); err != nil {
		t.Fatal(err)
	}
	if primary != "new.example.com" || sitePath != "/home/fluxo/old.example.com" {
		t.Fatalf("promoted site = (%q, %q)", primary, sitePath)
	}
	var aliasDomain string
	if err := DB.QueryRow("SELECT domain FROM domain_aliases WHERE id = ?", selectedID).Scan(&aliasDomain); err != nil {
		t.Fatal(err)
	}
	if aliasDomain != "old.example.com" {
		t.Fatalf("replacement alias is %q", aliasDomain)
	}
	active, err := GetActiveCertificate(siteID)
	if err != nil || active == nil || active.ID != newCertID {
		t.Fatalf("active certificate after promotion = %#v, %v", active, err)
	}
	bindings, err := GetCertificateDomainBindings(siteID)
	if err != nil || len(bindings) != 1 || bindings[0].Domain != "old.example.com" || bindings[0].CertificateID != oldCertID {
		t.Fatalf("bindings after promotion = %#v, %v", bindings, err)
	}

	if err := RestorePrimaryDomain(snapshot); err != nil {
		t.Fatalf("restore promotion: %v", err)
	}
	if err := DB.QueryRow("SELECT domain, path FROM sites WHERE id = ?", siteID).Scan(&primary, &sitePath); err != nil {
		t.Fatal(err)
	}
	if primary != "old.example.com" || sitePath != "/home/fluxo/old.example.com" {
		t.Fatalf("restored site = (%q, %q)", primary, sitePath)
	}
	if err := DB.QueryRow("SELECT domain FROM domain_aliases WHERE id = ?", selectedID).Scan(&aliasDomain); err != nil {
		t.Fatal(err)
	}
	if aliasDomain != "new.example.com" {
		t.Fatalf("restored alias is %q", aliasDomain)
	}
	active, err = GetActiveCertificate(siteID)
	if err != nil || active == nil || active.ID != oldCertID {
		t.Fatalf("active certificate after restore = %#v, %v", active, err)
	}
	bindings, err = GetCertificateDomainBindings(siteID)
	if err != nil || len(bindings) != 2 {
		t.Fatalf("bindings after restore = %#v, %v", bindings, err)
	}
	byDomain := map[string]CertificateDomainBinding{}
	for _, binding := range bindings {
		byDomain[binding.Domain] = binding
	}
	if byDomain["new.example.com"].CertificateID != newCertID || byDomain["new.example.com"].Origin != CertificateBindingOriginManual {
		t.Fatalf("selected alias binding was not restored: %#v", byDomain["new.example.com"])
	}
	if byDomain["other.example.com"].CertificateID != oldCertID || byDomain["other.example.com"].Origin != CertificateBindingOriginPreserved {
		t.Fatalf("other alias binding was not restored: %#v", byDomain["other.example.com"])
	}
}

func TestPromoteDomainAliasRollsBackInvalidCertificateMutation(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { _ = DB.Close() })

	result, err := DB.Exec(
		"INSERT INTO sites (domain, path) VALUES (?, ?)", "old.example.com", "/home/fluxo/old.example.com",
	)
	if err != nil {
		t.Fatal(err)
	}
	siteID64, _ := result.LastInsertId()
	aliasResult, err := DB.Exec(
		"INSERT INTO domain_aliases (site_id, domain) VALUES (?, ?)", int(siteID64), "new.example.com",
	)
	if err != nil {
		t.Fatal(err)
	}
	aliasID64, _ := aliasResult.LastInsertId()

	_, err = PromoteDomainAlias(int(siteID64), int(aliasID64), 0, []CertificateDomainBindingMutation{
		{Domain: "old.example.com", CertificateID: 999, Origin: CertificateBindingOriginPreserved},
	})
	if err == nil {
		t.Fatal("expected invalid certificate mutation to fail")
	}
	var primary, alias string
	if err := DB.QueryRow("SELECT domain FROM sites WHERE id = ?", siteID64).Scan(&primary); err != nil {
		t.Fatal(err)
	}
	if err := DB.QueryRow("SELECT domain FROM domain_aliases WHERE id = ?", aliasID64).Scan(&alias); err != nil {
		t.Fatal(err)
	}
	if primary != "old.example.com" || alias != "new.example.com" {
		t.Fatalf("failed promotion changed domains to (%q, %q)", primary, alias)
	}
}
