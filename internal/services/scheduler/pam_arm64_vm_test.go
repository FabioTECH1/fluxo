package scheduler

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func arm64LoaderVMHost() bool {
	host, _ := os.Hostname()
	return os.Geteuid() == 0 && runtime.GOARCH == "arm64" && os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") == "1" && (host == "fluxo-arm64-2204-acceptance" || host == "fluxo-arm64-2404-acceptance" || host == "fluxo-arm64-2604-acceptance" || host == "fluxo-arm64-2404-cronie-acceptance")
}

func TestVMARM64LoaderClosure(t *testing.T) {
	if !arm64LoaderVMHost() {
		t.Skip("dedicated native ARM fixture only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	id := nobleLoaderPAMProfileID
	observation := backendObserver(ctx)
	if observation.Engine == "cronie" {
		id = cronieLoaderPAMProfileID
	} else {
		for _, candidate := range []string{jammyCronCandidateID, resoluteCronCandidateID} {
			c, _ := distributionCandidate(candidate)
			if observation.Version == c.packageVersion {
				id = candidate
			}
		}
	}
	c, err := distributionCandidate(id)
	if err != nil {
		t.Fatal(err)
	}
	// Report all static mismatches in one observation without publishing their
	// contents. Admission remains the normal strict production path below.
	for path, want := range c.files {
		raw, readErr := os.ReadFile(path)
		if readErr != nil || hashBytes(raw) != want {
			t.Log("configuration mismatch", path, "sha256", hashBytes(raw), "read", readErr)
		}
	}
	provider, err := inspectLoaderProvider(ctx, nil)
	if err != nil {
		t.Fatal("native provider closure", err)
	}
	if !loaderDigestValid(provider.PolicyHash) || !loaderDigestValid(provider.RuntimeHash) {
		t.Fatal("provider lacks policy/runtime binding")
	}
	if err = VerifyCronBackendCurrent(ctx, provider.Identity); err != nil {
		t.Fatal("provider changed during inspection", err)
	}
	t.Log("native loader/provider closure qualified", provider.Profile, provider.PolicyHash)
}

func TestVMARM64LoaderRefusals(t *testing.T) {
	if !arm64LoaderVMHost() {
		t.Skip("dedicated native ARM fixture only")
	}
	cases := map[string]string{
		"pam": "/etc/pam.d/common-account", "nss": "/etc/nsswitch.conf",
		"loader": "/etc/ld.so.conf", "module": "/usr/lib/aarch64-linux-gnu/security/pam_unix.so",
	}
	selected := os.Getenv("FLUXO_ARM64_TAMPER_CASE")
	if selected == "" {
		executable, err := os.Executable()
		if err != nil || trustedFile(executable) != nil {
			t.Fatal("protected native test image required", err)
		}
		for name := range cases {
			t.Run(name, func(t *testing.T) {
				cmd := exec.Command("/usr/bin/unshare", "--mount", "--", executable, "-test.v", "-test.run=^TestVMARM64LoaderRefusals$")
				cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "LANG=C.UTF-8", "FLUXO_PHP_UPDATE_VM_TEST=1", "FLUXO_ARM64_TAMPER_CASE=" + name}
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("native private refusal: %s: %v", output, err)
				}
			})
		}
		return
	}
	target, known := cases[selected]
	if !known {
		t.Fatal("unknown private mutation")
	}
	self, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil {
		t.Fatal(err)
	}
	manager, err := os.Readlink("/proc/1/ns/mnt")
	if err != nil || self == manager {
		t.Fatal("separate mount namespace required")
	}
	if output, err := exec.Command("/usr/bin/mount", "--make-rprivate", "/").CombinedOutput(); err != nil {
		t.Fatalf("private propagation: %s %v", output, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := inspectLoaderProvider(ctx, nil); err != nil {
		t.Fatal("unmodified inputs did not qualify", err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	switch selected {
	case "pam":
		raw = append(raw, []byte("\naccount optional pam_exec.so /bin/true\n")...)
	case "nss":
		raw = []byte(strings.Replace(string(raw), "files systemd", "files ldap systemd", 1))
	case "loader":
		raw = append(raw, []byte("\ninclude /unreviewed-arm-loader/*\n")...)
	case "module":
		raw = append(raw, 0)
	}
	source := filepath.Join(t.TempDir(), "mutated-input")
	if err = os.WriteFile(source, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("/usr/bin/mount", "--bind", source, target).CombinedOutput(); err != nil {
		t.Fatalf("private input mount: %s %v", output, err)
	}
	if _, err = inspectLoaderProvider(ctx, nil); err == nil {
		t.Fatal("modified native loader input qualified", selected)
	}
	t.Log("altered native input refused without changing provider-visible files", selected, err)
}
