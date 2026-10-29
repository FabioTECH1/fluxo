package bootstrap

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/database"

	"golang.org/x/crypto/bcrypt"
)

func withBootstrapTestDB(t *testing.T) string {
	t.Helper()
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
	return dataDir
}

func TestImportInstallerFirewallRules(t *testing.T) {
	dataDir := withBootstrapTestDB(t)
	retireLegacyAssumedFirewallRules()
	manifest := installerFirewallManifest{
		Version: 1,
		Rules: []installerFirewallRule{
			{Name: "SSH", RuleType: "allow", Port: "2222/tcp", FromIP: "Any"},
			{Name: "Fluxo Dashboard", RuleType: "allow", Port: "9595/tcp", FromIP: "203.0.113.4/32"},
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	path := filepath.Join(dataDir, installerFirewallManifestName)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	importInstallerFirewallRules(dataDir)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("manifest was not consumed, stat error = %v", err)
	}
	var count int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM firewall_rules WHERE managed_by = 'installer'").Scan(&count); err != nil {
		t.Fatalf("count imported rules: %v", err)
	}
	if count != 2 {
		t.Fatalf("imported rule count = %d, want 2", count)
	}
	var port, source string
	if err := database.DB.QueryRow("SELECT port, from_ip FROM firewall_rules WHERE name = 'Fluxo Dashboard'").Scan(&port, &source); err != nil {
		t.Fatalf("read dashboard rule: %v", err)
	}
	if port != "9595/tcp" || source != "203.0.113.4/32" {
		t.Fatalf("dashboard rule = %s from %s", port, source)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("rewrite manifest: %v", err)
	}
	importInstallerFirewallRules(dataDir)
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM firewall_rules").Scan(&count); err != nil {
		t.Fatalf("count rules after repeated import: %v", err)
	}
	if count != 2 {
		t.Fatalf("repeated import created duplicates: count = %d", count)
	}
}

func TestImportInstallerFirewallRulesProtectsEquivalentDashboardRecord(t *testing.T) {
	dataDir := withBootstrapTestDB(t)
	if _, err := database.DB.Exec(`INSERT INTO firewall_rules
		(name, rule_type, port, from_ip, managed_by)
		VALUES ('Custom SSH', 'allow', '2222/tcp', 'Any', 'dashboard')`); err != nil {
		t.Fatalf("insert dashboard rule: %v", err)
	}
	manifest := installerFirewallManifest{
		Version: 1,
		Rules: []installerFirewallRule{
			{Name: "SSH", RuleType: "allow", Port: "2222/tcp", FromIP: "Any"},
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, installerFirewallManifestName), data, 0600); err != nil {
		t.Fatal(err)
	}
	importInstallerFirewallRules(dataDir)

	var name, managedBy string
	if err := database.DB.QueryRow("SELECT name, managed_by FROM firewall_rules WHERE port = '2222/tcp'").Scan(&name, &managedBy); err != nil {
		t.Fatal(err)
	}
	if name != "SSH" || managedBy != "installer" {
		t.Fatalf("imported rule = %q managed by %q, want protected SSH installer rule", name, managedBy)
	}
}

func TestRetireLegacyAssumedFirewallRules(t *testing.T) {
	withBootstrapTestDB(t)
	legacy := []installerFirewallRule{
		{Name: "SSH", RuleType: "allow", Port: "22", FromIP: "Any"},
		{Name: "HTTP", RuleType: "allow", Port: "80", FromIP: "Any"},
		{Name: "HTTPS", RuleType: "allow", Port: "443", FromIP: "Any"},
		{Name: "Fluxo Daemon", RuleType: "allow", Port: "9595", FromIP: "Any"},
	}
	for _, rule := range legacy {
		if _, err := database.DB.Exec("INSERT INTO firewall_rules (name, rule_type, port, from_ip) VALUES (?, ?, ?, ?)", rule.Name, rule.RuleType, rule.Port, rule.FromIP); err != nil {
			t.Fatalf("insert legacy rule: %v", err)
		}
	}
	if _, err := database.DB.Exec("INSERT INTO firewall_rules (name, rule_type, port, from_ip) VALUES ('Private API', 'allow', '8080/tcp', '10.0.0.0/8')"); err != nil {
		t.Fatalf("insert custom rule: %v", err)
	}
	retireLegacyAssumedFirewallRules()
	var count int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM firewall_rules").Scan(&count); err != nil {
		t.Fatalf("count remaining rules: %v", err)
	}
	if count != 1 {
		t.Fatalf("remaining rule count = %d, want only the custom rule", count)
	}
	retireLegacyAssumedFirewallRules()
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM firewall_rules").Scan(&count); err != nil {
		t.Fatalf("count rules after repeated migration: %v", err)
	}
	if count != 1 {
		t.Fatalf("repeated migration changed rules: count = %d", count)
	}
}

func TestRetireLegacyAssumedFirewallRulesRemovesPartialDefaultSet(t *testing.T) {
	withBootstrapTestDB(t)
	for _, rule := range []installerFirewallRule{
		{Name: "SSH", RuleType: "allow", Port: "22", FromIP: "Any"},
		{Name: "HTTPS", RuleType: "allow", Port: "443", FromIP: "Any"},
	} {
		if _, err := database.DB.Exec("INSERT INTO firewall_rules (name, rule_type, port, from_ip) VALUES (?, ?, ?, ?)", rule.Name, rule.RuleType, rule.Port, rule.FromIP); err != nil {
			t.Fatalf("insert partial legacy rule: %v", err)
		}
	}
	if _, err := database.DB.Exec("INSERT INTO firewall_rules (name, rule_type, port, from_ip) VALUES ('Private API', 'allow', '8080/tcp', '10.0.0.0/8')"); err != nil {
		t.Fatalf("insert custom rule: %v", err)
	}

	retireLegacyAssumedFirewallRules()
	var count int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM firewall_rules").Scan(&count); err != nil {
		t.Fatalf("count remaining rules: %v", err)
	}
	if count != 1 {
		t.Fatalf("remaining rule count = %d, want only the custom rule", count)
	}
}

func TestReconcileLegacyFirewallRulesRecoversOnlyVerifiedDefaults(t *testing.T) {
	withBootstrapTestDB(t)
	retireLegacyAssumedFirewallRules()
	addedRules := `Added user rules (see 'ufw status' for running firewall):
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw allow 9595/tcp
ufw allow from 10.0.0.0/8 to any port 3306 proto tcp
`
	recovered, err := reconcileLegacyFirewallRules(addedRules)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 4 {
		t.Fatalf("recovered rules = %d, want 4", recovered)
	}
	var count int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM firewall_rules WHERE managed_by = 'installer'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Fatalf("installer-managed rules = %d, want 4", count)
	}
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM firewall_rules WHERE port = '3306/tcp'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("external MySQL rule was incorrectly adopted")
	}
	recovered, err = reconcileLegacyFirewallRules(addedRules)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 0 {
		t.Fatalf("repeated reconciliation recovered %d rules, want 0", recovered)
	}
}

func TestReconcileLegacyFirewallRulesPreservesDashboardOwnership(t *testing.T) {
	withBootstrapTestDB(t)
	if _, err := database.DB.Exec(`INSERT INTO firewall_rules
		(name, rule_type, port, from_ip, managed_by)
		VALUES ('Public web', 'allow', '80/tcp', 'Any', 'dashboard')`); err != nil {
		t.Fatal(err)
	}
	recovered, err := reconcileLegacyFirewallRules("ufw allow 80/tcp\n")
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 0 {
		t.Fatalf("recovered rules = %d, want 0", recovered)
	}
	var managedBy string
	if err := database.DB.QueryRow("SELECT managed_by FROM firewall_rules WHERE port = '80/tcp'").Scan(&managedBy); err != nil {
		t.Fatal(err)
	}
	if managedBy != "dashboard" {
		t.Fatalf("existing dashboard rule ownership changed to %q", managedBy)
	}
}

func TestReconcileComposerUpdateCronPreservesIndependentRecords(t *testing.T) {
	withBootstrapTestDB(t)
	type row struct {
		id              int
		expr, cmd, user string
	}
	originals := []row{}
	for _, v := range []row{{expr: "0 0 * * 0", cmd: legacyComposerUpdateCommand, user: "root"}, {expr: "15 1 * * *", cmd: composerUpdateCronCommand, user: "fluxo"}, {expr: "7 2 * * *", cmd: composerUpdateCronCommand, user: "root"}, {expr: "0 3 * * *", cmd: "printf custom", user: "root"}} {
		result, err := database.DB.Exec("INSERT INTO crons(site_id,name,expression,command,user) VALUES(0,?,?,?,?)", composerUpdateCronName, v.expr, v.cmd, v.user)
		if err != nil {
			t.Fatal(err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		v.id = int(id)
		originals = append(originals, v)
	}
	for attempt := 0; attempt < 2; attempt++ {
		reconcileComposerUpdateCron(func(int, string, string, string, string) error {
			t.Error("existing trigger must not be rewritten or resurrected")
			return nil
		}, func(int) error { t.Error("independent duplicate must not be deleted"); return nil })
		rows, err := database.DB.Query("SELECT id,expression,command,user FROM crons ORDER BY id")
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for rows.Next() {
			var got row
			if err = rows.Scan(&got.id, &got.expr, &got.cmd, &got.user); err != nil {
				t.Fatal(err)
			}
			if n >= len(originals) || got != originals[n] {
				t.Fatalf("record changed: %#v", got)
			}
			n++
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if n != len(originals) {
			t.Fatal("record set changed", n)
		}
	}
}

func TestReconcileComposerUpdateCronSeedsMissingJob(t *testing.T) {
	withBootstrapTestDB(t)
	var created composerCronRecord
	reconcileComposerUpdateCron(
		func(id int, _ string, expression, command, user string) error {
			created = composerCronRecord{id: id, expression: expression, command: command, user: user}
			return nil
		},
		func(int) error { return nil },
	)
	if created.id == 0 || created.expression != composerUpdateCronExpression ||
		created.command != composerUpdateCronCommand || created.user != "root" {
		t.Fatalf("seeded Composer cron = %#v", created)
	}
	var count int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM crons WHERE id = ? AND site_id = 0", created.id).Scan(&count); err != nil {
		t.Fatalf("count seeded Composer cron: %v", err)
	}
	if count != 1 {
		t.Fatalf("seeded Composer cron count = %d, want 1", count)
	}
}

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
	if !strings.Contains(out.String(), "New token: "+token) {
		t.Fatalf("ResetAdminToken() did not reveal the generated token: %q", out.String())
	}
	if !strings.Contains(out.String(), "with root-only permissions") {
		t.Fatalf("ResetAdminToken() did not report the recovery copy: %q", out.String())
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

func TestCompletePendingAdminResetRecoversInterruptedReset(t *testing.T) {
	dataDir := withBootstrapTestDB(t)
	oldHash, err := bcrypt.GenerateFromPassword([]byte("old-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	result, err := database.DB.Exec("INSERT INTO users (username, token_hash) VALUES ('admin', ?)", string(oldHash))
	if err != nil {
		t.Fatal(err)
	}
	userID, _ := result.LastInsertId()
	token := strings.Repeat("a", 32)
	if err := writePendingAdminReset(dataDir, pendingAdminReset{UserID: int(userID), Token: token}); err != nil {
		t.Fatal(err)
	}
	if err := writeAccountRecoveryCredentials(dataDir, false, "admin", token); err != nil {
		t.Fatal(err)
	}

	username, completed, err := completePendingAdminReset(dataDir, false)
	if err != nil {
		t.Fatalf("completePendingAdminReset() error = %v", err)
	}
	if !completed || username != "admin" {
		t.Fatalf("completion = %v, username = %q", completed, username)
	}
	var storedHash string
	if err := database.DB.QueryRow("SELECT token_hash FROM users WHERE id = ?", userID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(token)); err != nil {
		t.Fatal("recovered token does not match the stored hash")
	}
	if _, err := os.Stat(pendingAdminResetPath(dataDir)); !os.IsNotExist(err) {
		t.Fatalf("pending reset marker was not removed: %v", err)
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
