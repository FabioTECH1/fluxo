package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// The initial bridge admits the installer's explicit profile. Unknown profiles
// require review; qualification must never silently harden a running machine.
func supportedServiceFile(text string) error {
	allowed := map[string]map[string]bool{
		"Unit":    {"Description": true, "After": true},
		"Service": {"ExecStart": true, "Restart": true, "User": true, "Environment": true, "PrivateTmp": true, "RestrictAddressFamilies": true},
		"Install": {"WantedBy": true},
	}
	section := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			if allowed[section] == nil {
				return errors.New("unsupported panel unit section")
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || !allowed[section][strings.TrimSpace(k)] || strings.HasSuffix(v, "\\") {
			return errors.New("review unsupported panel unit directives before qualification")
		}
	}
	return nil
}

func reloadCommands() map[string]bool {
	result := map[string]bool{}
	for _, prefix := range []string{"/usr/bin/systemctl", "/bin/systemctl"} {
		for _, version := range []string{"7.4", "8.0", "8.1", "8.2", "8.3", "8.4", "8.5"} {
			result[prefix+" reload php"+version+"-fpm"] = true
		}
	}
	return result
}

func checkEffectiveSudo(text string) error {
	parts := strings.Split(text, "Sudoers entry:")
	if len(parts) < 2 || strings.Contains(parts[0], "!authenticate") || strings.Contains(parts[0], "exempt_group") {
		return errors.New("review effective Fluxo sudo authentication policy")
	}
	allowed := reloadCommands()
	found := false
	for _, entry := range parts[1:] {
		var options, runAs, groups string
		var commands []string
		inCommands := false
		for _, line := range strings.Split(entry, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "RunAsUsers:") {
				runAs = strings.TrimSpace(strings.TrimPrefix(line, "RunAsUsers:"))
				continue
			}
			if strings.HasPrefix(line, "RunAsGroups:") {
				groups = strings.TrimSpace(strings.TrimPrefix(line, "RunAsGroups:"))
				continue
			}
			if strings.HasPrefix(line, "Options:") {
				options = strings.TrimSpace(strings.TrimPrefix(line, "Options:"))
				continue
			}
			if line == "Commands:" {
				inCommands = true
				continue
			}
			if inCommands {
				commands = append(commands, line)
			}
		}
		if len(commands) == 0 {
			return errors.New("unrecognized effective Fluxo sudo entry")
		}
		if options == "" || options == "authenticate" {
			if runAs != "ALL" || (groups != "" && groups != "ALL") || len(commands) != 1 || commands[0] != "ALL" {
				return errors.New("review custom password-authorized Fluxo sudo entry")
			}
			continue
		}
		if options != "!authenticate" || runAs != "root" || groups != "" {
			return errors.New("review custom passwordless Fluxo sudo entry")
		}
		for _, cmd := range commands {
			if !allowed[cmd] {
				return errors.New("Fluxo passwordless sudo exceeds approved PHP reload commands")
			}
		}
		found = true
	}
	if !found {
		return errors.New("install the approved Fluxo PHP reload sudo policy")
	}
	return nil
}

func effectiveSSHPolicy(text string) error {
	p := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 {
			p[f[0]] = f[1]
		}
	}
	if p["passwordauthentication"] != "no" || p["kbdinteractiveauthentication"] != "no" || p["pubkeyauthentication"] != "yes" || (p["permitrootlogin"] != "without-password" && p["permitrootlogin"] != "prohibit-password") {
		return errors.New("review effective SSH key-only authentication before qualification")
	}
	return nil
}

// Refuse Match profiles rather than sampling one address and inferring that its
// authentication policy applies to every future connection.
func inspectSSHFiles(path string, seen map[string]bool) error {
	if len(seen) > 128 || seen[path] {
		return errors.New("unsupported SSH include graph")
	}
	seen[path] = true
	b, err := readProtected(path, 1<<20)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch strings.ToLower(f[0]) {
		case "match":
			return errors.New("review SSH Match profiles before bridge qualification")
		case "include":
			for _, pattern := range f[1:] {
				if strings.ContainsAny(pattern, "\"'$\\") {
					return errors.New("unsupported SSH include syntax")
				}
				if !filepath.IsAbs(pattern) {
					pattern = filepath.Join("/etc/ssh", pattern)
				}
				paths, e := filepath.Glob(pattern)
				if e != nil {
					return e
				}
				for _, p := range paths {
					if err = inspectSSHFiles(p, seen); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

const authFilter = "[Definition]\nfailregex = ^.*fluxo_auth_failed remote=<HOST> username=.*$\nignoreregex =\n[Init]\njournalmatch = _SYSTEMD_UNIT=fluxo.service"

func policyLines(s string) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, ";") {
			lines = append(lines, strings.Join(strings.Fields(line), " "))
		}
	}
	return strings.Join(lines, "\n")
}
func checkAuthFiles(filter, jail, port string) error {
	expected := fmt.Sprintf("[fluxo-auth]\nenabled = true\nfilter = fluxo-auth\nbackend = systemd\nport = 80,443,%s\nprotocol = tcp\nmaxretry = 5\nfindtime = 15m\nbantime = 1h", port)
	if policyLines(filter) != policyLines(authFilter) || policyLines(jail) != policyLines(expected) {
		return errors.New("review Fluxo auth jail/filter against the supported installer profile")
	}
	return nil
}
func checkSecurity(ctx context.Context, environment string) error {
	if err := inspectSSHFiles("/etc/ssh/sshd_config", map[string]bool{}); err != nil {
		return err
	}
	for _, name := range []string{"root", "fluxo"} {
		out, err := run(ctx, "/usr/sbin/sshd", "-T", "-C", "user="+name+",host=localhost,addr=127.0.0.1")
		if err != nil {
			return errors.New("cannot verify effective SSH policy")
		}
		if err = effectiveSSHPolicy(out); err != nil {
			return err
		}
	}
	out, err := run(ctx, "/usr/bin/env", "LC_ALL=C", "/usr/bin/sudo", "-ll", "-U", "fluxo")
	if err != nil {
		return errors.New("cannot inspect effective Fluxo sudo authorization")
	}
	if err = checkEffectiveSudo(out); err != nil {
		return err
	}
	port := "9595"
	assignments, err := serviceAssignments(environment)
	if err != nil {
		return err
	}
	for _, a := range assignments {
		if strings.HasPrefix(a, "FLUXO_PORT=") {
			port = strings.TrimPrefix(a, "FLUXO_PORT=")
		}
	}
	filter, err := readProtected("/etc/fail2ban/filter.d/fluxo-auth.conf", 1<<20)
	if err != nil {
		return err
	}
	jail, err := readProtected("/etc/fail2ban/jail.d/fluxo-auth.conf", 1<<20)
	if err != nil {
		return err
	}
	if err = checkAuthFiles(string(filter), string(jail), port); err != nil {
		return err
	}
	// Check the loaded jail as well as its source: stale/disabled service state is
	// not evidence that the configured authentication protection is active.
	for _, check := range []struct {
		args []string
		want string
	}{
		{[]string{"get", "fluxo-auth", "maxretry"}, "5"},
		{[]string{"get", "fluxo-auth", "findtime"}, "900"},
		{[]string{"get", "fluxo-auth", "bantime"}, "3600"},
		{[]string{"get", "fluxo-auth", "journalmatch"}, "Current match filter:\n_SYSTEMD_UNIT=fluxo.service"},
	} {
		out, err = run(ctx, "/usr/bin/fail2ban-client", check.args...)
		if err != nil || strings.TrimSpace(out) != check.want {
			return errors.New("verify loaded Fluxo auth jail policy")
		}
	}
	expectedRegex, err := run(ctx, "/usr/bin/python3", "-c", "from fail2ban.server.failregex import FailRegex; print(FailRegex('^.*fluxo_auth_failed remote=<HOST> username=.*$').getRegex())")
	if err != nil {
		return errors.New("cannot compile the supported auth expression")
	}
	out, err = run(ctx, "/usr/bin/fail2ban-client", "get", "fluxo-auth", "failregex")
	if err != nil || !loadedAuthRegex(out, strings.TrimSpace(expectedRegex)) {
		return errors.New("verify the complete loaded Fluxo auth failure expression")
	}
	out, err = run(ctx, "/usr/bin/fail2ban-client", "get", "fluxo-auth", "ignoreregex")
	if err != nil || strings.Contains(out, "[0]:") {
		return errors.New("review loaded Fluxo auth ignore expressions")
	}
	out, err = run(ctx, "/usr/bin/fail2ban-client", "get", "fluxo-auth", "actions")
	if err != nil {
		return errors.New("verify active Fluxo auth firewall action")
	}
	action := strings.TrimSpace(strings.TrimPrefix(out, "The jail fluxo-auth has the following actions:"))
	if action != "nftables" && action != "nftables-multiport" && action != "iptables-multiport" {
		return errors.New("review the unsupported Fluxo auth firewall action profile")
	}
	out, err = run(ctx, "/usr/bin/python3", "-c", resolveLoadedAction, action)
	if err != nil {
		return errors.New("cannot resolve the loaded Fluxo auth firewall profile")
	}
	var loaded struct {
		Timeout  float64                      `json:"timeout"`
		Families map[string]map[string]string `json:"families"`
	}
	if json.Unmarshal([]byte(out), &loaded) != nil || loaded.Timeout != 60 {
		return errors.New("review loaded Fluxo auth firewall execution policy")
	}
	if action == "iptables-multiport" {
		return loadedIPTablesAction(port, loaded.Families)
	}
	return loadedNftAction(port, loaded.Families)
}

// Resolve the exact in-memory authority, including conditional family overrides,
// with the installed library's own substitution rules. This performs only a
// get on the local daemon and string substitution; it never executes actions.
const resolveLoadedAction = `import json, sys
from fail2ban.client.csocket import CSocket
from fail2ban.server.action import CommandAction
sock = CSocket('/var/run/fail2ban/fail2ban.sock')
try:
    reply = sock.send(['get', 'fluxo-auth', 'action', sys.argv[1], '_properties'])
    if reply[0] != 0 or not isinstance(reply[1], dict):
        raise RuntimeError('unsupported action authority')
    properties = reply[1]
    operations = ['actionstart', 'actioncheck', 'actionban', 'actionreban', 'actionrepair']
    families = {family: {operation: CommandAction.replaceTag('<'+operation+'>', properties, conditional='family='+family) for operation in operations} for family in ['inet4','inet6']}
    print(json.dumps({'timeout': properties.get('timeout'), 'families': families}))
finally:
    sock.close()
`

func loadedAuthRegex(output, expected string) bool {
	marker := "[0]: "
	i := strings.Index(output, marker)
	return i >= 0 && strings.Count(output, marker) == 1 && strings.TrimSpace(output[i+len(marker):]) == expected
}

func loadedNftAction(port string, loaded map[string]map[string]string) error {
	start := `nft add table inet f2b-table
nft -- add chain inet f2b-table f2b-chain \{ type filter hook input priority -1 \; \}
nft add set inet f2b-table <addr_set> \{ type <addr_type>\; \}
for proto in $(echo 'tcp' | sed 's/,/ /g'); do
nft add rule inet f2b-table f2b-chain $proto dport \{ $(echo '80,443,` + port + `' | sed s/:/-/g) \} <addr_family> saddr @<addr_set> reject
done`
	want := map[string]string{
		"actionstart": start,
		"actioncheck": `nft list chain inet f2b-table f2b-chain | grep -q '@<addr_set>[ \t]'`,
		"actionban":   `nft add element inet f2b-table <addr_set> \{ <ip> \}`,
	}
	want["actionreban"], want["actionrepair"] = "", ""
	for _, family := range []string{"inet4", "inet6"} {
		set, addressType, addressFamily := "addr-set-fluxo-auth", "ipv4_addr", "ip"
		if family == "inet6" {
			set, addressType, addressFamily = "addr6-set-fluxo-auth", "ipv6_addr", "ip6"
		}
		replace := strings.NewReplacer("<addr_set>", set, "<addr_type>", addressType, "<addr_family>", addressFamily)
		for key, template := range want {
			actual, ok := loaded[family][key]
			if !ok || actual != replace.Replace(template) {
				return fmt.Errorf("review resolved Fluxo auth %s firewall %s against the supported nftables profile", family, key)
			}
		}
	}
	return nil
}

// The Ubuntu 22.04 default uses the legacy command adapter, including distinct
// IPv6 rejection semantics. Compare fully resolved commands here too.
func loadedIPTablesAction(port string, loaded map[string]map[string]string) error {
	for _, family := range []string{"inet4", "inet6"} {
		binary, rejection := "iptables -w", "icmp-port-unreachable"
		if family == "inet6" {
			binary, rejection = "ip6tables -w", "icmp6-port-unreachable"
		}
		want := map[string]string{
			"actionstart": binary + " -N f2b-fluxo-auth\n" + binary + " -A f2b-fluxo-auth -j RETURN\n" + binary + " -I INPUT -p tcp -m multiport --dports 80,443," + port + " -j f2b-fluxo-auth",
			"actioncheck": binary + ` -n -L INPUT | grep -q 'f2b-fluxo-auth[ \t]'`,
			"actionban":   binary + " -I f2b-fluxo-auth 1 -s <ip> -j REJECT --reject-with " + rejection,
			"actionreban": "", "actionrepair": "",
		}
		for key, expected := range want {
			actual, ok := loaded[family][key]
			if !ok || actual != expected {
				return fmt.Errorf("review resolved Fluxo auth %s firewall %s against the supported iptables profile", family, key)
			}
		}
	}
	return nil
}
