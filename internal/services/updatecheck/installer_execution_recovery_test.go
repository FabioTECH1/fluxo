package updatecheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Execute the actual early recovery routine with a strict host boundary. It
// must authenticate the selected image and resume before ordinary preflight or
// provisioning, which cannot operate on an interrupted prepared reader floor.
func TestInstallerExecutionRecoveryBeforeProvisioning(t *testing.T) {
	raw, err := os.ReadFile("../../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	_, tail, ok := strings.Cut(script, "resume_installer_execution_setup() {\n")
	functions, _, end := strings.Cut(tail, "# Exact installer recovery precedes ordinary prepared-floor refusal and leases.")
	if !ok || !end || !strings.Contains(script, "resume_installer_execution_setup\npreflight_execution_reader_formats\nskip_current_fluxo_release\npreflight_host") {
		t.Fatal("recovery must precede ordinary preflight and provisioning")
	}
	for _, tc := range []struct {
		name string
		env  []string
		want string
		exit int
	}{
		{"exact image", nil, "RECOVERED_WITHOUT_PROVISIONING", 0},
		{"different candidate", []string{"TEST_CANDIDATE=other"}, "exact original release", 1},
		{"different installed image", []string{"TEST_INSTALLED=other"}, "exact original release", 1},
		{"provenance refusal", []string{"TEST_ATTEST=false"}, "ATTESTATION_REFUSED", 1},
		{"malformed intent", []string{"TEST_STATUS=false"}, "INVALID_INTENT", 1},
		{"already completed", []string{"TEST_PENDING=false"}, "ORDINARY_PREFLIGHT", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := `set -euo pipefail
EXISTING_INSTALL=false
PREPARED_BINARY=/selected-artifact
DASHBOARD_USE_HTTP=false
sudo() {
    case "$*" in
        'test -e /var/lib/fluxo/execution-installer.json') return 0 ;;
        'env FLUXO_ENV=prod FLUXO_DATA_DIR=/var/lib/fluxo /usr/local/bin/fluxo --installer-execution-status')
            [ "${TEST_STATUS:-true}" = true ] || { echo INVALID_INTENT >&2; return 1; }
            printf '{"pending":%s,"image_sha256":"expected","version":"0.4.46-bridge.8"}\n' "${TEST_PENDING:-true}" ;;
        'sha256sum /usr/local/bin/fluxo') printf '%s  /usr/local/bin/fluxo\n' "${TEST_INSTALLED:-expected}" ;;
        *) echo "UNEXPECTED_PRIVILEGED_ACTION" >&2; exit 99 ;;
    esac
}
sha256sum() { [ "$1" = /selected-artifact ] || exit 99; printf '%s  /selected-artifact\n' "${TEST_CANDIDATE:-expected}"; }
validate_existing_fluxo_installation() { EXISTING_INSTALL=true; }
prepare_fluxo_binary() { echo SELECTED_ARTIFACT; }
verify_release_attestation() { [ "${TEST_ATTEST:-true}" = true ] || { echo ATTESTATION_REFUSED; return 1; }; echo PROVENANCE_VERIFIED; }
detect_existing_dashboard_runtime() { :; }
` + "resume_installer_execution_setup() {\n" + functions + `
finish_installer_execution_setup() { echo RECOVERED_WITHOUT_PROVISIONING; }
resume_installer_execution_setup
echo ORDINARY_PREFLIGHT
`
			path := filepath.Join(t.TempDir(), "recovery.sh")
			if err = os.WriteFile(path, []byte(fixture), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", path)
			cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, tc.env...)
			out, runErr := cmd.CombinedOutput()
			code := 0
			if runErr != nil {
				if e, ok := runErr.(*exec.ExitError); ok {
					code = e.ExitCode()
				} else {
					t.Fatal(runErr)
				}
			}
			if code != tc.exit || !strings.Contains(string(out), tc.want) || strings.Contains(string(out), "UNEXPECTED_PRIVILEGED") {
				t.Fatalf("exit=%d want=%d: %s", code, tc.exit, out)
			}
			if tc.name == "exact image" && (!strings.Contains(string(out), "PROVENANCE_VERIFIED") || strings.Contains(string(out), "ORDINARY_PREFLIGHT")) {
				t.Fatalf("recovery admission/order lost: %s", out)
			}
			if tc.exit != 0 && strings.Contains(string(out), "RECOVERED_WITHOUT_PROVISIONING") {
				t.Fatal("refused image reached recovery")
			}
		})
	}
}
