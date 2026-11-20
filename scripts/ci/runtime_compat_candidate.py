#!/usr/bin/env python3
"""TEST ONLY: two native Ubuntu 24.04 hosted runtime fixtures, no ACME.

Invocation/opt-ins: .github/workflows/runtime-compat-candidate.yml. The normal
installer establishes the managed PHP provider; only its current, trusted
candidate may add Imagick. No downgrade, alternate repository, job/receipt
fabrication, or customer hook qualification. Raw logs/profiles/debs stay 0700
under /root. Only allowlisted results, exact candidate build artifacts and bounded redacted
failure diagnostics are uploaded; profiles and credentials stay private.
"""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import platform
import re
import shutil
import shlex
import stat
import signal
import uuid
import subprocess
import tarfile
import tempfile
import time
from urllib.parse import urlsplit

from base_candidate import HostedFixture, FLAGS, require, verify_build, public_bridge, verify_installed, observer_module, sanitized_diagnostic, seed_application
from arm64_candidate import download_public_subject, await_predecessor_checks, installation_invariants
from build_release_candidate import digest

BRANCHES = ('refs/heads/baseline/clean-base', 'refs/heads/test/runtime-compat-candidate')
WORKFLOW = 'Native runtime compatibility candidate'
# Two reviewed exact revisions. Selection still comes solely from the normal
# installer's managed provider candidate; the fixture never configures a
# repository or selects an older revision when the current candidate differs.
IMAGICK_VERSIONS = ('3.8.1-1+ubuntu24.04.1+deb.sury.org+1',
                    '3.8.1-1+0~20260621.55+ubuntu24.04~1.gbpde1d95')
TEST = 'TestCINativeRuntimeCompatibility'
PHP_STARTUP = 'exit(extension_loaded("imagick") ? 0 : 23);'
OWNED = ('.github/workflows/runtime-compat-candidate.yml',
         'scripts/ci/runtime_compat_candidate.py', 'scripts/ci/runtime_compat_candidate_test.py',
         'internal/services/maintenancetask/runtime_compat_vm_test.go')
CLEAN = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'HOME': '/root',
         'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8'}
APT = ['apt-get', '-o', 'APT::Get::AllowUnauthenticated=false',
       '-o', 'Acquire::AllowInsecureRepositories=false',
       '-o', 'Acquire::AllowDowngradeToInsecureRepositories=false',
       '-o', 'Acquire::Retries=2', '-o', 'Acquire::http::Timeout=30',
       '-o', 'Acquire::https::Timeout=30']

# Use Ubuntu's python-apt, never the runner's pip environment. Records come from
# signed indexes selected by APT, not the static layout-review JSON or dpkg status.
APT_RECORDS = r'''
import apt, json, sys
cache = apt.Cache()
result = {}
for name in sys.argv[2:]:
    package = cache[name]
    v = package.candidate if sys.argv[1] == 'candidate' else package.installed
    if v is None:
        raise RuntimeError('Required package version unavailable: ' + name)
    result[name] = {'version': v.version, 'architecture': v.architecture,
        'source': v.source_name, 'sha256': v.record.get('SHA256', ''), 'uris': v.uris,
        'origins': [{'site': o.site, 'archive': o.archive, 'component': o.component,
                     'trusted': o.trusted, 'origin': o.origin} for o in v.origins
                    if not (o.site == '' and o.archive == 'now' and o.component == 'now')]}
print(json.dumps(result))
'''


def hosted_profile():
    expected = {'FLUXO_RUNTIME_COMPAT_CANDIDATE': '1', 'GITHUB_ACTIONS': 'true',
                'RUNNER_ENVIRONMENT': 'github-hosted', 'GITHUB_WORKFLOW': WORKFLOW}
    require(os.geteuid() == 0 and all(os.environ.get(k) == v for k, v in expected.items()),
            'Explicit root disposable GitHub-hosted opt-in required')
    require(os.environ.get('GITHUB_REF') in BRANCHES, 'Investigation branch required')
    arch = {'x86_64': 'amd64', 'aarch64': 'arm64'}.get(platform.machine())
    require(arch and os.environ.get('RUNNER_ARCH') == {'amd64': 'X64', 'arm64': 'ARM64'}[arch],
            'Native hosted architecture required')
    release = dict(line.split('=', 1) for line in Path('/usr/lib/os-release').read_text().splitlines() if '=' in line)
    require(release.get('ID', '').strip('"') == 'ubuntu' and release.get('VERSION_ID', '').strip('"') == '24.04',
            'Only Ubuntu 24.04 is supported')
    require(platform.node() != 'fluxo-php-patch-test', 'Named VM is not a CI fixture')
    require(subprocess.check_output(['dpkg', '--print-architecture'], text=True, timeout=10).strip() == arch,
            'Native dpkg architecture differs')
    return arch


def build_identity(build):
    prefix = ['git', '-c', 'safe.directory=' + os.getcwd()]
    require(not subprocess.check_output(prefix + ['status', '--porcelain', '--untracked-files=all'], text=True),
            'Entire fixture checkout must be committed and clean')
    return {'source': subprocess.check_output(prefix + ['rev-parse', 'HEAD'], text=True).strip(),
            'maintenance_test_sha256': digest(build / 'maintenance.test'),
            'fixture_files': {name: digest(name) for name in OWNED}}


def validate_records(records, arch, expected=None, candidate=False, managed_sury=False):
    require(isinstance(records, dict) and bool(records), 'Empty APT provenance evidence')
    if expected is not None:
        require(set(records) == set(expected), 'APT package closure differs')
    for name, row in records.items():
        require(re.fullmatch(r'[a-z0-9][a-z0-9+.-]*(?::(?:amd64|arm64))?', name), 'Unsafe package name')
        require(row['architecture'] in (arch, 'all') and re.fullmatch('[0-9a-f]{64}', row['sha256']),
                'Signed package archive hash/native architecture unavailable: ' + name)
        require(re.fullmatch(r'[A-Za-z0-9.+:~_-]+', row['version']), 'Unsafe package version')
        require(row['origins'] and all(o['trusted'] is True and o['site'] in (
            'archive.ubuntu.com', 'security.ubuntu.com', 'ports.ubuntu.com', 'ppa.launchpadcontent.net',
            'ppa.launchpad.net', *(['packages.sury.org'] if managed_sury else [])) and o['archive'] in ('noble', 'noble-updates', 'noble-security')
            for o in row['origins']), 'Package lacks approved signed Noble provenance: ' + name)
        for origin in row['origins']:
            if origin['site'] == 'packages.sury.org':
                require(origin.get('origin') == 'deb.sury.org' and origin['archive'] == 'noble'
                        and origin['component'] == 'main', 'Unapproved Sury release identity: ' + name)
        require(row['uris'], 'Signed archive URI unavailable: ' + name)
        for uri in row['uris']:
            parsed = urlsplit(uri)
            require(parsed.scheme in ('https', 'http') and parsed.username is None and parsed.password is None
                    and not parsed.query and not parsed.fragment and parsed.port is None
                    and parsed.hostname in {o['site'] for o in row['origins']}, 'Unapproved package URI: ' + name)
            if parsed.hostname == 'packages.sury.org':
                require(managed_sury and parsed.scheme == 'https' and parsed.path.startswith('/php/pool/'),
                        'Unapproved managed Sury package URI: ' + name)
            if parsed.hostname in ('ppa.launchpadcontent.net', 'ppa.launchpad.net'):
                require(parsed.path.startswith('/ondrej/php/ubuntu/pool/'), 'Unapproved PPA package URI: ' + name)
        if expected is not None:
            require(row['version'] == expected[name], 'Installed/signed-index version differs: ' + name)
        if candidate:
            require(name == 'php8.4-imagick' and row['version'] in IMAGICK_VERSIONS and row['source'] == 'php-imagick'
                    and all(o['site'] in (('packages.sury.org',) if managed_sury else ('ppa.launchpadcontent.net', 'ppa.launchpad.net'))
                            and o['archive'] == 'noble' and o['component'] == 'main' for o in row['origins']),
                    'Approved current Imagick candidate unavailable; refuse fallback or downgrade')
    return records


SURY_KEY = '/usr/share/keyrings/fluxo-sury-archive.gpg'
SURY_KEY_SHA256 = 'b486fd5488185c4c46467960fa69c53d5085fec492cf76b9eaf3db33561c9d7c'
SURY_SOURCE = '/etc/apt/sources.list.d/fluxo-sury-php.sources'


def managed_php_provider(arch):
    """Verify the existing normal installer's source and pinned key; never write."""
    expected = ('Types: deb\nURIs: https://packages.sury.org/php/\nSuites: noble\nComponents: main\n'
                'Architectures: ' + arch + '\nSigned-By: ' + SURY_KEY + '\n').encode()
    source = protected_apt_input(SURY_SOURCE)
    key = protected_apt_input(SURY_KEY)
    require(source == expected and hashlib.sha256(key).hexdigest() == SURY_KEY_SHA256,
            'Normal installer managed PHP source/key differs')
    require(key == Path('internal/services/phprepository/sury-keyring.gpg').read_bytes(),
            'Managed PHP key differs from source-bound embedded key')
    return {'provider': 'packages.sury.org/php', 'source_sha256': hashlib.sha256(source).hexdigest(),
            'key_sha256': SURY_KEY_SHA256, 'key_fingerprint': '15058500A0235D97F5D10063B188E2B695BD4743',
            'configured_by_normal_installer': True, 'fixture_repository_changes': False}


def trace_oracle(raw):
    """Require exactly one PHP exec, no children, and no coder/filter access.

    Inspect attempted access too, including failed opens. Initialization XML is
    permitted; coder/filter discovery/loading and any delegate process refuse.
    -f catches descendants; -s 4096 and bounded trace size avoid silent truncation.
    """
    require(raw and len(raw) <= 16 << 20 and '+++ exited with 0 +++' in raw, 'Incomplete startup trace')
    require('unfinished ...' not in raw and 'resumed>' not in raw and '"...' not in raw,
            'Ambiguous or truncated startup trace')
    execs = [line for line in raw.splitlines() if re.search(r'\bexecve(?:at)?\(', line)]
    argv = json.dumps(['/usr/bin/php', '-r', PHP_STARTUP])
    require(len(execs) == 1 and 'execve("/usr/bin/php", ' + argv + ',' in execs[0]
            and execs[0].endswith('= 0'), 'Unexpected startup executable')
    require(not re.search(r'\b(?:clone3?|fork|vfork)\(', raw), 'PHP startup created a child process/thread')
    require(not re.search(r'(?i)(?:/coders?(?:/|[".])|/filters?(?:/|[".])|modules-Q16|delegates?/[^" ]+\.(?:so|la))', raw),
            'PHP startup accessed a coder/filter/delegate module')
    return {'single_php_exec': True, 'no_child_creation': True, 'no_coder_filter_module_access': True,
            'no_delegate_process': True, 'image_operations': False}


def native_result(raw, name=TEST):
    require('--- PASS: ' + name + ' (' in raw and '--- SKIP:' not in raw and '--- FAIL:' not in raw,
            'Native test did not execute and pass completely')


def failure_signals(raw):
    # Fixed strings only: never export a whole apt/test/installer output line.
    return [label for label, pattern in (
        ('network-dns-failure', r'Temporary failure resolving|Could not resolve'),
        ('network-connection-failure', r'Could not connect|Connection timed out|Connection failed'),
        ('apt-signature-key-unavailable', r'NO_PUBKEY'),
        ('apt-signature-refused', r'not signed|signatures couldn.t be verified|BADSIG|EXPKEYSIG'),
        ('apt-package-version-unavailable', r'Version .* was not found|has no installation candidate|Unable to locate package'),
        ('php-native-qualification-refused', r'actual packaged default PHP qualification|vendor configuration|Verified Imagick'),
        ('certbot-native-qualification-refused', r'actual Certbot/Python/Nginx qualification'),
        ('fixture-restore-failed', r'restore fixture|restored fixture does not reproduce'),
    ) if re.search(pattern, raw, re.I)]


KEY_ROOTS = ('/etc/apt/keyrings/', '/usr/share/keyrings/', '/etc/apt/trusted.gpg.d/')


def signed_by_paths(value):
    require(value.strip() and '-----' not in value, 'Unsupported inline/empty APT Signed-By')
    paths = set()
    for name in re.split(r'[\s,]+', value.strip()):
        path = Path(name)
        require(name.startswith(KEY_ROOTS) and str(path) == name and '..' not in path.parts
                and path.suffix in ('.gpg', '.asc'), 'Unsupported APT Signed-By reference')
        paths.add(name)
    return paths


def apt_source_keys(raw, suffix):
    keys = set()
    secure_boolean = {'trusted': {'no', 'false', '0'},
        'allow-insecure': {'no', 'false', '0'}, 'allow-weak': {'no', 'false', '0'},
        'allow-downgrade-to-insecure': {'no', 'false', '0'},
        'check-valid-until': {'yes', 'true', '1'}, 'check-date': {'yes', 'true', '1'}}
    if suffix == '.list':
        for line in raw.splitlines():
            tokens = shlex.split(line, comments=True)
            if not tokens:
                continue
            require(tokens[0] in ('deb', 'deb-src'), 'Unsupported APT list syntax')
            if len(tokens) > 1 and tokens[1].startswith('['):
                options = []
                for token in tokens[1:]:
                    options.append(token)
                    if token.endswith(']'):
                        break
                require(options[-1].endswith(']'), 'Incomplete APT source options')
                seen = set()
                for option in shlex.split(' '.join(options)[1:-1]):
                    name, sep, value = option.partition('=')
                    name = name.lower()
                    require(sep and name not in seen, 'Ambiguous APT source options')
                    seen.add(name)
                    if name == 'signed-by':
                        keys.update(signed_by_paths(value))
                    elif name == 'arch':
                        require(re.fullmatch(r'[a-z0-9,-]+', value), 'Unsupported APT architecture option')
                    else:
                        require(name in secure_boolean and value.lower() in secure_boolean[name],
                                'Unsupported or weakened APT trust option')
        return keys
    require(suffix == '.sources', 'Unsupported APT source format')
    for block in re.split(r'\n\s*\n', raw):
        fields, current = {}, None
        for line in block.splitlines():
            if not line.strip() or line.lstrip().startswith('#'):
                continue
            if line[0].isspace():
                require(current is not None, 'Ambiguous APT source continuation')
                fields[current] += ' ' + line.strip()
            else:
                name, sep, value = line.partition(':')
                name = name.lower()
                require(sep and re.fullmatch('[a-z][a-z0-9-]*', name) and name not in fields,
                        'Ambiguous APT source field')
                fields[name], current = value.strip(), name
        if not fields:
            continue
        enabled = fields.get('enabled', 'yes').lower()
        require(enabled in ('yes', 'true', '1', 'no', 'false', '0'), 'Invalid APT source Enabled value')
        if enabled in ('no', 'false', '0'):
            continue
        plain = {'types', 'uris', 'suites', 'components', 'architectures', 'languages',
                 'enabled', 'x-repolib-name', 'x-repolib-default-mirror'}
        for name, value in fields.items():
            if name == 'signed-by':
                keys.update(signed_by_paths(value))
            elif name not in plain:
                require(name in secure_boolean and value.lower() in secure_boolean[name],
                        'Unsupported or weakened APT trust option')
        require(fields.get('types') and fields.get('uris') and fields.get('suites'), 'Incomplete active APT source')
    return keys


def check_apt_input_metadata(path, info):
    require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o022
            and info.st_nlink == 1,
            'Unsafe APT source/key metadata: ' + str(path) + ' uid=' + str(info.st_uid)
            + ' mode=' + oct(info.st_mode & 0o777) + ' links=' + str(info.st_nlink))


def protected_apt_input(path):
    p = Path(path)
    for parent in p.parents:
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o022,
                'Unsafe APT input directory')
    before = p.lstat()
    check_apt_input_metadata(path, before)
    raw = p.read_bytes()
    after = p.lstat()
    require((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns)
            == (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns),
            'APT input changed during inspection')
    return raw


UBUNTU_KEY_SHA256 = '80a36b0a6de2f69f49d2df75ef473ccde121e9e190b9ea01d20a4f63778d5c31'
UBUNTU_KEY_DEB_SHA256 = '36de43b15853ccae0028e9a767613770c704833f82586f28eb262f0311adb8a8'


def packaged_ubuntu_key_needs_stock_mode(info, sha, version):
    require(stat.S_ISREG(info.st_mode) and not info.st_mode & 0o7000 and info.st_uid == 0
            and info.st_gid == 0 and info.st_nlink == 1 and info.st_mode & 0o777 in (0o644, 0o777)
            and sha == UBUNTU_KEY_SHA256 and version == '2023.11.28.1',
            'Ubuntu archive-key fixture differs from authenticated vendor evidence')
    return info.st_mode & 0o777 == 0o777


def stock_ubuntu_key_metadata(report, command):
    # Disposable hosted fixture only. The exact payload and 0644 mode were
    # independently verified from signed Noble Packages and the authenticated
    # ubuntu-keyring2023.11.28.1all archive (SHA above), never from these bytes
    # alone. Unknown owner, image, version, path or metadata is not repaired.
    hosted_profile()
    path = Path('/usr/share/keyrings/ubuntu-archive-keyring.gpg')
    for parent in path.parents:
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o022,
                'Unsafe Ubuntu key fixture directory')
    version = command('ubuntu-keyring-fixture-version', ['/usr/bin/dpkg-query', '-W', '-f=${Version}', 'ubuntu-keyring']).strip()
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        raw = os.read(fd, 1 << 20)
        sha = hashlib.sha256(raw).hexdigest()
        change = packaged_ubuntu_key_needs_stock_mode(before, sha, version)
        require((path.lstat().st_dev, path.lstat().st_ino) == (before.st_dev, before.st_ino),
                'Ubuntu key fixture identity changed')
        record = {'path': str(path), 'sha256': sha, 'package_version': version,
                  'authenticated_deb_sha256': UBUNTU_KEY_DEB_SHA256,
                  'vendor_mode': '0o644', 'before_mode': oct(before.st_mode & 0o777),
                  'mode_restored_to_stock': change, 'content_replaced': False,
                  'ctime_restoration_possible': False}
        report['ubuntu_key_vendor_fixture'] = record
        if change:
            os.fchmod(fd, 0o644)
        check_apt_input_metadata(path, os.fstat(fd))
    except Exception:
        os.close(fd)
        raise
    def restore():
        try:
            info = path.lstat()
            require((info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_nlink)
                    == (before.st_dev, before.st_ino, 0, 0, 1) and digest(path) == sha,
                    'Ubuntu key fixture changed; refuse to overwrite it during cleanup')
            if change:
                os.fchmod(fd, before.st_mode & 0o777)
            os.utime(fd, ns=(before.st_atime_ns, before.st_mtime_ns))
            require(path.lstat().st_mode & 0o777 == before.st_mode & 0o777,
                    'Ubuntu key fixture original mode was not restored')
            record['original_bytes_and_metadata_restored'] = True
        finally:
            os.close(fd)
    return restore


def trust_inventory():
    # Only these global stores are implicitly trusted by APT. keyrings/ and
    # /usr/share/keyrings are inert unless a configured source names Signed-By.
    sources = sorted(Path('/etc/apt/sources.list.d').glob('*.list'))
    sources += sorted(Path('/etc/apt/sources.list.d').glob('*.sources'))
    if Path('/etc/apt/sources.list').exists():
        sources.append(Path('/etc/apt/sources.list'))
    result, keys = {}, set()
    for p in sources:
        raw = protected_apt_input(p)
        result[str(p)] = hashlib.sha256(raw).hexdigest()
        keys.update(apt_source_keys(raw.decode(), '.list' if p.name == 'sources.list' else p.suffix))
    if Path('/etc/apt/trusted.gpg').exists():
        keys.add('/etc/apt/trusted.gpg')
    for suffix in ('*.gpg', '*.asc'):
        keys.update(str(p) for p in Path('/etc/apt/trusted.gpg.d').glob(suffix))
    for name in sorted(keys):
        result[name] = hashlib.sha256(protected_apt_input(name)).hexdigest()
    return result


def unused_key_inventory(active):
    result = {}
    for root in ('/usr/share/keyrings', '/etc/apt/keyrings'):
        for p in sorted(Path(root).glob('*')):
            if str(p) in active or p.suffix not in ('.gpg', '.asc'):
                continue
            info = p.lstat()
            # Evidence only, never an authority input or reason to change it.
            result[str(p)] = {'uid': info.st_uid, 'mode': oct(info.st_mode & 0o777),
                             'links': info.st_nlink, 'referenced_by_active_apt': False}
            if stat.S_ISREG(info.st_mode) and info.st_size <= 8 << 20:
                result[str(p)]['sha256'] = digest(p)
    return result


def vendor_fixture_entries(archives):
    """Read authenticated archive metadata without extracting or executing it."""
    result = {}
    for package, raw in archives.items():
        with tarfile.open(fileobj=io.BytesIO(raw), mode='r:') as archive:
            for member in archive:
                name = member.name.removeprefix('./').rstrip('/')
                require(not name.startswith('/') and '..' not in name.split('/'), 'Unsafe vendor archive path')
                path = '/' + name if name else '/'
                if not (member.isdir() or member.isfile()):
                    continue  # Effective input selection below refuses aliases.
                item = {'kind': 'directory' if member.isdir() else 'file',
                        'uid': member.uid, 'gid': member.gid, 'mode': member.mode,
                        'package': package}
                if member.isfile():
                    if member.size > 8 << 20:
                        continue
                    item['sha256'] = hashlib.sha256(archive.extractfile(member).read()).hexdigest()
                if path in result:
                    require({k: v for k, v in item.items() if k != 'package'} ==
                            {k: v for k, v in result[path].items() if k != 'package'},
                            'Incoherent signed vendor metadata')
                else:
                    result[path] = item
    return result


def vendor_fixture_mode(info, vendor, sha=None):
    require(info.st_uid == info.st_gid == vendor['uid'] == vendor['gid'] == 0,
            'Non-root vendor fixture input')
    directory = vendor['kind'] == 'directory'
    require((stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode) and info.st_nlink == 1)
            and vendor['mode'] == (0o755 if directory else 0o644)
            and stat.S_IMODE(info.st_mode) in (vendor['mode'], 0o777),
            'Unsupported vendor fixture type/mode/links')
    require(directory or sha == vendor['sha256'], 'Vendor fixture payload differs from signed archive')
    return stat.S_IMODE(info.st_mode) != vendor['mode']


def vendor_fixture_selection(vendor, roots):
    selected = set()
    for root in roots:
        p = Path(root)
        require(not p.is_symlink(), 'Aliased ImageMagick fixture directory')
        expected = {name for name in vendor if str(Path(name).parent) == root}
        actual = {str(path) for path in p.iterdir()} if p.is_dir() else set()
        if root == roots[-1]:
            expected = {name for name in expected if name.endswith('.xml')}
            actual = {name for name in actual if name.endswith('.xml')}
        if root not in vendor and not os.path.lexists(p):
            continue
        require(root in vendor and vendor[root]['kind'] == 'directory' and expected == actual,
                'ImageMagick fixture tree differs from signed package: ' + root)
        selected.update((root, *expected))
        selected.update(str(parent) for parent in p.parents if str(parent) in vendor)
    return selected


def stock_imagemagick_metadata(private, report, command, arch):
    """Exact signed package inputs only; restore every original inode/mode later."""
    hosted_profile()
    triplet = {'amd64': 'x86_64-linux-gnu', 'arm64': 'aarch64-linux-gnu'}[arch]
    roots = ('/etc/ImageMagick-6', '/usr/share/ImageMagick-6',
             '/usr/lib/' + triplet + '/ImageMagick-6.9.12/config-Q16',
             '/usr/share/doc/imagemagick-6-common/html')
    php_roots = ('/etc/php/8.4/cli', '/etc/php/8.4/cli/conf.d', '/etc/php/8.4/mods-available',
                 '/usr/lib/php/20240924', '/etc/ld.so.conf.d')
    inventory = {}
    original_metadata = {}
    # Capture every direct effective input and its ancestors before preparation,
    # including PHP's own symlinked INI selections. This inventory grants no trust.
    paths = set()
    for root in (*roots, *php_roots):
        p = Path(root)
        paths.update(p.parents)
        paths.add(p)
        if os.path.lexists(p):
            original_metadata[str(p)] = p.lstat()
        if p.is_dir() and not p.is_symlink():
            paths.update(p.iterdir())
    paths.update(Path(p) for p in ('/usr/bin/php', '/etc/alternatives/php', '/usr/bin/php8.4',
                                  '/etc/ld.so.cache', '/etc/ld.so.conf'))
    for pattern in ('libMagickCore-6.Q16.so*', 'libMagickWand-6.Q16.so*'):
        paths.update(Path('/usr/lib/' + triplet).glob(pattern))
    for path in sorted(paths):
        if not os.path.lexists(path):
            inventory[str(path)] = {'missing': True}
            continue
        info = path.lstat()
        info = original_metadata.setdefault(str(path), info)
        item = {'uid': info.st_uid, 'gid': info.st_gid, 'mode': oct(stat.S_IMODE(info.st_mode)),
                'type': stat.S_IFMT(info.st_mode), 'links': info.st_nlink}
        if stat.S_ISLNK(info.st_mode):
            item['target'] = os.readlink(path)
        elif stat.S_ISREG(info.st_mode) and info.st_size <= 8 << 20:
            item['sha256'] = digest(path)
        inventory[str(path)] = item
    report['runtime_input_metadata_before'] = inventory
    # Generated PHP selection links legitimately have no package file-list row.
    owners = subprocess.run(['/usr/bin/dpkg-query', '-S',
        *[str(p) for p in sorted(paths) if os.path.lexists(p) and str(p) != '/']],
        env=CLEAN, stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=60)
    require(owners.returncode in (0, 1), 'Runtime input package inventory failed')
    report['runtime_input_package_owners'] = owners.stdout.splitlines()
    report['runtime_input_unowned_paths'] = owners.stderr.splitlines()
    packages = ['imagemagick-6-common', 'libmagickcore-6.q16-7t64', 'libmagickwand-6.q16-7t64']
    records = validate_records(json.loads(command('imagick-vendor-records',
        ['/usr/bin/python3', '-I', '-c', APT_RECORDS, 'installed', *packages])), arch)
    directory = private / 'imagemagick-vendor'
    directory.mkdir(mode=0o700)
    command('imagick-vendor-download', [*APT, 'download',
        *[name + '=' + row['version'] for name, row in records.items()]], 180, cwd=directory)
    archives = {}
    for path in directory.glob('*.deb'):
        fields = command('imagick-vendor-identity-' + str(len(archives)),
                         ['dpkg-deb', '-f', str(path), 'Package', 'Version', 'Architecture'])
        values = dict(line.split(': ', 1) for line in fields.splitlines())
        name = values['Package']
        require(name in records and name not in archives and digest(path) == records[name]['sha256']
                and values['Version'] == records[name]['version']
                and values['Architecture'] == records[name]['architecture'], 'Signed ImageMagick fixture archive differs')
        archives[name] = subprocess.check_output(['dpkg-deb', '--fsys-tarfile', str(path)], timeout=30)
    require(set(archives) == set(records), 'Missing signed ImageMagick fixture archive')
    vendor = vendor_fixture_entries(archives)
    selected = vendor_fixture_selection(vendor, roots)
    held = []
    changes = []
    report['imagemagick_vendor_fixture'] = {'signed_packages': records, 'inputs': changes,
        'content_replaced': False, 'ctime_restoration_possible': False, 'original_metadata_restored': False}
    def restore():
        errors = []
        for path, fd, before, changed in reversed(held):
            try:
                now = os.lstat(path)
                require((now.st_dev, now.st_ino) == (before.st_dev, before.st_ino), 'Vendor fixture inode changed')
                if changed:
                    os.fchmod(fd, stat.S_IMODE(before.st_mode))
                os.utime(fd, ns=(before.st_atime_ns, before.st_mtime_ns))
            except Exception as error:
                errors.append(error)
            finally:
                os.close(fd)
        held.clear()
        require(not errors, 'Vendor fixture original metadata restoration failed')
        report['imagemagick_vendor_fixture']['original_metadata_restored'] = True
    try:
        for name in sorted(selected):
            item = vendor[name]
            fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC |
                         (os.O_DIRECTORY if item['kind'] == 'directory' else 0))
            before = os.fstat(fd)
            original = original_metadata[name]
            held.append((name, fd, original, False))
            require((before.st_dev, before.st_ino, before.st_mode, before.st_uid, before.st_gid) ==
                    (original.st_dev, original.st_ino, original.st_mode, original.st_uid, original.st_gid),
                    'Vendor fixture inventory changed')
            raw = os.read(fd, 8 << 20) if item['kind'] == 'file' else None
            sha = hashlib.sha256(raw).hexdigest() if raw is not None else None
            changed = vendor_fixture_mode(before, item, sha)
            held[-1] = (name, fd, original, changed)
            changes.append({'path': name, 'package': item['package'], 'before_mode': oct(stat.S_IMODE(before.st_mode)),
                            'vendor_mode': oct(item['mode']), 'payload_verified': item['kind'] == 'file', 'changed': changed})
        # No mode changes until all bytes, types, ownership and tree membership pass.
        for name, fd, before, changed in held:
            require((os.lstat(name).st_dev, os.lstat(name).st_ino) == (before.st_dev, before.st_ino), 'Vendor fixture input changed')
            if changed:
                os.fchmod(fd, vendor[name]['mode'])
        return restore
    except Exception:
        restore()
        raise


def verify_archives(directory, records, command, configs):
    """Bind installed payload integrity manifests to actual signed native debs."""
    seen, checked_configs = set(), set()
    for path in sorted(directory.glob('*.deb')):
        fields = command('deb-identity-' + str(len(seen)), ['dpkg-deb', '-f', str(path), 'Package', 'Version', 'Architecture'])
        values = dict(line.split(': ', 1) for line in fields.splitlines())
        name = values['Package']
        key = name if name in records else name + ':' + values['Architecture']
        require(key in records and key not in seen, 'Unexpected/duplicate native archive')
        row = records[key]
        require(values['Version'] == row['version'] and values['Architecture'] == row['architecture']
                and digest(path) == row['sha256'], 'Signed archive bytes differ: ' + key)
        # Control metadata is not code; inspect without executing maintainer scripts.
        data = subprocess.check_output(['dpkg-deb', '--ctrl-tarfile', str(path)], timeout=30)
        with tarfile.open(fileobj=io.BytesIO(data), mode='r:') as archive:
            try:
                member = archive.getmember('./md5sums')
            except KeyError:
                member = None
            if member is not None:
                require(member.isfile() and member.size <= 8 << 20, 'Unsafe package manifest')
                installed = Path('/var/lib/dpkg/info') / (key + '.md5sums')
                # dpkg uses :arch for Multi-Arch packages even if a root was
                # selected by an unqualified name.
                if not installed.exists():
                    installed = Path('/var/lib/dpkg/info') / (name + ':' + row['architecture'] + '.md5sums')
                require(installed.read_bytes() == archive.extractfile(member).read(),
                        'Installed payload manifest differs from signed archive: ' + key)
        if name in ('imagemagick-6-common', 'libmagickcore-6.q16-7t64'):
            data = subprocess.check_output(['dpkg-deb', '--fsys-tarfile', str(path)], timeout=30)
            with tarfile.open(fileobj=io.BytesIO(data), mode='r:') as archive:
                for member in archive:
                    config = '/' + member.name.removeprefix('./')
                    if config in configs:
                        require(member.isfile() and member.size <= 8 << 20, 'Unsafe vendor configuration archive')
                        require(hashlib.sha256(archive.extractfile(member).read()).hexdigest() == configs[config],
                                'ImageMagick configuration differs from signed vendor archive')
                        checked_configs.add(config)
        seen.add(key)
    require(seen == set(records), 'Signed closure archive download incomplete')
    require(checked_configs == set(configs), 'Signed ImageMagick configuration coverage incomplete')
    return {'packages': len(seen), 'signed_archive_sha256_verified': True,
            'installed_payload_manifests_match_signed_archives': True,
            'signed_imagemagick_configurations': len(checked_configs)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--build-dir', type=Path, required=True)
    parser.add_argument('--output', type=Path)
    parser.add_argument('--record-build', action='store_true')
    args = parser.parse_args()
    if args.record_build:
        (args.build_dir / 'runtime-fixture.manifest.json').write_text(json.dumps(build_identity(args.build_dir), indent=2) + '\n')
        return 0
    arch = hosted_profile()
    require(args.output is not None, 'Report destination required')
    require(not any(os.path.lexists(p) for p in ('/var/lib/fluxo', '/usr/local/bin/fluxo', '/home/fluxo',
            '/var/lib/fluxo-scheduler', '/var/lib/fluxo-system-tasks')), 'Refuse existing installation')
    require(not any(Path('/etc/letsencrypt').glob('renewal/*.conf')), 'Refuse existing certificate lineages')
    args.output.mkdir(mode=0o755, parents=True, exist_ok=False)
    private = Path(tempfile.mkdtemp(prefix='fluxo-runtime-compat-', dir='/root'))
    private.chmod(0o700)
    report = {'passed': False, 'architecture': arch, 'ubuntu': '24.04', 'steps': {},
              'qualification_only': True, 'customer_hooks_verified': False,
              'acme_issuance_or_renewal': False, 'production_approved': False,
              'provider_scope': 'normal installer managed signed Sury Noble repository',
              'direct_packages_sury_org_native_provider_verified': False}
    env = dict(CLEAN, DEBIAN_FRONTEND='noninteractive', NEEDRESTART_MODE='l',
               FLUXO_ENV='prod', FLUXO_DATA_DIR='/var/lib/fluxo')

    def command(name, argv, timeout=120, cwd=None, command_env=None):
        report['stage'] = name
        print(json.dumps({'stage': name, 'status': 'started'}), flush=True)
        log = private / (name + '.log')
        began = time.monotonic()
        with log.open('x') as stream:
            log.chmod(0o600)
            child = subprocess.Popen(argv, env=command_env or env, cwd=cwd, stdin=subprocess.DEVNULL,
                                     stdout=stream, stderr=subprocess.STDOUT, start_new_session=True)
            try:
                code = child.wait(timeout=timeout)
            except subprocess.TimeoutExpired:
                os.killpg(child.pid, signal.SIGKILL)
                child.wait(timeout=10)
                code = 124
        report['steps'][name] = {'exit_code': code, 'private_log_sha256': digest(log),
                                 'duration_seconds': round(time.monotonic() - began, 3)}
        require(code == 0, 'Command failed: ' + name + '; exit=' + str(code))
        return log.read_text(errors='replace')

    fixture = HostedFixture(private, report, env, command, arch, '24.04', 'cron')
    key_restore = None
    imagemagick_restore = None
    certbot_timer_active = subprocess.run(['systemctl', 'is-active', '--quiet', 'certbot.timer'],
        stdin=subprocess.DEVNULL, timeout=10).returncode == 0
    try:
        report['candidate'] = verify_build(args.build_dir, arch)
        require(report['candidate']['go_version'] == 'go version go1.26.3 linux/' + arch, 'Exact Go1.26.3 required')
        identity = build_identity(args.build_dir)
        require(json.loads((args.build_dir / 'runtime-fixture.manifest.json').read_text()) == identity,
                'Exact test/source artifact record differs')
        report['fixture_artifacts'] = identity
        report['static_layout_review'] = {'sha256': digest('internal/services/maintenancetask/testdata/php_imagick_review.json'),
                                         'signed_native_proof': False}
        for name in ('fluxo', 'maintenance.test'):
            shutil.copyfile(args.build_dir / name, private / name)
            (private / name).chmod(0o700)
            require(digest(private / name) == digest(args.build_dir / name), 'Protected artifact copy differs')
        fixture.prepare()
        key_restore = stock_ubuntu_key_metadata(report, command)
        report['stage'] = 'preexisting-apt-trust-inventory'
        report['preexisting_apt_trust_hashes'] = trust_inventory()
        report['unused_hosted_key_metadata'] = unused_key_inventory(report['preexisting_apt_trust_hashes'])
        command('runtime-hostname', ['hostnamectl', 'set-hostname', 'fluxo-runtime-compat-' + arch])
        subject = public_bridge()
        report['public_predecessor'] = subject
        bridge_installer = private / 'public-bridge-install.sh'
        download_public_subject(subject, 'install.sh', bridge_installer, subject['installer_sha256'])
        env['FLUXO_VERSION'] = subject['public_tag']
        raw = command('public-bridge-install', ['bash', str(bridge_installer), *FLAGS], 1200)
        require('Fluxo release provenance verified' in raw and 'Skipping release attestation' not in raw,
                'Public bridge provenance was not verified')
        verify_installed(subject[arch + '_sha256'], env)
        command('bridge-nginx-start', ['systemctl', 'start', 'nginx.service'])
        observer = observer_module()
        original_api = observer.API
        class FixtureAPI(original_api):
            def request(self, method, path, body=None):
                report['stage'] = 'api ' + method + ' ' + path
                return super().request(method, path, body)
        observer.API = FixtureAPI
        owner_api = observer.API('https://127.0.0.1:9595')
        owner_api.login(False, 'runtime-profile-owner')
        report['predecessor_owner_claimed_through_normal_login'] = True
        seed = seed_application(observer, command, private)
        observer.wait_for(lambda: all(v['received'] for v in json.loads(
            Path('/var/lib/fluxo-system-tasks/state.json').read_text())['entries'].values() if v['enabled']),
            190, 'genuine public task acknowledgements')
        original = installation_invariants()
        def preserved_data():
            current = installation_invariants(original['columns'])
            for key in ('users', 'sites', 'crons', 'encryption.key', 'tls/cert.pem', 'tls/key.pem'):
                require(current[key] == original[key], 'Original data/key/TLS changed: ' + key)
            for path, sha in original['retained'].items():
                require(digest(path) == sha, 'Original recovery image/archive changed')
            return True
        await_predecessor_checks(Path('/var/lib/fluxo/fluxo.db'), report)
        env.pop('FLUXO_VERSION')
        env['FLUXO_LOCAL_BINARY_SHA256'] = report['candidate']['binary_sha256']
        installer = private / 'install.sh'
        shutil.copyfile('install.sh', installer)
        require(digest(installer) == report['candidate']['installer_sha256'], 'Installer copy differs')
        raw = command('automatic-base-handoff', ['bash', str(installer), '--local-binary=' + str(private / 'fluxo'), *FLAGS], 1200)
        require('Completed bridge verified. Continuing to the base automatically.' in raw,
                'Expected automatic authentic bridge handoff')
        require(digest('/usr/local/bin/fluxo') == report['candidate']['binary_sha256'], 'Installed candidate differs')
        report['initial_handoff_data_preserved'] = preserved_data()
        current_helper = private / report['candidate']['binary_sha256']
        shutil.copyfile(private / 'fluxo', current_helper)
        current_helper.chmod(0o700)
        command('stop-fixture-panel', ['systemctl', 'stop', 'fluxo.service'])
        command('pause-fixture-certbot', ['systemctl', 'stop', 'certbot.timer', 'certbot.service'])
        require(not any(Path('/etc/letsencrypt').glob('renewal/*.conf')), 'Fresh fixture unexpectedly has certificate lineages')
        command('nginx-start', ['systemctl', 'start', 'nginx.service'])
        trust = trust_inventory()
        report['managed_php_provider'] = managed_php_provider(arch)
        command('signed-index-refresh', [*APT, '-o', 'APT::Update::Error-Mode=any',
                '-o', 'Debug::Acquire::gpgv=true', 'update'], 240)
        candidate = validate_records(json.loads(command('imagick-candidate',
            ['/usr/bin/python3', '-I', '-c', APT_RECORDS, 'candidate', 'php8.4-imagick'])), arch, managed_sury=True)
        report['imagick_candidate'] = candidate
        validate_records(candidate, arch, candidate=True, managed_sury=True)
        command('install-imagick', [*APT, '--no-remove', '--no-install-recommends', 'install', '-y',
                                   'php8.4-imagick=' + candidate['php8.4-imagick']['version']], 300)
        command('install-tracer', [*APT, '--no-remove', '--no-install-recommends', 'install', '-y', 'strace'], 180)
        require(trust_inventory() == trust, 'APT source/key inputs changed while installing fixture')
        report['apt_source_key_hashes'] = trust
        unused_now = unused_key_inventory(trust)
        for name, metadata in report['unused_hosted_key_metadata'].items():
            require(unused_now.get(name) == metadata, 'Unused hosted key bytes/metadata changed')
        report['unused_hosted_keys_preserved'] = True
        imagemagick_restore = stock_imagemagick_metadata(private, report, command, arch)
        test_env = dict(CLEAN, **{key: os.environ[key] for key in (
            'FLUXO_RUNTIME_COMPAT_CANDIDATE', 'GITHUB_ACTIONS', 'RUNNER_ENVIRONMENT',
            'GITHUB_WORKFLOW', 'GITHUB_REF', 'GITHUB_SHA', 'RUNNER_ARCH')},
            FLUXO_RUNTIME_BINARY_SHA256=report['candidate']['binary_sha256'],
            FLUXO_RUNTIME_TEST_SHA256=identity['maintenance_test_sha256'],
            FLUXO_RUNTIME_PROFILE_OUTPUT=str(private / 'profiles.json'),
            FLUXO_RUNTIME_CURRENT_HELPER=str(current_helper),
            FLUXO_RUNTIME_PREDECESSOR_SHA256=subject[arch + '_sha256'])
        native_result(command('native-qualification', [str(private / 'maintenance.test'),
            '-test.v', '-test.count=1', '-test.timeout=5m', '-test.run=^' + TEST + '$'], 330, command_env=test_env))
        report['native_checks'] = {'default_php84_imagick_startup_profile': True,
            'synthetic_hook_families': ['renewed-lineage-exact', 'renewed-domains-exact', 'renewed-domains-member'],
            'php_ini_and_imagemagick_config_drift_refused': True,
            'writable_and_extra_command_hooks_refused': True, 'hook_and_nginx_profile_drift_refused': True,
            'frozen_v1_producers_refuse_new_inputs': True, 'restored_profiles_match': True}
        profiles = json.loads((private / 'profiles.json').read_text())
        report['old_published_helper'] = {'release': subject['public_tag'],
            'image_sha256': subject[arch + '_sha256'], 'actual_acknowledged_entries_verified': True,
            'new_profile_refused_before_submission': True, 'normal_installer_repair_reported': True,
            'entry_identity_nonce_and_existing_authority_preserved': True,
            'capable_candidate_helper_verified': True, 'automatic_refresh_or_execution_claimed': False}
        expected = {}
        for p in profiles.values():
            for key, value in p['external'].items():
                if key.startswith('package:'):
                    name = key[len('package:'):]
                    require(name not in expected or expected[name] == value, 'Incoherent package profiles')
                    expected[name] = value
        records = validate_records(json.loads(command('signed-closure',
            ['/usr/bin/python3', '-I', '-c', APT_RECORDS, 'installed', *sorted(expected)])), arch, expected, managed_sury=True)
        report['signed_package_closure'] = records
        debs = private / 'debs'
        debs.mkdir(mode=0o700)
        command('signed-closure-download', [*APT, 'download', *[name + '=' + row['version'] for name, row in records.items()]], 600, cwd=debs)
        configs = {path: sha for path, sha in profiles['php']['files'].items() if path.startswith('/etc/ImageMagick-6/')}
        require(configs, 'ImageMagick vendor configuration evidence missing')
        report['vendor_payload'] = verify_archives(debs, records, command, configs)
        report['direct_packages_sury_org_native_provider_verified'] = True
        trace = private / 'php-startup.strace'
        command('php-startup-only', ['/usr/bin/strace', '-f', '-s', '4096', '-e',
            'trace=%process,%file', '-o', str(trace), '/usr/bin/php', '-r',
            PHP_STARTUP], 30, cwd='/', command_env=CLEAN)
        trace.chmod(0o600)
        report['startup_trace'] = trace_oracle(trace.read_text())
        report['startup_trace']['private_trace_sha256'] = digest(trace)
        report['profile_hashes'] = {key: hashlib.sha256(json.dumps(p, sort_keys=True).encode()).hexdigest()
                                    for key, p in profiles.items()}
        core_path = Path('/var/lib/fluxo-system-tasks/state.json')
        original_core = json.loads(core_path.read_text())
        original_floor = json.loads(Path('/var/lib/fluxo/execution-readers.json').read_text())
        hook = Path('/etc/letsencrypt/renewal-hooks/deploy/50-runtime-compat-nginx')
        require(not os.path.lexists(hook), 'Synthetic hook from profile inspection was not removed')
        created_dirs = []
        for directory in (hook.parent.parent, hook.parent):
            if not directory.exists():
                directory.mkdir(mode=0o755)
                created_dirs.append(directory)
        try:
            with hook.open('x') as stream:
                stream.write('#!/bin/sh\nset -eu\ncase "$RENEWED_LINEAGE" in\n'
                             '/etc/letsencrypt/live/runtime-compat.invalid)\n'
                             '/usr/sbin/nginx -t && /usr/bin/systemctl reload nginx\n;;\nesac\n')
            hook.chmod(0o750)
            hook_sha = digest(hook)
            command('ordinary-update-automatic-helper-refresh', ['bash', str(installer),
                    '--local-binary=' + str(private / 'fluxo'), *FLAGS], 1200)
            report['post_refresh_running'] = verify_installed(report['candidate']['binary_sha256'], env)
            require(digest(hook) == hook_sha, 'Normal update changed the operator hook')
            current_floor = json.loads(Path('/var/lib/fluxo/execution-readers.json').read_text())
            operation = current_floor['activation_id']
            require(str(uuid.UUID(operation)) == operation and operation != original_floor['activation_id'],
                    'Normal update did not publish an archived helper transition')
            plan_path = Path('/var/lib/fluxo/execution-activations') / operation / 'plan.json'
            plan = json.loads(plan_path.read_text())
            require(plan['kind'] == 'task-helper-refresh' and current_floor['phase'] == 'committed',
                    'Expected committed archived helper refresh')
            for key in ('scheduler_schema', 'system_task_schema', 'role_contracts'):
                require(current_floor[key] == original_floor[key], 'Helper refresh changed reader semantics')
            current_core = json.loads(core_path.read_text())
            require(set(current_core['entries']) == set(original_core['entries']), 'Task registrations changed')
            refreshed = []
            for task_id, before in original_core['entries'].items():
                after = current_core['entries'][task_id]
                if not before['enabled']:
                    require(after == before, 'Disabled task changed')
                    continue
                for key in ('contract', 'identity', 'database', 'enabled', 'predecessor', 'predecessor_absent'):
                    require(after[key] == before[key], 'Task intent/identity changed')
                require(Path(after['helper']).name == report['candidate']['binary_sha256']
                        and digest(after['helper']) == report['candidate']['binary_sha256']
                        and after['nonce'] != before['nonce'] and after['received'] is True
                        and after['published'] is True, 'New helper lacks genuine completed publication')
                require(after['refresh_from']['helper'] == before['helper']
                        and digest(before['helper']) == Path(before['helper']).name,
                        'Original helper lineage was not preserved')
                refreshed.append(task_id)
            native_result(command('native-refreshed-profile-qualification', [str(private / 'maintenance.test'),
                '-test.v', '-test.count=1', '-test.timeout=5m',
                '-test.run=^TestCINativeAutomaticHelperRefresh$'], 330, command_env=test_env),
                'TestCINativeAutomaticHelperRefresh')
            report['automatic_helper_refresh'] = {'normal_installer_only': True,
                'no_manual_certification_or_refresh_cli': True, 'new_genuine_acknowledgements': True,
                'same_reader_contracts': True, 'original_helpers_retained': True,
                'data_key_tls_preserved': preserved_data(), 'task_count': len(refreshed),
                'plan_sha256': digest(plan_path), 'operation_id': operation,
                'site_id_preserved': seed['site_id'], 'hook_preserved': True,
                'qualified_actual_new_helper_and_current_profiles': True, 'task_execution_claimed': False}
        finally:
            if hook.is_file():
                hook.unlink()
            for directory in reversed(created_dirs):
                directory.rmdir()
        report['passed'] = True
    except Exception as error:
        report['error_type'] = type(error).__name__
        # Only our own static refusal messages are public. No subprocess output,
        # exception traceback, profile, or ambient environment leaves /root.
        if isinstance(error, RuntimeError):
            report['refusal'] = str(error)
        report['passed'] = False
    finally:
        failed_log = private / (report.get('stage', '') + '.log')
        if not report['passed'] and failed_log.is_file():
            failure_text = failed_log.read_text(errors='replace')
            report['failure_signals'] = failure_signals(failure_text)
            # The same existing redactor used by native base acceptance knows
            # protected bootstrap secrets; only a bounded sanitized tail leaves.
            (args.output / 'failure-sanitized.log').write_text(sanitized_diagnostic(failure_text)[-16000:])
        try:
            if imagemagick_restore is not None:
                imagemagick_restore()
        except Exception:
            report['passed'] = False
            report['imagemagick_fixture_restore_failed'] = True
        try:
            if key_restore is not None:
                key_restore()
        except Exception:
            report['passed'] = False
            report['ubuntu_key_restore_failed'] = True
        try:
            fixture.cleanup()
            if any(Path(p).exists() for p in ('/usr/lib/systemd/system/certbot.timer', '/lib/systemd/system/certbot.timer')):
                require(subprocess.run(['systemctl', 'start' if certbot_timer_active else 'stop', 'certbot.timer'],
                                       timeout=30).returncode == 0,
                        'Certbot timer state failed restoration')
        except Exception:
            report['passed'] = False
            report['fixture_cleanup_passed'] = False
        (args.output / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
        print(json.dumps({key: report.get(key) for key in ('passed', 'stage', 'error_type', 'refusal', 'fixture_cleanup_passed')}))
    return 0 if report['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
