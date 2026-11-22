package ssl

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func certificateFixture(t *testing.T, names []string, usage x509.ExtKeyUsage, expiredIssuer bool) ([]byte, []byte, []byte) {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	issuerKey, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	issuer := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Private fixture issuer"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	if expiredIssuer {
		issuer.NotAfter = now.Add(-time.Minute)
	}
	issuerDER, e := x509.CreateCertificate(rand.Reader, issuer, issuer, &issuerKey.PublicKey, issuerKey)
	if e != nil {
		t.Fatal(e)
	}
	issuer, e = x509.ParseCertificate(issuerDER)
	if e != nil {
		t.Fatal(e)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: names, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
	der, e := x509.CreateCertificate(rand.Reader, leaf, issuer, &key.PublicKey, issuerKey)
	if e != nil {
		t.Fatal(e)
	}
	pk, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		t.Fatal(e)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: issuerDER})
}
func TestWildcardCertificateCoverageKeepsApexAndDepthDistinct(t *testing.T) {
	leaf, key, issuer := certificateFixture(t, []string{"*.example.test"}, x509.ExtKeyUsageServerAuth, false)
	inspected, e := InspectCertificatePEM(append(leaf, issuer...), key)
	if e != nil {
		t.Fatal("private CA import should not promise public browser trust", e)
	}
	for _, host := range []string{"*.example.test", "shop.example.test", "www.example.test"} {
		if e := VerifyCertificateDomains(inspected.Certificate, []string{host}); e != nil {
			t.Fatal(host, e)
		}
	}
	for _, host := range []string{"example.test", "deep.shop.example.test", "*.shop.example.test", "other.test"} {
		if e := VerifyCertificateDomains(inspected.Certificate, []string{host}); e == nil {
			t.Fatal("invented wildcard coverage", host)
		}
	}
	leaf, key, issuer = certificateFixture(t, []string{"example.test", "*.example.test"}, x509.ExtKeyUsageServerAuth, false)
	inspected, e = InspectCertificatePEM(append(leaf, issuer...), key)
	if e != nil {
		t.Fatal(e)
	}
	if e := VerifyCertificateDomains(inspected.Certificate, []string{"example.test", "www.example.test", "*.example.test"}); e != nil {
		t.Fatal(e)
	}
	leaf, key, _ = certificateFixture(t, []string{"example.test", "www.example.test"}, x509.ExtKeyUsageServerAuth, false)
	inspected, e = InspectCertificatePEM(leaf, key)
	if e != nil {
		t.Fatal(e)
	}
	if e := VerifyCertificateDomains(inspected.Certificate, []string{"*.example.test"}); e == nil {
		t.Fatal("root plus WWW is not wildcard coverage")
	}
}
func TestWildcardCertificateRejectsWrongKeyChainAndUsage(t *testing.T) {
	leaf, key, issuer := certificateFixture(t, []string{"example.test", "*.example.test"}, x509.ExtKeyUsageServerAuth, false)
	otherLeaf, otherKey, otherIssuer := certificateFixture(t, []string{"example.test"}, x509.ExtKeyUsageServerAuth, false)
	for _, v := range []struct{ cert, key []byte }{{leaf, otherKey}, {append(append([]byte{}, leaf...), otherIssuer...), key}, {append(append([]byte{}, issuer...), leaf...), key}, {otherLeaf, key}} {
		if _, e := InspectCertificatePEM(v.cert, v.key); e == nil {
			t.Fatal("accepted invalid certificate pair/chain")
		}
	}
	leaf, key, issuer = certificateFixture(t, []string{"example.test"}, x509.ExtKeyUsageClientAuth, false)
	if _, e := InspectCertificatePEM(append(leaf, issuer...), key); e == nil {
		t.Fatal("accepted client-only usage")
	}
	leaf, key, issuer = certificateFixture(t, []string{"example.test"}, x509.ExtKeyUsageServerAuth, true)
	if _, e := InspectCertificatePEM(append(leaf, issuer...), key); e == nil {
		t.Fatal("accepted expired issuer")
	}
}
