"""Exercise the real installer functions with inert system/API stubs."""
import pathlib
import re
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[2]
SOURCE = (ROOT / "install.sh").read_text()


def function(name):
    match = re.search(r"^" + name + r"\(\) \{.*?^\}", SOURCE, re.M | re.S)
    if not match:
        raise AssertionError("installer function missing: " + name)
    return match.group()


class InstallerTaskPasses(unittest.TestCase):
    def run_case(self, transitions, recovery=False, compatible=True, malformed=False,
                 fail_install=False, fail_health=False, initial=True):
        with tempfile.TemporaryDirectory() as directory:
            calls = pathlib.Path(directory) / "calls"
            calls.touch()
            ids = ["11111111-1111-4111-8111-" + f"{i:012d}" for i in range(1, 5)]
            sequence = " ".join(ids[i] for i in transitions)
            script = r'''
set -euo pipefail
DASHBOARD_USE_HTTP=false
INSTALLED_FLUXO_VERSION=0.4.53-test.1
task_step=0
sudo() {
    local action="${@: -1}"
    case "$action" in
    inspect)
        if [ "$TASK_BAD" = 1 ]; then printf 'not-json'; return 1; fi
        if [ -z "$task_floor" ]; then
            printf '{"compatible":true,"capabilities":{},"journals":[]}'
        else
            printf '{"compatible":%s,"minimum_readers":{"activation_id":"%s"}}' "$TASK_COMPATIBLE" "$task_floor"
        fi
        ;;
    install)
        printf 'install\n' >> "$TASK_CALLS"
        if [ "$TASK_FAIL_INSTALL" = 1 ]; then return 1; fi
        task_floor="${task_floors[$task_step]:-${task_floors[${#task_floors[@]}-1]}}"
        task_step=$((task_step+1))
        ;;
    install-complete) printf 'complete\n' >> "$TASK_CALLS" ;;
    *) return 92 ;;
    esac
}
wait_for_fluxo_dashboard() {
    printf 'health\n' >> "$TASK_CALLS"
    [ "$TASK_FAIL_HEALTH" != 1 ]
}
read_active_panel_domain() { printf ''; }
restore_installer_github_links() { printf 'links\n' >> "$TASK_CALLS"; }
'''
            script += "\nTASK_CALLS=" + repr(str(calls))
            script += "\ntask_floor=" + repr(ids[0] if initial else "")
            script += "\ntask_floors=(" + sequence + ")\n"
            script += f"TASK_BAD={int(malformed)}\nTASK_FAIL_INSTALL={int(fail_install)}\nTASK_FAIL_HEALTH={int(fail_health)}\n"
            script += "TASK_COMPATIBLE=" + ("true" if compatible else "false") + "\n"
            script += function("read_installer_execution_floor_id") + "\n"
            script += function("finish_installer_execution_setup") + "\n"
            script += "finish_installer_execution_setup" + (" recover" if recovery else "")
            result = subprocess.run(["bash", "-c", script], text=True, capture_output=True, timeout=10)
            return result, calls.read_text().splitlines()

    def test_ordinary_unchanged_update_stops_after_one_pass(self):
        result, calls = self.run_case([0, 0, 0])
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(["install", "health", "complete", "links"], calls)

    def test_backend_then_helper_then_unchanged(self):
        result, calls = self.run_case([1, 2, 2])
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(["install", "health", "complete"] * 3 + ["links"], calls)

    def test_recovery_does_not_stop_at_unchanged_operation_id(self):
        result, calls = self.run_case([0, 1, 1], recovery=True, compatible=False)
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(3, calls.count("install"))

    def test_recovery_without_more_changes_stops_after_two_passes(self):
        result, calls = self.run_case([0, 0, 0], recovery=True)
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(2, calls.count("install"))

    def test_absent_first_floor_is_not_a_parse_failure(self):
        result, calls = self.run_case([1, 1], initial=False)
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(2, calls.count("install"))

    def test_failed_install_does_not_complete_or_recover_github(self):
        result, calls = self.run_case([1], fail_install=True)
        self.assertNotEqual(0, result.returncode)
        self.assertEqual(["install"], calls)

    def test_failed_health_keeps_completion_pending(self):
        result, calls = self.run_case([1], fail_health=True)
        self.assertNotEqual(0, result.returncode)
        self.assertEqual(["install", "health"], calls)

    def test_unreadable_floor_report_does_not_start_mutation(self):
        result, calls = self.run_case([0], malformed=True)
        self.assertNotEqual(0, result.returncode)
        self.assertEqual([], calls)

    def test_public_installer_is_identical(self):
        self.assertEqual((ROOT / "install.sh").read_bytes(),
                         (ROOT / "ui-site/public/install.sh").read_bytes())


if __name__ == "__main__":
    unittest.main()
