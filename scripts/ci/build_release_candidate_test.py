"""Exact-source transport must not qualify a dirty or mismatched candidate."""
import json
import os
from pathlib import Path
import shlex
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from build_release_candidate import digest, main, source_identity


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


class CandidateToolchainTest(unittest.TestCase):
    def setUp(self):
        pins = (Path(__file__).resolve().parents[2] / '.github/release-tool-versions.conf').read_text()
        self.temp = tempfile.TemporaryDirectory()
        self.previous = os.getcwd()
        os.chdir(self.temp.name)
        self.addCleanup(self.restore)
        Path('go.mod').write_text('module fixture\n\ngo 1.26.3\n')
        Path('.github').mkdir()
        Path('.github/release-tool-versions.conf').write_text(pins)
        self.observed_go = 'go1.26.3'
        self.built_go = 'go1.26.3'
        self.goarch = 'amd64'
        self.commands = []
        self.flags = ''

    def restore(self):
        os.chdir(self.previous)
        self.temp.cleanup()

    def check_environment(self, command, env):
        self.assertEqual(env['GOTOOLCHAIN'], 'go1.26.3', command)
        self.assertEqual(env['CGO_ENABLED'], '0', command)
        self.assertEqual(env['GOOS'], 'linux', command)
        self.commands.append(command)

    def output(self, command, *, env, text):
        self.check_environment(command, env)
        if command == ['go', 'version']:
            return f'go version {self.observed_go} linux/amd64\n'
        if command == ['go', 'env', 'GOARCH']:
            return self.goarch + '\n'
        if command == ['go', 'version', '-m', 'candidate']:
            return f'candidate: {self.built_go}\n\tbuild\t-ldflags={self.flags}\n'
        self.fail(f'Unexpected compiler observation: {command}')

    def build(self, command, *, env, check):
        self.check_environment(command, env)
        self.assertEqual(command[:2], ['go', 'build'])
        self.assertTrue(check)
        self.flags = command[command.index('-ldflags') + 1]
        Path('candidate').write_bytes(b'checksummed fixture compilation')

    def invoke(self):
        with patch('build_release_candidate.source_identity', return_value={'source': 'a' * 40}), \
             patch('build_release_candidate.platform.machine', return_value='x86_64'), \
             patch('build_release_candidate.subprocess.check_output', side_effect=self.output), \
             patch('build_release_candidate.subprocess.run', side_effect=self.build), \
             patch('build_release_candidate.sys.argv', ['builder', 'candidate', 'fixture']), \
             patch.dict(os.environ, {'GOTOOLCHAIN': 'auto'}):
            main()

    def test_project_toolchain_selected_for_all_observations_and_build(self):
        self.invoke()
        manifest = json.loads(Path('candidate.manifest.json').read_text())
        self.assertEqual(manifest['go_version'], 'go version go1.26.3 linux/amd64')
        self.assertEqual(manifest['source'], 'a' * 40)
        self.assertEqual(manifest['binary_sha256'], digest('candidate'))
        self.assertEqual(len(manifest['official_pin_assignments']), 19)
        self.assertEqual(shlex.split(self.flags).count('-X'), 20)
        self.assertEqual(len(self.commands), 4)

    def test_newer_host_observation_cannot_qualify_candidate(self):
        self.observed_go = 'go1.27.4'
        with self.assertRaisesRegex(RuntimeError, 'exact repository Go toolchain'):
            self.invoke()
        self.assertFalse(Path('candidate').exists())

    def test_newer_compiled_binary_cannot_gain_manifest(self):
        self.built_go = 'go1.27.4'
        with self.assertRaisesRegex(RuntimeError, 'compiler does not match'):
            self.invoke()
        self.assertFalse(Path('candidate.manifest.json').exists())

    def test_inexact_project_version_is_refused_before_compiler(self):
        Path('go.mod').write_text('module fixture\n\ngo 1.26\n')
        with self.assertRaisesRegex(RuntimeError, 'Missing exact project Go'):
            self.invoke()
        self.assertEqual(self.commands, [])

    def test_native_builder_still_refuses_cross_compilation(self):
        self.goarch = 'arm64'
        with self.assertRaisesRegex(RuntimeError, 'cannot cross-compile'):
            self.invoke()
        self.assertFalse(Path('candidate').exists())


if __name__ == '__main__':
    unittest.main()
