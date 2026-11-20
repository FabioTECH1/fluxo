package updatecheck

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestInstallerGitHubLinksFailuresNeverStopUpdate(t *testing.T) {
	raw, e := os.ReadFile("../../../install.sh")
	if e != nil {
		t.Fatal(e)
	}
	_, tail, ok := strings.Cut(string(raw), "restore_installer_github_links() {\n")
	body, _, end := strings.Cut(tail, "\n}\n\nfinish_installer_execution_setup()")
	if !ok || !end {
		t.Fatal("missing optional recovery function")
	}
	for _, tc := range []struct{ name, support, exit, expected string }{
		{"healthy", "1", "0", "UPDATE_FINISHED"},
		{"unavailable", "1", "1", "WARNING:"},
		{"deadline", "1", "124", "WARNING:"},
		{"older-release", "0", "0", "UPDATE_FINISHED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := `set -euo pipefail
sudo(){
 case "$*" in
 '/usr/local/bin/fluxo --installer-github-links-support') echo ` + tc.support + ` ;;
 'timeout 30 env FLUXO_ENV=prod FLUXO_DATA_DIR=/var/lib/fluxo /usr/local/bin/fluxo installer-github-links') return ` + tc.exit + ` ;;
 *) echo UNEXPECTED_ACTION; exit 99 ;;
 esac
}
restore_installer_github_links(){
` + body + `
}
restore_installer_github_links
echo UPDATE_FINISHED
`
			out, e := exec.Command("bash", "-c", script).CombinedOutput()
			if e != nil || !strings.Contains(string(out), "UPDATE_FINISHED") || !strings.Contains(string(out), tc.expected) {
				t.Fatal(string(out), e)
			}
		})
	}
	_, finish, _ := strings.Cut(string(raw), "finish_installer_execution_setup() {\n")
	finish, _, _ = strings.Cut(finish, "# Exact installer recovery precedes")
	if strings.Index(finish, "restore_installer_github_links") < strings.Index(finish, "execution-compatibility install-complete") {
		t.Fatal("recovery must follow successful compatibility completion")
	}
}
