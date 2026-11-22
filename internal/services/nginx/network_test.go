package nginx

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"fluxo/internal/sitepolicy"
	"golang.org/x/crypto/bcrypt"
)

func testNetworkSettings() sitepolicy.SiteNetworkSettings {
	return sitepolicy.SiteNetworkSettings{Format: 1, Enabled: true, Username: "staging", PasswordHash: "$2a$12$" + strings.Repeat(".", 53), Redirects: []sitepolicy.SiteRedirect{{ID: "rule", Source: "/old-pricing", Destination: "/pricing?plan=annual", Status: 301}}}
}
func TestNetworkProtectionHashAndInput(t *testing.T) {
	for _, v := range []struct{ u, p string }{{"user:name", "strong-password"}, {"user", "short"}, {"user", strings.Repeat("x", 73)}, {"user", "password\x00secret"}, {"user\nadmin", "password"}} {
		if ValidateProtection(v.u, v.p) == nil {
			t.Fatal("accepted invalid credentials")
		}
	}
	password := "valid-private-password"
	if e := ValidateProtection("staging", password); e != nil {
		t.Fatal(e)
	}
	hash, e := HashProtection(password)
	if e != nil || !networkHash.MatchString(hash) || hash == password || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		t.Fatal("invalid protected hash", e)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte("wrong-password")) == nil {
		t.Fatal("wrong password accepted")
	}
}
func TestNetworkRedirectValidation(t *testing.T) {
	for _, v := range []struct {
		from, to string
		status   int
	}{{"/old", "/new", 301}, {"/old/", "https://example.org/new?q=1", 302}, {"/", "https://[2001:db8::1]:8443/path", 302}} {
		if e := ValidateRedirect(v.from, v.to, v.status); e != nil {
			t.Fatal(v, e)
		}
	}
	for _, v := range []struct {
		from, to string
		status   int
	}{{"/old.*", "/new", 301}, {"/old?x=1", "/new", 301}, {"/old%2fpath", "/new", 301}, {"/old/../new", "/ok", 301}, {"//host", "/new", 301}, {"/.well-known/acme-challenge/token", "/new", 302}, {"/old", "javascript:bad", 301}, {"/old", "//foreign.test", 301}, {"/old", "https://user:secret@example.org/", 301}, {"/old", "/old?x=1", 302}, {"/old", "/./old", 302}, {"/old", "/new$uri", 301}, {"/old", "/new\r\nHeader:bad", 301}, {"/old", "https://example.org/a b", 301}, {"/old", "/new", 307}} {
		if ValidateRedirect(v.from, v.to, v.status) == nil {
			t.Fatal("accepted invalid redirect", v)
		}
	}
}
func TestNetworkRendersWholeContentAndACMEException(t *testing.T) {
	for _, app := range []string{"php", "wordpress", "html", "node", "python-django"} {
		t.Run(app, func(t *testing.T) {
			raw := renderSiteTemplateWithPool("example.test", "/srv/public", "8.4", "stable-pool", app, 3000, "/cert", "/key", "", "", []string{"example.test"})
			out, e := ApplyNetworkSettings(raw, 7, testNetworkSettings())
			if e != nil {
				t.Fatal(e)
			}
			tokens, e := lexTrafficConfig(out)
			if e != nil {
				t.Fatal(e)
			}
			tree, e := parseTrafficSyntax(tokens, true)
			if e != nil {
				t.Fatal(e)
			}
			for _, server := range tree.children {
				hasAuth := false
				for _, d := range server.directives {
					if d[0].value == "auth_basic" && d[1].value == "Protected site" {
						hasAuth = true
					}
				}
				if !hasAuth {
					t.Fatal("server content not covered")
				}
				for _, loc := range server.children {
					if strings.Contains(out[loc.start:loc.open], "/.well-known/acme-challenge/") {
						off := false
						for _, d := range loc.directives {
							if d[0].value == "auth_basic" && d[1].value == "off" {
								off = true
							}
						}
						if !off {
							t.Fatal("blocked ACME")
						}
					}
				}
			}
			if !strings.Contains(out, `if ($request_uri ~ "^/old-pricing([?].*)?$")`) {
				t.Fatal("redirect did not bind original exact path")
			}
			if strings.Contains(out, `if ($uri `) {
				t.Fatal("front-controller/internal redirect could match another source")
			}
			if !strings.Contains(out, `return 301 "/pricing?plan=annual"`) {
				t.Fatal("lost exact target")
			}
		})
	}
}
func TestNetworkDisabledIsByteStable(t *testing.T) {
	raw := renderSiteTemplateWithPool("example.test", "/srv/public", "8.4", "pool", "php", 0, "/cert", "/key", "", "", nil)
	out, e := ApplyNetworkSettings(raw, 7, sitepolicy.SiteNetworkSettings{Format: 1})
	if e != nil || out != raw {
		t.Fatal("disabled features changed baseline")
	}
}
func TestNetworkCredentialFilesAreImmutableProtectedAndNoPlaintext(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "auth")
	s := testNetworkSettings()
	s.Redirects = nil
	hash, e := HashProtection("actual-secret-input")
	if e != nil {
		t.Fatal(e)
	}
	s.PasswordHash = hash
	if e = ensureAuthFile(dir, 7, s, os.Geteuid(), os.Getegid()); e != nil {
		t.Fatal(e)
	}
	filename := filepath.Join(dir, filepath.Base(AuthFilePath(7, s)))
	b, e := os.ReadFile(filename)
	if e != nil || strings.Contains(string(b), "actual-secret-input") || string(b) != "staging:"+hash+"\n" {
		t.Fatal("credential leak or invalid file")
	}
	st, _ := os.Stat(filename)
	if st.Mode().Perm() != 0640 {
		t.Fatal(st.Mode())
	}
	if e = ensureAuthFile(dir, 7, s, os.Geteuid(), os.Getegid()); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filename, []byte("tampered"), 0640); e != nil {
		t.Fatal(e)
	}
	if ensureAuthFile(dir, 7, s, os.Geteuid(), os.Getegid()) == nil {
		t.Fatal("blessed unknown bytes")
	}
	got, _ := os.ReadFile(filename)
	if string(got) != "tampered" {
		t.Fatal("overwrote evidence")
	}
}
func TestNetworkCredentialFilesRejectUnsafeInputs(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "fifo", "writable"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "auth")
			s := testNetworkSettings()
			if e := ensureAuthFile(dir, 7, s, os.Geteuid(), os.Getegid()); e != nil {
				t.Fatal(e)
			}
			file := filepath.Join(dir, filepath.Base(AuthFilePath(7, s)))
			switch kind {
			case "symlink":
				os.Remove(file)
				os.Symlink("/etc/passwd", file)
			case "hardlink":
				os.Link(file, file+"-other")
			case "fifo":
				os.Remove(file)
				syscall.Mkfifo(file, 0600)
			case "writable":
				os.Chmod(file, 0666)
			}
			if ensureAuthFile(dir, 7, s, os.Geteuid(), os.Getegid()) == nil {
				t.Fatal("accepted unsafe input")
			}
		})
	}
}
func TestWildcardTLSNeverUsesFallbackOrWrongDepth(t *testing.T) {
	pending, e := RenderWildcard("example.test", "example.test", "/srv/public", "8.4", "pool", "php", 0, "", "")
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(pending, "server_name *.example.test;") || !strings.Contains(pending, "ssl_reject_handshake on;") || strings.Contains(pending, "ssl_certificate") {
		t.Fatal("pending wildcard served a certificate")
	}
	active, e := RenderWildcard("example.test", "example.test", "/srv/public", "8.4", "pool", "php", 0, "/combined.crt", "/combined.key")
	if e != nil {
		t.Fatal(e)
	}
	single := strings.Index(active, `server_name ~^[a-z0-9-]+\.example\.test$;`)
	reject := strings.Index(active, `server_name ~^.+\.example\.test$;`)
	if single < 0 || reject <= single {
		t.Fatal("TLS depth or reject priority incorrect")
	}
	if !strings.Contains(active, "listen [::]:80;") || !strings.Contains(active, "listen [::]:443 ssl http2;") {
		t.Fatal("IPv6 routing missing")
	}
	if strings.Count(active, "ssl_certificate /combined.crt;") != 1 {
		t.Fatal("changed unrelated certificate scope")
	}
}
func TestWildcardUncoveredWWWRejectsTLS(t *testing.T) {
	g, needs := groupHostCertificates([]HostCertificate{{Domain: "www.example.test", RedirectTo: "example.test", RejectTLS: true}})
	if needs || len(g) != 1 {
		t.Fatal("requested fallback for rejection")
	}
	raw := renderHostGroupsWithPool("example.test", "/srv/public", "8.4", "php", 0, "pool", "", "", g)
	if strings.Contains(raw, "ssl_certificate") || !strings.Contains(raw, "ssl_reject_handshake on;") || !strings.Contains(raw, "https://example.test$request_uri") {
		t.Fatal("changed WWW or exposed certificate")
	}
}
func TestNetworkServerNamesDoesNotConfuseLocationRegexWithHostname(t *testing.T) {
	names, e := ServerNames(`server { server_name example.test www.example.test; location ~ \.php$ { deny all; } }`)
	if e != nil || len(names) != 2 {
		t.Fatal(names, e)
	}
}
