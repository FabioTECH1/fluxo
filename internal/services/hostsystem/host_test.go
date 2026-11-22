package hostsystem

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

const oldBoot = "00000000-0000-4000-8000-000000000001"
const newBoot = "00000000-0000-4000-8000-000000000002"

func fixture(t *testing.T) *Host {
	t.Helper()
	h := &Host{Root: t.TempDir()}
	if e := os.Chmod(h.Root, 0700); e != nil {
		t.Fatal(e)
	}
	for _, dir := range []string{"/etc/apt/apt.conf.d", "/usr/bin", "/usr/lib", "/proc/sys/kernel/random", "/run/lock", "/var/lib/fluxo"} {
		if e := os.MkdirAll(h.path(dir), 0700); e != nil {
			t.Fatal(e)
		}
	}
	put(t, h, "/etc/os-release", "ID=ubuntu\n")
	put(t, h, "/usr/bin/unattended-upgrade", "tool")
	os.Chmod(h.path("/usr/bin/unattended-upgrade"), 0755)
	put(t, h, "/proc/sys/kernel/random/boot_id", oldBoot)
	h.Run = func(_ context.Context, name string, args ...string) (string, error) {
		if name == "/usr/bin/dpkg-query" {
			return "installed", nil
		}
		if name == "/usr/bin/apt-config" {
			value := "1"
			if b, e := os.ReadFile(h.path(policyPath)); e == nil && strings.Contains(string(b), `"0"`) {
				value = "0"
			}
			return `Dir::Etc::parts "apt.conf.d";
Dir::Etc::main "apt.conf";
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "` + value + `";
Unattended-Upgrade::Allowed-Origins:: "${distro_id}:${distro_codename}";
Unattended-Upgrade::Allowed-Origins:: "${distro_id}:${distro_codename}-security";
Unattended-Upgrade::Automatic-Reboot "false";
Unattended-Upgrade::Package-Blacklist:: "keep-this";
`, nil
		}
		if name == "/usr/bin/systemctl" {
			if strings.HasSuffix(args[1], ".timer") {
				return "LoadState=loaded\nActiveState=active\nUnitFileState=enabled\nFragmentPath=/usr/lib/systemd/system/" + args[1] + "\nTriggers=" + strings.TrimSuffix(args[1], ".timer") + ".service\n", nil
			}
			if args[1] == "apt-daily-upgrade.service" {
				return "ExecMainStartTimestamp=Mon 2026-11-22 02:00:00 UTC\nExecMainExitTimestamp=Mon 2026-11-22 02:00:10 UTC\nExecMainStatus=0\nResult=success\nActiveState=inactive\n", nil
			}
			if args[1] == "nginx.service" {
				return "LoadState=loaded\nActiveState=active\nSubState=running\nUnitFileState=enabled\n", nil
			}
			return "LoadState=not-found\n", nil
		}
		return "", errors.New("unexpected command: " + name)
	}
	return h
}
func put(t *testing.T, h *Host, p, v string) {
	t.Helper()
	if e := os.WriteFile(h.path(p), []byte(v), 0600); e != nil {
		t.Fatal(e)
	}
}
func TestHostSystemPolicyPreservesAndVerifies(t *testing.T) {
	h := fixture(t)
	ctx := context.Background()
	u := h.Updates(ctx)
	if !u.Editable || u.State != "enabled" || u.LastResult != "successful" {
		t.Fatalf("%+v", u)
	}
	put(t, h, "/etc/apt/apt.conf.d/10-user", "keep exclusions and schedule")
	u, e := h.SetUpdates(ctx, false)
	if e != nil || u.State != "disabled" {
		t.Fatalf("%+v %v", u, e)
	}
	b, _ := os.ReadFile(h.path("/etc/apt/apt.conf.d/10-user"))
	if string(b) != "keep exclusions and schedule" {
		t.Fatal("changed user config")
	}
	if _, e = h.SetUpdates(ctx, true); e != nil {
		t.Fatal(e)
	}
}
func TestHostSystemUnknownExternalAndMissing(t *testing.T) {
	cases := []struct{ name, old, replacement, state string }{
		{"auto reboot", `"false"`, `"true"`, "external"}, {"third party", `${distro_id}:${distro_codename}-security`, `Vendor:noble-security`, "external"}, {"timer inactive", "ActiveState=active", "ActiveState=inactive", "external"}, {"unknown interval", `Unattended-Upgrade "1"`, `Unattended-Upgrade "7"`, "external"}, {"package lists off", `Update-Package-Lists "1"`, `Update-Package-Lists "0"`, "external"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := fixture(t)
			run := h.Run
			h.Run = func(ctx context.Context, n string, a ...string) (string, error) {
				v, e := run(ctx, n, a...)
				return strings.ReplaceAll(v, c.old, c.replacement), e
			}
			u := h.Updates(context.Background())
			if u.State != c.state || u.Editable {
				t.Fatalf("%+v", u)
			}
			if _, e := h.SetUpdates(context.Background(), true); e == nil {
				t.Fatal("mutated external policy")
			}
			if _, e := os.Stat(h.path(policyPath)); !os.IsNotExist(e) {
				t.Fatal("wrote policy")
			}
		})
	}
	h := fixture(t)
	os.Remove(h.path("/usr/bin/unattended-upgrade"))
	if u := h.Updates(context.Background()); u.State != "not_installed" {
		t.Fatal(u)
	}
	put(t, h, "/etc/os-release", "ID=debian\n")
	if u := h.Updates(context.Background()); u.State != "unsupported" {
		t.Fatal(u)
	}
}
func TestHostSystemVerificationRollbackAndForeignFile(t *testing.T) {
	h := fixture(t)
	run := h.Run
	h.Run = func(ctx context.Context, n string, a ...string) (string, error) {
		v, e := run(ctx, n, a...)
		if _, err := os.Stat(h.path(policyPath)); err == nil && n == "/usr/bin/apt-config" {
			v += `Unrelated::Policy "changed";` + "\n"
		}
		return v, e
	}
	if _, e := h.SetUpdates(context.Background(), false); e == nil {
		t.Fatal("accepted verification mismatch")
	}
	if _, e := os.Stat(h.path(policyPath)); !os.IsNotExist(e) {
		t.Fatal("did not rollback")
	}
	put(t, h, policyPath, "administrator policy")
	if _, e := h.SetUpdates(context.Background(), false); e == nil {
		t.Fatal("overwrote foreign policy")
	}
	b, _ := os.ReadFile(h.path(policyPath))
	if string(b) != "administrator policy" {
		t.Fatal("lost foreign config")
	}
}
func TestHostSystemRestartFlagNotAvailableUpdate(t *testing.T) {
	h := fixture(t)
	if r := h.RestartRequired(); r.State != "not_required" {
		t.Fatal(r)
	}
	put(t, h, "/run/reboot-required", "*** System restart required ***")
	put(t, h, "/run/reboot-required.pkgs", "libc6\nlinux-image-generic\n")
	r := h.RestartRequired()
	if r.State != "required" || len(r.Packages) != 2 {
		t.Fatal(r)
	}
	os.Chmod(h.path("/run/reboot-required"), 0666)
	if r = h.RestartRequired(); r.State != "unknown" {
		t.Fatal(r)
	}
}
func TestHostSystemProtectedInputs(t *testing.T) {
	h := fixture(t)
	os.Remove(h.path("/usr/bin/unattended-upgrade"))
	os.Symlink("/etc/passwd", h.path("/usr/bin/unattended-upgrade"))
	if u := h.Updates(context.Background()); u.Editable {
		t.Fatal("followed link")
	}
	os.Remove(h.path("/etc/os-release"))
	put(t, h, "/usr/lib/os-release", "ID=ubuntu\n")
	os.Symlink("../usr/lib/os-release", h.path("/etc/os-release"))
	if b, e := h.read("/etc/os-release"); e == nil || b != nil {
		t.Fatal("general reader followed link")
	}
}
func TestHostSystemGateExcludesConcurrentTabs(t *testing.T) {
	h := fixture(t)
	release, e := h.Gate(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if _, e = h.Gate(ctx); e == nil {
		t.Fatal("duplicate gate")
	}
	release()
	release, e = h.Gate(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	release()
}
func TestHostSystemRebootSchedulingAndDuplicate(t *testing.T) {
	h := fixture(t)
	run := h.Run
	count := 0
	h.Run = func(ctx context.Context, n string, a ...string) (string, error) {
		if n == "/usr/bin/systemd-run" {
			count++
			if !strings.Contains(strings.Join(a, " "), "server-reboot-execute") {
				t.Fatal(a)
			}
			return "", nil
		}
		return run(ctx, n, a...)
	}
	v, e := h.Schedule(context.Background(), oldBoot)
	if e != nil || v.Phase != "preparing" {
		t.Fatalf("%+v %v", v, e)
	}
	if len(v.Expected) != 1 || v.Expected[0] != "nginx.service" {
		t.Fatal(v)
	}
	if _, e = h.Schedule(context.Background(), oldBoot); e == nil || count != 1 {
		t.Fatal("duplicate scheduled")
	}
	if _, e = h.Services(context.Background(), v.ID); e == nil {
		t.Fatal("same boot counted complete")
	}
	put(t, h, "/proc/sys/kernel/random/boot_id", newBoot)
	s, e := h.Services(context.Background(), v.ID)
	if e != nil || len(s) != 1 || s[0].State != "running" {
		t.Fatalf("%+v %v", s, e)
	}
}
func TestHostSystemSchedulingFailureAndAdmissionRefusal(t *testing.T) {
	h := fixture(t)
	run := h.Run
	h.Run = func(ctx context.Context, n string, a ...string) (string, error) {
		if n == "/usr/bin/systemd-run" {
			return "", errors.New("manager unavailable")
		}
		return run(ctx, n, a...)
	}
	v, e := h.Schedule(context.Background(), oldBoot)
	if e == nil || v.Phase != "failed" {
		t.Fatal(v, e)
	}
	v.Phase = "preparing"
	h.saveReboot(v)
	e = h.Execute(context.Background(), v.ID, func(context.Context) (func(), error) { return nil, errors.New("APT busy") })
	if e == nil {
		t.Fatal("ignored incompatible operation")
	}
	stored, _ := h.RebootState()
	if stored.Phase != "failed" || stored.Detail != "APT busy" {
		t.Fatal(stored)
	}
}

func TestHostSystemHelperHoldsAdmissionUntilShutdownAttempt(t *testing.T) {
	h := fixture(t)
	run := h.Run
	h.Run = func(ctx context.Context, n string, a ...string) (string, error) {
		if n == "/usr/bin/systemd-run" {
			return "", nil
		}
		return run(ctx, n, a...)
	}
	v, e := h.Schedule(context.Background(), oldBoot)
	if e != nil {
		t.Fatal(e)
	}
	held := false
	released := false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.shutdownWait = func() error { return errors.New("simulated shutdown boundary; no real reboot") }
	h.wait = func(context.Context, time.Duration) error {
		state, e := h.RebootState()
		if e != nil || state.Phase != "accepted" || !held {
			t.Fatalf("READY missing or locks released: %+v %v", state, e)
		}
		return nil
	}
	h.Run = func(c context.Context, n string, a ...string) (string, error) {
		if n == "/usr/bin/systemctl" && len(a) == 1 && a[0] == "reboot" {
			if !held {
				t.Fatal("reboot after admission released")
			}
			cancel()
			return "", nil
		}
		t.Fatal(n, a)
		return "", nil
	}
	e = h.Execute(ctx, v.ID, func(context.Context) (func(), error) {
		held = true
		return func() { held = false; released = true }, nil
	})
	if e == nil || !released || held {
		t.Fatal("reported shutdown success without a changed boot or leaked admission", e)
	}
	state, _ := h.RebootState()
	if state.Phase != "accepted" {
		t.Fatal(state)
	}
}
func TestHostSystemAPTListsAndEscapedConfigArePreserved(t *testing.T) {
	raw := `Package-Blacklist:: "one";
Package-Blacklist:: "two";
DPkg::Post-Invoke:: "echo \"quoted\"";
`
	m, e := parseAPT(raw)
	if e != nil || m["Package-Blacklist::"] != "one\x00two\x00" {
		t.Fatal(m, e)
	}
	if _, e = parseAPT("invalid config"); e == nil {
		t.Fatal("unknown syntax accepted")
	}
}

func TestHostSystemStockAPTRegexValuesPermitVerifiedPolicyToggle(t *testing.T) {
	h := fixture(t)
	run := h.Run
	const extra = `Dir::Ignore-Files-Silently:: "\.disabled$";
Dir::Ignore-Files-Silently:: "\.dpkg-[a-z]+$";
DPkg::Post-Invoke:: "echo \"quoted\"";
`
	h.Run = func(ctx context.Context, name string, args ...string) (string, error) {
		out, err := run(ctx, name, args...)
		if name == "/usr/bin/apt-config" {
			out += extra
		}
		return out, err
	}
	values, err := parseAPT(extra)
	if err != nil || values["Dir::Ignore-Files-Silently::"] != "\\.disabled$\x00\\.dpkg-[a-z]+$\x00" || values["DPkg::Post-Invoke::"] != "echo \\\"quoted\\\"\x00" {
		t.Fatalf("APT serialization was not retained: %#v, %v", values, err)
	}
	if u := h.Updates(context.Background()); !u.Editable || u.State != "enabled" {
		t.Fatalf("stock Ubuntu patterns blocked policy: %+v", u)
	}
	for _, enabled := range []bool{false, true} {
		if u, err := h.SetUpdates(context.Background(), enabled); err != nil || (u.State == "enabled") != enabled {
			t.Fatalf("verified toggle failed: %+v, %v", u, err)
		}
	}
}

func TestHostSystemAPTValuesDoNotAcquireGoEscapeSemantics(t *testing.T) {
	h := fixture(t)
	run := h.Run
	h.Run = func(ctx context.Context, name string, args ...string) (string, error) {
		out, err := run(ctx, name, args...)
		if name == "/usr/bin/apt-config" {
			out = strings.Replace(out, `APT::Periodic::Unattended-Upgrade "1";`, `APT::Periodic::Unattended-Upgrade "\x31";`, 1)
		}
		return out, err
	}
	if u := h.Updates(context.Background()); u.Editable || u.State != "external" {
		t.Fatalf("interpreted an APT value as a Go escape: %+v", u)
	}
	if _, err := h.SetUpdates(context.Background(), true); err == nil {
		t.Fatal("changed an unsupported interval")
	}
	if _, err := parseAPT("List:: \"before\x00after\";\n"); err == nil {
		t.Fatal("accepted an ambiguous list separator")
	}
}

func TestHostSystemStoppedDisabledAndAbsentServicesAreNotExpected(t *testing.T) {
	h := fixture(t)
	run := h.Run
	h.Run = func(c context.Context, n string, a ...string) (string, error) {
		if n == "/usr/bin/systemctl" {
			if a[1] == "mariadb.service" {
				return "LoadState=loaded\nActiveState=inactive\nUnitFileState=enabled\n", nil
			}
			if a[1] == "redis-server.service" {
				return "LoadState=loaded\nActiveState=active\nUnitFileState=disabled\n", nil
			}
		}
		return run(c, n, a...)
	}
	list, e := h.ExpectedServices(context.Background())
	if e != nil || len(list) != 1 || list[0] != "nginx.service" {
		t.Fatal(list, e)
	}
}
func TestHostSystemUnknownLogsAndCustomTimers(t *testing.T) {
	h := fixture(t)
	run := h.Run
	h.Run = func(c context.Context, n string, a ...string) (string, error) {
		v, e := run(c, n, a...)
		if n == "/usr/bin/systemctl" && a[1] == "apt-daily-upgrade.service" {
			return "LoadState=loaded\nExecMainStartTimestamp=n/a\nResult=success\nExecMainStatus=0\n", nil
		}
		return v, e
	}
	u := h.Updates(context.Background())
	if !u.Editable || u.LastResult != "unknown" || u.LastAttempt != "" {
		t.Fatal(u)
	}
	h.Run = func(c context.Context, n string, a ...string) (string, error) {
		v, e := run(c, n, a...)
		if n == "/usr/bin/systemctl" && a[1] == "apt-daily.timer" {
			v += "DropInPaths=/etc/systemd/system/apt-daily.timer.d/custom.conf\n"
		}
		return v, e
	}
	if u = h.Updates(context.Background()); u.Editable || u.State != "external" {
		t.Fatal(u)
	}
}

func TestHostSystemLargeExecutableAndWritableLockDirectory(t *testing.T) {
	h := fixture(t)
	put(t, h, "/usr/bin/unattended-upgrade", strings.Repeat("x", 128*1024))
	os.Chmod(h.path("/usr/bin/unattended-upgrade"), 0755)
	os.Chmod(h.path("/run/lock"), 0777)
	if u := h.Updates(context.Background()); !u.Editable {
		t.Fatal(u)
	}
	release, e := h.Gate(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	release()
}

func TestHostSystemUncertainShutdownKeepsAdmissionUntilOperatorBoundary(t *testing.T) {
	h := fixture(t)
	run := h.Run
	h.Run = func(c context.Context, n string, a ...string) (string, error) {
		if n == "/usr/bin/systemd-run" {
			return "", nil
		}
		return run(c, n, a...)
	}
	v, e := h.Schedule(context.Background(), oldBoot)
	if e != nil {
		t.Fatal(e)
	}
	held := false
	h.wait = func(context.Context, time.Duration) error { return nil }
	h.Run = func(context.Context, string, ...string) (string, error) { return "", context.DeadlineExceeded }
	h.shutdownWait = func() error {
		state, _ := h.RebootState()
		if !held || state.Phase != "unconfirmed" {
			t.Fatalf("released ambiguous shutdown: %+v held %v", state, held)
		}
		return errors.New("simulated explicit operator boundary")
	}
	if e = h.Execute(context.Background(), v.ID, func(context.Context) (func(), error) { held = true; return func() { held = false }, nil }); e == nil {
		t.Fatal("claimed completed reboot")
	}
	if held {
		t.Fatal("test boundary leaked gate")
	}
}

func TestHostSystemExistingPolicyRollbackPreservesItsBytes(t *testing.T) {
	h := fixture(t)
	original := policyHeader + `APT::Periodic::Unattended-Upgrade "1";` + "\n"
	put(t, h, policyPath, original)
	run := h.Run
	h.Run = func(c context.Context, n string, a ...string) (string, error) {
		v, e := run(c, n, a...)
		b, _ := os.ReadFile(h.path(policyPath))
		if n == "/usr/bin/apt-config" && strings.Contains(string(b), `"0"`) {
			v += `External::Changed "yes";` + "\n"
		}
		return v, e
	}
	if _, e := h.SetUpdates(context.Background(), false); e == nil {
		t.Fatal("verified changed external config")
	}
	got, _ := os.ReadFile(h.path(policyPath))
	if string(got) != original {
		t.Fatal("lost original owned policy")
	}
}

func TestHostSystemMissingRuntimeDirectoryIsUnknown(t *testing.T) {
	h := fixture(t)
	os.Remove(h.path("/run/lock"))
	os.Remove(h.path("/run"))
	if r := h.RestartRequired(); r.State != "unknown" {
		t.Fatal(r)
	}
}
