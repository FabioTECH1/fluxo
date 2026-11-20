"""Hermetic safety/oracle tests; no packages, services, VM or network touched."""
import hashlib
import io
import json
import os
from pathlib import Path
import tarfile
import tempfile
import stat
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

import runtime_compat_candidate as c


def record(arch='amd64'):
    return {'version': c.IMAGICK_VERSIONS[0], 'architecture': arch, 'source': 'php-imagick',
            'sha256': 'a' * 64, 'uris': ['https://ppa.launchpadcontent.net/ondrej/php/ubuntu/pool/main/p/php-imagick/test.deb'],
            'origins': [{'site': 'ppa.launchpadcontent.net', 'archive': 'noble', 'component': 'main', 'trusted': True}]}


class NativeGuardTest(unittest.TestCase):
    def test_only_root_opted_in_native_hosted_noble_investigation_branches(self):
        env = {'FLUXO_RUNTIME_COMPAT_CANDIDATE': '1', 'GITHUB_ACTIONS': 'true',
               'RUNNER_ENVIRONMENT': 'github-hosted', 'GITHUB_WORKFLOW': c.WORKFLOW,
               'GITHUB_REF': c.BRANCHES[0], 'RUNNER_ARCH': 'X64'}
        with patch.dict(os.environ, env, clear=True), patch.object(c.os, 'geteuid', return_value=0), \
             patch.object(c.platform, 'machine', return_value='x86_64'), \
             patch.object(c.platform, 'node', return_value='disposable-runner'), \
             patch.object(c.Path, 'read_text', return_value='ID=ubuntu\nVERSION_ID="24.04"\n'), \
             patch.object(c.subprocess, 'check_output', return_value='amd64\n'):
            self.assertEqual(c.hosted_profile(), 'amd64')
            for changes in ({'FLUXO_RUNTIME_COMPAT_CANDIDATE': '0'}, {'GITHUB_ACTIONS': 'false'},
                            {'RUNNER_ENVIRONMENT': 'self-hosted'}, {'RUNNER_ARCH': 'ARM64'},
                            {'GITHUB_WORKFLOW': 'Other'}, {'GITHUB_REF': 'refs/heads/main'},
                            {'GITHUB_REF': 'refs/tags/v1.0.0'}):
                with self.subTest(changes=changes), patch.dict(os.environ, changes), self.assertRaises(RuntimeError):
                    c.hosted_profile()
            with patch.object(c.os, 'geteuid', return_value=1000), self.assertRaises(RuntimeError):
                c.hosted_profile()
            with patch.object(c.platform, 'node', return_value='fluxo-php-patch-test'), self.assertRaises(RuntimeError):
                c.hosted_profile()
            with patch.object(c.Path, 'read_text', return_value='ID=ubuntu\nVERSION_ID="22.04"\n'), self.assertRaises(RuntimeError):
                c.hosted_profile()
            with patch.object(c.platform, 'machine', return_value='aarch64'), \
                 patch.dict(os.environ, RUNNER_ARCH='ARM64'), \
                 patch.object(c.subprocess, 'check_output', return_value='arm64\n'):
                self.assertEqual(c.hosted_profile(), 'arm64')


class ProvenanceTest(unittest.TestCase):
    def test_exact_current_candidate_from_signed_approved_native_provider(self):
        for arch in ('amd64', 'arm64'):
            for version in c.IMAGICK_VERSIONS:
                records = {'php8.4-imagick': dict(record(arch), version=version)}
                self.assertEqual(c.validate_records(records, arch, candidate=True), records)

    def test_managed_sury_requires_exact_signed_release_and_package_location(self):
        for arch in ('amd64', 'arm64'):
            item = record(arch)
            item['uris'] = ['https://packages.sury.org/php/pool/main/p/php-imagick/test.deb']
            item['origins'] = [{'site': 'packages.sury.org', 'archive': 'noble', 'component': 'main',
                                'trusted': True, 'origin': 'deb.sury.org'}]
            records = {'php8.4-imagick': item}
            c.validate_records(records, arch, candidate=True, managed_sury=True)
            with self.assertRaises(RuntimeError):
                c.validate_records(records, arch, candidate=True)
            for field, value in (('origin', 'foreign'), ('archive', 'noble-updates'),
                                 ('component', 'unreviewed'), ('trusted', False)):
                bad = dict(item, origins=[dict(item['origins'][0], **{field: value})])
                with self.subTest(field=field), self.assertRaises(RuntimeError):
                    c.validate_records({'php8.4-imagick': bad}, arch, candidate=True, managed_sury=True)
            for uri in ('http://packages.sury.org/php/pool/test.deb',
                        'https://packages.sury.org/other/pool/test.deb',
                        'https://packages.sury.org.evil.invalid/php/pool/test.deb',
                        'https://packages.sury.org:443/php/pool/test.deb'):
                with self.subTest(uri=uri), self.assertRaises(RuntimeError):
                    c.validate_records({'php8.4-imagick': dict(item, uris=[uri])}, arch,
                                       candidate=True, managed_sury=True)
        with self.assertRaises(RuntimeError):
            c.validate_records({'php8.4-imagick': record()}, 'amd64', candidate=True, managed_sury=True)

    def test_existing_managed_provider_is_read_only_exact_source_and_embedded_key(self):
        key = Path('internal/services/phprepository/sury-keyring.gpg').read_bytes()
        self.assertEqual(hashlib.sha256(key).hexdigest(), c.SURY_KEY_SHA256)
        for arch in ('amd64', 'arm64'):
            source = ('Types: deb\nURIs: https://packages.sury.org/php/\nSuites: noble\nComponents: main\n'
                      'Architectures: ' + arch + '\nSigned-By: ' + c.SURY_KEY + '\n').encode()
            for change in ('none', 'suite', 'architecture', 'key-path', 'content'):
                changed = source
                if change == 'suite': changed = source.replace(b'noble', b'jammy')
                if change == 'architecture': changed = source.replace(arch.encode(), b'other')
                if change == 'key-path': changed = source.replace(c.SURY_KEY.encode(), b'/tmp/key.gpg')
                supplied = {c.SURY_SOURCE: changed, c.SURY_KEY: key if change != 'content' else key + b'x'}
                with self.subTest(arch=arch, change=change), patch.object(c, 'protected_apt_input', side_effect=lambda p: supplied[p]):
                    if change == 'none':
                        result = c.managed_php_provider(arch)
                        self.assertFalse(result['fixture_repository_changes'])
                    else:
                        with self.assertRaises(RuntimeError):
                            c.managed_php_provider(arch)

    def test_no_untrusted_provider_arch_version_fallback_or_downgrade(self):
        for key, value in (('version', '3.8.2-1'), ('version', '3.8.0-1'),
                           ('version', c.IMAGICK_VERSIONS[1] + '+local'), ('architecture', 'arm64'),
                           ('sha256', ''), ('source', 'other'), ('origins', []), ('uris', []),
                           ('uris', ['https://ppa.launchpadcontent.net/unreviewed/php/ubuntu/pool/test.deb']),
                           ('uris', ['https://user:secret@ppa.launchpadcontent.net/ondrej/php/ubuntu/pool/test.deb']),
                           ('uris', ['https://ppa.launchpadcontent.net/ondrej/php/ubuntu/pool/test.deb?token=x'])):
            bad = dict(record(), **{key: value})
            with self.subTest(key=key, value=value), self.assertRaises(RuntimeError):
                c.validate_records({'php8.4-imagick': bad}, 'amd64', candidate=True)
        for key, value in (('trusted', False), ('site', 'unreviewed.invalid'), ('archive', 'jammy')):
            bad = record()
            bad['origins'][0][key] = value
            with self.subTest(key=key), self.assertRaises(RuntimeError):
                c.validate_records({'php8.4-imagick': bad}, 'amd64', candidate=True)
        with self.assertRaises(RuntimeError):
            c.validate_records({'php8.4-imagick': record()}, 'amd64', {'missing': '1'})
        with self.assertRaises(RuntimeError):
            c.validate_records({'php8.4-imagick': record()}, 'amd64', {'php8.4-imagick': 'other'})


class APTTrustScopeTest(unittest.TestCase):
    def test_only_exact_authenticated_vendor_key_metadata_can_be_prepared(self):
        info = SimpleNamespace(st_uid=0, st_gid=0, st_nlink=1, st_mode=stat.S_IFREG | 0o777)
        self.assertTrue(c.packaged_ubuntu_key_needs_stock_mode(info, c.UBUNTU_KEY_SHA256, '2023.11.28.1'))
        info.st_mode = stat.S_IFREG | 0o644
        self.assertFalse(c.packaged_ubuntu_key_needs_stock_mode(info, c.UBUNTU_KEY_SHA256, '2023.11.28.1'))
        for field, value in (('st_uid', 1), ('st_gid', 1), ('st_nlink', 2),
                             ('st_mode', stat.S_IFLNK | 0o777), ('st_mode', stat.S_IFREG | 0o664),
                             ('st_mode', stat.S_IFREG | 0o4644)):
            bad = SimpleNamespace(**vars(info))
            setattr(bad, field, value)
            with self.subTest(field=field, value=value), self.assertRaises(RuntimeError):
                c.packaged_ubuntu_key_needs_stock_mode(bad, c.UBUNTU_KEY_SHA256, '2023.11.28.1')
        for sha, version in (('0' * 64, '2023.11.28.1'), (c.UBUNTU_KEY_SHA256, 'unknown')):
            with self.assertRaises(RuntimeError):
                c.packaged_ubuntu_key_needs_stock_mode(info, sha, version)

    def test_signed_by_lists_and_multiline_deb822(self):
        a, b = '/usr/share/keyrings/ubuntu-archive-keyring.gpg', '/etc/apt/keyrings/php.asc'
        self.assertEqual(c.apt_source_keys('deb [arch=amd64 signed-by=' + a + ',' + b + '] https://example.invalid noble main\n', '.list'), {a, b})
        source = 'Types: deb\nURIs: https://example.invalid\nSuites: noble\nComponents: main\nSigned-By:\n ' + a + '\n ' + b + '\n'
        self.assertEqual(c.apt_source_keys(source, '.sources'), {a, b})
        self.assertEqual(c.apt_source_keys('deb https://example.invalid noble main', '.list'), set())
        self.assertEqual(c.apt_source_keys(source + 'Enabled: no\n', '.sources'), set())

    def test_ambiguous_or_weakened_trust_never_drops_reference(self):
        a = '/usr/share/keyrings/vendor.gpg'
        for source in (
            'deb [signed-by=' + a + ' signed-by=' + a + '] https://example.invalid noble main',
            'deb [signed-by=' + a + ' trusted=yes] https://example.invalid noble main',
            'deb [signed-by=' + a + ' allow-insecure=true] https://example.invalid noble main',
            'deb [signed-by=' + a + ' check-date=no] https://example.invalid noble main',
            'deb [signed-by=' + a + ' unknown-trust=value] https://example.invalid noble main',
            'deb [signed-by=/etc/shadow] https://example.invalid noble main',
            'deb [signed-by=ABCDEF] https://example.invalid noble main',
        ):
            with self.subTest(source=source), self.assertRaises(RuntimeError):
                c.apt_source_keys(source, '.list')
        base = 'Types: deb\nURIs: https://example.invalid\nSuites: noble\n'
        for fields in ('Signed-By: ' + a + '\nSigned-By: ' + a,
                       'Signed-By: ' + a + '\nTrusted: yes',
                       'Signed-By: -----BEGIN PGP PUBLIC KEY BLOCK-----\n text',
                       'Signed-By: ' + a + '\nCheck-Valid-Until: no'):
            with self.subTest(fields=fields), self.assertRaises(RuntimeError):
                c.apt_source_keys(base + fields, '.sources')

    def test_unused_writable_hosted_key_is_not_authority_but_active_is_refused(self):
        source = Path('/etc/apt/sources.list.d/ubuntu.sources')
        ubuntu = '/usr/share/keyrings/ubuntu-archive-keyring.gpg'
        hosted = '/usr/share/keyrings/microsoft-prod.gpg'
        global_key = '/etc/apt/trusted.gpg.d/global.gpg'
        visited = []
        active = ubuntu
        global_mode = 0o644
        def read(path):
            name = str(path)
            visited.append(name)
            mode = 0o777 if name == hosted else global_mode if name == global_key else 0o644
            c.check_apt_input_metadata(name, SimpleNamespace(st_uid=0, st_mode=stat.S_IFREG | mode, st_nlink=1))
            if path == source:
                return ('Types: deb\nURIs: https://example.invalid\nSuites: noble\nSigned-By: ' + active).encode()
            return b'public-key-fixture'
        def glob(path, pattern):
            return iter({('/etc/apt/sources.list.d', '*.sources'): [source],
                         ('/etc/apt/trusted.gpg.d', '*.gpg'): [Path(global_key)]}.get((str(path), pattern), []))
        with patch.object(c.Path, 'glob', glob), patch.object(c.Path, 'exists', return_value=False), \
             patch.object(c, 'protected_apt_input', side_effect=read):
            result = c.trust_inventory()
            self.assertIn(ubuntu, result)
            self.assertIn(global_key, result)
            self.assertNotIn(hosted, visited)
            active = hosted
            with self.assertRaisesRegex(RuntimeError, 'microsoft-prod.gpg.*0o777'):
                c.trust_inventory()
            active, global_mode = ubuntu, 0o777
            with self.assertRaisesRegex(RuntimeError, 'global.gpg.*0o777'):
                c.trust_inventory()


class StartupTraceTest(unittest.TestCase):
    def trace(self):
        argv = json.dumps(['/usr/bin/php', '-r', c.PHP_STARTUP])
        return ('123 execve("/usr/bin/php", ' + argv + ', 0x123 /* 4 vars */) = 0\n'
                '123 openat(AT_FDCWD, "/etc/ImageMagick-6/policy.xml", O_RDONLY) = 3\n'
                '123 +++ exited with 0 +++\n')

    def test_only_extension_loaded_startup(self):
        self.assertTrue(c.trace_oracle(self.trace())['no_delegate_process'])

    def test_rejects_extra_execs_children_discovery_truncation_and_info_probes(self):
        for extra in ('123 execve("/usr/bin/convert", [], 0x0) = 0\n',
                      '123 clone(child_stack=NULL) = 124\n', '123 clone3({flags=1}, 88) = -1 ENOSYS\n',
                      '123 vfork() = 124\n',
                      '123 openat(AT_FDCWD, "/usr/lib/ImageMagick/modules-Q16/coders/jpeg.so", O_RDONLY) = 3\n',
                      '123 access("/usr/lib/ImageMagick/filters", F_OK) = -1 ENOENT\n',
                      '123 stat("/opt/coder/test", 0x0) = -1 ENOENT\n',
                      '123 openat(AT_FDCWD, "/etc/"... <unfinished ...>\n'):
            with self.subTest(extra=extra), self.assertRaises(RuntimeError):
                c.trace_oracle(self.trace() + extra)
        for raw in ('', self.trace().replace('exited with 0', 'exited with 1'),
                    self.trace().replace('"-r"', '"-i"'),
                    self.trace().replace(' ? 0 : 23', ' ? Imagick::queryFormats() : 23'),
                    self.trace().replace('/usr/bin/php', '/usr/bin/php8.4')):
            with self.subTest(raw=raw), self.assertRaises(RuntimeError):
                c.trace_oracle(raw)

    def test_native_test_must_pass_without_skips(self):
        for raw in ('PASS\n', '--- SKIP: ' + c.TEST, '--- PASS: ' + c.TEST + ' (0s)\n--- SKIP: child'):
            with self.assertRaises(RuntimeError):
                c.native_result(raw)
        c.native_result('--- PASS: ' + c.TEST + ' (0.5s)\nPASS\n')


def archive_file(name, raw):
    output = io.BytesIO()
    with tarfile.open(fileobj=output, mode='w') as archive:
        member = tarfile.TarInfo(name)
        member.size = len(raw)
        archive.addfile(member, io.BytesIO(raw))
    return output.getvalue()


class SignedArchiveTest(unittest.TestCase):
    def test_manifest_and_configuration_must_match_signed_deb_bytes(self):
        for change in ('none', 'archive', 'manifest', 'configuration', 'missing-config'):
            with self.subTest(change=change), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                package = root / 'common.deb'
                package.write_bytes(b'signed fixture bytes')
                row = record()
                row.update(version='8:6.9.12.98+dfsg1-5.2build2', architecture='all', sha256=c.digest(package))
                if change == 'archive': row['sha256'] = '0' * 64
                manifest = b'abcdef  usr/share/example\n'
                config = b'<policymap/>\n'
                config_path = '/etc/ImageMagick-6/policy.xml'
                configs = {config_path: hashlib.sha256(config).hexdigest()}
                if change == 'configuration': configs[config_path] = '0' * 64
                if change == 'missing-config': configs['/etc/ImageMagick-6/missing.xml'] = '1' * 64
                outputs = [archive_file('./md5sums', manifest), archive_file('.' + config_path, config)]
                command = Mock(return_value='Package: imagemagick-6-common\nVersion: ' + row['version'] + '\nArchitecture: all\n')
                with patch.object(c.subprocess, 'check_output', side_effect=outputs), \
                     patch.object(c.Path, 'read_bytes', return_value=b'drift' if change == 'manifest' else manifest):
                    if change == 'none':
                        result = c.verify_archives(root, {'imagemagick-6-common': row}, command, configs)
                        self.assertEqual(result['signed_imagemagick_configurations'], 1)
                    else:
                        with self.assertRaises(RuntimeError):
                            c.verify_archives(root, {'imagemagick-6-common': row}, command, configs)


class WorkflowContractTest(unittest.TestCase):
    def test_two_profiles_and_only_allowlisted_result_upload(self):
        workflow = Path('.github/workflows/runtime-compat-candidate.yml').read_text()
        self.assertIn('branches: [test/runtime-compat-candidate]', workflow)
        self.assertEqual(workflow.count('- {runner:'), 2)
        for runner, arch in (('ubuntu-24.04', 'amd64'), ('ubuntu-24.04-arm', 'arm64')):
            self.assertIn('{runner: ' + runner + ', arch: ' + arch + '}', workflow)
        self.assertLess(workflow.index('(cd ui && npm ci && npm run build)'), workflow.index('build_release_candidate.py'))
        self.assertIn("go-version: '1.26.3'", workflow)
        self.assertIn("CGO_ENABLED: '0'", workflow)
        upload = workflow[workflow.index('uses: actions/upload-artifact'):]
        paths = upload.split('path: |\n', 1)[1].split('          if-no-files-found:', 1)[0]
        self.assertEqual({p.strip() for p in paths.splitlines() if p.strip()}, {
            '${{ runner.temp }}/runtime-report/report.json',
            '${{ runner.temp }}/runtime-report/failure-sanitized.log',
            '${{ runner.temp }}/runtime-build/fluxo',
            '${{ runner.temp }}/runtime-build/fluxo.manifest.json',
            '${{ runner.temp }}/runtime-build/install.sh',
        })
        self.assertNotIn('*.log', upload)
        self.assertNotIn('/root', upload)
        self.assertNotIn('pull_request:', workflow)

    def test_diagnostics_export_only_fixed_labels(self):
        raw = 'NO_PUBKEY secret\nCould not resolve private-host password=secret\nprivate-key-material'
        self.assertEqual(c.failure_signals(raw), ['network-dns-failure', 'apt-signature-key-unavailable'])
        self.assertNotIn('secret', json.dumps(c.failure_signals(raw)))


if __name__ == '__main__':
    unittest.main()
