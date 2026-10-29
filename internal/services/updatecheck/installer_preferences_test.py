"""Run the installer's actual embedded preference code and selection functions.

Host fixtures substitute only the private path/expected owner; no sudo, packages,
services or production preference file is used.
"""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[3] / 'install.sh'
SOURCE = SCRIPT.read_text()
CODE = SOURCE.split("<<'PY_INSTALLER_PREFERENCES'\n", 1)[1].split('\nPY_INSTALLER_PREFERENCES', 1)[0]
FUNCTIONS = SOURCE[SOURCE.index('load_installer_preferences() {'):SOURCE.index('while [ $# -gt 0 ]; do')]


class InstallerPreferences(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / 'preferences'
        self.code = CODE.replace('ROOT = "/var/lib/fluxo-installer"', f'ROOT = {str(self.root)!r}')
        self.code = self.code.replace('info.st_uid != 0 or info.st_gid != 0', 'info.st_uid != os.getuid() or info.st_gid != os.getgid()')
        self.code = self.code.replace('("/", "/var", "/var/lib")', f'({self.temp.name!r},)')

    def run_code(self, *args, success=True, code=None):
        result = subprocess.run(['python3', '-c', code or self.code, *args], capture_output=True, text=True)
        self.assertEqual(result.returncode == 0, success, result.stderr)
        return result

    def save(self):
        self.run_code('write', 'true', 'false', 'true', 'false', 'false')
        return self.root / 'preferences.json'

    def test_roundtrip_and_changed_choices(self):
        self.assertEqual(self.run_code('read').stdout.strip(), '- - - - -')
        path = self.save()
        self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        self.assertEqual(self.root.stat().st_mode & 0o777, 0o700)
        self.assertEqual(self.run_code('read').stdout.strip(), 'true false true false false')
        self.run_code('write', 'false', 'true', 'false', 'true', 'true')
        self.assertEqual(self.run_code('read').stdout.strip(), 'false true false true true')

    def test_production_owner_check_refuses_unprivileged_fixture(self):
        if os.getuid() == 0:
            self.skipTest('requires an unprivileged fixture owner')
        self.save()
        production_owner = self.code.replace('info.st_uid != os.getuid() or info.st_gid != os.getgid()', 'info.st_uid != 0 or info.st_gid != 0')
        self.run_code('read', code=production_owner, success=False)

    def test_fresh_explicit_choices_do_not_prompt(self):
        self.assertIn('write false false false false false', self.selection('- - - - -', False,
            'INSTALL_NODE=false; INSTALL_PYTHON=false; INSTALL_MYSQL=false; INSTALL_POSTGRES=false; INSTALL_REDIS=false'))

    def test_invalid_records_refuse_read_and_overwrite(self):
        path = self.save()
        for value in ['{', '{"schema":2,"components":{}}', '{"schema":true,"components":{}}',
                      json.dumps({'schema': 1, 'components': dict.fromkeys(('node', 'python', 'mysql', 'postgres', 'redis'), 0)})]:
            path.write_text(value)
            self.run_code('read', success=False)
            self.run_code('write', *(['false'] * 5), success=False)
            self.assertEqual(path.read_text(), value)

    def test_unsafe_paths(self):
        path = self.save()
        original = path.read_bytes()
        path.chmod(0o666)
        self.run_code('read', success=False)
        path.chmod(0o600)
        link = self.root / 'hardlink'
        os.link(path, link)
        self.run_code('read', success=False)
        link.unlink()
        path.unlink()
        path.symlink_to(link)
        self.run_code('read', success=False)
        self.run_code('write', *(['false'] * 5), success=False)
        path.unlink()
        path.write_bytes(original)
        self.root.chmod(0o777)
        self.run_code('read', success=False)

    def test_failed_publication_preserves_previous_record(self):
        path = self.save()
        before = path.read_bytes()
        injected = self.code.replace('os.replace(temporary, ROOT + "/preferences.json")', 'raise OSError("injected publish failure")')
        self.run_code('write', *(['false'] * 5), code=injected, success=False)
        self.assertEqual(path.read_bytes(), before)
        self.assertEqual(list(self.root.glob('.preferences-*')), [])

    def selection(self, saved, existing, explicit='', installed=False):
        # All upgrade choices must finish without accessing /dev/tty. The mock
        # fails unrecognized privileged operations; only observation is allowed.
        shell = '''set -euo pipefail
INSTALL_NODE=""; INSTALL_PYTHON=""; INSTALL_MYSQL=""; INSTALL_POSTGRES=""; INSTALL_REDIS=""
PREFERENCE_MYSQL=""; PREFERENCE_POSTGRES=""
MYSQL_EXISTS=INSTALLED; POSTGRES_EXISTS=INSTALLED; REDIS_EXISTS=INSTALLED
EXISTING_INSTALL=@EXISTING@
command() { if [ "$*" = '-v node' ]; then INSTALLED; else builtin command "$@"; fi; }
sudo() { [ "$*" = 'test -f /var/lib/fluxo/python-toolchain.json' ] && INSTALLED; }
installer_preferences() {
 if [ "$1" = read ]; then echo 'SAVED'; else printf 'SAVED_CHOICES %s\\n' "$*"; fi
}
'''.replace('INSTALLED', str(installed).lower()).replace('@EXISTING@', str(existing).lower()).replace('SAVED\'', saved + "'")
        result = subprocess.run(['bash', '-c', shell + FUNCTIONS + '\n' + explicit + '\nload_installer_preferences\nselect_optional_components'], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        return result.stdout

    def test_saved_no_reused_and_explicit_overrides(self):
        self.assertIn('write false false false false false', self.selection('false false false false false', True))
        self.assertIn('write true false false true true', self.selection('false false false false false', True,
            'INSTALL_NODE=true; INSTALL_POSTGRES=true; INSTALL_REDIS=true'))

    def test_legacy_upgrade_and_in_app_components_preserved(self):
        self.assertIn('write false false false false false', self.selection('- - - - -', True))
        self.assertIn('write true true true true true', self.selection('- - - - -', True, installed=True))
        self.assertIn('write false false false false false', self.selection('false false false false false', True, installed=True))


if __name__ == '__main__':
    unittest.main()
