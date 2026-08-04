package bootstrap

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/database"

	"golang.org/x/crypto/bcrypt"
)

func TestAdminUsernameMessage(t *testing.T) {
	tests := []struct {
		name     string
		username string
		want     string
	}{
		{name: "claimed account", username: "admin", want: "Admin username: admin"},
		{name: "bootstrap account", username: "__bootstrap__", want: "No admin username is configured yet. Choose a username for first login."},
		{name: "missing account", username: "", want: "No admin username is configured yet. Choose a username for first login."},
		{name: "legacy control character", username: "admin\nInjected", want: `Admin username: "admin\nInjected"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := adminUsernameMessage(test.username); got != test.want {
				t.Fatalf("adminUsernameMessage(%q) = %q, want %q", test.username, got, test.want)
			}
		})
	}
}

func TestShowAdminUsername(t *testing.T) {
	previousDB := database.DB
	dbPath := filepath.Join(t.TempDir(), "fluxo.db")
	if err := database.InitDB(dbPath); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			database.DB.Close()
		}
		database.DB = previousDB
	})

	var out bytes.Buffer
	if err := ShowAdminUsername(dbPath, &out); err != nil {
		t.Fatalf("ShowAdminUsername() empty database error = %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "No admin username is configured yet. Choose a username for first login." {
		t.Fatalf("ShowAdminUsername() empty database = %q", got)
	}

	if _, err := database.DB.Exec("INSERT INTO users (username, token_hash) VALUES (?, ?)", "admin", "old-hash"); err != nil {
		t.Fatalf("insert admin user: %v", err)
	}
	out.Reset()
	if err := ShowAdminUsername(dbPath, &out); err != nil {
		t.Fatalf("ShowAdminUsername() error = %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "Admin username: admin" {
		t.Fatalf("ShowAdminUsername() = %q, want %q", got, "Admin username: admin")
	}
}

func TestResetAdminTokenReportsAndPersistsUsername(t *testing.T) {
	previousDB := database.DB
	dataDir := t.TempDir()
	if err := database.InitDB(filepath.Join(dataDir, "fluxo.db")); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			database.DB.Close()
		}
		database.DB = previousDB
	})
	if _, err := database.DB.Exec("INSERT INTO users (username, token_hash) VALUES (?, ?)", "admin", "old-hash"); err != nil {
		t.Fatalf("insert admin user: %v", err)
	}
	if err := appendCredential(dataDir, false, "Fluxo bootstrap token", strings.Repeat("a", 32)); err != nil {
		t.Fatalf("seed bootstrap token: %v", err)
	}
	if err := appendCredential(dataDir, false, "Fluxo sudo password", strings.Repeat("b", 16)); err != nil {
		t.Fatalf("seed unrelated credential: %v", err)
	}

	var out bytes.Buffer
	ResetAdminToken(dataDir, false, &out)
	if !strings.Contains(out.String(), "Admin username: admin") {
		t.Fatalf("ResetAdminToken() output = %q, want admin username", out.String())
	}

	credentials, err := os.ReadFile(CredentialsPath(dataDir))
	if err != nil {
		t.Fatalf("read credentials file: %v", err)
	}
	if !strings.Contains(string(credentials), "Fluxo admin username: admin") {
		t.Fatalf("credentials file does not contain admin username: %q", credentials)
	}
	if strings.Contains(string(credentials), strings.Repeat("a", 32)) {
		t.Fatalf("credentials file retained the obsolete bootstrap token: %q", credentials)
	}
	if !strings.Contains(string(credentials), "Fluxo sudo password: "+strings.Repeat("b", 16)) {
		t.Fatalf("credentials file lost an unrelated credential: %q", credentials)
	}
	info, err := os.Stat(CredentialsPath(dataDir))
	if err != nil {
		t.Fatalf("stat credentials file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("credentials file permissions = %o, want 600", info.Mode().Perm())
	}

	token := ReadBootstrapToken(dataDir)
	if token == "" {
		t.Fatal("reset token was not persisted")
	}
	var tokenHash string
	if err := database.DB.QueryRow("SELECT token_hash FROM users WHERE username = ?", "admin").Scan(&tokenHash); err != nil {
		t.Fatalf("read reset token hash: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(tokenHash), []byte(token)); err != nil {
		t.Fatalf("persisted reset token does not match database hash: %v", err)
	}

	out.Reset()
	ResetAdminToken(dataDir, false, &out)
	credentials, err = os.ReadFile(CredentialsPath(dataDir))
	if err != nil {
		t.Fatalf("read credentials after second reset: %v", err)
	}
	if count := strings.Count(string(credentials), "Fluxo bootstrap token"); count != 1 {
		t.Fatalf("bootstrap token entry count after repeated reset = %d, want 1: %q", count, credentials)
	}
	if count := strings.Count(string(credentials), "Fluxo admin username:"); count != 1 {
		t.Fatalf("admin username entry count after repeated reset = %d, want 1: %q", count, credentials)
	}
}

func TestResetAdminTokenLeavesBootstrapUsernameUnclaimed(t *testing.T) {
	previousDB := database.DB
	dataDir := t.TempDir()
	if err := database.InitDB(filepath.Join(dataDir, "fluxo.db")); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			database.DB.Close()
		}
		database.DB = previousDB
	})

	var out bytes.Buffer
	ResetAdminToken(dataDir, false, &out)
	if !strings.Contains(out.String(), "No admin username is configured yet. Choose a username for first login.") {
		t.Fatalf("ResetAdminToken() bootstrap output = %q", out.String())
	}

	credentials, err := os.ReadFile(CredentialsPath(dataDir))
	if err != nil {
		t.Fatalf("read credentials file: %v", err)
	}
	if strings.Contains(string(credentials), "Fluxo admin username:") {
		t.Fatalf("credentials file contains a stale bootstrap username: %q", credentials)
	}
	var username string
	if err := database.DB.QueryRow("SELECT username FROM users ORDER BY id ASC LIMIT 1").Scan(&username); err != nil {
		t.Fatalf("read bootstrap username: %v", err)
	}
	if username != "__bootstrap__" {
		t.Fatalf("bootstrap username = %q, want __bootstrap__", username)
	}
}

func TestResetAdminTokenEscapesLegacyUnsafeUsername(t *testing.T) {
	previousDB := database.DB
	dataDir := t.TempDir()
	if err := database.InitDB(filepath.Join(dataDir, "fluxo.db")); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	t.Cleanup(func() {
		if database.DB != nil {
			database.DB.Close()
		}
		database.DB = previousDB
	})
	if _, err := database.DB.Exec("INSERT INTO users (username, token_hash) VALUES (?, ?)", "admin\nInjected", "old-hash"); err != nil {
		t.Fatalf("insert legacy admin user: %v", err)
	}

	var out bytes.Buffer
	ResetAdminToken(dataDir, false, &out)
	if !strings.Contains(out.String(), `Admin username: "admin\nInjected"`) {
		t.Fatalf("ResetAdminToken() did not safely escape the legacy username: %q", out.String())
	}
	if strings.Contains(out.String(), "admin\nInjected") {
		t.Fatalf("ResetAdminToken() emitted an unsafe raw username: %q", out.String())
	}
	credentials, err := os.ReadFile(CredentialsPath(dataDir))
	if err != nil {
		t.Fatalf("read credentials file: %v", err)
	}
	if strings.Contains(string(credentials), "Fluxo admin username:") {
		t.Fatalf("credentials file persisted an unsafe username: %q", credentials)
	}
	if token := ReadBootstrapToken(dataDir); token == "" {
		t.Fatal("reset token was not persisted for a legacy unsafe username")
	}
}

func TestManagedSiteOwnershipTarget(t *testing.T) {
	tests := []struct {
		name       string
		domain     string
		storedPath string
		want       string
		ok         bool
	}{
		{name: "managed site", domain: "example.com", storedPath: "/home/fluxo/example.com", want: "/home/fluxo/example.com", ok: true},
		{name: "promoted domain keeps managed path", domain: "new.example.com", storedPath: "/home/fluxo/old.example.com", want: "/home/fluxo/old.example.com", ok: true},
		{name: "outside managed root", domain: "example.com", storedPath: "/srv/example.com", ok: false},
		{name: "parent traversal", domain: "example.com", storedPath: "/home/fluxo/example.com/../other", ok: false},
		{name: "invalid domain", domain: "../example.com", storedPath: "/home/fluxo/example.com", ok: false},
		{name: "home root", domain: "example.com", storedPath: "/home/fluxo", ok: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := managedSiteOwnershipTarget(test.domain, test.storedPath)
			if got != test.want || ok != test.ok {
				t.Fatalf("managedSiteOwnershipTarget() = (%q, %t), want (%q, %t)", got, ok, test.want, test.ok)
			}
		})
	}
}
