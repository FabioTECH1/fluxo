package server

import (
	"path/filepath"
	"testing"

	"fluxo/internal/database"
)

func TestPlanPrimaryDomainCertificatesPromotesAliasCertificateAndPreservesOldCoverage(t *testing.T) {
	siteID, selectedID, oldCertID, newCertID := setupPrimaryDomainCertificateTest(t)

	plan, err := planPrimaryDomainCertificatesWithCoverage(siteID, selectedID, certificateCoverage(map[int][]string{
		oldCertID: {"old.example.com", "other.example.com"},
		newCertID: {"new.example.com"},
	}))
	if err != nil {
		t.Fatalf("plan promotion: %v", err)
	}
	if plan.activeCertificateID != newCertID {
		t.Fatalf("active certificate = %d, want %d", plan.activeCertificateID, newCertID)
	}
	assertPreservedMutations(t, plan.mutations, oldCertID, "old.example.com", "other.example.com")
}

func TestPlanPrimaryDomainCertificatesUsesFallbackForUncoveredPrimary(t *testing.T) {
	siteID, selectedID, oldCertID, _ := setupPrimaryDomainCertificateTest(t)
	if err := database.DeleteCertificateDomainBinding(siteID, "new.example.com"); err != nil {
		t.Fatal(err)
	}

	plan, err := planPrimaryDomainCertificatesWithCoverage(siteID, selectedID, certificateCoverage(map[int][]string{
		oldCertID: {"old.example.com", "other.example.com"},
	}))
	if err != nil {
		t.Fatalf("plan promotion: %v", err)
	}
	if plan.activeCertificateID != 0 {
		t.Fatalf("active certificate = %d, want fallback", plan.activeCertificateID)
	}
	assertPreservedMutations(t, plan.mutations, oldCertID, "old.example.com", "other.example.com")
}

func TestPlanPrimaryDomainCertificatesKeepsInheritedCertificate(t *testing.T) {
	siteID, selectedID, oldCertID, _ := setupPrimaryDomainCertificateTest(t)
	if err := database.DeleteCertificateDomainBinding(siteID, "new.example.com"); err != nil {
		t.Fatal(err)
	}

	plan, err := planPrimaryDomainCertificatesWithCoverage(siteID, selectedID, certificateCoverage(map[int][]string{
		oldCertID: {"old.example.com", "new.example.com", "other.example.com"},
	}))
	if err != nil {
		t.Fatalf("plan promotion: %v", err)
	}
	if plan.activeCertificateID != oldCertID {
		t.Fatalf("active certificate = %d, want %d", plan.activeCertificateID, oldCertID)
	}
	if len(plan.mutations) != 0 {
		t.Fatalf("unexpected binding mutations: %#v", plan.mutations)
	}
}

func setupPrimaryDomainCertificateTest(t *testing.T) (siteID, selectedID, oldCertID, newCertID int) {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { _ = database.DB.Close() })

	result, err := database.DB.Exec(
		"INSERT INTO sites (domain, path) VALUES (?, ?)", "old.example.com", "/home/fluxo/old.example.com",
	)
	if err != nil {
		t.Fatal(err)
	}
	siteID64, _ := result.LastInsertId()
	siteID = int(siteID64)
	selectedResult, err := database.DB.Exec(
		"INSERT INTO domain_aliases (site_id, domain) VALUES (?, ?)", siteID, "new.example.com",
	)
	if err != nil {
		t.Fatal(err)
	}
	selectedID64, _ := selectedResult.LastInsertId()
	selectedID = int(selectedID64)
	if _, err := database.DB.Exec(
		"INSERT INTO domain_aliases (site_id, domain) VALUES (?, ?)", siteID, "other.example.com",
	); err != nil {
		t.Fatal(err)
	}

	oldCertID64, err := database.CreateCertificate(siteID, "old.example.com", "custom", "/tmp/old.pem", "/tmp/old.key", "")
	if err != nil {
		t.Fatal(err)
	}
	newCertID64, err := database.CreateCertificate(siteID, "new.example.com", "custom", "/tmp/new.pem", "/tmp/new.key", "")
	if err != nil {
		t.Fatal(err)
	}
	oldCertID, newCertID = int(oldCertID64), int(newCertID64)
	if err := database.SetActiveCertificateWithBindings(siteID, oldCertID, []database.CertificateDomainBindingMutation{{
		Domain: "new.example.com", CertificateID: newCertID, Origin: database.CertificateBindingOriginManual,
	}}); err != nil {
		t.Fatal(err)
	}
	return siteID, selectedID, oldCertID, newCertID
}

func certificateCoverage(covered map[int][]string) func(database.Certificate, string) bool {
	return func(cert database.Certificate, hostname string) bool {
		for _, domain := range covered[cert.ID] {
			if domain == hostname {
				return true
			}
		}
		return false
	}
}

func assertPreservedMutations(t *testing.T, mutations []database.CertificateDomainBindingMutation, certID int, domains ...string) {
	t.Helper()
	if len(mutations) != len(domains) {
		t.Fatalf("mutations = %#v, want preserved bindings for %v", mutations, domains)
	}
	byDomain := make(map[string]database.CertificateDomainBindingMutation, len(mutations))
	for _, mutation := range mutations {
		byDomain[mutation.Domain] = mutation
	}
	for _, domain := range domains {
		mutation, ok := byDomain[domain]
		if !ok || mutation.CertificateID != certID || mutation.Origin != database.CertificateBindingOriginPreserved {
			t.Fatalf("mutation for %s = %#v", domain, mutation)
		}
	}
}
