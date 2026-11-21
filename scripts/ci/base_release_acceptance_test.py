"""Nonprivileged fail-closed tests; synthetic identities never authorize a run."""
import copy
import hashlib
import io
import json
import os
from pathlib import Path
import shlex
import subprocess
import sys
import tempfile
import tarfile
import time
import unittest
from unittest.mock import Mock, patch

import base_release_acceptance as acceptance


def subject_fixture():
    releases = {}
    for role, tag in acceptance.TAGS.items():
        releases[role] = {'tag': tag, 'source': acceptance.OLD_SOURCE if role == 'old_base' else 'ab' * 20,
            'release_id': 123, 'assets': {name: {'id': index + 1, 'size': 10,
                'sha256': hashlib.sha256(name.encode()).hexdigest()}
                for index, name in enumerate(sorted(acceptance.ASSETS))}}
    return {'schema_version': 1, 'releases': releases}


class SubjectTest(unittest.TestCase):
    def load(self, value):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'subject.json'
            path.write_text(json.dumps(value))
            return acceptance.load_subject(path)

    def test_exact_three_release_eight_asset_subject_only(self):
        value = subject_fixture()
        self.assertEqual(self.load(value), value)
        for change in ('tag', 'source', 'missing', 'extra', 'override', 'placeholder', 'size', 'id', 'duplicate-id', 'boolean'):
            value = subject_fixture()
            subject = value['releases']['base']
            if change == 'tag': subject['tag'] = 'v0.4.53-base-test.2'
            if change == 'source': value['releases']['old_base']['source'] = 'fe' * 20
            if change == 'missing': subject['assets'].pop('LICENSE')
            if change == 'extra': subject['assets']['extra'] = subject['assets']['LICENSE']
            if change == 'override': value['binary_url'] = 'https://example.invalid/unsigned'
            if change == 'placeholder': subject['assets']['install.sh']['sha256'] = '0' * 64
            if change == 'size': subject['assets']['install.sh']['size'] = 0
            if change == 'id': subject['assets']['install.sh']['id'] = -1
            if change == 'duplicate-id': subject['assets']['install.sh']['id'] = subject['assets']['LICENSE']['id']
            if change == 'boolean': subject['release_id'] = True
            with self.subTest(change=change), self.assertRaises(RuntimeError):
                self.load(value)

    def test_duplicate_json_fields_refused(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'subject.json'
            path.write_text('{"schema_version":1,"schema_version":1,"releases":{}}')
            with self.assertRaisesRegex(RuntimeError, 'Duplicate'):
                acceptance.load_subject(path)


class PublicMetadataTest(unittest.TestCase):
    def setUp(self):
        self.subject = subject_fixture()['releases']['base']
        self.release = {'id': 123, 'tag_name': self.subject['tag'], 'draft': False, 'immutable': True, 'prerelease': True,
            'assets': [dict(name=name, id=a['id'], size=a['size'], digest='sha256:' + a['sha256'], state='uploaded',
                browser_download_url='https://github.com/' + acceptance.REPO + '/releases/download/' + self.subject['tag'] + '/' + name)
                for name, a in self.subject['assets'].items()]}

    def test_eight_asset_identity_must_match_reviewed_subject(self):
        acceptance.verify_metadata(self.subject, self.release)
        for field, bad in (('id', 999), ('tag_name', 'v0.4.52'), ('immutable', False), ('draft', True), ('prerelease', False)):
            with self.subTest(field=field), self.assertRaises(RuntimeError):
                acceptance.verify_metadata(self.subject, dict(self.release, **{field: bad}))
        for field, bad in (('id', 999), ('digest', 'sha256:' + 'ab' * 32), ('size', 11),
                           ('state', 'new'), ('browser_download_url', 'https://example.invalid')):
            value = copy.deepcopy(self.release)
            value['assets'][0][field] = bad
            with self.subTest(field=field), self.assertRaises(RuntimeError):
                acceptance.verify_metadata(self.subject, value)
        value = copy.deepcopy(self.release)
        value['assets'][-1] = value['assets'][0]
        with self.assertRaises(RuntimeError): acceptance.verify_metadata(self.subject, value)


class BinaryProofTest(unittest.TestCase):
    def setUp(self):
        self.subject = subject_fixture()['releases']['base']
        self.pins = acceptance.official_pins(Path('.github/release-tool-versions.conf').read_bytes())
        self.settings = {'CGO_ENABLED': '0', 'GOOS': 'linux', 'GOARCH': 'amd64',
            'vcs.revision': self.subject['source'], 'vcs.modified': 'true'}
        self.assignments = dict(self.pins, **{'main.version': self.subject['tag'][1:]})
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.binary = Path(self.temp.name) / 'fluxo'
        header = bytearray(64)
        header[:6] = b'\x7fELF\x02\x01'
        header[18:20] = (62).to_bytes(2, 'little')
        self.binary.write_bytes(header)

    def verify(self):
        flags = shlex.join([value for k, v in self.assignments.items() for value in ('-X', k + '=' + v)])
        raw = 'fluxo: go1.26.3\n' + '\n'.join('\tbuild\t' + k + '=' + v for k, v in
            dict(self.settings, **{'-ldflags': '"-s -w ' + flags + '"'}).items())
        with patch.object(acceptance.base, 'run', return_value={'exit_code': 0, 'stdout': raw}):
            return acceptance.verify_binary(self.binary, 'amd64', self.subject, self.pins, '/go')

    def test_official_modified_true_is_recorded_and_all_19_assignments_checked(self):
        result = self.verify()
        self.assertEqual(result['official_pin_assignments_checked'], 19)
        self.assertEqual(result['vcs_modified'], 'true')
        self.assertNotIn('build_metadata', result)
        self.settings['vcs.modified'] = 'false'
        self.assertEqual(self.verify()['vcs_modified'], 'false')

    def test_wrong_source_cgo_arch_and_each_individual_pin_fail(self):
        for key, bad in (('CGO_ENABLED', '1'), ('GOOS', 'darwin'), ('GOARCH', 'arm64'),
                         ('vcs.revision', 'fe' * 20), ('vcs.modified', 'unknown')):
            with self.subTest(key=key), patch.dict(self.settings, {key: bad}), self.assertRaises(RuntimeError):
                self.verify()
        for key in self.pins:
            with self.subTest(key=key), patch.dict(self.assignments, {key: 'wrong'}), self.assertRaises(RuntimeError):
                self.verify()
        self.assignments['extra.assignment'] = 'unreviewed'
        with self.assertRaises(RuntimeError): self.verify()

    def test_real_elf_machine_required(self):
        self.binary.write_bytes(b'\x7fELF' + b'0' * 60)
        with self.assertRaisesRegex(RuntimeError, 'ELF'):
            self.verify()


class SignedReleaseTest(unittest.TestCase):
    def test_all_four_signed_subjects_bind_tag_source_and_official_hosted_workflow_before_metadata_execution(self):
        subject = subject_fixture()['releases']['base']
        pin_raw = Path('.github/release-tool-versions.conf').read_bytes()
        pins = acceptance.parse_pins(pin_raw)
        payloads = {name: name.encode() for name in acceptance.ASSETS}
        payloads['TOOLCHAIN-VERSIONS'] = ('\n'.join(label + ' ' + pins[key] for label, key in
            zip(acceptance.TOOLCHAIN_LABELS, (*acceptance.PIN_NAMES.values(), 'ATTESTATION_GH_VERSION'))) + '\n').encode()
        payloads['SHA256SUMS'] = ''.join(hashlib.sha256(payloads[name]).hexdigest() + '  ' + name + '\n'
            for name in sorted(acceptance.CHECKSUMMED)).encode()
        subject['assets'] = {name: {'id': i + 1, 'size': len(raw), 'sha256': hashlib.sha256(raw).hexdigest()}
            for i, (name, raw) in enumerate(payloads.items())}
        def download(_subject, name, target, expected):
            target.write_bytes(payloads[name])
            acceptance.require(acceptance.base.digest(target) == expected, 'Pinned bytes differ')
        sources = {'install.sh': payloads['install.sh'], 'ui-site/public/install.sh': payloads['install.sh'],
            'LICENSE': payloads['LICENSE'], '.github/release-tool-versions.conf': pin_raw, 'go.mod': b'module fluxo\ngo 1.26.3\n'}
        with tempfile.TemporaryDirectory() as directory, \
             patch.object(acceptance, 'git', return_value=(subject['source'] + '\n').encode()), \
             patch.object(acceptance, 'public_json', return_value={}), \
             patch.object(acceptance, 'verify_metadata') as metadata, \
             patch.object(acceptance, 'download_public_subject', side_effect=download), \
             patch.object(acceptance, 'source_file', side_effect=lambda source, path: sources[path]), \
             patch.object(acceptance, 'pinned_verifier', return_value=(Path('/pinned/gh'), {'verified': True})), \
             patch.object(acceptance, 'verify_binary', return_value={'checked': True}) as binary:
            command = Mock(return_value='verified')
            result = acceptance.verify_release('base', subject, Path(directory), command, '/go')
            metadata.assert_called_once_with(subject, {})
            self.assertEqual(binary.call_count, 2)
            self.assertEqual({call.args[1] for call in binary.call_args_list}, {'amd64', 'arm64'})
            self.assertEqual(result['signed_subjects_verified'], list(acceptance.SIGNED))
            self.assertEqual(command.call_count, 4)
            for call in command.call_args_list:
                argv = call.args[1]
                self.assertIn('--deny-self-hosted-runners', argv)
                self.assertEqual(argv[argv.index('--source-digest') + 1], subject['source'])
                self.assertEqual(argv[argv.index('--source-ref') + 1], 'refs/tags/' + subject['tag'])
                self.assertEqual(argv[argv.index('--signer-workflow') + 1], acceptance.REPO + '/.github/workflows/release.yml')
            binary.reset_mock()
            with self.assertRaisesRegex(RuntimeError, 'signature refused'):
                acceptance.verify_release('refused', subject, Path(directory), Mock(side_effect=RuntimeError('signature refused')), '/go')
            binary.assert_not_called()
            sources['install.sh'] = b'other source installer'
            with self.assertRaisesRegex(RuntimeError, 'exact source'):
                acceptance.verify_release('different-source', subject, Path(directory), command, '/go')
            binary.assert_not_called()


class VerifierBootstrapTest(unittest.TestCase):
    def test_exact_source_pinned_archive_precedes_any_verifier_execution(self):
        for change in ('none', 'digest', 'alias', 'duplicate'):
            with self.subTest(change=change), tempfile.TemporaryDirectory() as directory:
                data = io.BytesIO()
                with tarfile.open(fileobj=data, mode='w:gz') as bundle:
                    member = tarfile.TarInfo('gh_2.94.0_linux_amd64/bin/gh')
                    if change == 'alias':
                        member.type, member.linkname = tarfile.SYMTYPE, '/untrusted'
                        bundle.addfile(member)
                    else:
                        member.size = 4
                        bundle.addfile(member, io.BytesIO(b'tool'))
                        if change == 'duplicate': bundle.addfile(member, io.BytesIO(b'tool'))
                raw = data.getvalue()
                sha = hashlib.sha256(raw).hexdigest() if change != 'digest' else 'a' * 64
                source = ('ATTESTATION_GH_VERSION="2.94.0"\nATTESTATION_GH_AMD64_SHA256="' + sha + '"\n').encode()
                command = Mock(return_value='gh version 2.94.0 (2026-01-01)\n')
                with patch.object(acceptance, 'source_file', return_value=source), \
                     patch.object(acceptance.platform, 'machine', return_value='x86_64'), \
                     patch.object(acceptance.urllib.request, 'urlopen', return_value=io.BytesIO(raw)):
                    if change == 'none':
                        path, proof = acceptance.pinned_verifier(subject_fixture()['releases']['base'], Path(directory), command)
                        self.assertEqual(path.read_bytes(), b'tool')
                        self.assertEqual(proof['archive_sha256'], sha)
                        self.assertEqual(path.stat().st_mode & 0o777, 0o700)
                        command.assert_called_once()
                    else:
                        with self.assertRaises(RuntimeError):
                            acceptance.pinned_verifier(subject_fixture()['releases']['base'], Path(directory), command)
                        command.assert_not_called()


class RetentionCompatibilityTest(unittest.TestCase):
    def test_a_new_origin_request_cannot_mask_lost_old_aggregate(self):
        requests = [0]
        old = {'retention_days': 30, 'retained_requests': 1}
        observer, api = Mock(), Mock()
        observer.API.return_value = api
        api.request.side_effect = [{'retention_days': 30, 'master_enabled': True,
                                    'sites': [{'site_id': 1, 'enabled': True}]},
                                   {'totals': {'requests': requests[0]}}]
        def origin(*args):
            requests[0] += 1
            return {'request_ok': True}
        with patch.object(acceptance.base, 'application_checks', side_effect=origin) as application, \
             patch.object(acceptance.base, 'retention_check') as select_manual, self.assertRaises(RuntimeError):
            acceptance.release_application_checks(observer, {'site_id': 1}, 'base', old, Mock())
        self.assertEqual(requests, [0])
        application.assert_not_called()
        select_manual.assert_not_called()

    def test_old_trial_uses_only_its_supported_policy_then_new_base_preserves_default(self):
        seed = {'site_id': 1}
        old = {'retention_days': 30, 'retained_requests': 4}
        api = Mock()
        state = {'retention_days': 30, 'master_enabled': True, 'sites': [{'site_id': 1, 'enabled': True}]}
        api.request.side_effect = [state, {'totals': {'requests': 4}}]
        observer = Mock()
        observer.API.return_value = api
        with patch.object(acceptance.base, 'retention_check', return_value=old) as check:
            self.assertEqual(acceptance.release_retention(observer, seed, 'old_base', None), old)
            check.assert_called_once_with(observer, seed, manual=False)
            check.reset_mock()
            acceptance.release_retention(observer, seed, 'base', old)
            check.assert_called_once_with(observer, seed)
        for bad in (dict(state, retention_days=0), dict(state, master_enabled=False), dict(state, sites=[]), {'totals': {'requests': 3}}):
            api.request.side_effect = ([bad] if 'retention_days' in bad else [state, bad])
            with patch.object(acceptance.base, 'retention_check') as check, self.assertRaises(RuntimeError):
                acceptance.release_retention(observer, seed, 'base', old)
            check.assert_not_called()


class PublicInstallerTest(unittest.TestCase):
    def test_every_same_release_update_forces_full_public_path(self):
        command = acceptance.installer_command(Path('/private/base/install.sh'), reinstall=True)
        self.assertEqual(command.count('--reinstall'), 1)
        self.assertEqual(command[0], 'bash')
        self.assertFalse(any('local-binary' in flag or 'attestation' in flag for flag in command))
        self.assertNotIn('--reinstall', acceptance.installer_command(Path('/private/base/install.sh')))
        good = 'Fluxo release provenance verified against signed provenance.\nFluxo 0.4.53-base-test.1 installed successfully!'
        acceptance.require_public_install(good)
        for raw in ('Fluxo is already up to date', good + '\nSkipping release attestation',
                    good + '\nRelease attestation verification was explicitly disabled',
                    'Fluxo release provenance verified', 'Fluxo 0.4.53 installed successfully!'):
            with self.subTest(raw=raw), self.assertRaises(RuntimeError):
                acceptance.require_public_install(raw)

    def test_private_stream_observer_exports_only_fixed_phase_labels(self):
        with tempfile.TemporaryDirectory() as directory:
            log = Path(directory) / 'private.log'
            boundaries = []
            code = acceptance.logged_process([sys.executable, '-c',
                'print("Downloading and validating Fluxo secret-output"); print("sensitive private line"); '
                'print("Fluxo 0.4.53-base-test.1 installed successfully!")'], dict(os.environ), None,
                log, 5, time.monotonic(), boundaries, acceptance.installer_boundaries('0.4.53-base-test.1'))
            self.assertEqual(code, 0)
            self.assertEqual([x['boundary'] for x in boundaries], ['public_download', 'success'])
            self.assertNotIn('secret', json.dumps(boundaries))
            self.assertEqual(log.stat().st_mode & 0o777, 0o600)

    def test_shared_node_stream_default_rules_remain_unchanged(self):
        with tempfile.TemporaryDirectory() as directory:
            boundaries = []
            code = acceptance.logged_process([sys.executable, '-c',
                'print("Configuring systemd service..."); '
                'print("Fluxo 0.0.0-base.dev installed successfully!"); '
                'print("Fluxo 0.4.53-base-test.1 installed successfully!")'], dict(os.environ), None,
                Path(directory) / 'private.log', 5, time.monotonic(), boundaries)
            self.assertEqual(code, 0)
            self.assertEqual([x['boundary'] for x in boundaries], ['systemd', 'success'])


class NativeGuardTest(unittest.TestCase):
    def test_original_database_projection_preserves_complete_history_snapshot(self):
        retained = {'/protected/archive': 'ab' * 32, '/protected/history/receipt.json': 'cd' * 32}
        captured = {'retained': retained, 'root_history': {'unit': 'ef' * 32}}
        projected = {'columns': {'users': ['id'], 'sites': ['id'], 'crons': ['id']},
                     'users': 'users-hash', 'sites': 'sites-hash', 'crons': 'crons-hash',
                     'retained': {'/protected/archive': 'ab' * 32}}
        database = Mock()
        database.execute.side_effect = [
            [(0, 'id'), (1, 'created_at')], Mock(fetchall=Mock(return_value=[(1, 'old-date')]))]
        connection = Mock()
        connection.__enter__ = Mock(return_value=database)
        connection.__exit__ = Mock(return_value=False)
        with patch.object(acceptance.base, 'capture_invariants', return_value=captured), \
             patch.object(acceptance.base, 'installation_invariants', return_value=projected), \
             patch.object(acceptance.sqlite3, 'connect', return_value=connection), \
             patch.object(acceptance.pwd, 'getpwnam', return_value=Mock(pw_uid=123, pw_gid=123)), \
             patch.object(acceptance.base, 'digest', return_value='ff' * 32), \
             patch.object(acceptance.Path, 'rglob', return_value=[]):
            after = acceptance.preservation_snapshot({}, {'columns': projected['columns'],
                                                        'deployment_columns': ['id', 'created_at']})
        self.assertEqual(after['retained'], retained)
        self.assertEqual(after['root_history'], {'unit': 'ef' * 32})
        for key in ('columns', 'users', 'sites', 'crons'):
            self.assertEqual(after[key], projected[key])

    def test_preservation_diagnostics_keep_original_refusal(self):
        before = {'retained': {'/protected/archive': 'ab' * 32}, 'root_history': {'unit': 'cd' * 32}}
        report = {}
        self.assertTrue(acceptance.compare_preservation(before, copy.deepcopy(before), report, 'predecessor'))
        self.assertEqual(report, {})
        after = copy.deepcopy(before)
        after['root_history']['unit'] = 'ef' * 32
        with self.assertRaisesRegex(RuntimeError, 'Retained execution'):
            acceptance.compare_preservation(before, after, report, 'predecessor')
        self.assertEqual(report['predecessor_retained_mismatches'], [{'category': 'root_history',
            'path': 'unit', 'before_sha256': 'cd' * 32, 'after_sha256': 'ef' * 32}])
        self.assertEqual(report['predecessor_retained_mismatch_count'], 1)

    def test_public_metadata_only_retries_one_transient_http_error(self):
        def error(code):
            return acceptance.urllib.error.HTTPError('https://api.github.com/', code, 'failure', {}, None)
        with patch.object(acceptance.urllib.request, 'urlopen', side_effect=[error(503), io.BytesIO(b'{"id":1}')]) as fetch, \
             patch.object(acceptance.time, 'sleep') as sleep:
            self.assertEqual(acceptance.public_json('releases/latest'), {'id': 1})
            self.assertEqual(fetch.call_count, 2)
            sleep.assert_called_once_with(2)
        for code in (403, 404, 503):
            with self.subTest(code=code), \
                 patch.object(acceptance.urllib.request, 'urlopen', side_effect=error(code)) as fetch, \
                 patch.object(acceptance.time, 'sleep'), self.assertRaises(acceptance.urllib.error.HTTPError):
                acceptance.public_json('releases/latest')
            self.assertEqual(fetch.call_count, 2 if code == 503 else 1)

    def test_post_prepare_guard_uses_actual_hostname_despite_cached_uname(self):
        for arch in ('amd64', 'arm64'):
            expected = acceptance.base.HOSTS[arch, '24.04', 'cron']
            with self.subTest(arch=arch), \
                 patch.object(acceptance.platform, 'node', return_value='original-hosted-runner'), \
                 patch.object(acceptance.socket, 'gethostname', return_value=expected):
                acceptance.require_named_fixture(arch)
            with patch.object(acceptance.platform, 'node', return_value=expected), \
                 patch.object(acceptance.socket, 'gethostname', return_value='unexpected-host'), \
                 self.assertRaisesRegex(RuntimeError, 'Original named native fixture'):
                acceptance.require_named_fixture(arch)

    def test_only_four_native_noble_cases_and_original_named_hosts(self):
        self.assertEqual(acceptance.CASES, {'amd64-fresh': ('amd64', None),
            'amd64-bridge': ('amd64', 'bridge'), 'arm64-bridge': ('arm64', 'bridge'),
            'amd64-old-base': ('amd64', 'old_base')})
        env = {'FLUXO_BASE_RELEASE_ACCEPTANCE': '1', 'GITHUB_ACTIONS': 'true',
            'RUNNER_ENVIRONMENT': 'github-hosted', 'GITHUB_WORKFLOW': acceptance.WORKFLOW,
            'GITHUB_REF': acceptance.BRANCH, 'GITHUB_REPOSITORY': acceptance.REPO,
            'RUNNER_ARCH': 'X64', 'ImageOS': 'ubuntu24', 'ImageVersion': '20261120.1'}
        with patch.dict(os.environ, env, clear=True), patch.object(acceptance.os, 'geteuid', return_value=0), \
             patch.object(acceptance.platform, 'machine', return_value='x86_64'), \
             patch.object(acceptance.Path, 'read_text', return_value='ID=ubuntu\nVERSION_ID="24.04"\n'), \
             patch.object(acceptance.base, 'run', return_value={'stdout': 'amd64'}):
            self.assertEqual(acceptance.hosted_profile('amd64-fresh'), 'amd64')
            for key, bad in (('GITHUB_REF', 'refs/heads/main'), ('RUNNER_ARCH', 'ARM64'),
                             ('RUNNER_ENVIRONMENT', 'self-hosted'), ('FLUXO_BASE_RELEASE_ACCEPTANCE', '0'),
                             ('GITHUB_WORKFLOW', 'Other'), ('GITHUB_REPOSITORY', 'other/repo'), ('ImageOS', '')):
                with self.subTest(key=key), patch.dict(os.environ, {key: bad}), self.assertRaises(RuntimeError):
                    acceptance.hosted_profile('amd64-fresh')
            with self.assertRaises(RuntimeError): acceptance.hosted_profile('arm64-bridge')
            with patch.object(acceptance.os, 'geteuid', return_value=1000), self.assertRaises(RuntimeError):
                acceptance.hosted_profile('amd64-fresh')


if __name__ == '__main__':
    unittest.main()
