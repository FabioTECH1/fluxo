"""Exercise the real bounded rollback inventory without touching host paths."""
import os
import pathlib
import subprocess
import tempfile
import unittest

source = (pathlib.Path(__file__).resolve().parents[3] / 'install.sh').read_text()
script = source.split("<<'PY_GUARDED_CRON_ROLLBACK'\n", 1)[1].split('\nPY_GUARDED_CRON_ROLLBACK', 1)[0]


class GuardedCronSnapshot(unittest.TestCase):
    def test_exact_inventory_and_unsafe_changes(self):
        for scenario in ('same', 'changed', 'new', 'missing', 'symlink', 'fifo', 'oversized'):
            with self.subTest(scenario=scenario), tempfile.TemporaryDirectory() as root:
                before, after = pathlib.Path(root) / 'before', pathlib.Path(root) / 'after'
                before.mkdir(mode=0o700)
                after.mkdir(mode=0o700)
                name = 'fluxo-cron-7'
                for directory in (before, after):
                    (directory / name).write_bytes(b'guarded-nonce-original\n')
                    (directory / name).chmod(0o644)
                if scenario == 'changed':
                    (after / name).write_bytes(b'guarded-nonce-new\n')
                elif scenario == 'new':
                    (after / 'fluxo-cron-8').write_bytes(b'new\n')
                elif scenario in ('missing', 'symlink', 'fifo'):
                    (after / name).unlink()
                    if scenario == 'symlink':
                        (after / name).symlink_to('missing-target')
                    elif scenario == 'fifo':
                        os.mkfifo(after / name)
                elif scenario == 'oversized':
                    with (after / name).open('wb') as output:
                        output.truncate(1024 * 1024 + 1)
                fixture = script.replace("TRUST_ANCHOR = '/'", 'TRUST_ANCHOR = ' + repr(root)).replace('PROTECTED_UID = 0', 'PROTECTED_UID = ' + str(os.geteuid()))
                result = subprocess.run(['python3', '-c', fixture, str(before), str(after)], capture_output=True, text=True, timeout=5)
                self.assertEqual(result.returncode == 0, scenario == 'same', result.stderr)


if __name__ == '__main__':
    unittest.main()
