package updatecheck

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerRequiresLegacyMigrationAwareCandidate(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	_, tail, ok := strings.Cut(string(source), "<<'PY_NODE_COMPAT'\n")
	script, _, end := strings.Cut(tail, "\nPY_NODE_COMPAT")
	if !ok || !end {
		t.Fatal("compatibility check not found")
	}
	for _, tc := range []struct {
		name, payload   string
		aware, accepted bool
	}{
		{"unused", "", false, true},
		{"ordinary update", `{"plan":{"after":{"version":"24.22.1"}}}`, false, true},
		{"migration unsupported", `{"plan":{"migration":{"schema":1}}}`, false, false},
		{"migration supported", `{"plan":{"migration":{"schema":1}}}`, true, true},
		{"malformed journal", `not-json`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			candidate := filepath.Join(root, "candidate")
			statePath := filepath.Join(root, "node-toolchain.json")
			dbPath := filepath.Join(root, "fluxo.db")
			shell := "#!/bin/sh\ncase \"$1\" in --node-update-support) echo 1;; --node-migration-support) "
			if tc.aware {
				shell += "echo 1"
			} else {
				shell += "exit 2"
			}
			shell += ";; *) exit 3;; esac\n"
			if err := os.WriteFile(candidate, []byte(shell), 0755); err != nil {
				t.Fatal(err)
			}
			quote := func(v string) string { b, _ := json.Marshal(v); return string(b) }
			relocated := strings.ReplaceAll(script, "pathlib.Path('/var/lib/fluxo/node-toolchain.json')", "pathlib.Path("+quote(statePath)+")")
			relocated = strings.ReplaceAll(relocated, "pathlib.Path('/var/lib/fluxo/fluxo.db')", "pathlib.Path("+quote(dbPath)+")")
			setup := "import sqlite3\nc=sqlite3.connect(" + quote(dbPath) + ")\nc.execute('CREATE TABLE node_update_generations(payload TEXT)')\n"
			if tc.payload != "" {
				setup += "c.execute('INSERT INTO node_update_generations VALUES (?)',(" + quote(tc.payload) + ",))\n"
			}
			setup += "c.commit()\nc.close()\n"
			cmd := exec.Command("python3", "-", candidate)
			cmd.Stdin = strings.NewReader(setup + relocated)
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.accepted {
				t.Fatalf("wrong compatibility decision: %v %s", err, out)
			}
			if !tc.accepted && tc.name != "malformed journal" && !strings.Contains(string(out), "migration") {
				t.Fatalf("unhelpful rejection: %s", out)
			}
		})
	}
}

// Run the real shell rollback code inside EXIT cleanup. All paths are relocated
// to a disposable tree; sudo is a guarded pass-through, never a host escalation.
func TestInstallerNodeRollbackInsideFailedExitTrap(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	_, tail, ok := strings.Cut(string(source), "node_toolchain_link_names() {")
	functions, _, end := strings.Cut(tail, "\nread_recorded_node_daemons() {")
	if !ok || !end {
		t.Fatal("Node rollback functions not found")
	}
	functions = "node_toolchain_link_names() {" + functions
	for _, failure := range []string{"none", "copy", "rename", "remove"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			snapshot := filepath.Join(root, "snapshot")
			write := func(path, content string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(snapshot, "node.absent"), "")
			write(filepath.Join(root, "opt/node/candidate"), "candidate")
			for label, target := range map[string]string{
				"node-toolchain":      "opt/node-toolchain/receipt",
				"node-toolchain.json": "data/node-toolchain.json",
				"corepack-home":       "home/corepack/receipt",
			} {
				backup := label
				if label != "node-toolchain.json" {
					backup += "/receipt"
				}
				write(filepath.Join(snapshot, label+".present"), "")
				write(filepath.Join(snapshot, backup), "original-"+label)
				write(filepath.Join(root, target), "candidate")
			}
			write(filepath.Join(snapshot, "links/node.present"), "")
			write(filepath.Join(root, "external-node"), "external node remains unchanged")
			for path, target := range map[string]string{
				filepath.Join(snapshot, "links/node"): filepath.Join(root, "external-node"),
				filepath.Join(root, "bin/node"):       filepath.Join(root, "opt/node/current/bin/node"),
			} {
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			}
			relocated := strings.NewReplacer(
				"/opt/fluxo/node-toolchain", root+"/opt/node-toolchain",
				"/opt/fluxo/node", root+"/opt/node",
				"/var/lib/fluxo/node-toolchain.json", root+"/data/node-toolchain.json",
				"/home/fluxo/.cache/node/corepack", root+"/home/corepack",
				"/usr/local/bin", root+"/bin",
			).Replace(functions)
			script := `set -Eeuo pipefail
sudo() {
    case "$1" in test|mkdir|rm|mv|cp|readlink) ;; *) echo UNEXPECTED_COMMAND; exit 99;; esac
    local argument
    for argument in "$@"; do
        if [[ "$argument" == /* && "$argument" != "$TEST_ROOT"/* ]]; then echo HOST_PATH_FORBIDDEN; exit 99; fi
    done
    case "$TEST_FAILURE:$1" in
        copy:cp) if [[ "$*" == *"$NODE_ROLLBACK_DIR/corepack-home"* ]]; then echo INJECTED_FAILURE >&2; return 71; fi ;;
        rename:mv) if [[ "${!#}" == "$TEST_ROOT/opt/node-toolchain" ]]; then echo INJECTED_FAILURE >&2; return 72; fi ;;
        remove:rm) if [[ "${!#}" == "$TEST_ROOT/data/node-toolchain.json" ]]; then echo INJECTED_FAILURE >&2; return 73; fi ;;
    esac
    "$@"
}

NODE_ROLLBACK_DIR="$TEST_ROOT/snapshot"
NODE_ROLLBACK_ARMED=true
` + relocated + `
cleanup() {
    local status=$?
    trap - EXIT
    set +e
    if ! rollback_node_toolchain; then echo RESTORE_FAILED; else echo RESTORE_OK; fi
    exit "$status"
}
trap cleanup EXIT
exit 37
`
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "TEST_ROOT=" + root, "TEST_FAILURE=" + failure}
			output, err := cmd.CombinedOutput()
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 37 {
				t.Fatalf("original installation error lost: %v\n%s", err, output)
			}
			if failure != "none" {
				if !strings.Contains(string(output), "INJECTED_FAILURE") || !strings.Contains(string(output), "RESTORE_FAILED") || strings.Contains(string(output), "RESTORE_OK") {
					t.Fatalf("real rollback failure hidden:\n%s", output)
				}
				if _, err := os.Stat(snapshot); err != nil {
					t.Fatal("failed recovery snapshot was discarded", err)
				}
				return
			}
			if !strings.Contains(string(output), "RESTORE_OK") || strings.Contains(string(output), "ERROR:") {
				t.Fatalf("successful rollback reported failure in EXIT trap:\n%s", output)
			}
			for label, target := range map[string]string{"node-toolchain": "opt/node-toolchain/receipt", "node-toolchain.json": "data/node-toolchain.json", "corepack-home": "home/corepack/receipt"} {
				got, err := os.ReadFile(filepath.Join(root, target))
				if err != nil || string(got) != "original-"+label {
					t.Fatal("original Node files not restored", label, string(got), err)
				}
			}
			if _, err := os.Stat(snapshot); !os.IsNotExist(err) {
				t.Fatal("successful rollback left its disposable snapshot", err)
			}
			if _, err := os.Stat(filepath.Join(root, "opt/node")); !os.IsNotExist(err) {
				t.Fatal("previously absent managed Node tree not restored", err)
			}
			if target, err := os.Readlink(filepath.Join(root, "bin/node")); err != nil || target != filepath.Join(root, "external-node") {
				t.Fatal("external Node command selection not preserved", target, err)
			}
		})
	}
}

func TestInstallerNodeHelpersReturnSuccessDuringExitCleanup(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	_, tail, ok := strings.Cut(string(source), "read_recorded_node_daemons() {")
	functions, _, end := strings.Cut(tail, "\nstop_recorded_managed_node_daemons() {")
	if !ok || !end {
		t.Fatal("Node health helpers not found")
	}
	script := `set -Eeuo pipefail
NODE_DAEMON_SNAPSHOT=""
sudo() {
    [ "$1" = systemctl ] && [ "$2" = show ] || exit 99
    printf 'ActiveState=active\nSubState=running\nMainPID=12345\n'
}
python3() { cat >/dev/null; return 0; }
sleep() { :; }
read_recorded_node_daemons() {` + functions + `
cleanup() {
    local status=$?
    trap - EXIT
    set +e
    if ! read_recorded_node_daemons; then echo EMPTY_SNAPSHOT_FAILED; exit 98; fi
    if ! wait_for_managed_node_daemon 1 app.example.test 12345; then echo HEALTH_FAILED; exit 97; fi
    echo HEALTH_OK
    exit "$status"
}
trap cleanup EXIT
exit 37
`
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	output, err := cmd.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 37 || !strings.Contains(string(output), "HEALTH_OK") {
		t.Fatalf("successful application checks inherited failed EXIT status: %v\n%s", err, output)
	}
}

func TestInstallerNodeChildReceivesProductionConfiguration(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	_, tail, ok := strings.Cut(string(source), "install_fluxo_binary() {")
	function, _, end := strings.Cut(tail, "\ninstall_fluxo_binary\n")
	if !ok || !end {
		t.Fatal("installer binary publication function not found")
	}
	for _, lease := range []string{"", "fixture-lease"} {
		t.Run(lease, func(t *testing.T) {
			script := `set -Eeuo pipefail
INSTALL_NODE=true
INSTALL_PYTHON=false
SCHEDULER_LEASE_ID="$TEST_LEASE"
PREPARED_BINARY=fixture
COMPOSER_VERSION=fixture
WP_CLI_VERSION=fixture
INSTALLER_APT_ENV=("APT_CONFIG=/fixture/apt.conf")
prepare_upgrade_snapshot() { :; }
install_composer() { :; }
install_wp_cli() { :; }
prepare_node_toolchain_snapshot() { :; }
sudo() {
    case "$1" in
        mkdir|install|mv) return 0 ;;
        mktemp) printf '/fixture/candidate\n'; return 0 ;;
        /fixture/candidate)
            case "$2" in --version|--supports-node-toolchain) return 0;; --node-update-support) echo 1; return 0;; esac ;;
        env)
            shift
            [ "$1" = FLUXO_ENV=prod ] && [ "$2" = FLUXO_DATA_DIR=/var/lib/fluxo ] && [ "$3" = APT_CONFIG=/fixture/apt.conf ] || exit 98
            shift 3
            [ "$1" = /fixture/candidate ] && [ "$2" = node-toolchain ] && [ "$3" = install ] || exit 97
            if [ -n "$TEST_LEASE" ]; then [ "$4" = --installer-lease ] && [ "$5" = "$TEST_LEASE" ] || exit 96; else [ "$#" = 3 ] || exit 95; fi
            echo PRODUCTION_CONTEXT_OK
            return 0 ;;
    esac
    echo UNEXPECTED_COMMAND >&2
    exit 99
}
install_fluxo_binary() {` + function + `
install_fluxo_binary
`
			cmd := exec.Command("bash", "-c", script)
			cmd.Dir = t.TempDir()
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "TEST_LEASE=" + lease}
			output, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(output), "PRODUCTION_CONTEXT_OK") {
				t.Fatalf("Node child could inherit SSH development configuration: %v\n%s", err, output)
			}
		})
	}
}
