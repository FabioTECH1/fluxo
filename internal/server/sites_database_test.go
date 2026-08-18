package server

import (
	"strings"
	"testing"
)

func TestValidateSiteDatabaseCredentials(t *testing.T) {
	tests := []struct {
		name    string
		request CreateSiteRequest
		wantErr string
	}{
		{name: "site without database", request: CreateSiteRequest{AppType: "laravel"}},
		{name: "dedicated credentials", request: CreateSiteRequest{AppType: "laravel", DatabaseName: "app_db", DatabaseUser: "app_user", DatabasePassword: "secret"}},
		{name: "wordpress dedicated credentials", request: CreateSiteRequest{AppType: "wordpress", DatabaseName: "wp_db", DatabaseUser: "wp_user", DatabasePassword: "secret"}},
		{name: "wordpress requires database", request: CreateSiteRequest{AppType: "wordpress"}, wantErr: "WordPress requires a database"},
		{name: "database requires user", request: CreateSiteRequest{AppType: "laravel", DatabaseName: "app_db", DatabasePassword: "secret"}, wantErr: "dedicated database username"},
		{name: "database requires password", request: CreateSiteRequest{AppType: "laravel", DatabaseName: "app_db", DatabaseUser: "app_user"}, wantErr: "dedicated database username"},
		{name: "rejects fluxo administrator", request: CreateSiteRequest{AppType: "laravel", DatabaseName: "app_db", DatabaseUser: "fluxo", DatabasePassword: "secret"}, wantErr: "control-plane account"},
		{name: "rejects mysql root", request: CreateSiteRequest{AppType: "laravel", DatabaseName: "app_db", DatabaseUser: "root", DatabasePassword: "secret"}, wantErr: "control-plane account"},
		{name: "credentials without database", request: CreateSiteRequest{AppType: "laravel", DatabaseUser: "app_user", DatabasePassword: "secret"}, wantErr: "selected database"},
		{name: "rejects database for node", request: CreateSiteRequest{AppType: "node", DatabaseName: "app_db", DatabaseUser: "app_user", DatabasePassword: "secret"}, wantErr: "not supported"},
		{name: "rejects unsafe dotenv quote", request: CreateSiteRequest{AppType: "laravel", DatabaseName: "app_db", DatabaseUser: "app_user", DatabasePassword: "bad'password"}, wantErr: "single quote"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateSiteDatabaseCredentials(test.request)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestValidateApplicationTypeUnchanged(t *testing.T) {
	if err := validateApplicationTypeUnchanged("wordpress", "wordpress"); err != nil {
		t.Fatalf("same application type should remain compatible with older clients: %v", err)
	}
	if err := validateApplicationTypeUnchanged("php", ""); err != nil {
		t.Fatalf("omitted application type should be accepted: %v", err)
	}
	if err := validateApplicationTypeUnchanged("php", "node"); err == nil {
		t.Fatal("changing an existing site's application type must be rejected")
	}
}
