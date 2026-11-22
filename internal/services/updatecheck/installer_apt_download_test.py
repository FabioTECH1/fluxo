"""Run the real installer APT wrapper against an unprivileged fake package manager."""
import json
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[3]
SOURCE = (ROOT / 'install.sh').read_text()
PRESENTATION = SOURCE.split('# BEGIN TERMINAL PRESENTATION\n',1)[1].split('# END TERMINAL PRESENTATION',1)[0]
WRAPPER = PRESENTATION + SOURCE[SOURCE.index('installer_apt() ('):SOURCE.index('prepare_installer_php_guard() {')]
FAKE_APT = '''#!{python}
import json, os, sys
from pathlib import Path
args = sys.argv[1:]
log = Path(os.environ['TEST_CALLS'])
calls = [json.loads(line) for line in log.read_text().splitlines()] if log.exists() else []
phase = 'download' if '--download-only' in args else 'update' if args[-1] == 'update' else 'install'
with log.open('a') as f:
    f.write(json.dumps(dict(phase=phase, args=args, guard=os.environ.get('APT_CONFIG'),
                           frontend=os.environ.get('DEBIAN_FRONTEND'), locale=os.environ.get('LC_ALL'))) + '\\n')
case = os.environ['TEST_CASE']
downloads = sum(c['phase'] == 'download' for c in calls)
if phase == 'download':
    if case in ('persistent', 'refresh-failed') or (case == 'recover' and downloads == 0):
        print('E: Failed to fetch http://mirror.example/ubuntu/nginx.deb  404  Not Found [IP: 192.0.2.1 80]', file=sys.stderr)
        sys.exit(100)
    if case in ('missing-package', 'authentication', 'network'):
        print(dict(**{'missing-package': 'E: Unable to locate package nginx',
                      'authentication': 'E: There were unauthenticated packages and -y was used without --allow-unauthenticated',
                      'network': 'E: Failed to fetch https://mirror.example/nginx.deb Connection timed out'})[case], file=sys.stderr)
        sys.exit(100)
if phase == 'update' and case == 'refresh-failed':
    print('E: A repository signature could not be verified', file=sys.stderr)
    sys.exit(100)
if phase == 'install':
    assert '--no-download' in args
    if case == 'install-failed':
        print('E: Sub-process /usr/bin/dpkg returned an error code (1)', file=sys.stderr)
        sys.exit(100)
print('FAKE APT: ' + phase)
'''


class InstallerAPTDownload(unittest.TestCase):
    def exercise(self, scenario, expected, phases, command=('install', '-y', 'nginx', 'certbot')):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            binary = root / 'apt-get'
            binary.write_text(FAKE_APT.replace('{python}', sys.executable))
            binary.chmod(0o700)
            if scenario == 'logging':
                tee = root / 'tee'
                tee.write_text('#!/bin/sh\ncat >/dev/null\nexit 1\n')
                tee.chmod(0o700)
            env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ['PATH'],
                       TEST_CALLS=str(root / 'calls.jsonl'), TEST_CASE=scenario, TMPDIR=directory)
            script = '''set -Eeuo pipefail
trap 'printf "parent cleanup\\n" >> "$TMPDIR/parent-exit"' EXIT
sudo() { [ "$1" = env ] || exit 99; shift; env "$@"; }
INSTALLER_APT_ENV=("APT_CONFIG=$TMPDIR/protected-apt.conf")
''' + WRAPPER + '\ninstaller_apt "$@"\n'
            result = subprocess.run(['bash', '-c', script, 'fixture', *command],
                                    env=env, capture_output=True, text=True, timeout=5)
            calls = [json.loads(line) for line in (root / 'calls.jsonl').read_text().splitlines()]
            self.assertEqual(result.returncode, expected, result.stdout + result.stderr)
            self.assertEqual((root / 'parent-exit').read_text(), 'parent cleanup\n')
            self.assertEqual([c['phase'] for c in calls], phases)
            for call in calls:
                self.assertEqual(call['guard'], str(root / 'protected-apt.conf'))
                self.assertEqual(call['frontend'], 'noninteractive')
                self.assertFalse(any(x in call['args'] for x in ('--fix-missing', '--allow-unauthenticated')))
                if call['phase'] in ('download', 'install'):
                    self.assertEqual(call['args'][-len(command):], list(command))
                if call['phase'] == 'download':
                    self.assertEqual(call['locale'], 'C')
            self.assertFalse(list(root.glob('tmp.*')), 'download logs must be cleaned on success and failure')
            return result.stdout + result.stderr, calls

    def test_success_installs_cached_archives_once(self):
        self.exercise('success', 0, ['download', 'install'])

    def test_404_refreshes_strictly_then_installs_once(self):
        output, calls = self.exercise('recover', 0, ['download', 'update', 'download', 'install'])
        self.assertIn('retrying downloads once', output)
        self.assertIn('APT::Update::Error-Mode=any', calls[1]['args'])

    def test_persistent_404_stops_before_installation(self):
        output, _ = self.exercise('persistent', 100, ['download', 'update', 'download'])
        self.assertIn('No package installation was started by this download attempt', output)
        self.assertIn('rerun the same Fluxo installer with your original options', output)
        self.assertIn('https://fluxo.fabiotech.dev/docs/reference/troubleshooting#', output)

    def test_failed_refresh_cancels_retry(self):
        output, _ = self.exercise('refresh-failed', 100, ['download', 'update'])
        self.assertIn('download retry was cancelled', output)

    def test_other_failures_do_not_retry(self):
        for scenario in ('missing-package', 'authentication', 'network'):
            with self.subTest(scenario=scenario):
                self.exercise(scenario, 100, ['download'])

    def test_dpkg_failure_is_not_replayed(self):
        output, _ = self.exercise('install-failed', 100, ['download', 'install'])
        self.assertIn('Package installation failed. It was not retried automatically', output)
        self.assertNotIn('No package installation was started', output)

    def test_output_failure_does_not_start_installation(self):
        self.exercise('logging', 1, ['download'])

    def test_non_install_command_keeps_original_arguments(self):
        command = ('-o', 'APT::Update::Error-Mode=any', 'update')
        _, calls = self.exercise('success', 0, ['update'], command)
        self.assertEqual(calls[0]['args'], list(command))


if __name__ == '__main__':
    unittest.main()
