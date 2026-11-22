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
import textwrap
import time
import unittest
from unittest.mock import Mock, patch

import base_release_acceptance as acceptance
import base_public_preserve_busy as busy_preserve


def subject_fixture():
    releases = {}
    for role, tag in acceptance.TAGS.items():
        releases[role] = {'tag': tag, 'source': acceptance.PINNED_SOURCES.get(role, 'ab' * 20),
            'release_id': 123, 'assets': {name: {'id': index + 1, 'size': 10,
                'sha256': hashlib.sha256(name.encode()).hexdigest()}
                for index, name in enumerate(sorted(acceptance.ASSETS))}}
    for role, images in acceptance.PINNED_IMAGES.items():
        for arch, digest in images.items():
            releases[role]['assets']['fluxo-linux-' + arch]['sha256'] = digest
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
            if change == 'source': value['releases']['bootstrap']['source'] = 'fe' * 20
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

    def test_both_predecessors_bind_exact_source_and_both_public_images(self):
        for role in ('bridge', 'bootstrap'):
            for arch in ('amd64', 'arm64'):
                value = subject_fixture()
                value['releases'][role]['assets']['fluxo-linux-' + arch]['sha256'] = 'ef' * 32
                with self.subTest(role=role, arch=arch), self.assertRaisesRegex(RuntimeError, 'exact predecessor image'):
                    self.load(value)
            value = subject_fixture()
            value['releases'][role]['source'] = 'fe' * 20
            with self.subTest(role=role), self.assertRaisesRegex(RuntimeError, 'exact predecessor source'):
                self.load(value)


class PublicMetadataTest(unittest.TestCase):
    def test_metadata_token_is_consumed_before_first_child_and_excluded_from_custom_environment(self):
        token = 'ghs_test_read_only_in_memory_transport'
        class StopBeforeNativeWork(Exception):
            pass
        def guard(case):
            self.assertEqual(case, 'amd64-fresh')
            self.assertNotIn('FLUXO_METADATA_TOKEN', os.environ)
            self.assertEqual(acceptance._METADATA_TOKEN, token)
            inherited = subprocess.check_output([sys.executable, '-c',
                'import os; print("FLUXO_METADATA_TOKEN" in os.environ)'], text=True)
            self.assertEqual(inherited.strip(), 'False')
            custom = acceptance.command_environment(Path('/root/private-fixture'))
            self.assertNotIn('GH_TOKEN', custom)
            self.assertNotIn('FLUXO_METADATA_TOKEN', custom)
            self.assertNotIn(token, repr(custom))
            raise StopBeforeNativeWork()
        with patch.dict(os.environ, {'FLUXO_METADATA_TOKEN': token}, clear=True), \
             patch.object(acceptance, '_METADATA_TOKEN', None), \
             patch.object(sys, 'argv', ['fixture', '--subject', '/unused/subject', '--output', '/unused/output',
                '--case', 'amd64-fresh', '--go', '/unused/go']), \
             patch.object(acceptance, 'hosted_profile', side_effect=guard), \
             self.assertRaises(StopBeforeNativeWork):
            acceptance.main()

    def test_metadata_redirects_are_refused_without_forwarding_authorization(self):
        token = 'ghs_dummy_redirect_secret'
        request = acceptance.urllib.request.Request('https://api.github.com/repos/' + acceptance.REPO + '/releases/latest',
            headers={'Authorization': 'Bearer ' + token})
        opener = acceptance.urllib.request.build_opener(acceptance.RejectMetadataRedirects())
        handler = next(item for item in opener.handlers if isinstance(item, acceptance.RejectMetadataRedirects))
        for destination in ('https://example.invalid/steal', request.full_url):
            response = Mock()
            with self.subTest(destination=destination), \
                 patch.object(opener, 'open') as forward, self.assertRaises(acceptance.urllib.error.HTTPError) as refusal:
                handler.redirect_request(request, response, 302, 'Found', {'Location': destination}, destination)
            forward.assert_not_called()
            response.close.assert_called_once()
            self.assertNotIn(token, str(refusal.exception))

    def test_read_only_token_authenticates_only_fixed_api_request_without_output_or_url_exposure(self):
        token = 'ghs_test_read_only_secret_not_a_real_token'
        for configured in (None, token):
            seen = []
            def opened(request, timeout):
                seen.append(request)
                self.assertEqual(timeout, 60)
                return io.BytesIO(b'{"tag_name":"v0.4.54-base-test.1"}')
            environment = dict(os.environ)
            environment.pop('FLUXO_METADATA_TOKEN', None)
            if configured:
                environment['FLUXO_METADATA_TOKEN'] = configured
            with self.subTest(authenticated=bool(configured)), patch.dict(os.environ, environment, clear=True), \
                 patch.object(acceptance.urllib.request.OpenerDirector, 'open', side_effect=opened), \
                 patch.object(sys, 'stdout', io.StringIO()) as output, patch.object(sys, 'stderr', io.StringIO()) as errors:
                with patch.object(acceptance, '_METADATA_TOKEN', acceptance.take_metadata_token()):
                    result = acceptance.public_json('releases/latest')
            self.assertEqual(seen[0].full_url, 'https://api.github.com/repos/' + acceptance.REPO + '/releases/latest')
            self.assertEqual(seen[0].get_header('Authorization'), 'Bearer ' + token if configured else None)
            self.assertNotIn(token, seen[0].full_url + json.dumps(result) + output.getvalue() + errors.getvalue())
        with patch.object(acceptance, '_METADATA_TOKEN', token), \
             patch.object(acceptance.urllib.request.OpenerDirector, 'open') as opened:
            for path in ('https://example.invalid', 'releases/latest?token=secret', '../other', 'releases/tags/unknown'):
                with self.subTest(path=path), self.assertRaises(RuntimeError): acceptance.public_json(path)
            opened.assert_not_called()
        with patch.dict(os.environ, {'FLUXO_METADATA_TOKEN': 'bad\nheader'}), \
             patch.object(acceptance.urllib.request.OpenerDirector, 'open') as opened, self.assertRaises(RuntimeError):
            acceptance.take_metadata_token()
        opened.assert_not_called()

    def test_api_failure_diagnostic_contains_only_bounded_rate_headers(self):
        error = acceptance.urllib.error.HTTPError('https://api.github.com/repos/' + acceptance.REPO + '/releases/latest',
            403, 'private response text not exported', {'X-RateLimit-Remaining': '0', 'X-RateLimit-Resource': 'core',
                'X-RateLimit-Reset': '1795350000', 'Authorization': 'Bearer private-secret'}, None)
        self.assertEqual(acceptance.public_api_diagnostic(error), {'http_status': 403, 'rate_limit_remaining': '0',
            'rate_limit_resource': 'core', 'rate_limit_reset': '1795350000'})
        error.headers = {'X-RateLimit-Remaining': 'private-secret', 'X-RateLimit-Reset': '1' * 21,
                         'X-RateLimit-Resource': 'private-secret'}
        self.assertEqual(acceptance.public_api_diagnostic(error), {'http_status': 403})
        error.url = 'https://example.invalid/'
        self.assertIsNone(acceptance.public_api_diagnostic(error))

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
        self.assignments = dict(self.pins, **{'main.version': self.subject['tag'][1:],
            acceptance.PREVIEW_ASSIGNMENT: acceptance.TAGS['base']})
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

    def test_preview_build_assignment_required_exactly_for_base(self):
        value = self.assignments.pop(acceptance.PREVIEW_ASSIGNMENT)
        with self.assertRaises(RuntimeError): self.verify()
        self.assignments[acceptance.PREVIEW_ASSIGNMENT] = 'v0.4.54-bridge-test.7'
        with self.assertRaises(RuntimeError): self.verify()
        self.assignments[acceptance.PREVIEW_ASSIGNMENT] = value
        self.subject = subject_fixture()['releases']['bridge']
        self.settings['vcs.revision'] = self.subject['source']
        self.assignments['main.version'] = self.subject['tag'][1:]
        with self.assertRaises(RuntimeError): self.verify()
        self.assignments.pop(acceptance.PREVIEW_ASSIGNMENT)
        self.assertEqual(self.verify()['official_pin_assignments_checked'], 19)


class PublicRelayTest(unittest.TestCase):
    def fixture(self, directory):
        source, output = directory / 'private', directory / 'output'
        source.mkdir(mode=0o700)
        output.mkdir()
        subject = subject_fixture()['releases']['base']
        for name in acceptance.ASSETS:
            raw = ('authentic-public-' + name).encode()
            (source / name).write_bytes(raw)
            subject['assets'][name].update(size=len(raw), sha256=hashlib.sha256(raw).hexdigest())
        proof = {key: subject[key] for key in ('tag', 'source', 'release_id', 'assets')}
        proof.update(immutable=True, source_identical_installer=True, signed_subjects_verified=list(acceptance.SIGNED),
            pinned_verifier={'version': '2.94.0', 'archive_sha256': 'ab' * 32, 'executable_sha256': 'cd' * 32},
            binaries={arch: {'sha256': subject['assets']['fluxo-linux-' + arch]['sha256'],
                'goarch': arch, 'cgo_enabled': False, 'source': subject['source'], 'vcs_modified': 'true',
                'official_pin_assignments_checked': 19} for arch in ('amd64', 'arm64')})
        (source / 'credentials.log').write_text('private-secret-never-export')
        return source, output, subject, proof

    def test_only_eight_public_assets_and_safe_complete_verification_are_atomically_relayed(self):
        with tempfile.TemporaryDirectory() as directory:
            source, output, subject, proof = self.fixture(Path(directory))
            result = acceptance.export_public_assets(source, output, subject, proof, 'ef' * 20)
            relay = output / 'public-assets'
            self.assertEqual({p.name for p in relay.iterdir()}, acceptance.ASSETS | {'verified-public-release.json'})
            self.assertTrue(result['verified_before_native_execution'])
            for name in acceptance.ASSETS:
                self.assertEqual((relay / name).read_bytes(), (source / name).read_bytes())
                self.assertEqual((relay / name).stat().st_mode & 0o777, 0o644)
            report = json.loads((relay / 'verified-public-release.json').read_text())
            self.assertEqual(report['release'], proof)
            self.assertEqual(report['fixture_source'], 'ef' * 20)
            self.assertNotIn('private-secret', json.dumps(report))

    def test_partial_provenance_changed_bytes_symlinks_or_extra_private_proof_never_export(self):
        for defect in ('signature', 'binary-proof', 'private-proof', 'tamper', 'symlink'):
            with self.subTest(defect=defect), tempfile.TemporaryDirectory() as directory:
                source, output, subject, proof = self.fixture(Path(directory))
                if defect == 'signature': proof['signed_subjects_verified'].pop()
                if defect == 'binary-proof': proof['binaries']['arm64']['source'] = 'fe' * 20
                if defect == 'private-proof': proof['pinned_verifier']['raw_private_log'] = 'must not export'
                if defect == 'tamper': (source / 'LICENSE').write_text('different')
                if defect == 'symlink':
                    (source / 'LICENSE').unlink()
                    (source / 'LICENSE').symlink_to(source / 'credentials.log')
                with self.assertRaises(RuntimeError):
                    acceptance.export_public_assets(source, output, subject, proof, 'ef' * 20)
                self.assertFalse((output / 'public-assets').exists())
                self.assertFalse((output / '.public-assets.pending').exists())


class PreviewAndNodeTest(unittest.TestCase):
    def test_busy_preserve_requires_owned_real_lease_state(self):
        state = {'upgrade': {'id': 'owned', 'pid': 123}, 'holds': {'actual-entry': 'owned'}}
        busy_preserve.verify_lease_state(state, 'owned', 123)
        for changed in (dict(state, upgrade={'id': 'other', 'pid': 123}),
                        dict(state, upgrade={'id': 'owned', 'pid': 456}),
                        dict(state, holds={}), dict(state, holds={'actual-entry': 'foreign'})):
            with self.assertRaises(RuntimeError): busy_preserve.verify_lease_state(changed, 'owned', 123)

    def test_busy_preserve_requires_wait_before_reuse_and_actual_child_completion_inside_call(self):
        waited = ('Waiting for earlier cron workers to finish before continuing the upgrade...\n'
                  'Earlier cron workers finished; continuing the upgrade.\n' + acceptance.node.REUSED)
        busy_preserve.verify_natural_preserve_completion(waited, 10, 20, 30)
        for begun, complete, end in ((20, 10, 30), (20, 20, 30), (10, 31, 30)):
            with self.subTest(complete=complete), self.assertRaises(RuntimeError):
                busy_preserve.verify_natural_preserve_completion(waited, begun, complete, end)
        for raw in (acceptance.node.REUSED, acceptance.node.REUSED + '\n' + waited,
                    waited.replace('Earlier cron workers finished; continuing the upgrade.', ''),
                    waited + '\nInstalling Corepack'):
            with self.subTest(raw=raw), self.assertRaises(RuntimeError):
                busy_preserve.verify_natural_preserve_completion(raw, 10, 20, 30)

    def test_preview_contract_does_not_replace_stable_gate_or_accept_an_unknown_image(self):
        preview = {'base_tag': acceptance.TAGS['base'], 'bridge_tag': acceptance.TAGS['bridge'],
            'bridge_source': acceptance.PINNED_SOURCES['bridge'],
            **{arch + '_sha256': sha for arch, sha in acceptance.PINNED_IMAGES['bridge'].items()}}
        contract = {'bridge_release': 'v0.4.53', 'preview_bridge_admission': preview}
        acceptance.preview_contract(contract)
        for field in preview:
            changed = copy.deepcopy(contract)
            changed['preview_bridge_admission'][field] = 'unknown'
            with self.subTest(field=field), self.assertRaises(RuntimeError):
                acceptance.preview_contract(changed)
        for changed in ({'bridge_release': acceptance.TAGS['bridge'], 'preview_bridge_admission': preview},
                        {'bridge_release': 'v0.4.53'},
                        dict(contract, preview_bridge_admission=dict(preview, repair_required=False))):
            with self.assertRaises(RuntimeError): acceptance.preview_contract(changed)

    def test_actual_selected_update_requires_reuse_message_no_repair_and_identical_payload(self):
        proof = {'sha256': 'ab' * 32}
        before = {'selection': {'node': '24.16.0'}, 'payload': {'trees': 'independent-tree-proof'},
                  'versions': {'version': 'v24.16.0'}, 'receipt': proof}
        after = copy.deepcopy(before)
        result = acceptance.verified_node_preservation(acceptance.node.REUSED, before, after, False)
        self.assertTrue(result['actual_preserve_path'])
        self.assertFalse(result['registry_install_prepare_invoked'])
        for key in before:
            changed = copy.deepcopy(after)
            changed[key] = None
            with self.subTest(key=key), self.assertRaises(RuntimeError):
                acceptance.verified_node_preservation(acceptance.node.REUSED, before, changed, False)
        for suffix in ('Installing Corepack', 'Preparing pnpm', 'Preparing Yarn', 'registry.npmjs.org',
                       'Authenticating existing Bun'):
            with self.subTest(suffix=suffix), self.assertRaises(RuntimeError):
                acceptance.verified_node_preservation(acceptance.node.REUSED + '\n' + suffix, before, after, False)
        with self.assertRaises(RuntimeError): acceptance.verified_node_preservation('Node ready', before, after, False)

    def test_bridge_enrollment_requires_authentication_but_ordinary_update_must_already_have_proof(self):
        before = {'selection': {}, 'payload': {}, 'versions': {}, 'receipt': None}
        after = dict(before, receipt={'sha256': 'ab' * 32})
        enrolled = acceptance.node.REUSED + '\nAuthenticating the installed Node.js release\n' + '\n'.join(
            'Authenticating existing ' + name for name in ('Corepack', 'pnpm', 'Yarn', 'Bun'))
        self.assertTrue(acceptance.verified_node_preservation(enrolled, before, after, True)['authenticated_enrollment'])
        with self.assertRaises(RuntimeError): acceptance.verified_node_preservation(enrolled, before, after, False)
        with self.assertRaises(RuntimeError): acceptance.verified_node_preservation(acceptance.node.REUSED, before, after, True)


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
    def test_workflow_shell_passes_exact_unquoted_argument_values_and_real_continuations(self):
        workflow = Path('.github/workflows/base-release-acceptance.yml').read_text()
        step = workflow.split('      - name: Bounded signed public installer and actual Node preservation acceptance\n', 1)[1]
        block = step.split('      - name: Retain only allowlisted public results\n', 1)[0].split('        run: |\n', 1)[1]
        block = textwrap.dedent(block).replace('${{ matrix.case }}', 'amd64-bridge')
        values = {'GITHUB_ACTIONS': 'true', 'GITHUB_REF': acceptance.BRANCH, 'GITHUB_SHA': 'ab' * 20,
            'GITHUB_REPOSITORY': acceptance.REPO, 'GITHUB_WORKFLOW': acceptance.WORKFLOW,
            'RUNNER_ARCH': 'X64', 'RUNNER_ENVIRONMENT': 'github-hosted',
            'ImageOS': 'ubuntu24', 'ImageVersion': '20261122.1', 'RUNNER_TEMP': '/tmp/runner directory with spaces'}
        self.assertIn('          FLUXO_METADATA_TOKEN: ${{ github.token }}\n', step)
        self.assertNotIn('${{ github.token }}', block)
        token = 'ghs_dummy_read_only_test_secret'
        with tempfile.TemporaryDirectory() as directory:
            capture = Path(directory) / 'argv'
            # The shell functions intercept sudo and go: this test never runs
            # the root installer, env, package commands, or a Go executable.
            program = ('sudo() { test "$FLUXO_METADATA_TOKEN" = "$EXPECTED_TOKEN"; printf "%s\\0" "$@" > "$CAPTURE"; }\n'
                       'go() { :; }\n' + block)
            subprocess.run(['bash', '-eu'], input=program, text=True, check=True, capture_output=True,
                env=dict(os.environ, **values, CAPTURE=str(capture), FLUXO_METADATA_TOKEN=token, EXPECTED_TOKEN=token))
            argv = [value.decode() for value in capture.read_bytes().split(b'\0')[:-1]]
        expected = ['--preserve-env=FLUXO_METADATA_TOKEN', 'env', 'FLUXO_BASE_RELEASE_ACCEPTANCE=1',
            *[key + '=' + value for key, value in values.items() if key != 'RUNNER_TEMP'],
            'PYTHONDONTWRITEBYTECODE=1', 'python3', 'scripts/ci/base_release_acceptance.py',
            '--subject', 'scripts/ci/evidence/base-release-acceptance-subject.json',
            '--output', values['RUNNER_TEMP'] + '/base-public-report', '--case', 'amd64-bridge', '--go', 'go']
        self.assertEqual(argv, expected)
        self.assertNotIn(token, repr(argv))

    def test_selected_architecture_workflow_inputs_do_not_expand_unique_native_cases(self):
        workflow = Path('.github/workflows/base-release-acceptance.yml').read_text()
        self.assertIn('options: [all, upgrades, fresh, amd64, arm64]', workflow)
        self.assertIn("group: base-public-release-${{ github.ref }}-${{ inputs.cases || 'all' }}", workflow)
        self.assertIn('cancel-in-progress: false', workflow)
        # Decode the literal matrix alternatives, including the no-input push
        # default. No YAML parser or privileged/native execution is needed.
        matrix = next(line for line in workflow.splitlines() if 'include: ${{ fromJSON(' in line)
        alternatives = __import__('re').findall(r"'([\[][^']*\])'", matrix)
        rows = [json.loads(value) for value in alternatives]
        self.assertEqual([[row['case'] for row in group] for group in rows], [
            ['amd64-fresh'], ['amd64-fresh', 'amd64-bridge'], ['arm64-bridge'],
            ['amd64-bridge', 'arm64-bridge'], ['amd64-fresh', 'amd64-bridge', 'arm64-bridge']])
        self.assertEqual({row['case'] for group in rows for row in group}, set(acceptance.CASES))

    def test_healthy_ordinary_update_uses_remembered_node_selection_not_a_new_install_request(self):
        installer = Path('/private/base/install.sh')
        self.assertIn('--no-node', acceptance.installer_command(installer))
        selected = acceptance.installer_command(installer, managed_node=True)
        self.assertIn('--node', selected)
        self.assertNotIn('--no-node', selected)
        remembered = acceptance.installer_command(installer, reinstall=True, remembered_node=True)
        self.assertIn('--reinstall', remembered)
        self.assertNotIn('--node', remembered)
        self.assertNotIn('--no-node', remembered)
        with self.assertRaises(RuntimeError):
            acceptance.installer_command(installer, managed_node=True, remembered_node=True)

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
    def test_v2_release_retains_v1_and_requires_exact_extended_capability(self):
        def cap(features):
            return {'format': 1, 'role_contracts': {'system-task-optional-profile': list(features)}}
        self.assertEqual(set(acceptance.optional_profiles(cap(acceptance.OPTIONAL_V2), 'v0.4.54-bridge-test.1')), acceptance.OPTIONAL_V2)
        self.assertEqual(set(acceptance.optional_profiles(cap(acceptance.OPTIONAL_MANUAL), 'v0.4.54')), acceptance.OPTIONAL_MANUAL)
        self.assertEqual(set(acceptance.optional_profiles(cap(acceptance.OPTIONAL_MANUAL), 'v0.4.54-bridge-test.2')), acceptance.OPTIONAL_MANUAL)
        for tag in ('v0.4.54-bridge-test.6', 'v0.4.54-bridge-test.7', 'v0.4.54-base-test.1'):
            self.assertEqual(set(acceptance.optional_profiles(cap(acceptance.OPTIONAL_MANUAL), tag)), acceptance.OPTIONAL_MANUAL)
        self.assertEqual(set(acceptance.optional_profiles(cap(acceptance.OPTIONAL_V1), 'v0.4.53')), acceptance.OPTIONAL_V1)
        self.assertEqual(acceptance.optional_profiles({'format': 1}, 'v0.4.52-base-test.2'), [])
        for features, tag in ((acceptance.OPTIONAL_V1, 'v0.4.54'),
                              (acceptance.OPTIONAL_V2, 'v0.4.54'),
                              (acceptance.OPTIONAL_MANUAL, 'v0.4.54-bridge-test.1'),
                              ({'nginx-conditional-v2'}, 'v0.4.54'),
                              (acceptance.OPTIONAL_V2, 'v0.4.53'),
                              (list(acceptance.OPTIONAL_V2) * 2, 'v0.4.54')):
            with self.subTest(tag=tag, features=features), self.assertRaises(RuntimeError):
                acceptance.optional_profiles(cap(features), tag)

    def test_apt_trace_exports_only_apt_stderr_and_redacts_urls(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'bridge-apt-trace.1').write_text(
                'execve("/usr/bin/apt-get", ["apt-get"], []) = 0\n'
                'write(2, "E", 1) = 1\n'
                'write(2, ": Failed to fetch https://name:secret@example.invalid/private?token=private\\n", 99) = 99\n'
                'write(2, "W: https://packages.sury.org/php/private?token=private\\n", 99) = 99\n'
                'write(1, "bootstrap-private", 17) = 17\n')
            (root / 'bridge-apt-trace.2').write_text('write(2, "E: unrelated-private", 20) = 20\n')
            values = acceptance.apt_trace_diagnostics(root)
            self.assertEqual(values, ['E: Failed to fetch <repository-url>', 'W: <repository:packages.sury.org>'])
            self.assertNotIn('private', repr(values))
            self.assertNotIn('secret', repr(values))

    def test_retained_scheduler_execution_requires_original_or_current_signed_image(self):
        old, current, unknown = ('ab' * 32, 'cd' * 32, 'ef' * 32)
        def invocation(image):
            return {'helper_sha256': image, 'helper': '/var/lib/fluxo-scheduler/helpers/' + image}
        for actual, first, dispatcher in ((old, None, old), (old, old, current), (current, old, old)):
            panel = old if first is None else current
            result = acceptance.scheduler_image_proof(invocation(actual), panel, first, actual, actual, dispatcher)
            self.assertEqual(result['helper_sha256'], actual)
        for actual, first, worker, helper, dispatcher in (
                (old, None, old, old, current), (old, None, old, old, old),
                (unknown, old, unknown, unknown, current), (old, old, unknown, old, current),
                (old, old, old, unknown, current), (old, old, old, old, unknown)):
            with self.subTest(actual=actual, first=first, worker=worker, helper=helper, dispatcher=dispatcher), \
                 self.assertRaisesRegex(RuntimeError, 'authenticated public images'):
                acceptance.scheduler_image_proof(invocation(actual), current, first, worker, helper, dispatcher)
        value = invocation(old)
        value['helper'] = '/unbound/helper'
        with self.assertRaises(RuntimeError):
            acceptance.scheduler_image_proof(value, current, old, old, old, current)

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
        with patch.object(acceptance.urllib.request.OpenerDirector, 'open', side_effect=[error(503), io.BytesIO(b'{"id":1}')]) as fetch, \
             patch.object(acceptance.time, 'sleep') as sleep:
            self.assertEqual(acceptance.public_json('releases/latest'), {'id': 1})
            self.assertEqual(fetch.call_count, 2)
            sleep.assert_called_once_with(2)
        for code in (403, 404, 503):
            with self.subTest(code=code), \
                 patch.object(acceptance.urllib.request.OpenerDirector, 'open', side_effect=error(code)) as fetch, \
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

    def test_only_three_native_noble_cases_and_original_named_hosts(self):
        self.assertEqual(acceptance.CASES, {'amd64-fresh': ('amd64', None),
            'amd64-bridge': ('amd64', 'bridge'), 'arm64-bridge': ('arm64', 'bridge')})
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
