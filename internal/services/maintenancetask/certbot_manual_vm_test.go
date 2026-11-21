package maintenancetask

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/services/packages"
	"fluxo/internal/syscmd"
)

// Read-only profile qualification plus private offline vendor startup. No
// certificate issuance, renewal, DNS request, cron registration or job launch.
func TestVMManualCertbotProfileBoundary(t *testing.T) {
	host, _ := os.Hostname()
	if os.Geteuid() != 0 || host != "fluxo-php-patch-test" || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" || os.Getenv("FLUXO_CERTBOT_MANUAL_VM_TEST") != "1" {
		t.Skip("explicit root reusable VM manual-profile acceptance only")
	}
	const out = "/root/fluxo-manual-compat-20261121"
	for _, name := range []string{"current", "previous"} {
		path := os.Getenv("FLUXO_CERTBOT_" + strings.ToUpper(name) + "_HELPER")
		if filepath.Dir(path) != filepath.Join(out, "helpers") {
			t.Fatal("protected, digest-named supplied helper required")
		}
		raw, err := readRegular(path, 128<<20)
		if err != nil || filepath.Base(path) != digest(raw) {
			t.Fatal("helper identity mismatch", name, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	lifecycle, err := packages.Lifecycle(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer lifecycle()
	unlock, err := packages.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	prior, err := qualifyProfile(ctx, CertificateRenewal)
	if err != nil {
		t.Fatal("original host profile", err)
	}
	if prior.External[certbotManualProfileKey] != "" {
		t.Fatal("dedicated fixture requires no preexisting manual lineage")
	}
	const path = "/etc/letsencrypt/renewal/fluxo-manual-compat-acceptance.conf"
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("fixture already exists", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(manualLineageFixture), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(path); err != nil {
			t.Error(err)
		}
	})
	p, err := qualifyProfile(ctx, CertificateRenewal)
	if err != nil || p.External[certbotManualProfileKey] != certbotManualNoHooksContract {
		t.Fatal("current manual profile", err)
	}
	if compatibleProfiles(prior, p, false, false) || compatibleProfiles(p, prior, false, false) {
		t.Fatal("new semantics reused an old invocation observation")
	}
	if err := requireProfileHelper(ctx, os.Getenv("FLUXO_CERTBOT_CURRENT_HELPER"), p); err != nil {
		t.Fatal(err)
	}
	if err := requireProfileHelper(ctx, os.Getenv("FLUXO_CERTBOT_PREVIOUS_HELPER"), p); err == nil {
		t.Fatal("actual public v2-hook helper authorized manual profile")
	}
	if notice := certbotRenewalNotice(ctx, filepath.Dir(path)); notice != manualCertbotNotice {
		t.Fatal("manual renewal warning missing", notice)
	}
	negatives := 0
	for _, name := range []string{"temp_checkpoint", "IN_PROGRESS"} {
		checkpoint := filepath.Join("/var/lib/letsencrypt", name)
		if _, err := os.Lstat(checkpoint); !os.IsNotExist(err) {
			t.Fatal("original unfinished checkpoint retained", err)
		}
		if err := os.Mkdir(checkpoint, 0700); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(out, "must-not-exist")
		if err := os.WriteFile(filepath.Join(checkpoint, "COMMANDS"), []byte("/usr/bin/touch "+marker+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		_, e := qualifyProfile(ctx, CertificateRenewal)
		if e == nil || !strings.Contains(e.Error(), "unfinished rollback state") {
			t.Fatal("checkpoint accepted", name, e)
		}
		if _, err := os.Lstat(marker); !os.IsNotExist(err) {
			t.Fatal("checkpoint was executed")
		}
		if err := os.Remove(filepath.Join(checkpoint, "COMMANDS")); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(checkpoint); err != nil {
			t.Fatal(err)
		}
		negatives++
	}
	for _, key := range []string{"manual_auth_hook", "manual_cleanup_hook"} {
		if err := os.WriteFile(path, []byte(manualLineageFixture+key+" = /root/unreviewed-script\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := qualifyProfile(ctx, CertificateRenewal); err == nil {
			t.Fatal("executable hook accepted", key)
		}
		negatives++
	}
	if err := os.WriteFile(path, []byte(manualLineageFixture), 0600); err != nil {
		t.Fatal(err)
	}
	// Exercise the actual installed vendor implementation in private dirs and
	// a kernel network namespace, not a mock Certbot and not the real ACME flow.
	program := `import json,sys
from pathlib import Path
from types import SimpleNamespace
from certbot._internal.plugins.manual import Authenticator
from certbot import errors
root=Path(sys.argv[1]);root.mkdir(mode=0o700)
config=SimpleNamespace(backup_dir=str(root/'backups'),temp_checkpoint_dir=str(root/'temp_checkpoint'),in_progress_dir=str(root/'IN_PROGRESS'),strict_permissions=True,noninteractive_mode=True,manual_auth_hook=None,manual_cleanup_hook=None,validate_hooks=True)
auth=Authenticator(config,'manual')
try:
    auth.prepare()
except errors.PluginError as error:
    assert 'manual-auth-hook' in str(error)
else:
    raise AssertionError('vendor accepted unattended manual renewal without a hook')
assert sorted(p.name for p in root.iterdir())==['backups']
print(json.dumps({'vendor_manual_prepare_refused_without_auth_hook':True,'isolated_network_namespace':True,'ACME_or_renewal_executed':False}))
`
	text, err := syscmd.Run(ctx, 20*time.Second, "/usr/bin/unshare", "--net", "/usr/bin/python3", "-I", "-c", program, filepath.Join(out, "vendor-offline"))
	if err != nil {
		t.Fatal("offline vendor probe", err, text)
	}
	var vendor map[string]any
	if err := json.Unmarshal([]byte(text), &vendor); err != nil {
		t.Fatal(err, text)
	}
	if vendor["vendor_manual_prepare_refused_without_auth_hook"] != true {
		t.Fatal(vendor)
	}
	if current, err := os.ReadFile(path); err != nil || string(current) != manualLineageFixture {
		t.Fatal("lineage changed", err)
	}
	report := map[string]any{"passed": true, "manual_profile_qualified": true, "original_profile_did_not_authorize_new_semantics": true, "actual_old_helper_refused": true, "actual_current_helper_capable": true, "renewal_warning_present": true, "checkpoint_and_hook_negatives": negatives, "certificate_or_DNS_configuration_changed": false, "vendor": vendor}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "native-profile.json"), append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	t.Log(string(raw))
}
