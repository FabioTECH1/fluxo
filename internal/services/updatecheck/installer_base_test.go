package updatecheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Execute the actual gate and entry ordering against synthetic authenticated
// candidates. Every later installer phase is a trace sentinel, so refusal cannot
// reach runtime recovery, version shortcuts or host provisioning.
func TestInstallerBaseGateBeforeRecoveryAndProvisioning(t *testing.T) {
	raw, err := os.ReadFile("../../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	_, tail, ok := strings.Cut(string(raw), "base_install_command() {\n")
	gateAndEntry, _, end := strings.Cut(tail, "\nselect_optional_components\n")
	if !ok || !end {
		t.Fatal("base gate must precede component selection")
	}
	for _, tc := range []struct {
		name, mode, wantTrace, wantOutput string
		env                               []string
		exit                              int
	}{
		{"new server", "fresh", "stage\nauth\ninspect\nformats\nskip\nhost\n", "MODE=fresh", nil, 0},
		{"interrupted fresh", "fresh-pending", "stage\nauth\ninspect\nformats\nskip\nhost\n", "MODE=fresh-pending", nil, 0},
		{"completed bridge", "bridge", "stage\nauth\ninspect\nformats\nskip\nhost\n", "Completed bridge verified. Continuing to the base automatically.", nil, 0},
		{"ordinary base update", "base", "stage\nauth\ninspect\nresume\nformats\nskip\nhost\n", "MODE=base", nil, 0},
		{"base recovery", "base-recovery", "stage\nauth\ninspect\nresume\ninspect\nformats\nskip\nhost\n", "MODE=base-recovery", nil, 0},
		{"old installation", "refuse", "stage\nauth\ninspect\n", "Finish any recovery reported by that installer, then rerun this base installer.", nil, 1},
		{"partial installation", "refuse", "stage\nauth\ninspect\n", "No packages, repositories, configuration, services or database were changed.", nil, 1},
		{"unknown response", "unknown", "stage\nauth\ninspect\n", "Unsupported base admission response", nil, 1},
		{"non-base binary", "fresh", "stage\nauth\n", "selected binary is not a base release", []string{"TEST_BASE_SUPPORT=old"}, 1},
		{"unauthenticated candidate", "fresh", "stage\nauth\n", "AUTH_REFUSED", []string{"TEST_AUTH=false"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			trace := filepath.Join(dir, "trace")
			candidate := filepath.Join(dir, "candidate")
			candidateProgram := `#!/bin/bash
set -euo pipefail
case "$*" in
 '--base-support') printf '%s\n' "${TEST_BASE_SUPPORT:-fluxo.base.v1}" ;;
 'base-install inspect')
   echo inspect >> "$TRACE"
   [ "$TEST_MODE" != refuse ] || { echo 'original bridge evidence incomplete' >&2;exit 1; }
   printf '{"mode":"%s"}\n' "$TEST_MODE" ;;
 *) echo UNEXPECTED_CANDIDATE_COMMAND >&2;exit 99 ;;
esac
`
			if err = os.WriteFile(candidate, []byte(candidateProgram), 0700); err != nil {
				t.Fatal(err)
			}
			fixture := `set -euo pipefail
PREPARED_BINARY=""
MAINTENANCE_AUTH_BINARY=""
SCHEDULER_LEASE_ID=""
prepare_fluxo_binary() { echo stage >> "$TRACE"; PREPARED_BINARY="$CANDIDATE"; }
authenticate_maintenance_candidate() {
 echo auth >> "$TRACE"
 [ "${TEST_AUTH:-true}" = true ] || { echo AUTH_REFUSED;return 1; }
 MAINTENANCE_AUTH_BINARY="$PREPARED_BINARY"
}
sudo() {
 case "$1" in
  env|timeout) "$@" ;;
  *) echo UNEXPECTED_PRIVILEGED_ACTION >&2;exit 99 ;;
 esac
}
resume_installer_execution_setup() { echo resume >> "$TRACE"; }
preflight_execution_reader_formats() { echo formats >> "$TRACE"; }
skip_current_fluxo_release() { echo skip >> "$TRACE"; }
preflight_host() { echo host >> "$TRACE"; }
` + "base_install_command() {\n" + gateAndEntry + "\necho MODE=\"$BASE_INSTALL_MODE\"\n"
			path := filepath.Join(dir, "gate.sh")
			if err = os.WriteFile(path, []byte(fixture), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", path)
			cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "TRACE=" + trace, "CANDIDATE=" + candidate, "TEST_MODE=" + tc.mode}, tc.env...)
			out, runErr := cmd.CombinedOutput()
			code := 0
			if runErr != nil {
				if e, ok := runErr.(*exec.ExitError); ok {
					code = e.ExitCode()
				} else {
					t.Fatal(runErr)
				}
			}
			gotTrace, _ := os.ReadFile(trace)
			if code != tc.exit || string(gotTrace) != tc.wantTrace || !strings.Contains(string(out), tc.wantOutput) || strings.Contains(string(out), "UNEXPECTED_") {
				t.Fatalf("exit=%d want=%d trace=%q want=%q out=%s", code, tc.exit, gotTrace, tc.wantTrace, out)
			}
			if tc.mode == "refuse" {
				const bridgeCommand = "curl -fsSL https://github.com/FabioTECH1/fluxo/releases/download/v0.4.51/install.sh | sudo env FLUXO_VERSION=v0.4.51 bash"
				if !strings.Contains(string(out), bridgeCommand) {
					t.Fatalf("refusal lacks exact published bridge command: %s", out)
				}
			}
		})
	}
}
