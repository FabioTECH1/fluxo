package nginx

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNginxUnknownHostGuardRouting(t *testing.T) {
	nginxBinary, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("nginx is not installed")
	}

	dir := t.TempDir()
	certPath, keyPath := writeNginxTestCertificate(t, dir)
	ports := freeTCPPorts(t, 6)
	explicitHTTP, explicitHTTPS := ports[0], ports[1]
	compatHTTP, compatHTTPS := ports[2], ports[3]
	fallbackHTTP, fallbackHTTPS := ports[4], ports[5]
	configPath := filepath.Join(dir, "nginx.conf")
	pidPath := filepath.Join(dir, "nginx.pid")
	webRoot := filepath.Join(dir, "public")
	if err := os.MkdirAll(webRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("fallback HTTPS application"), 0644); err != nil {
		t.Fatal(err)
	}

	explicitGuard := renderDefaultServerConfigForPorts(certPath, keyPath, true, explicitHTTP, explicitHTTPS)
	compatGuard := renderDefaultServerConfigForPorts(certPath, keyPath, false, compatHTTP, compatHTTPS)
	fallbackGuard := renderDefaultServerConfigForPorts(certPath, keyPath, true, fallbackHTTP, fallbackHTTPS)
	fallbackSite := renderSiteTemplate(
		"fallback.test", webRoot, "8.4", "html", 0,
		"", "", certPath, keyPath, []string{"fallback.test"},
	)
	fallbackSite = rewriteNginxTestPorts(fallbackSite, fallbackHTTP, fallbackHTTPS)
	fallbackSite = strings.ReplaceAll(fallbackSite, "/var/log/nginx/fallback.test.access.log", filepath.Join(dir, "fallback.access.log"))
	fallbackSite = strings.ReplaceAll(fallbackSite, "/var/log/nginx/fallback.test.error.log", filepath.Join(dir, "fallback.error.log"))
	config := fmt.Sprintf(`pid %s;
error_log %s notice;
events {}
http {
    access_log off;
%s
    server { listen %d; listen [::]:%d; server_name known-explicit.test; return 204; }
    server { listen %d ssl http2; listen [::]:%d ssl http2; server_name known-explicit.test; ssl_certificate %s; ssl_certificate_key %s; return 204; }
    server { listen %d default_server; listen [::]:%d default_server; server_name legacy-default.test; return 418; }
    server { listen %d ssl http2 default_server; listen [::]:%d ssl http2 default_server; server_name legacy-default.test; ssl_certificate %s; ssl_certificate_key %s; return 418; }
    server { listen %d; listen [::]:%d; server_name known-compat.test; return 204; }
    server { listen %d ssl http2; listen [::]:%d ssl http2; server_name known-compat.test; ssl_certificate %s; ssl_certificate_key %s; return 204; }
    server { listen %d; listen [::]:%d; server_name ~^regex-[a-z]+\.test$; return 206; }
    server { listen %d ssl http2; listen [::]:%d ssl http2; server_name ~^regex-[a-z]+\.test$; ssl_certificate %s; ssl_certificate_key %s; return 206; }
%s
%s
%s
}
`, pidPath, filepath.Join(dir, "error.log"), explicitGuard,
		explicitHTTP, explicitHTTP, explicitHTTPS, explicitHTTPS, certPath, keyPath,
		compatHTTP, compatHTTP, compatHTTPS, compatHTTPS, certPath, keyPath,
		compatHTTP, compatHTTP, compatHTTPS, compatHTTPS, certPath, keyPath,
		compatHTTP, compatHTTP, compatHTTPS, compatHTTPS, certPath, keyPath,
		compatGuard, fallbackGuard, fallbackSite)
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	if output, err := exec.Command(nginxBinary, "-t", "-c", configPath).CombinedOutput(); err != nil {
		t.Fatalf("nginx config validation failed: %v\n%s", err, output)
	}
	if output, err := exec.Command(nginxBinary, "-c", configPath).CombinedOutput(); err != nil {
		t.Fatalf("start test nginx: %v\n%s", err, output)
	}
	t.Cleanup(func() {
		stop := exec.Command(nginxBinary, "-c", configPath, "-s", "stop")
		if output, err := stop.CombinedOutput(); err != nil {
			t.Logf("stop test nginx: %v\n%s", err, output)
		}
	})
	waitForTCP(t, explicitHTTP)

	assertNginxStatus(t, "known-explicit.test", explicitHTTP, false, http.StatusNoContent)
	assertNginxDropped(t, "unknown-explicit.test", explicitHTTP, false)
	assertNginxStatus(t, "known-explicit.test", explicitHTTPS, true, http.StatusNoContent)
	assertNginxDropped(t, "unknown-explicit.test", explicitHTTPS, true)
	assertNginxStatus(t, "known-compat.test", compatHTTP, false, http.StatusNoContent)
	assertNginxStatus(t, "regex-app.test", compatHTTP, false, http.StatusPartialContent)
	assertNginxDropped(t, "unknown-compat.test", compatHTTP, false)
	assertNginxStatus(t, "known-compat.test", compatHTTPS, true, http.StatusNoContent)
	assertNginxStatus(t, "regex-app.test", compatHTTPS, true, http.StatusPartialContent)
	assertNginxDropped(t, "unknown-compat.test", compatHTTPS, true)
	assertNginxStatus(t, "fallback.test", fallbackHTTP, false, http.StatusOK)
	assertNginxDropped(t, "unknown-fallback.test", fallbackHTTP, false)
	assertNginxStatus(t, "fallback.test", fallbackHTTPS, true, http.StatusOK)
	assertNginxDropped(t, "unknown-fallback.test", fallbackHTTPS, true)
}

func TestPanelProxyNginxSyntax(t *testing.T) {
	nginxBinary, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("nginx is not installed")
	}

	dir := t.TempDir()
	certPath, keyPath := writeNginxTestCertificate(t, dir)
	ports := freeTCPPorts(t, 2)
	challengeRoot := filepath.Join(dir, "acme")
	if err := os.MkdirAll(challengeRoot, 0755); err != nil {
		t.Fatal(err)
	}

	panel, err := renderPanelProxy(PanelProxyConfig{
		Domain:         "panel.example.test",
		CertPath:       certPath,
		KeyPath:        keyPath,
		ChallengeRoot:  challengeRoot,
		UpstreamScheme: "https",
		UpstreamPort:   9595,
	})
	if err != nil {
		t.Fatalf("render panel proxy: %v", err)
	}
	panel = rewriteNginxTestPorts(panel, ports[0], ports[1])
	panel = strings.ReplaceAll(panel, "/var/log/nginx/fluxo-panel.access.log", filepath.Join(dir, "panel.access.log"))
	panel = strings.ReplaceAll(panel, "/var/log/nginx/fluxo-panel.error.log", filepath.Join(dir, "panel.error.log"))

	configPath := filepath.Join(dir, "nginx.conf")
	config := fmt.Sprintf("pid %s;\nerror_log %s notice;\nevents {}\nhttp {\naccess_log off;\n%s\n}\n",
		filepath.Join(dir, "nginx.pid"), filepath.Join(dir, "error.log"), panel)
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(nginxBinary, "-t", "-c", configPath).CombinedOutput(); err != nil {
		t.Fatalf("panel nginx config validation failed: %v\n%s", err, output)
	}
}

func rewriteNginxTestPorts(config string, httpPort, httpsPort int) string {
	replacements := []struct {
		old string
		new string
	}{
		{"listen 80;", fmt.Sprintf("listen %d;", httpPort)},
		{"listen [::]:80;", fmt.Sprintf("listen [::]:%d;", httpPort)},
		{"listen 443 ssl http2;", fmt.Sprintf("listen %d ssl http2;", httpsPort)},
		{"listen [::]:443 ssl http2;", fmt.Sprintf("listen [::]:%d ssl http2;", httpsPort)},
	}
	for _, replacement := range replacements {
		config = strings.ReplaceAll(config, replacement.old, replacement.new)
	}
	return config
}

func freeTCPPorts(t *testing.T, count int) []int {
	t.Helper()
	listeners := make([]net.Listener, 0, count)
	ports := make([]int, 0, count)
	for range count {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			for _, existing := range listeners {
				existing.Close()
			}
			t.Fatal(err)
		}
		listeners = append(listeners, listener)
		ports = append(ports, listener.Addr().(*net.TCPAddr).Port)
	}
	for _, listener := range listeners {
		listener.Close()
	}
	return ports
}

func waitForTCP(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 100*time.Millisecond)
		if err == nil {
			connection.Close()
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("nginx did not listen on port %d", port)
}

func nginxClient(host string, tlsEnabled bool) *http.Client {
	transport := &http.Transport{DisableKeepAlives: true}
	if tlsEnabled {
		transport.TLSClientConfig = &tls.Config{ServerName: host, InsecureSkipVerify: true} // Test-only self-signed certificate.
	}
	return &http.Client{Transport: transport, Timeout: 2 * time.Second}
}

func nginxRequest(host string, port int, tlsEnabled bool) (*http.Response, error) {
	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fmt.Sprintf("%s://127.0.0.1:%d/", scheme, port), nil)
	if err != nil {
		return nil, err
	}
	request.Host = host
	return nginxClient(host, tlsEnabled).Do(request)
}

func assertNginxStatus(t *testing.T, host string, port int, tlsEnabled bool, expected int) {
	t.Helper()
	response, err := nginxRequest(host, port, tlsEnabled)
	if err != nil {
		t.Fatalf("request configured host %s: %v", host, err)
	}
	defer response.Body.Close()
	if response.StatusCode != expected {
		t.Fatalf("configured host %s returned %d, want %d", host, response.StatusCode, expected)
	}
}

func assertNginxDropped(t *testing.T, host string, port int, tlsEnabled bool) {
	t.Helper()
	response, err := nginxRequest(host, port, tlsEnabled)
	if err == nil {
		defer response.Body.Close()
		t.Fatalf("unknown host %s reached an HTTP server and returned %d", host, response.StatusCode)
	}
}

func writeNginxTestCertificate(t *testing.T, dir string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "invalid.fluxo.local"},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"invalid.fluxo.local"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(dir, "test.crt")
	keyPath := filepath.Join(dir, "test.key")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}
