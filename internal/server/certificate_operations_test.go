package server

import "testing"

func TestCertificateOperationsCoordinateIssuanceAndDeletionPerSite(t *testing.T) {
	server := &Server{}
	if !server.beginCertificateIssuance(1) {
		t.Fatal("expected issuance to start")
	}
	if server.beginCertificateSiteDeletion(1) {
		t.Fatal("deletion must not start while the same site is issuing a certificate")
	}
	if !server.beginCertificateSiteDeletion(2) {
		t.Fatal("another site's deletion should not be blocked")
	}
	if server.beginCertificateIssuance(2) {
		t.Fatal("issuance must not start while the same site is being deleted")
	}
	server.endCertificateSiteDeletion(2)
	server.endCertificateIssuance(1)
	if !server.beginCertificateSiteDeletion(1) {
		t.Fatal("deletion should start after issuance finishes")
	}
	server.endCertificateSiteDeletion(1)
}
