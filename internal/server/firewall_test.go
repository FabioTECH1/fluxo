package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"fluxo/internal/database"
	"fluxo/internal/services/firewall"
)

func TestDeleteFirewallRuleProtectsInstallerManagedRules(t *testing.T) {
	previousDB := database.DB
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	t.Cleanup(func() {
		_ = database.DB.Close()
		database.DB = previousDB
	})

	result, err := database.DB.Exec(`INSERT INTO firewall_rules
		(name, rule_type, port, from_ip, managed_by)
		VALUES ('SSH', 'allow', '22/tcp', 'Any', 'installer')`)
	if err != nil {
		t.Fatalf("insert installer firewall rule: %v", err)
	}
	id, _ := result.LastInsertId()

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/firewall/1", nil)
	request.SetPathValue("id", "1")
	recorder := httptest.NewRecorder()
	(&Server{}).handleDeleteFirewallRule().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("delete status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	var count int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM firewall_rules WHERE id = ?", id).Scan(&count); err != nil {
		t.Fatalf("count protected firewall rule: %v", err)
	}
	if count != 1 {
		t.Fatal("installer-managed firewall rule was deleted")
	}
}

func TestMergeFirewallRulesIncludesExternalRulesReadOnly(t *testing.T) {
	managed := []database.FirewallRule{
		{ID: 1, Name: "HTTP", RuleType: "allow", Port: "80/tcp", FromIP: "Any", ManagedBy: "installer"},
		{ID: 2, Name: "HTTPS", RuleType: "allow", Port: "443/tcp", FromIP: "Any", ManagedBy: "installer"},
	}
	added := firewall.ParseAddedRules(`ufw allow 80/tcp
ufw allow 80/tcp
ufw deny from 10.0.0.0/8 to any port 3306 proto tcp
`)
	rules := mergeFirewallRules(managed, added)
	if len(rules) != 4 {
		t.Fatalf("merged rules = %d, want 4", len(rules))
	}
	if !rules[0].Active || rules[1].Active {
		t.Fatalf("managed active state = %v, %v; want true, false", rules[0].Active, rules[1].Active)
	}
	for _, rule := range rules[2:] {
		if rule.ID >= 0 || rule.ManagedBy != "external" || !rule.Active || rule.RawCommand == "" {
			t.Fatalf("external rule was not read-only API data: %+v", rule)
		}
	}
}

func TestMergeFirewallRulesDoesNotConsumeConstrainedExternalRule(t *testing.T) {
	managed := []database.FirewallRule{
		{ID: 1, Name: "HTTPS", RuleType: "allow", Port: "443/tcp", FromIP: "Any", ManagedBy: "installer"},
	}
	added := firewall.ParseAddedRules("ufw allow in on eth0 to any port 443 proto tcp\n")
	rules := mergeFirewallRules(managed, added)
	if len(rules) != 2 {
		t.Fatalf("merged rules = %d, want managed and external entries", len(rules))
	}
	if rules[0].Active {
		t.Fatal("interface-bound external rule incorrectly activated unrestricted managed rule")
	}
	if rules[1].ManagedBy != "external" || rules[1].RawCommand == "" {
		t.Fatalf("constrained rule was not preserved as read-only external data: %+v", rules[1])
	}
}
