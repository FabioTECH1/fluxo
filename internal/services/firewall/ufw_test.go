package firewall

import (
	"reflect"
	"testing"
)

func TestRuleCommandArgsPreservesProtocolForSourceRules(t *testing.T) {
	tests := []struct {
		name     string
		port     string
		fromIP   string
		ruleType string
		want     []string
	}{
		{
			name: "any source",
			port: "443/tcp", fromIP: "Any", ruleType: "allow",
			want: []string{"allow", "443/tcp"},
		},
		{
			name: "restricted tcp source",
			port: "9595/tcp", fromIP: "203.0.113.4/32", ruleType: "allow",
			want: []string{"allow", "from", "203.0.113.4/32", "to", "any", "port", "9595", "proto", "tcp"},
		},
		{
			name: "restricted port without protocol",
			port: "3306", fromIP: "10.0.0.0/8", ruleType: "deny",
			want: []string{"deny", "from", "10.0.0.0/8", "to", "any", "port", "3306"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ruleCommandArgs(test.port, test.fromIP, test.ruleType); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ruleCommandArgs() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestRuleExistsUsesCanonicalUFWCommands(t *testing.T) {
	added := `Added user rules (see 'ufw status' for running firewall):
ufw allow 22/tcp
ufw allow from 203.0.113.0/24 to any port 9595 proto tcp
ufw deny from 10.0.0.0/8 to any port 3306
ufw allow 'Nginx Full'
`
	if !RuleExists(added, "22/tcp", "Any", "allow") {
		t.Fatal("expected unrestricted SSH rule to exist")
	}
	if !RuleExists(added, "9595/tcp", "203.0.113.9/24", "allow") {
		t.Fatal("expected restricted dashboard rule to use the canonical CIDR")
	}
	if !RuleExists(added, "3306", "10.0.0.0/8", "deny") {
		t.Fatal("expected deny rule to exist")
	}
	if RuleExists(added, "9595/tcp", "Any", "allow") {
		t.Fatal("restricted dashboard rule must not match an unrestricted rule")
	}
	if !RuleExists(added, "Nginx Full", "Any", "allow") {
		t.Fatal("expected quoted UFW application profile to exist")
	}
	if RuleExists("ufw allow in on eth0 to any port 443 proto tcp\n", "443/tcp", "Any", "allow") {
		t.Fatal("interface-bound rule must not satisfy an unrestricted managed rule")
	}
	if RuleExists("ufw route allow from 10.0.0.0/8 to any port 3306 proto tcp\n", "3306/tcp", "10.0.0.0/8", "allow") {
		t.Fatal("routed rule must not satisfy a non-routed managed rule")
	}
}

func TestParseAddedRulesPreservesExternalUFWRules(t *testing.T) {
	output := `Added user rules (see 'ufw status' for running firewall):
ufw allow 22/tcp
ufw allow from 203.0.113.0/24 to any port 9595 proto tcp
ufw deny from 10.0.0.0/8 to any port 3306
ufw allow 'Nginx Full'
ufw limit in on eth0 to any port 2222 proto tcp comment 'Rate limited SSH'
ufw route allow in on wg0 out on eth0 from 10.10.0.0/16 to any port 443 proto tcp
ufw allow 'unterminated
`
	rules := ParseAddedRules(output)
	want := []AddedRule{
		{RuleType: "allow", Port: "22/tcp", FromIP: "Any", Command: "ufw allow 22/tcp", ManagedEquivalent: true},
		{RuleType: "allow", Port: "9595/tcp", FromIP: "203.0.113.0/24", Command: "ufw allow from 203.0.113.0/24 to any port 9595 proto tcp", ManagedEquivalent: true},
		{RuleType: "deny", Port: "3306", FromIP: "10.0.0.0/8", Command: "ufw deny from 10.0.0.0/8 to any port 3306", ManagedEquivalent: true},
		{RuleType: "allow", Port: "Nginx Full", FromIP: "Any", Command: "ufw allow 'Nginx Full'", ManagedEquivalent: true},
		{RuleType: "limit", Port: "2222/tcp", FromIP: "Any", Command: "ufw limit in on eth0 to any port 2222 proto tcp comment 'Rate limited SSH'"},
		{RuleType: "allow", Port: "443/tcp", FromIP: "10.10.0.0/16", Command: "ufw route allow in on wg0 out on eth0 from 10.10.0.0/16 to any port 443 proto tcp"},
	}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("ParseAddedRules() = %#v, want %#v", rules, want)
	}
}

func TestNormalizeFirewallRuleValues(t *testing.T) {
	if got := NormalizePort("0080/tcp"); got != "80/tcp" {
		t.Fatalf("NormalizePort() = %q, want 80/tcp", got)
	}
	if got := NormalizePort("01000:02000/udp"); got != "1000:2000/udp" {
		t.Fatalf("NormalizePort() range = %q, want 1000:2000/udp", got)
	}
	if got := NormalizeSource("203.0.113.9/24"); got != "203.0.113.0/24" {
		t.Fatalf("NormalizeSource() = %q, want 203.0.113.0/24", got)
	}
	if got := NormalizeSource("Anywhere"); got != "Any" {
		t.Fatalf("NormalizeSource() = %q, want Any", got)
	}
}
