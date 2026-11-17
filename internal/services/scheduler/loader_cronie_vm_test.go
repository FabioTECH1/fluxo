package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Each mutation is a bind mount in a new private mount namespace. Cronie and
// the installed panel continue to see the original host files throughout.
func TestVMInstalledCronieLoaderRefusals(t *testing.T) {
	hostname, _ := os.Hostname()
	if os.Geteuid() != 0 || hostname != "fluxo-bridge-noble-cronie-20261117" || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("explicit isolated installed Cronie fixture, root and opt-in only")
	}
	cases := map[string]string{
		"pam-stack": "/etc/pam.d/cronie", "account-policy": "/etc/security/access.conf",
		"provider-defaults": "/etc/default/cronie", "nss": "/etc/nsswitch.conf",
		"loader": "/etc/ld.so.conf", "module": "/usr/lib/x86_64-linux-gnu/security/pam_access.so",
	}
	selected := os.Getenv("FLUXO_CRONIE_TAMPER_CASE")
	if selected == "" {
		executable, err := os.Executable()
		if err != nil || trustedFile(executable) != nil {
			t.Fatal("native test executable requires its protected root-owned staging path", err)
		}
		for name := range cases {
			t.Run(name, func(t *testing.T) {
				cmd := exec.Command("/usr/bin/unshare", "--mount", "--", executable, "-test.v", "-test.run=^TestVMInstalledCronieLoaderRefusals$")
				cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "LANG=C.UTF-8", "FLUXO_PHP_UPDATE_VM_TEST=1", "FLUXO_CRONIE_TAMPER_CASE=" + name}
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("isolated refusal failed: %s: %v", output, err)
				}
			})
		}
		return
	}
	target, known := cases[selected]
	if !known {
		t.Fatal("unknown native mutation")
	}
	selfNamespace, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil {
		t.Fatal(err)
	}
	managerNamespace, err := os.Readlink("/proc/1/ns/mnt")
	if err != nil || selfNamespace == managerNamespace {
		t.Fatal("a negative case requires an independently created private mount namespace", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	observed := backendObserver(ctx)
	if observed.Engine != "cronie" || observed.State != "active" {
		t.Fatal("actual Cronie prerequisite missing")
	}
	if _, err := inspectPAMCandidate(cronieLoaderPAMProfileID, observed.PID); err != nil {
		t.Fatal("unmodified input did not qualify before the negative case", err)
	}
	if raw, err := exec.Command("/usr/bin/mount", "--make-rprivate", "/").CombinedOutput(); err != nil {
		t.Fatalf("private mount propagation: %s: %v", raw, err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	switch selected {
	case "pam-stack":
		raw = append(raw, []byte("\nsession optional pam_exec.so /bin/true\n")...)
	case "account-policy":
		raw = append(raw, []byte("\n+:ALL:ALL\n")...)
	case "provider-defaults":
		raw = append(raw, []byte("\nDAEMON_ARGS=-i\n")...)
	case "nss":
		raw = []byte(strings.Replace(string(raw), "files systemd", "files ldap systemd", 1))
	case "loader":
		raw = append(raw, []byte("\ninclude /unreviewed-cronie-loader/*\n")...)
	case "module":
		raw = append(raw, 0)
	}
	source := filepath.Join(t.TempDir(), "mutated-input")
	if err = os.WriteFile(source, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("/usr/bin/mount", "--bind", source, target).CombinedOutput(); err != nil {
		t.Fatalf("private input mount: %s: %v", output, err)
	}
	if _, err = inspectPAMCandidate(cronieLoaderPAMProfileID, observed.PID); err == nil {
		t.Fatal("altered loader input gained current Cronie qualification")
	}
	if now := backendObserver(ctx); now.PID != observed.PID || now.Engine != "cronie" || now.State != "active" {
		t.Fatal("the actual provider changed during the isolated refusal")
	}
	t.Log("altered input refused without changing the daemon's host files", selected)
}

// This fixture installs the real Ubuntu package and its ordinary service.
// No production adapter, namespace, executable or provider override is used.
func TestVMInstalledCronieStrictLoaderClosure(t *testing.T) {
	hostname, _ := os.Hostname()
	if os.Geteuid() != 0 || hostname != "fluxo-bridge-noble-cronie-20261117" || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("explicit isolated installed Cronie fixture, root and opt-in only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	floor, floorErr := os.ReadFile("/var/lib/fluxo/execution-readers.json")
	observation := backendObserver(ctx)
	if observation.Engine != "cronie" || observation.State != "active" || observation.Version != "1.7.1-1" || observation.Architecture != "amd64" {
		t.Fatal("actual installed Cronie prerequisite differs")
	}
	historical, err := qualifyCronieHost(ctx, observation)
	if err != nil {
		t.Fatal("historical Cronie provider qualification", err)
	}
	report, err := inspectPAMCandidate(cronieLoaderPAMProfileID, historical.PID)
	if err != nil || !report.Qualified {
		t.Fatal("actual Cronie closure refused", err)
	}
	current, err := QualifyCronBackend(ctx)
	if err != nil || current != historical {
		t.Fatal("current closure altered the historical provider identity", err)
	}
	provider, err := qualifyLoaderProvider(ctx, nil)
	if err != nil || provider.Identity != historical || provider.Profile != cronieLoaderPAMProfileID+"/loader-policy-v1" {
		t.Fatal("current Cronie loader provider refused or changed identity", err)
	}
	evidence := map[string]any{"passed": true, "profile": cronieLoaderPAMProfileID, "historical_identity_preserved": true, "closure": report, "provider": provider, "format_activated": false}
	raw, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := "/var/lib/fluxo-cronie-loader-closure-" + uuid.NewString() + ".json"
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(append(raw, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	after, afterErr := os.ReadFile("/var/lib/fluxo/execution-readers.json")
	if (floorErr == nil) != (afterErr == nil) || !bytes.Equal(floor, after) {
		t.Fatal("closure inspection observed a changed reader floor")
	}
	t.Log("actual installed Cronie closure, preserved identity, current provider; evidence", path)
}
