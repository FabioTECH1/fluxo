package maintenancetask

// Test-only hosted native adapter. It never runs Composer, Certbot renew, a
// deploy hook, image operations, or scheduler/receipt publication. Keep the
// separate named-VM guard in php_imagick_test.go unchanged.
import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func runtimeCompatCIGuard(t *testing.T) {
	t.Helper()
	if os.Getenv("FLUXO_RUNTIME_COMPAT_CANDIDATE") == "" {
		t.Skip("explicit disposable GitHub-hosted runtime fixture only")
	}
	host, _ := os.Hostname()
	branch := os.Getenv("GITHUB_REF")
	arch := map[string]string{"amd64": "X64", "arm64": "ARM64"}[runtime.GOARCH]
	if os.Geteuid() != 0 || os.Getenv("FLUXO_RUNTIME_COMPAT_CANDIDATE") != "1" ||
		os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("RUNNER_ENVIRONMENT") != "github-hosted" ||
		os.Getenv("GITHUB_WORKFLOW") != "Native runtime compatibility candidate" || arch == "" ||
		os.Getenv("RUNNER_ARCH") != arch || host != "fluxo-runtime-compat-"+runtime.GOARCH ||
		(branch != "refs/heads/baseline/clean-base" && branch != "refs/heads/test/runtime-compat-candidate") {
		t.Fatal("root, explicit opt-in, native hosted runner and investigation branch required")
	}
	raw, err := os.ReadFile("/usr/lib/os-release")
	if err != nil || !strings.Contains("\n"+string(raw), "\nID=ubuntu\n") || !strings.Contains("\n"+string(raw), "\nVERSION_ID=\"24.04\"\n") {
		t.Fatal("Ubuntu 24.04 fixture required", err)
	}
	for key, size := range map[string]int{"GITHUB_SHA": 20, "FLUXO_RUNTIME_BINARY_SHA256": 32, "FLUXO_RUNTIME_TEST_SHA256": 32} {
		value := os.Getenv(key)
		decoded, err := hex.DecodeString(value)
		if err != nil || len(decoded) != size || strings.ToLower(value) != value {
			t.Fatal("exact source/artifact identity required", key)
		}
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for path, key := range map[string]string{self: "FLUXO_RUNTIME_TEST_SHA256", "/usr/local/bin/fluxo": "FLUXO_RUNTIME_BINARY_SHA256"} {
		raw, err := readRegular(path, 128<<20)
		if err != nil || digest(raw) != os.Getenv(key) {
			t.Fatal("protected artifact differs", key, err)
		}
	}
}

// Restore bytes, ownership, permissions and access/modification timestamps on
// the same inode, even when a negative assertion fails. ctime is not restorable.
func runtimeCompatRestoreFile(t *testing.T, path string) []byte {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	st := *info.Sys().(*syscall.Stat_t)
	raw, err := readRegular(path, 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.WriteFile(path, raw, info.Mode().Perm()); err != nil {
			t.Error("restore fixture bytes", err)
		}
		if err := os.Chown(path, int(st.Uid), int(st.Gid)); err != nil {
			t.Error("restore fixture owner", err)
		}
		if err := os.Chmod(path, info.Mode().Perm()); err != nil {
			t.Error("restore fixture mode", err)
		}
		if err := syscall.UtimesNano(path, []syscall.Timespec{st.Atim, st.Mtim}); err != nil {
			t.Error("restore fixture timestamps", err)
		}
	})
	return raw
}

func TestCINativeRuntimeCompatibility(t *testing.T) {
	runtimeCompatCIGuard(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	php := newProfile(ComposerUpdate)
	if err := qualifyPHPCLI(ctx, &php); err != nil {
		t.Fatal("actual packaged default PHP qualification", err)
	}
	if php.External["php-interpreter"] != "/usr/bin/php8.4" || php.External["php-imagick-startup"] != imagickStartupProfile || phpPackageVersion(&php, "php8.4-imagick") == "" {
		t.Fatal("default PHP8.4 with reviewed Imagick startup profile required")
	}
	ini := "/etc/php/8.4/mods-available/imagick.ini"
	raw := runtimeCompatRestoreFile(t, ini)
	if validateMaintenancePHPINI(raw) != nil || validatePHPINI(raw) == nil {
		t.Fatal("current-only Imagick INI admission changed")
	}
	oldPHP := newProfile(ComposerUpdate)
	if err := legacyV1QualifyPHPCLI(ctx, &oldPHP); err == nil || !strings.Contains(err.Error(), "Unsupported PHP CLI extension: imagick") {
		t.Fatal("frozen V1 PHP producer must refuse specifically at Imagick", err)
	}
	if err := os.WriteFile(ini, append(append([]byte{}, raw...), []byte("\nauto_prepend_file=/root/unreviewed.php\n")...), 0644); err != nil {
		t.Fatal(err)
	}
	refused := newProfile(ComposerUpdate)
	if qualifyPHPCLI(ctx, &refused) == nil {
		t.Fatal("changed executable PHP INI admitted")
	}
	if err := os.WriteFile(ini, raw, 0644); err != nil {
		t.Fatal(err)
	}
	config := "/etc/ImageMagick-6/policy.xml"
	raw = runtimeCompatRestoreFile(t, config)
	if err := os.WriteFile(config, append(append([]byte{}, raw...), []byte("\n<!-- fixture drift -->\n")...), 0644); err != nil {
		t.Fatal(err)
	}
	refused = newProfile(ComposerUpdate)
	if qualifyPHPCLI(ctx, &refused) == nil {
		t.Fatal("ImageMagick vendor configuration drift admitted")
	}
	if err := os.WriteFile(config, raw, 0644); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"/etc/letsencrypt/renewal-hooks", "/etc/letsencrypt/renewal-hooks/pre", "/etc/letsencrypt/renewal-hooks/post", certbotDeployHookDirectory} {
		if err := os.Mkdir(dir, 0755); err == nil {
			t.Cleanup(func() {
				if err := os.Remove(dir); err != nil {
					t.Error("remove own empty fixture directory", err)
				}
			})
		} else if !os.IsExist(err) {
			t.Fatal(err)
		}
	}
	hookPath := certbotDeployHookDirectory + "/50-runtime-compat-nginx"
	file, err := os.OpenFile(hookPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0750)
	if err != nil {
		t.Fatal("refuse to overwrite a pre-existing hook", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(hookPath); err != nil {
			t.Error("remove own synthetic hook", err)
		}
	})
	var cert profile
	for _, row := range []struct{ selector, pattern, family string }{
		{`"$RENEWED_LINEAGE"`, "/etc/letsencrypt/live/runtime-compat.invalid", "renewed-lineage-exact"},
		{`"$RENEWED_DOMAINS"`, "runtime-compat.invalid", "renewed-domains-exact"},
		{`" $RENEWED_DOMAINS "`, `*" runtime-compat.invalid "*`, "renewed-domains-member"},
	} {
		hook := "#!/bin/sh\nset -eu\ncase " + row.selector + " in\n" + row.pattern + ")\n/usr/sbin/nginx -t && /usr/bin/systemctl reload nginx\n;;\nesac\n"
		if err := os.WriteFile(hookPath, []byte(hook), 0750); err != nil {
			t.Fatal(err)
		}
		p := newProfile(CertificateRenewal)
		if err := qualifyCertbot(ctx, &p); err != nil {
			t.Fatal("actual Certbot/Python/Nginx qualification", row.family, err)
		}
		if p.Files[hookPath] != digest([]byte(hook)) || p.External["certbot-directory-hook:"+hookPath] != row.family+":/bin/sh" || p.External["certbot-directory-hook-contract"] != certbotDirectoryHookContract || p.External["nginx-master"] == "" {
			t.Fatal("full hook/program/Nginx profile evidence missing")
		}
		if cert.Kind != "" && compatibleProfiles(cert, p, false, false) {
			t.Fatal("changed valid hook profile compared equal")
		}
		cert = p
		if err := os.Chmod(hookPath, 0770); err != nil {
			t.Fatal(err)
		}
		bad := newProfile(CertificateRenewal)
		if qualifyCertbotDirectoryHooks(ctx, &bad) == nil {
			t.Fatal("writable hook admitted")
		}
		if err := os.Chmod(hookPath, 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(hookPath, []byte(hook+"/usr/bin/true\n"), 0750); err != nil {
			t.Fatal(err)
		}
		bad = newProfile(CertificateRenewal)
		if qualifyCertbotDirectoryHooks(ctx, &bad) == nil {
			t.Fatal("extra hook command admitted")
		}
		if err := os.WriteFile(hookPath, []byte(hook), 0750); err != nil {
			t.Fatal(err)
		}
	}
	oldCert := newProfile(CertificateRenewal)
	if err := legacyV1QualifyCertbot(ctx, &oldCert); err == nil || !strings.Contains(err.Error(), "Custom Certbot directory hooks need maintenance review") {
		t.Fatal("frozen V1 Certbot producer must refuse specifically at the directory hook", err)
	}
	nginx := "/etc/nginx/nginx.conf"
	raw = runtimeCompatRestoreFile(t, nginx)
	if err := os.WriteFile(nginx, append(append([]byte{}, raw...), []byte("\n# runtime-compat drift\n")...), 0644); err != nil {
		t.Fatal(err)
	}
	changed := newProfile(CertificateRenewal)
	if err := qualifyCertbot(ctx, &changed); err != nil {
		t.Fatal(err)
	}
	if compatibleProfiles(cert, changed, false, false) || compatibleRecoveryObservations(cert, changed, true) {
		t.Fatal("Nginx configuration drift ignored")
	}
	if err := os.WriteFile(nginx, raw, 0644); err != nil {
		t.Fatal(err)
	}
	finalPHP, finalCert := newProfile(ComposerUpdate), newProfile(CertificateRenewal)
	if err := qualifyPHPCLI(ctx, &finalPHP); err != nil {
		t.Fatal("restored PHP fixture", err)
	}
	if err := qualifyCertbot(ctx, &finalCert); err != nil {
		t.Fatal("restored Certbot fixture", err)
	}
	if !compatibleProfiles(php, finalPHP, false, false) || !compatibleProfiles(cert, finalCert, false, false) {
		t.Fatal("restored fixture does not reproduce the original profiles")
	}
	// These are actual registrations published by the exact public predecessor
	// and retained through the normal base installer, with genuine cron receipts.
	j, err := read()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[Kind]bool{}
	for _, r := range j.Entries {
		var p profile
		switch r.Contract.Kind {
		case ComposerUpdate:
			p = php
		case CertificateRenewal:
			p = cert
		default:
			continue
		}
		if !r.Enabled || !r.Published || !r.Received || filepath.Base(r.Helper) != os.Getenv("FLUXO_RUNTIME_PREDECESSOR_SHA256") {
			t.Fatal("actual acknowledged predecessor registration required")
		}
		before, _ := json.Marshal(r)
		err := requireProfileHelper(ctx, r.Helper, p)
		if err == nil || !strings.Contains(err.Error(), "refresh-task-helpers") {
			t.Fatal("old published helper failed to report supported refresh procedure", err)
		}
		if err := requireProfileHelper(ctx, os.Getenv("FLUXO_RUNTIME_CURRENT_HELPER"), p); err != nil {
			t.Fatal("actual capable candidate helper refused", err)
		}
		after, err := read()
		if err != nil {
			t.Fatal(err)
		}
		entry, _ := json.Marshal(after.Entries[r.Contract.ID])
		if string(entry) != string(before) || len(after.Invocations) != len(j.Invocations) {
			t.Fatal("helper compatibility inspection changed identity, nonce or invocation authority")
		}
		seen[r.Contract.Kind] = true
	}
	if !seen[ComposerUpdate] || !seen[CertificateRenewal] {
		t.Fatal("both original public helper registrations must remain available")
	}
	// Output is only read by the private controller. No installed task state is
	// created or rewritten; these profiles never serve as execution authority.
	path := os.Getenv("FLUXO_RUNTIME_PROFILE_OUTPUT")
	if filepath.Dir(path) != filepath.Dir(os.Args[0]) || filepath.Base(path) != "profiles.json" {
		t.Fatal("private output must be beside protected test artifact")
	}
	output, err := json.Marshal(map[string]profile{"php": php, "certbot": cert})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, output, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("native PHP8.4 Imagick and synthetic conditional Certbot hook profiles passed; no renewal/hook execution or customer-hook claim")
}
