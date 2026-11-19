"""Exact-source transport must not qualify a dirty or mismatched candidate."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from build_release_candidate import digest, source_identity


class CandidateSourceIdentityTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.previous = os.getcwd()
        os.chdir(self.temp.name)
        self.addCleanup(self.restore)
        self.git('init', '-q')
        for name, value in [('install.sh', 'fixture installer\n'),
                            ('ui-site/public/install.sh', 'fixture installer\n'),
                            ('.github/release-tool-versions.conf', 'fixture pins\n')]:
            Path(name).parent.mkdir(parents=True, exist_ok=True)
            Path(name).write_text(value)
        self.commit()
        self.source = self.git('rev-parse', 'HEAD').strip()
        self.environment = patch.dict(os.environ, {'GITHUB_SHA': self.source})
        self.environment.start()
        self.addCleanup(self.environment.stop)

    def restore(self):
        os.chdir(self.previous)
        self.temp.cleanup()

    def git(self, *args):
        return subprocess.check_output(['git', '-c', 'user.name=Fixture',
                                       '-c', 'user.email=fixture@example.invalid',
                                       '-c', 'commit.gpgsign=false', *args], text=True)

    def commit(self):
        self.git('add', '.')
        self.git('commit', '-qm', 'fixture')

    def test_clean_subject_binds_commit_tree_installer_and_pin_file(self):
        result = source_identity()
        self.assertEqual(result['source'], self.source)
        self.assertEqual(result['source_tree'], self.git('rev-parse', 'HEAD^{tree}').strip())
        self.assertEqual(result['installer_sha256'], digest('install.sh'))
        self.assertEqual(result['tool_pins_sha256'], digest('.github/release-tool-versions.conf'))

    def test_changed_installer_cannot_keep_old_source_identity(self):
        Path('install.sh').write_text('changed installer\n')
        with self.assertRaisesRegex(RuntimeError, 'differs from the recorded commit'):
            source_identity()

    def test_untracked_go_source_cannot_enter_a_candidate(self):
        Path('internal').mkdir()
        Path('internal/injected.go').write_text('package injected\n')
        with self.assertRaisesRegex(RuntimeError, 'differs from the recorded commit'):
            source_identity()

    def test_workflow_source_mismatch_is_refused(self):
        with patch.dict(os.environ, {'GITHUB_SHA': '0' * 40}):
            with self.assertRaisesRegex(RuntimeError, 'requested workflow source'):
                source_identity()

    def test_committed_website_installer_must_match_release_installer(self):
        Path('ui-site/public/install.sh').write_text('stale website installer\n')
        self.commit()
        with patch.dict(os.environ, {'GITHUB_SHA': self.git('rev-parse', 'HEAD').strip()}):
            with self.assertRaisesRegex(RuntimeError, 'Website and release installers differ'):
                source_identity()


if __name__ == '__main__':
    unittest.main()
