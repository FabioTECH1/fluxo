package maintenancetask

import (
	"fmt"
	"testing"

	"fluxo/internal/config"
)

func TestCertbotPanelWebrootMatchesFluxoIssuance(t *testing.T) {
	for _, root := range []string{config.ProductionPanelChallengeRoot, "/home/fluxo/example.test/current/public", "/var/www/html", "/var/www/html/example/public"} {
		t.Run(root, func(t *testing.T) {
			// Certbot persists both the ConfigObj list and the domain mapping.
			raw := fmt.Sprintf("[renewalparams]\nauthenticator = webroot\nwebroot_path = %s,\ndeploy_hook = systemctl reload nginx\n[[webroot_map]]\npanel.example.test = %s\n", root, root)
			if err := validateCertbotConfig([]byte(raw), true); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, root := range []string{"/var/lib/fluxo-acme-other", "/var/lib/fluxo-acme/../fluxo", "/var/lib/fluxo-acme/", "/var/lib/fluxo", "/var/lib/fluxo/acme", "/root", "/tmp/acme"} {
		for _, field := range []string{"webroot_path = %s,", "[[webroot_map]]\npanel.example.test = %s"} {
			raw := "[renewalparams]\nauthenticator = webroot\n" + fmt.Sprintf(field, root) + "\n"
			if err := validateCertbotConfig([]byte(raw), true); err == nil {
				t.Fatalf("unsupported webroot accepted: %q in %q", root, field)
			}
		}
	}
	raw := fmt.Sprintf("[renewalparams]\nauthenticator = webroot\nwebroot_path = %s,\ndeploy_hook = systemctl reload nginx && node /root/job.js\n", config.ProductionPanelChallengeRoot)
	if err := validateCertbotConfig([]byte(raw), true); err == nil {
		t.Fatal("panel webroot bypassed custom hook rejection")
	}
}

func TestCertbotPanelRenewalReportedByProductionHost(t *testing.T) {
	// Match the reported Certbot file, with the domain/account anonymized.
	// Unlike fresh/no-lineage fixtures, this exercises both persisted webroot
	// forms and Certbot's legacy renew_hook spelling.
	raw := []byte(`archive_dir = /etc/letsencrypt/archive/panel.example.test
cert = /etc/letsencrypt/live/panel.example.test/cert.pem
privkey = /etc/letsencrypt/live/panel.example.test/privkey.pem
chain = /etc/letsencrypt/live/panel.example.test/chain.pem
fullchain = /etc/letsencrypt/live/panel.example.test/fullchain.pem

# Options used in the renewal process
[renewalparams]
account = abc123
renew_hook = systemctl reload nginx
authenticator = webroot
webroot_path = /var/lib/fluxo-acme,
server = https://acme-v02.api.letsencrypt.org/directory
key_type = ecdsa
[[webroot_map]]
panel.example.test = /var/lib/fluxo-acme
`)
	if err := validateCertbotConfig(raw, true); err != nil {
		t.Fatal(err)
	}
}

func TestCertbotGeneratedWebrootHookAndCustomHookRejection(t *testing.T) {
	base := "version = 2.9.0\narchive_dir = /etc/letsencrypt/archive/example.test\n[renewalparams]\nauthenticator = webroot\naccount = abc123\nserver = https://acme-v02.api.letsencrypt.org/directory\ndeploy_hook = systemctl reload nginx\n[[webroot_map]]\nexample.test = /home/fluxo/example.test/current/public\n"
	if err := validateCertbotConfig([]byte(base), true); err != nil {
		t.Fatal(err)
	}
	for _, extra := range []string{"pre_hook = node /root/job.js", "post_hook = /root/notify.sh", "deploy_hook = systemctl reload nginx && node job.js", "installer = nginx", "authenticator = dns-custom", "renew_hook = /root/reload.sh"} {
		raw := "[renewalparams]\nauthenticator = webroot\n" + extra + "\n"
		if err := validateCertbotConfig([]byte(raw), true); err == nil {
			t.Fatal("custom execution accepted", extra)
		}
	}
	if err := validateCertbotEntryPoints([]byte("[certbot.plugins]\nnull = certbot._internal.plugins.null:Installer\nwebroot = certbot._internal.plugins.webroot:Authenticator\n")); err != nil {
		t.Fatal(err)
	}
	if err := validateCertbotEntryPoints([]byte("[certbot.plugins]\nunknown = local_plugin:Authenticator\n")); err == nil {
		t.Fatal("custom plugin accepted")
	}
}

func TestComposerRejectsExecutableAndInsecureConfig(t *testing.T) {
	if err := validateComposerConfig(map[string]any{"config": map[string]any{"secure-http": true, "allow-plugins": false}}); err != nil {
		t.Fatal(err)
	}
	for _, m := range []map[string]any{
		{"scripts": map[string]any{}}, {"require": map[string]any{}}, {"autoload": map[string]any{}},
		{"config": map[string]any{"allow-plugins": map[string]any{"custom/plugin": true}}},
		{"config": map[string]any{"disable-tls": true}}, {"config": map[string]any{"secure-http": false}},
	} {
		if err := validateComposerConfig(m); err == nil {
			t.Fatal("unsupported Composer config accepted", m)
		}
	}
	for _, ini := range []string{"auto_prepend_file=/root/boot.php", "opcache.preload=/root/boot.php", "extension=/root/module.so", "extension_dir=/root/modules"} {
		if err := validatePHPINI([]byte(ini)); err == nil {
			t.Fatal("executable PHP startup override accepted", ini)
		}
	}
}

func TestEmptyAPTPlanIsTheOnlyInitialPackageEffectProfile(t *testing.T) {
	if err := validateNoPackageChanges("Reading package lists...\n0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\n"); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"Remv nodejs [22.22.3]\n0 upgraded, 0 newly installed, 0 to remove", "Inst other [1.0]\n0 upgraded, 0 newly installed, 0 to remove", "unrecognized output"} {
		if err := validateNoPackageChanges(s); err == nil {
			t.Fatal("package effects accepted", s)
		}
	}
	if err := validateAPTConfig("DPkg::Pre-Invoke:: \"/root/hook\";\n"); err == nil {
		t.Fatal("earlier dpkg hook accepted")
	}
}

func TestNginxTokenizerDoesNotConfuseCommentsOrDataWithExecutableDirectives(t *testing.T) {
	s, err := nginxStatements("# load_module ignored;\nhttp { types { application/x-perl pl; } server { set $label 'load_module example'; include sites-enabled/*; } }\n")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range s {
		if v[0] == "include" {
			found = len(v) == 2 && v[1] == "sites-enabled/*"
		}
	}
	if !found {
		t.Fatal(s)
	}
	if _, err = nginxStatements("include 'unclosed"); err == nil {
		t.Fatal("malformed syntax accepted")
	}
}
