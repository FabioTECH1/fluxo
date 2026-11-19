"""Nonprivileged regressions for native base acceptance safety and oracles."""
import copy
import json
import os
import stat
from types import SimpleNamespace
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch

import base_candidate as candidate


class DiagnosticsTest(unittest.TestCase):
    def test_failure_diagnostics_preserve_cause_but_never_secrets(self):
        raw = ('ERROR: unsupported vendor profile\npassword: labeled-secret\n'
               'opaque-exact-secret\neyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.signature\n'
               + 'a' * 32 + '\n-----BEGIN OPENSSH PRIVATE KEY-----\nprivate-material\n'
               '-----END OPENSSH PRIVATE KEY-----\n--- FAIL: TestNative (0.01s)')
        with patch.object(candidate.Path, 'is_file', return_value=False):
            clean = candidate.sanitized_diagnostic(raw, ['opaque-exact-secret'])
        self.assertIn('unsupported vendor profile', clean)
        self.assertIn('--- FAIL: TestNative', clean)
        for secret in ('labeled-secret', 'opaque-exact-secret', 'eyJhbGci', 'a' * 32, 'private-material'):
            self.assertNotIn(secret, clean)


class NativeGuardTest(unittest.TestCase):
    def test_hosted_profile_requires_real_native_arch_and_investigation_branch(self):
        environment = {'FLUXO_BASE_CANDIDATE': '1', 'GITHUB_ACTIONS': 'true',
                       'RUNNER_ENVIRONMENT': 'github-hosted', 'RUNNER_ARCH': 'ARM64',
                       'GITHUB_REF': 'refs/heads/test/base-native-acceptance'}
        with patch.dict(os.environ, environment, clear=True), \
             patch.object(candidate.os, 'geteuid', return_value=0), \
             patch.object(candidate.platform, 'machine', return_value='aarch64'), \
             patch.object(candidate.Path, 'read_text', return_value='ID=ubuntu\nVERSION_ID="24.04"\n'), \
             patch.object(candidate, 'run', return_value={'stdout': 'arm64'}):
            self.assertEqual(candidate.hosted_profile(), ('arm64', '24.04'))
            for changes in ({'GITHUB_REF': 'refs/heads/main'}, {'GITHUB_REF': 'refs/tags/v0.5.0'},
                            {'RUNNER_ARCH': 'X64'}, {'RUNNER_ENVIRONMENT': 'self-hosted'},
                            {'FLUXO_BASE_CANDIDATE': '0'}):
                with self.subTest(changes=changes), patch.dict(os.environ, changes):
                    with self.assertRaises(RuntimeError):
                        candidate.hosted_profile()
            with patch.object(candidate, 'run', return_value={'stdout': 'amd64'}):
                with self.assertRaisesRegex(RuntimeError, 'dpkg'):
                    candidate.hosted_profile()
            with patch.object(candidate.os, 'geteuid', return_value=1000):
                with self.assertRaises(RuntimeError):
                    candidate.hosted_profile()

    def test_only_requested_native_profiles_exist(self):
        self.assertEqual(len(candidate.HOSTS), 5)
        self.assertIn(('amd64', '24.04', 'cron'), candidate.HOSTS)
        for release in ('22.04', '24.04', '26.04'):
            self.assertIn(('arm64', release, 'cron'), candidate.HOSTS)
        self.assertIn(('arm64', '24.04', 'cronie'), candidate.HOSTS)


class LoaderFixtureTest(unittest.TestCase):
    raw = b'# Legacy biarch compatibility support\n/lib32\n/usr/lib32\n'

    def fixture(self, directory):
        root = Path(directory)
        source = root / 'loader'
        private = root / 'private'
        source.mkdir()
        private.mkdir()
        path = source / 'zz_i386-biarch-compat.conf'
        path.write_bytes(self.raw)
        info = SimpleNamespace(st_mode=stat.S_IFREG | 0o644, st_uid=0, st_nlink=1, st_size=len(self.raw))
        return source, private, path, info

    def test_only_known_ci_addon_is_archived_before_cache_refresh(self):
        with tempfile.TemporaryDirectory() as directory:
            source, private, path, info = self.fixture(directory)
            report, command = {}, Mock()
            with patch.object(candidate.Path, 'lstat', return_value=info):
                candidate.isolate_ci_biarch_loader(private, report, command, source)
            self.assertFalse(path.exists())
            self.assertEqual((private / (path.name + '.original')).read_bytes(), self.raw)
            self.assertEqual(report['biarch_loader_fixture']['archived_search_paths'], ['/lib32', '/usr/lib32'])
            self.assertFalse(report['biarch_loader_fixture']['production_validation_changed'])
            command.assert_called_once_with('stock-loader-cache', ['/sbin/ldconfig'], 30)

    def test_unknown_or_unsafe_inputs_never_move_files_or_refresh_cache(self):
        for change in ('path', 'symlink', 'owner', 'writable', 'hardlink', 'oversized', 'existing-archive'):
            with self.subTest(change=change), tempfile.TemporaryDirectory() as directory:
                source, private, path, info = self.fixture(directory)
                if change == 'path': path.write_bytes(b'/lib32\n/usr/lib32\n/tmp/unreviewed\n')
                if change == 'symlink': info.st_mode = stat.S_IFLNK | 0o777
                if change == 'owner': info.st_uid = 1000
                if change == 'writable': info.st_mode |= 0o002
                if change == 'hardlink': info.st_nlink = 2
                if change == 'oversized': info.st_size = 257
                if change == 'existing-archive': (private / (path.name + '.original')).write_text('preserve')
                command = Mock()
                with patch.object(candidate.Path, 'lstat', return_value=info), self.assertRaises(RuntimeError):
                    candidate.isolate_ci_biarch_loader(private, {}, command, source)
                self.assertTrue(path.exists())
                command.assert_not_called()


class BridgeIdentityTest(unittest.TestCase):
    def test_pins_authentic_bridge_without_source_equality_to_base(self):
        subject = candidate.public_bridge()
        self.assertEqual(subject['public_tag'], 'v0.4.51')
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'subject.json'
            path.write_text(json.dumps(subject))
            with patch.object(candidate.subprocess, 'run', side_effect=AssertionError('no source diff')):
                self.assertEqual(candidate.public_bridge(path), subject)
            for key, bad in (('public_tag', 'v0.4.50'), ('source', 'x' * 40),
                             ('amd64_sha256', '0' * 63), ('arm64_sha256', ''),
                             ('installer_sha256', 'A' * 64)):
                broken = dict(subject, **{key: bad})
                path.write_text(json.dumps(broken))
                with self.subTest(key=key), self.assertRaises(RuntimeError):
                    candidate.public_bridge(path)


class BuildIdentityTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.build = Path(self.temp.name)
        self.identity = {'source': 'a' * 40, 'source_tree': 'b' * 40,
                         'installer_sha256': 'c' * 64, 'tool_pins_sha256': 'd' * 64,
                         'tracked_source_clean': True}
        self.manifest = dict(self.identity, binary_sha256='e' * 64, goarch='arm64',
            goos='linux', cgo_enabled=False, version=candidate.VERSION, go_version='go version go1.26.3 linux/arm64',
            official_pin_assignments={'pin%d' % i: str(i) for i in range(19)})
        self.manifest['build_metadata'] = '\n'.join(key + '=' + value for key, value in self.manifest['official_pin_assignments'].items())
        for patcher in (patch.dict(os.environ, {'GITHUB_SHA': self.identity['source']}),
                        patch.object(candidate, 'source_identity', return_value=self.identity),
                        patch.object(candidate, 'digest', return_value='e' * 64),
                        patch.object(candidate, 'run', return_value={'exit_code': 0, 'stdout': 'fluxo version ' + candidate.VERSION})):
            patcher.start()
            self.addCleanup(patcher.stop)

    def verify(self, value):
        (self.build / 'fluxo.manifest.json').write_text(json.dumps(value))
        return candidate.verify_build(self.build, 'arm64')

    def test_exact_manifest_is_allowlisted(self):
        self.manifest['private_environment'] = 'must never be exported'
        result = self.verify(self.manifest)
        self.assertEqual(result['source_tree'], self.identity['source_tree'])
        self.assertNotIn('private_environment', result)
        self.assertNotIn('build_metadata', result)

    def test_wrong_source_installer_binary_pins_arch_or_build_is_refused(self):
        for key, value in (('source', 'f' * 40), ('source_tree', 'f' * 40),
                           ('installer_sha256', 'f' * 64), ('tool_pins_sha256', 'f' * 64),
                           ('tracked_source_clean', False), ('binary_sha256', 'f' * 64),
                           ('goarch', 'amd64'), ('goos', 'darwin'), ('cgo_enabled', True),
                           ('version', '0.4.51'), ('official_pin_assignments', {}),
                           ('build_metadata', 'no pinned assignments')):
            with self.subTest(key=key), self.assertRaises(RuntimeError):
                self.verify(dict(self.manifest, **{key: value}))
        with patch.object(candidate, 'run', return_value={'exit_code': 0, 'stdout': 'fluxo version 0.4.51'}):
            with self.assertRaisesRegex(RuntimeError, 'dev version'):
                self.verify(self.manifest)


class PreservationTest(unittest.TestCase):
    def setUp(self):
        self.before = {key: key + '-original' for key in ('users', 'sites', 'crons', 'commands',
            'ssh_keys', 'system_metadata', 'encryption.key', 'tls/key.pem', 'execution-readers.json')}
        self.before['retained'] = {'activation/plan.json': 'plan-sha', 'helpers/image': 'image-sha',
                                  'history/result.json': 'history-sha'}
        self.before['root_history'] = {'fixture.service': 'completed-sha'}

    def test_original_rows_files_and_archives_must_all_survive(self):
        after = copy.deepcopy(self.before)
        after['retained']['history/new-result.json'] = 'new-sha'
        self.assertTrue(candidate.compare_invariants(self.before, after))
        for key in self.before:
            with self.subTest(key=key):
                broken = copy.deepcopy(after)
                if key == 'root_history':
                    broken[key].pop('fixture.service')
                elif key == 'retained':
                    broken[key].pop('history/result.json')
                else:
                    broken[key] = 'changed'
                with self.assertRaises(RuntimeError):
                    candidate.compare_invariants(self.before, broken)
        broken = copy.deepcopy(after)
        broken['retained']['activation/plan.json'] = 'rewritten'
        with self.assertRaisesRegex(RuntimeError, 'archive/helper'):
            candidate.compare_invariants(self.before, broken)


class NativeExecutionTest(unittest.TestCase):
    def test_native_test_cannot_pass_by_skipping_or_matching_no_tests(self):
        for raw in ('PASS\n', '--- SKIP: TestNative (0.00s)\nPASS\n',
                    '--- PASS: TestNative (0.01s)\n--- SKIP: TestNative/subtest (0.00s)'):
            with self.subTest(raw=raw), self.assertRaises(RuntimeError):
                candidate.native_test(Mock(return_value=raw), 'native', Path('/private/test'), 'TestNative')
        command = Mock(return_value='--- PASS: TestNative (0.01s)\nPASS\n')
        candidate.native_test(command, 'native', Path('/private/test'), 'TestNative')
        self.assertIn('-test.timeout=4m', command.call_args.args[1])

    def test_root_probe_retains_yearly_job_and_requires_genuine_terminal_evidence(self):
        image = 'a' * 64
        entry = {'received': True, 'enabled': True, 'contract': {'kind': 'certificate-renewal', 'id': 7},
                 'helper': '/var/lib/fluxo-system-tasks/helpers/' + image}
        state = {'entries': {'7': entry}, 'invocations': {}}
        api = Mock()
        def request(method, path):
            self.assertEqual((method, path), ('POST', '/crons/7/run'))
            unit = 'test%d.service' % len(state['invocations'])
            state['invocations'][unit] = {'unit': unit, 'status': 'completed',
                'registration': entry, 'result_profile': {'qualified': True}}
        api.request.side_effect = request
        observer = Mock()
        observer.API.return_value = api
        observer.protected_read.side_effect = lambda _: json.dumps(state).encode()
        def wait_for(test, timeout, description):
            result = test()
            if not result:
                raise RuntimeError(description)
            return result
        observer.wait_for.side_effect = wait_for
        observer.command.return_value.returncode = 1
        with patch.object(candidate.Path, 'glob', return_value=[]), patch.object(candidate, 'digest', return_value=image):
            result = candidate.root_execution(observer, {'root_cron_id': 7})
            self.assertTrue(result['yearly_cron_retained'])
            self.assertTrue(result['completed_result_profile'])
            self.assertEqual(api.request.call_count, 1)  # No DELETE or receipt-writing request.
            observer.command.return_value.returncode = 0
            with self.assertRaisesRegex(RuntimeError, 'replayed'):
                candidate.root_execution(observer, {'root_cron_id': 7})
            entry['received'] = False
            with self.assertRaisesRegex(RuntimeError, 'acknowledgement'):
                candidate.root_execution(observer, {'root_cron_id': 7})


if __name__ == '__main__':
    unittest.main()
