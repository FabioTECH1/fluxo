package server

import (
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/database"
	"fluxo/internal/services/mysql"
)

func TestAttachedDatabaseCleanupPreservesUnmanagedAndSharedUsers(t *testing.T) {
	previousDB := database.DB
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.DB.Close(); database.DB = previousDB })
	for _, user := range []string{"external_user", "pending_user", "shared_user"} {
		if user != "external_user" {
			if _, err := database.BeginManagedDatabaseUser("mysql", user, mysql.LocalTCPHost); err != nil {
				t.Fatal(err)
			}
		}
		if user == "shared_user" {
			if err := database.ActivateManagedDatabaseUser("mysql", user, mysql.LocalTCPHost); err != nil {
				t.Fatal(err)
			}
			if _, err := database.DB.Exec("INSERT INTO databases (id, site_id, engine, name, username) VALUES (2, 2, 'mysql', 'other_db', ?)", user); err != nil {
				t.Fatal(err)
			}
		}
		// These paths must return before contacting the engine or dropping an account.
		if err := cleanupUnusedAttachedDatabaseUser(database.Database{ID: 1, Engine: "mysql", Name: "deleted_db", Username: user}); err != nil {
			t.Fatalf("preserve %s: %v", user, err)
		}
	}
	state, err := database.ManagedDatabaseUserState("mysql", "shared_user", mysql.LocalTCPHost)
	if err != nil || state != database.ManagedDatabaseUserActive {
		t.Fatalf("shared user's management record changed: %q, %v", state, err)
	}
}

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
