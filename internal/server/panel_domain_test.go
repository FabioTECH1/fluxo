package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizePanelDomain(t *testing.T) {
	domain, err := normalizePanelDomain("  PANEL.Example.COM. ")
	if err != nil {
		t.Fatalf("normalize panel domain: %v", err)
	}
	if domain != "panel.example.com" {
		t.Fatalf("normalized domain = %q", domain)
	}

	for _, invalid := range []string{"localhost", "https://panel.example.com", "panel.example.com:443", "-panel.example.com", "192.0.2.10"} {
		if _, err := normalizePanelDomain(invalid); err == nil {
			t.Fatalf("invalid panel domain %q was accepted", invalid)
		}
	}
}

func TestPanelChallengeRootIsOutsidePrivateProductionData(t *testing.T) {
	root, err := panelChallengeRootPath("prod", "/var/lib/fluxo")
	if err != nil {
		t.Fatalf("resolve production challenge root: %v", err)
	}
	if root != productionPanelChallengeRoot {
		t.Fatalf("production challenge root = %q", root)
	}
	if root == "/var/lib/fluxo" || filepath.Dir(root) == "/var/lib/fluxo" {
		t.Fatalf("ACME webroot must not be nested under private Fluxo data: %q", root)
	}
}

func TestEnsurePanelChallengeRootIsTraversable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "acme")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := ensurePanelChallengeRoot(root); err != nil {
		t.Fatalf("ensure panel challenge root: %v", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0755 {
		t.Fatalf("panel challenge root mode = %o, want 755", mode)
	}
}

func TestRetryPanelDomainHealthSucceedsAfterTransientReloadMismatch(t *testing.T) {
	attempts := 0
	err := retryPanelDomainHealth(context.Background(), time.Nanosecond, time.Nanosecond, func(context.Context) error {
		attempts++
		if attempts < 3 {
			return errUnexpectedPanelCertificate
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retry panel health: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("panel health attempts = %d, want 3", attempts)
	}
}

func TestRetryPanelDomainHealthReturnsLastErrorAtDeadline(t *testing.T) {
	want := errors.New("still serving the previous configuration")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	attempts := 0
	err := retryPanelDomainHealth(ctx, time.Hour, time.Hour, func(context.Context) error {
		attempts++
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("retry error = %v, want %v", err, want)
	}
	if attempts != 1 {
		t.Fatalf("panel health attempts = %d, want 1", attempts)
	}
}

func TestRetryPanelDomainHealthHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false
	err := retryPanelDomainHealth(ctx, time.Millisecond, time.Millisecond, func(context.Context) error {
		called = true
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("retry error = %v, want context cancellation", err)
	}
	if called {
		t.Fatal("health check ran after its context was cancelled")
	}
}

func TestVerifyPanelDomainHealthAtChecksCertificateAndHealthResponse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/health" {
			http.NotFound(w, r)
			return
		}
		if r.Host != "panel.example.com" || r.TLS == nil || r.TLS.ServerName != "panel.example.com" {
			http.Error(w, "unexpected panel hostname", http.StatusMisdirectedRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	certificate := server.Certificate()
	if certificate == nil {
		t.Fatal("test TLS server has no certificate")
	}
	address := server.Listener.Addr().String()
	if err := verifyPanelDomainHealthAt(context.Background(), "panel.example.com", certificate.Raw, address); err != nil {
		t.Fatalf("verify healthy panel proxy: %v", err)
	}

	err := verifyPanelDomainHealthAt(context.Background(), "panel.example.com", []byte("different certificate"), address)
	if !errors.Is(err, errUnexpectedPanelCertificate) {
		t.Fatalf("certificate mismatch error = %v", err)
	}
}

func TestVerifyPanelDomainHealthAtRejectsUnexpectedHealthResponse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"starting"}`))
	}))
	defer server.Close()

	err := verifyPanelDomainHealthAt(
		context.Background(),
		"panel.example.com",
		server.Certificate().Raw,
		server.Listener.Addr().String(),
	)
	if err == nil || err.Error() != "panel health check returned an unexpected response" {
		t.Fatalf("unexpected health response error = %v", err)
	}
}
