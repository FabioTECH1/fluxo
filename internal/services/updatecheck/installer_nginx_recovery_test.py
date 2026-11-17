"""Execute the actual rollback admission prefix without any host commands."""
import os
import pathlib
import subprocess
import unittest


INSTALLER = pathlib.Path(__file__).resolve().parents[3] / 'install.sh'
source = INSTALLER.read_text()
prefix = source[source.index('rollback_upgrade() {'):]
prefix = prefix.split('    local required_support=2 previous_support', 1)[0]

MOCK = r'''
check_guarded_cron_rollback_snapshot() { [ "${FIXTURE_CRON_FAILURE:-0}" = 0 ]; }
sudo() {
    case "$1" in
        test)
            [ "$3" = /etc/nginx/conf.d/.fluxo-http-defaults-transaction ] || return 90
            case "$2:$FIXTURE_STATE" in -e:idle|-e:pending|-L:symlink) return 0;; *) return 1;; esac
            ;;
        /candidate)
            [ "$2" = --nginx-http-defaults-recovery-ready ] || return 91
            [ "$FIXTURE_STATE" = idle ]
            ;;
        systemctl)
            [ "$2:$3" = stop:fluxo ] || return 92
            echo stopped
            ;;
        *) echo unexpected-command >&2; return 93;;
    esac
}
'''


class NginxRollbackRecovery(unittest.TestCase):
    def run_case(self, state, candidate, expected, cron_failure='0'):
        environment = dict(os.environ, FIXTURE_STATE=state, SCHEDULER_LEASE_BINARY=candidate, FIXTURE_CRON_FAILURE=cron_failure)
        result = subprocess.run(['bash', '-c', MOCK + prefix + '    return 0\n}\nrollback_upgrade\n'],
                                env=environment, text=True, capture_output=True)
        self.assertEqual(result.returncode, expected, result.stdout + result.stderr)
        self.assertNotIn('unexpected-command', result.stderr)
        if expected:
            self.assertIn('stopped', result.stdout)
            self.assertIn('retain the candidate', result.stdout)
        else:
            self.assertNotIn('stopped', result.stdout)

    def test_absence_and_completed_recovery_allow_remaining_checks(self):
        self.run_case('absent', '', 0)
        self.run_case('idle', '/candidate', 0)

    def test_pending_symlink_and_missing_candidate_preserve_candidate(self):
        self.run_case('pending', '/candidate', 1)
        self.run_case('symlink', '/candidate', 1)
        self.run_case('pending', '', 1)

    def test_stale_cron_snapshot_stops_before_remaining_rollback_writes(self):
        self.run_case('absent', '/candidate', 1, cron_failure='1')


if __name__ == '__main__':
    unittest.main()
