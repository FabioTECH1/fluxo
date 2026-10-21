"""Exercise the exact embedded transaction guard, without running APT/dpkg."""
import contextlib
import io
import json
import pathlib
import sys
import tempfile
import unittest
from unittest.mock import patch


class SchedulerTransactionGuard(unittest.TestCase):
    def run_guard(self, state, package='', provides='', allowed=(), preserve_php=True, baseline=None, pending=False):
        source=pathlib.Path(sys.argv[1]).read_text()
        marker='sudo tee "$INSTALLER_APT_GUARD_DIR/guard.py" >/dev/null <<\'PY\'\n'
        start=source.index(marker)+len(marker)
        code=source[start:source.index('\nPY\n',start)]
        with tempfile.TemporaryDirectory() as directory:
            root=pathlib.Path(directory)
            (root/'scheduler.json').write_text(json.dumps(dict(packages=baseline if baseline is not None else {'cronie':'1.7.1-1'},allowed_new=list(allowed))))
            if preserve_php:(root/'preserve-php').touch()
            def check(args, **kwargs):
                if args[0]=='/usr/bin/dpkg-query':return state
                if args[-1]=='Package':return package
                if args[-1]=='Provides':return provides
                raise AssertionError(args)
            with patch('subprocess.check_output',side_effect=check),patch.object(sys,'stdin',io.StringIO('/fixture/archive.deb\n' if package else '')),patch.object(sys,'argv',['guard.py']+(['--pending'] if pending else [])):
                try:exec(compile(code,str(root/'guard.py'),'exec'),{'__file__':str(root/'guard.py')})
                except SystemExit as error:return error.code
            return None

    def test_existing_provider_cannot_be_reconfigured_or_replaced(self):
        state='cronie:amd64\tinstalled\tok\tcron-daemon\t1.7.1-1\n'
        for package,provides in [('cronie','cron-daemon'),('cron','cron-daemon'),('other-provider','cron-daemon (= 1)'),('cron-daemon-common','')]:
            with self.subTest(package=package):self.assertIsInstance(self.run_guard(state,package,provides),str)
        self.assertIsNone(self.run_guard(state,'nginx'))

    def test_pending_and_changed_provider_refused(self):
        self.assertIsInstance(self.run_guard('cronie\thalf-configured\tok\tcron-daemon\t1.7.1-1\n',pending=True),str)
        self.assertIsInstance(self.run_guard('cronie\tinstalled\tok\tcron-daemon\t1.7.2\n',pending=True),str)
        self.assertIsInstance(self.run_guard('',pending=True),str)

    def test_fresh_intended_default_and_pending_steps(self):
        self.assertIsNone(self.run_guard('', 'cron','cron-daemon',allowed=['cron','cron-daemon-common'],baseline={},preserve_php=False))
        self.assertEqual(self.run_guard('cron\tunpacked\tok\tcron-daemon\t3.0pl1-184ubuntu2\n',allowed=['cron'],baseline={},preserve_php=False,pending=True),0)
        self.assertIsInstance(self.run_guard('','cronie','cron-daemon',allowed=['cron'],baseline={},preserve_php=False),str)

    def test_existing_php_guard_preserved(self):
        state='cronie\tinstalled\tok\tcron-daemon\t1.7.1-1\n'
        self.assertIsInstance(self.run_guard(state,'php8.4-fpm'),str)
        self.assertIsNone(self.run_guard(state,'php8.4-fpm',preserve_php=False))


if __name__=='__main__':unittest.main(argv=[sys.argv[0]],verbosity=2)
