package nginx

import (
	"reflect"
	"strings"
	"testing"
)

func TestGroupHostCertificates(t *testing.T) {
	groups, needsFallback := groupHostCertificates([]HostCertificate{
		{Domain: "example.com", CertPath: "/certs/default.pem", KeyPath: "/certs/default.key"},
		{Domain: "www.example.com", CertPath: "/certs/default.pem", KeyPath: "/certs/default.key"},
		{Domain: "new.example.com"},
		{Domain: "app.example.com", CertPath: "/certs/app.pem", KeyPath: "/certs/app.key"},
	})

	if !needsFallback {
		t.Fatal("expected an uncovered hostname to require the HTTPS guard")
	}
	if len(groups) != 3 {
		t.Fatalf("expected three certificate groups, got %d", len(groups))
	}
	if !reflect.DeepEqual(groups[0].domains, []string{"example.com", "www.example.com"}) {
		t.Fatalf("unexpected default certificate domains: %#v", groups[0].domains)
	}
	if groups[1].certPath != "" || !reflect.DeepEqual(groups[1].domains, []string{"new.example.com"}) {
		t.Fatalf("unexpected HTTP-only group: %#v", groups[1])
	}
	if groups[2].certPath != "/certs/app.pem" || !reflect.DeepEqual(groups[2].domains, []string{"app.example.com"}) {
		t.Fatalf("unexpected alias certificate group: %#v", groups[2])
	}
}

func TestRenderHostGroupsUsesIndependentCertificatesAndPrimaryRuntime(t *testing.T) {
	groups, _ := groupHostCertificates([]HostCertificate{
		{Domain: "example.com", CertPath: "/certs/default.pem", KeyPath: "/certs/default.key"},
		{Domain: "www.example.com", CertPath: "/certs/default.pem", KeyPath: "/certs/default.key"},
		{Domain: "new.example.com"},
		{Domain: "app.example.com", CertPath: "/certs/app.pem", KeyPath: "/certs/app.key"},
	})
	config := renderHostGroups(
		"example.com", "/srv/example.com/public", "8.4", "php", 0,
		"/certs/fallback.pem", "/certs/fallback.key", groups,
	)

	for _, expected := range []string{
		"server_name example.com www.example.com;",
		"ssl_certificate /certs/default.pem;",
		"server_name new.example.com;",
		"ssl_certificate /certs/fallback.pem;",
		"server_name app.example.com;",
		"ssl_certificate /certs/app.pem;",
		"fastcgi_pass unix:/var/run/php/php8.4-fpm-example.com.sock;",
	} {
		if !strings.Contains(config, expected) {
			t.Fatalf("rendered config does not contain %q:\n%s", expected, config)
		}
	}
	if strings.Contains(config, "php8.4-fpm-app.example.com.sock") {
		t.Fatalf("alias block must use the primary site's PHP runtime:\n%s", config)
	}
}
