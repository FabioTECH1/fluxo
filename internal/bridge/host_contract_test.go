package bridge

import (
	"strings"
	"testing"

	"fluxo/internal/services/nginx"
)

func TestSecurityContractRejectsValidButUnsafePolicies(t *testing.T) {
	ssh := "passwordauthentication no\nkbdinteractiveauthentication no\npubkeyauthentication yes\npermitrootlogin without-password\n"
	if err := effectiveSSHPolicy(ssh); err != nil {
		t.Fatal(err)
	}
	for _, old := range []string{"passwordauthentication no", "kbdinteractiveauthentication no", "pubkeyauthentication yes", "permitrootlogin without-password"} {
		value := "yes"
		if old == "pubkeyauthentication yes" {
			value = "no"
		}
		if effectiveSSHPolicy(strings.Replace(ssh, old, strings.Fields(old)[0]+" "+value, 1)) == nil {
			t.Fatal("accepted unsafe SSH policy", old)
		}
	}
	sudo := `Matching Defaults entries for fluxo on fixture:
    env_reset, use_pty

Sudoers entry: /etc/sudoers
    RunAsUsers: ALL
    RunAsGroups: ALL
    Commands:
        ALL

Sudoers entry: /etc/sudoers.d/fluxo
    RunAsUsers: root
    Options: !authenticate
    Commands:
        /usr/bin/systemctl reload php8.4-fpm
`
	if err := checkEffectiveSudo(sudo); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(sudo, "/usr/bin/systemctl reload php8.4-fpm", "ALL", 1),
		strings.Replace(sudo, "env_reset, use_pty", "env_reset, !authenticate", 1),
		strings.Replace(sudo, "RunAsUsers: root", "RunAsUsers: ALL", 1),
		strings.Split(sudo, "Sudoers entry: /etc/sudoers.d/fluxo")[0],
	} {
		if checkEffectiveSudo(bad) == nil {
			t.Fatal("accepted unsafe or incomplete effective sudo policy")
		}
	}
	jail := "[fluxo-auth]\nenabled = true\nfilter = fluxo-auth\nbackend = systemd\nport = 80,443,9595\nprotocol = tcp\nmaxretry = 5\nfindtime = 15m\nbantime = 1h"
	if err := checkAuthFiles(authFilter, jail, "9595"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", strings.Replace(jail, "enabled = true", "enabled = false", 1), strings.Replace(jail, "9595", "8080", 1), jail + "\naction = echo"} {
		if checkAuthFiles(authFilter, bad, "9595") == nil {
			t.Fatal("accepted unsupported auth jail")
		}
	}
	if checkAuthFiles("", jail, "9595") == nil {
		t.Fatal("accepted empty auth filter")
	}
}

func TestServiceRejectsFilesystemRemappingAndUnknownFragmentDirectives(t *testing.T) {
	base := map[string]string{"LoadState": "loaded", "DynamicUser": "no", "NeedDaemonReload": "no", "FragmentPath": "/etc/systemd/system/fluxo.service", "ProtectHome": "no", "ProtectSystem": "no", "PrivateUsers": "no", "ExecStart": "{ path=/usr/local/bin/fluxo ; argv[]=/usr/local/bin/fluxo ; ignore_errors=no ; }", "Environment": "FLUXO_ENV=prod"}
	if err := serviceContext(base, "/var/lib/fluxo", false); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"BindPaths", "BindReadOnlyPaths", "TemporaryFileSystem", "MountImages", "ExtensionImages", "ExtensionDirectories", "DropInPaths", "RootDirectory", "RootImage", "ReadOnlyPaths", "ReadWritePaths", "InaccessiblePaths", "PrivateUsers", "ProtectHome", "ProtectSystem", "NeedDaemonReload"} {
		p := map[string]string{}
		for k, v := range base {
			p[k] = v
		}
		p[field] = "unsupported"
		if serviceContext(p, "/var/lib/fluxo", false) == nil {
			t.Fatal("accepted filesystem remapping or stale service", field)
		}
	}
	p := map[string]string{}
	for k, v := range base {
		p[k] = v
	}
	p["Environment"] += " LD_PRELOAD=/other.so"
	if serviceContext(p, "/var/lib/fluxo", false) == nil {
		t.Fatal("accepted custom preload environment")
	}
	good := "[Unit]\nDescription=Fluxo\nAfter=network.target\n[Service]\nExecStart=/usr/local/bin/fluxo\nUser=root\nEnvironment=FLUXO_ENV=prod\nPrivateTmp=true\n[Install]\nWantedBy=multi-user.target\n"
	if err := supportedServiceFile(good); err != nil {
		t.Fatal(err)
	}
	for _, directive := range []string{"BindPaths=/other:/var/lib/fluxo", "TemporaryFileSystem=/var", "ExecStartPre=/bin/anything", "RootImage=/other"} {
		if supportedServiceFile(strings.Replace(good, "PrivateTmp=true", "PrivateTmp=true\n"+directive, 1)) == nil {
			t.Fatal("accepted unsupported fragment directive", directive)
		}
	}
}

func TestSiteContractRejectsDisabledAndMisroutedVhosts(t *testing.T) {
	path := "/etc/nginx/sites-available/old-infrastructure.test"
	if nginxHTTPIncluded(map[string][]string{}, path) {
		t.Fatal("disabled site accepted")
	}
	if nginxHTTPIncluded(map[string][]string{path: {"stream"}}, path) {
		t.Fatal("non-http include accepted")
	}
	if !nginxHTTPIncluded(map[string][]string{path: {"http"}}, path) {
		t.Fatal("enabled http site refused")
	}
	for _, app := range []string{"php", "laravel", "wordpress", "node", "html", "python", "python-django"} {
		expected, err := nginx.RenderExistingConfigWithHostsNamed("old-infrastructure.test", "old-infrastructure.test", "new-domain.test", "/home/fluxo/old-infrastructure.test/public", "8.4", app, 3456, []nginx.HostCertificate{{Domain: "new-domain.test", CertPath: "/existing/cert", KeyPath: "/existing/key"}})
		if err != nil {
			t.Fatal(err)
		}
		if err = sameNginxConfig("# preserved comments\n"+expected, expected); err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{
			strings.ReplaceAll(expected, "new-domain.test", "unrelated.test"),
			strings.ReplaceAll(expected, "/home/fluxo/old-infrastructure.test/public", "/other/root"),
		} {
			if sameNginxConfig(bad, expected) == nil {
				t.Fatal("accepted misrouted vhost", app)
			}
		}
		if strings.Contains(expected, "proxy_pass http://127.0.0.1:3456") && sameNginxConfig(strings.ReplaceAll(expected, ":3456", ":9999"), expected) == nil {
			t.Fatal("accepted wrong upstream")
		}
		if strings.Contains(expected, "php8.4-fpm") && sameNginxConfig(strings.ReplaceAll(expected, "php8.4-fpm", "php8.3-fpm"), expected) == nil {
			t.Fatal("accepted wrong PHP pool")
		}
	}
}

func TestLoadedAuthRejectsNonmatchingExpressionAndDisconnectedFirewall(t *testing.T) {
	regex := `^.*fluxo_auth_failed remote=(?P<ip4>[0-9.]+) username=.*$`
	output := "The following regular expression are defined:\n`- [0]: " + regex + "\n"
	if !loadedAuthRegex(output, regex) {
		t.Fatal("supported loaded expression refused")
	}
	for _, bad := range []string{strings.Replace(output, `(?P<ip4>[0-9.]+)`, `(?!)`, 1), output + "|- [1]: another\n"} {
		if loadedAuthRegex(bad, regex) {
			t.Fatal("unsupported loaded expression admitted")
		}
	}
	loaded := map[string]string{
		"name": "fluxo-auth", "addr_set": "addr-set-<name>",
		"actionstart": `nft add table inet f2b-table
nft -- add chain inet f2b-table f2b-chain \{ type filter hook input priority -1 \; \}
nft add set inet f2b-table <addr_set> \{ type <addr_type>\; \}
for proto in $(echo 'tcp' | sed 's/,/ /g'); do
nft add rule inet f2b-table f2b-chain $proto dport \{ $(echo '80,443,9595' | sed s/:/-/g) \} <addr_family> saddr @<addr_set> reject
done`,
		"actioncheck": `nft list chain inet f2b-table f2b-chain | grep -q '@<addr_set>[ \t]'`,
		"actionban":   `nft add element inet f2b-table <addr_set> \{ <ip> \}`,
	}
	families := map[string]map[string]string{}
	for _, family := range []string{"inet4", "inet6"} {
		set, typ, ip := "addr-set-fluxo-auth", "ipv4_addr", "ip"
		if family == "inet6" {
			set, typ, ip = "addr6-set-fluxo-auth", "ipv6_addr", "ip6"
		}
		r := strings.NewReplacer("<addr_set>", set, "<addr_type>", typ, "<addr_family>", ip)
		families[family] = map[string]string{"actionreban": "", "actionrepair": ""}
		for _, key := range []string{"actionstart", "actioncheck", "actionban"} {
			families[family][key] = r.Replace(loaded[key])
		}
	}
	if err := loadedNftAction("9595", families); err != nil {
		t.Fatal(err)
	}
	for _, family := range []string{"inet4", "inet6"} {
		for _, key := range []string{"actionstart", "actioncheck", "actionban", "actionreban", "actionrepair"} {
			original := families[family][key]
			families[family][key] = "unsupported"
			if loadedNftAction("9595", families) == nil {
				t.Fatal("unsupported loaded operation admitted", family, key)
			}
			families[family][key] = original
		}
		for _, old := range []string{"ipv4_addr", "ipv6_addr", "ip saddr", "ip6 saddr"} {
			original := families[family]["actionstart"]
			if strings.Contains(original, old) {
				families[family]["actionstart"] = strings.Replace(original, old, "invalid", 1)
				if loadedNftAction("9595", families) == nil {
					t.Fatal("invalid family override admitted", family, old)
				}
				families[family]["actionstart"] = original
			}
		}
	}
	if loadedNftAction("8080", families) == nil {
		t.Fatal("wrong protected ports admitted")
	}

}

func TestNginxClaimsUseCaseInsensitiveHostnames(t *testing.T) {
	claims := map[string]string{}
	if err := addNginxHostClaim(claims, "EXAMPLE.COM", "/earlier.conf"); err != nil {
		t.Fatal(err)
	}
	if addNginxHostClaim(claims, "example.com", "/canonical-fluxo.conf") == nil {
		t.Fatal("case-insensitive Nginx collision admitted")
	}
	if err := addNginxHostClaim(claims, "example.com", "/earlier.conf"); err != nil {
		t.Fatal("same-file HTTP/HTTPS names refused", err)
	}
}

func TestLoadedUbuntu22IPTablesProfileAndFamilyRejection(t *testing.T) {
	// Captured from the unmodified installer profile on Ubuntu 22.04, Fail2Ban 0.11.2.
	loaded := map[string]map[string]string{
		"inet4": {
			"actionstart": "iptables -w -N f2b-fluxo-auth\niptables -w -A f2b-fluxo-auth -j RETURN\niptables -w -I INPUT -p tcp -m multiport --dports 80,443,9595 -j f2b-fluxo-auth",
			"actioncheck": `iptables -w -n -L INPUT | grep -q 'f2b-fluxo-auth[ \t]'`,
			"actionban":   "iptables -w -I f2b-fluxo-auth 1 -s <ip> -j REJECT --reject-with icmp-port-unreachable",
			"actionreban": "", "actionrepair": "",
		},
		"inet6": {
			"actionstart": "ip6tables -w -N f2b-fluxo-auth\nip6tables -w -A f2b-fluxo-auth -j RETURN\nip6tables -w -I INPUT -p tcp -m multiport --dports 80,443,9595 -j f2b-fluxo-auth",
			"actioncheck": `ip6tables -w -n -L INPUT | grep -q 'f2b-fluxo-auth[ \t]'`,
			"actionban":   "ip6tables -w -I f2b-fluxo-auth 1 -s <ip> -j REJECT --reject-with icmp6-port-unreachable",
			"actionreban": "", "actionrepair": "",
		},
	}
	if err := loadedIPTablesAction("9595", loaded); err != nil {
		t.Fatal(err)
	}
	for _, family := range []string{"inet4", "inet6"} {
		for _, key := range []string{"actionstart", "actioncheck", "actionban", "actionreban", "actionrepair"} {
			original := loaded[family][key]
			loaded[family][key] = "unsupported"
			if loadedIPTablesAction("9595", loaded) == nil {
				t.Fatal("unsupported firewall action admitted", family, key)
			}
			loaded[family][key] = original
		}
	}
	if loadedIPTablesAction("8080", loaded) == nil {
		t.Fatal("wrong protected ports admitted")
	}
	original := loaded["inet6"]["actionban"]
	loaded["inet6"]["actionban"] = strings.Replace(original, "icmp6-port-unreachable", "icmp-port-unreachable", 1)
	if loadedIPTablesAction("9595", loaded) == nil {
		t.Fatal("incorrect IPv6 behavior admitted")
	}
}
