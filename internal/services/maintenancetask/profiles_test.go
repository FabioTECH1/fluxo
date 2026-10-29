package maintenancetask

import "testing"

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
