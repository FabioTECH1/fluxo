package updatecheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the actual installer entry path through its version guard. Every
// host-facing dependency is replaced; reaching provisioning is a test sentinel.
func TestInstallerCurrentReleaseGuard(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	prefix, suffix, ok := strings.Cut(string(script), "\npreflight_base_installation\n")
	if !ok || !strings.Contains(suffix, "\nskip_current_fluxo_release\npreflight_host\nselect_optional_components\n") {
		t.Fatal("version guard must run before host preflight and component selection")
	}
	_, readerTail, ok := strings.Cut(string(script), "<<'PY_EXECUTION_FORMATS'\n")
	readerProgram, _, readerOK := strings.Cut(readerTail, "\nPY_EXECUTION_FORMATS")
	if !ok || !readerOK {
		t.Fatal("missing early read-only execution-format preflight")
	}
	cases := []struct {
		name, installed, target, latest, expected string
		args, env                                 []string
		exit                                      int
	}{
		{name: "already current", installed: "0.4.33", expected: "Fluxo is already up to date"},
		{name: "finish current release reader setup", installed: "0.4.33", env: []string{"TEST_INSTALLER_EXECUTION=true"}, expected: "PROVISIONING", exit: 42},
		{name: "current floor is retained", installed: "0.4.33", env: []string{"TEST_INSTALLER_EXECUTION=true", "TEST_READER_FLOOR=true"}, expected: "Fluxo is already up to date"},
		{name: "current release checks Scheduler without provisioning", installed: "0.4.33", env: []string{"TEST_INSTALLER_SCHEDULER=true", "TEST_INSTALLER_EXECUTION=true", "TEST_READER_FLOOR=true"}, expected: "COMPATIBILITY_SETUP"},
		{name: "staged worker with older running panel", installed: "0.4.33", env: []string{"TEST_HEALTHY=false", "TEST_STAGED=true"}, expected: "PROVISIONING", exit: 42},
		{name: "normalizes installed v", installed: "v0.4.33", expected: "Fluxo is already up to date"},
		{name: "pinned same release", installed: "0.4.33", target: "0.4.33", expected: "Fluxo is already up to date"},
		{name: "new release", installed: "0.4.32", expected: "PROVISIONING", exit: 42},
		{name: "numeric comparison", installed: "0.4.9", latest: "v0.4.10", expected: "PROVISIONING", exit: 42},
		{name: "newer installed", installed: "0.4.34", expected: "newer than the latest published release"},
		{name: "explicit downgrade", installed: "0.4.34", target: "v0.4.33", expected: "PROVISIONING", exit: 42},
		{name: "prerelease to stable", installed: "0.4.33-rc.2", expected: "PROVISIONING", exit: 42},
		{name: "prerelease numeric identifiers", installed: "0.4.33-rc.10", latest: "v0.4.33-rc.2", expected: "newer than the latest published release"},
		{name: "stable over prerelease", installed: "0.4.33", latest: "v0.4.33-rc.2", expected: "newer than the latest published release"},
		{name: "build metadata", installed: "0.4.33+build.1", expected: "Fluxo is already up to date"},
		{name: "explicit pin differs in build metadata", installed: "0.4.33+custom.1", target: "v0.4.33", expected: "PROVISIONING", exit: 42},
		{name: "unknown development build", installed: "dev", expected: "PROVISIONING", exit: 42},
		{name: "invalid prerelease", installed: "0.4.33-rc.01", expected: "PROVISIONING", exit: 42},
		{name: "fresh install", env: []string{"TEST_EXISTING=false"}, expected: "PROVISIONING", exit: 42},
		{name: "reinstall", installed: "0.4.33", args: []string{"--reinstall"}, expected: "PROVISIONING", exit: 42},
		{name: "local binary", installed: "0.4.33", args: []string{"--local-binary=/tmp/local-fluxo"}, expected: "PROVISIONING", exit: 42},
		{name: "custom binary", installed: "0.4.33", env: []string{"FLUXO_BINARY_URL=https://example.test/fluxo"}, expected: "PROVISIONING", exit: 42},
		{name: "same version from explicit fork", installed: "0.4.33", target: "v0.4.33", env: []string{"FLUXO_GITHUB_REPO=myorg/fluxo"}, expected: "PROVISIONING", exit: 42},
		{name: "invalid attestation bypass", installed: "0.4.33", args: []string{"--skip-release-attestation"}, expected: "only accepted with FLUXO_BINARY_URL", exit: 1},
		{name: "component flags require reinstall", installed: "0.4.33", args: []string{"--node", "--python", "--redis", "--db-engine=mysql", "--harden-ssh"}, expected: "Use --reinstall"},
		{name: "unresolved latest", installed: "0.4.33", env: []string{"TEST_OFFLINE=true"}, expected: "Could not determine the latest Fluxo release", exit: 1},
		{name: "pinned release offline", installed: "0.4.33", target: "v0.4.33", env: []string{"TEST_OFFLINE=true"}, expected: "Fluxo is already up to date"},
		{name: "stopped service", installed: "0.4.33", env: []string{"TEST_ACTIVE=false"}, expected: "service is not running", exit: 1},
		{name: "unhealthy dashboard", installed: "0.4.33", env: []string{"TEST_HEALTHY=false"}, expected: "dashboard health/version checks failed", exit: 1},
		{name: "unreadable panel domain", installed: "0.4.33", env: []string{"TEST_DOMAIN_READ=false"}, expected: "Could not read the installed panel-domain", exit: 1},
		{name: "unhealthy panel domain", installed: "0.4.33", env: []string{"TEST_DOMAIN=panel.example.test", "TEST_DOMAIN_HEALTHY=false"}, expected: "panel domain is unhealthy", exit: 1},
		{name: "untrusted existing install", installed: "0.4.33", env: []string{"TEST_VALID_INSTALL=false"}, expected: "UNTRUSTED", exit: 1},
		{name: "help documents reinstall", args: []string{"--help"}, expected: "--reinstall"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Recognize the exact embedded reader preflight without executing it
			// against this host. Its format cases belong to the dedicated fixtures.
			// Any other unexpected privileged command still fails.
			stubs := `
sudo() {
    case "$*" in
        -v) return 0 ;;
        '/usr/local/bin/fluxo --version') printf 'fluxo version %s\n' "$TEST_INSTALLED" ;;
        '/usr/local/bin/fluxo --installer-execution-support') [ "${TEST_INSTALLER_EXECUTION:-false}" = true ] && echo 1 ;;
        '/usr/local/bin/fluxo --installer-scheduler-support') [ "${TEST_INSTALLER_SCHEDULER:-false}" = true ] && echo 1 ;;
        'test -e /var/lib/fluxo/execution-readers.json') [ "${TEST_READER_FLOOR:-false}" = true ] ;;
        'test -e /var/lib/fluxo/execution-installer.json'|'test -L /var/lib/fluxo/execution-installer.json'|'test -e /var/lib/fluxo/scheduler-installer.json'|'test -L /var/lib/fluxo/scheduler-installer.json') return 1 ;;
        'systemctl is-active --quiet fluxo') [ "${TEST_ACTIVE:-true}" = true ] ;;
        '/usr/bin/python3 -')
            local program
            program="$(cat)"
            [ "$program" = "$TEST_EXECUTION_READER_PREFLIGHT" ] || { echo 'UNEXPECTED PRIVILEGED PYTHON' >&2; exit 99; }
            return 0 ;;
        python3\ -\ *) [ "${TEST_STAGED:-false}" = true ] ;;
        *) echo "UNEXPECTED PRIVILEGED COMMAND: $*" >&2; exit 99 ;;
    esac
}
curl() {
    if [ "${TEST_OFFLINE:-false}" = true ]; then return 22; fi
    printf 'https://github.com/FabioTECH1/fluxo/releases/tag/%s' "$TEST_LATEST"
}
`
			overrides := `
validate_existing_fluxo_installation() {
    if [ "${TEST_VALID_INSTALL:-true}" != true ]; then echo UNTRUSTED; exit 1; fi
    EXISTING_INSTALL="${TEST_EXISTING:-true}"
}
detect_existing_dashboard_runtime() { :; }
wait_for_fluxo_dashboard() {
    [ "$1" = "$TEST_INSTALLED" ] && [ "${TEST_HEALTHY:-true}" = true ]
}
read_active_panel_domain() {
    [ "${TEST_DOMAIN_READ:-true}" = true ] || return 1
    printf '%s' "${TEST_DOMAIN:-}"
}
wait_for_fluxo_panel_domain() { [ "${TEST_DOMAIN_HEALTHY:-true}" = true ]; }
finish_installer_execution_setup() { echo COMPATIBILITY_SETUP; }
# This suite tests the version guard after base eligibility; the base gate has
# its own strict candidate/entry-order harness in installer_base_test.go.
preflight_base_installation() { BASE_INSTALL_MODE=base; }
base_install_command() { echo UNEXPECTED_BASE_ACTION >&2; exit 99; }
preflight_base_installation
resume_installer_execution_setup
preflight_execution_reader_formats
skip_current_fluxo_release
echo PROVISIONING
exit 42
`
			path := filepath.Join(t.TempDir(), "installer-guard.sh")
			if err := os.WriteFile(path, []byte(stubs+prefix+overrides), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", append([]string{path}, tc.args...)...)
			latest := tc.latest
			if latest == "" {
				latest = "v0.4.33"
			}
			// Keep credentials, caller installer options, and host state out of tests.
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "TEST_INSTALLED=" + tc.installed, "TEST_LATEST=" + latest, "FLUXO_VERSION=" + tc.target, "TEST_EXECUTION_READER_PREFLIGHT=" + readerProgram}
			cmd.Env = append(cmd.Env, tc.env...)
			output, err := cmd.CombinedOutput()
			exit := 0
			if err != nil {
				if e, ok := err.(*exec.ExitError); ok {
					exit = e.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if exit != tc.exit || !strings.Contains(string(output), tc.expected) || strings.Contains(string(output), "UNEXPECTED PRIVILEGED") {
				t.Fatalf("exit=%d want=%d; expected %q; output:\n%s", exit, tc.exit, tc.expected, output)
			}
		})
	}
}

// Keep the exact embedded preference and shell-selection fixtures in the normal
// Go/CI suite; they do not run the installer or touch host services/packages.
func TestInstallerRememberedPreferences(t *testing.T) {
	cmd := exec.Command("python3", "installer_preferences_test.py")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("installer preference fixtures: %v\n%s", err, output)
	}
}
