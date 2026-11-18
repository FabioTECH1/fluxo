package updatecheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerResumesOriginalOperationBeforeFreshCompatibilityPass(t *testing.T) {
	raw, err := os.ReadFile("../../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	_, tail, ok := strings.Cut(string(raw), "finish_installer_execution_setup() {\n")
	body, _, end := strings.Cut(tail, "\n# Exact installer recovery precedes")
	if !ok || !end {
		t.Fatal("installer completion routine missing")
	}
	for _, failure := range []string{"none", "install", "health", "complete"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			trace := filepath.Join(root, "trace")
			fixture := `set -euo pipefail
DASHBOARD_USE_HTTP=false
INSTALLED_FLUXO_VERSION=0.4.46-bridge.15
installs=0
completed=0
sudo() {
 case "$*" in
 'env FLUXO_ENV=prod FLUXO_DATA_DIR=/var/lib/fluxo FLUXO_USE_HTTP=0 /usr/local/bin/fluxo execution-compatibility install')
   installs=$((installs+1));echo install >> "$TRACE"
   [ "$installs" = 1 ] || [ "$completed" = 1 ] || { echo FRESH_PASS_BEFORE_OLD_COMPLETION;return 99; }
   [ "$FAIL_AT" != install ] ;;
 'env FLUXO_ENV=prod FLUXO_DATA_DIR=/var/lib/fluxo FLUXO_USE_HTTP=0 /usr/local/bin/fluxo execution-compatibility install-complete')
   echo complete >> "$TRACE";completed=$((completed+1));[ "$FAIL_AT" != complete ] ;;
 *) echo UNEXPECTED_PRIVILEGED_ACTION;return 99 ;;
 esac
}
wait_for_fluxo_dashboard() { echo health >> "$TRACE";[ "$FAIL_AT" != health ]; }
read_active_panel_domain() { printf ''; }
` + "finish_installer_execution_setup() {\n" + body + "\nfinish_installer_execution_setup\n"
			path := filepath.Join(root, "finish.sh")
			if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", path)
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "TRACE=" + trace, "FAIL_AT=" + failure}
			out, runErr := cmd.CombinedOutput()
			got, _ := os.ReadFile(trace)
			want := map[string]string{"none": "install\nhealth\ncomplete\ninstall\nhealth\ncomplete\n", "install": "install\n", "health": "install\nhealth\n", "complete": "install\nhealth\ncomplete\n"}[failure]
			if string(got) != want || (runErr == nil) != (failure == "none") || strings.Contains(string(out), "UNEXPECTED_") || strings.Contains(string(out), "FRESH_PASS_BEFORE") {
				t.Fatalf("failure=%s trace=%q err=%v out=%s", failure, got, runErr, out)
			}
		})
	}
}
