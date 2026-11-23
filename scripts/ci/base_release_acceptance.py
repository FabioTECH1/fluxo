#!/usr/bin/env python3
"""Three bounded PUBLIC base update cases on disposable native Noble runners.

The isolated test/base-public-release-acceptance branch must first commit an
exact, independently reviewed post-publication subject JSON. No subject is
shipped before publication. --subject has schema_version=1 and releases with
exactly base, bridge, bootstrap, old_base. Each release has tag, source, release_id, assets;
each of the eight asset entries has id, size, sha256. Unknown fields are refused.
All identities are checked again against public immutable metadata, downloaded
bytes, source, and signed hosted release-workflow provenance before execution.
Fixture source and signed release source are separate report identities.

Run only via base-release-acceptance.yml. Raw output, credentials, profiles,
private keys and snapshots stay under /root. Only report.json and the explicit
verified public release-asset relay are publishable.
No local binary, digest override, attestation bypass, existing-job deletion, authority
repair, optional runtime matrix, fault injection or promotion is performed.
"""
import argparse
import ast
import hashlib
import json
import os
from pathlib import Path
import platform
import pwd
import re
import shlex
import shutil
import ssl
import signal
import socket
import sqlite3
import stat
import subprocess
import tarfile
import tempfile
import time
import traceback
import urllib.error
import urllib.request

import base_candidate as base
import base_release_feature_checks as feature_checks
from arm64_candidate import await_predecessor_checks, download_public_subject, optional_runtime_fixture
import node_preservation_candidate as node
from node_preservation_candidate import logged_process
from runtime_compat_candidate import packaged_ubuntu_key_needs_stock_mode

REPO = 'FabioTECH1/fluxo'
BRANCH = 'refs/heads/test/base-public-release-acceptance'
WORKFLOW = 'Native public base release acceptance'
TAGS = {'base': 'v0.4.56', 'bridge': 'v0.4.55',
        'bootstrap': 'v0.4.54-bridge-test.6', 'old_base': 'v0.4.55-base-test.3'}
PINNED_SOURCES = {'bridge': 'e5e27a7925e315150ff43987d474b647613aabfa',
                  'bootstrap': '8d14ea5344b6d61d5572127075f5171d518c3329',
                  'old_base': 'fd950fe0cac44f66fa360deaac3655b98aadc29f'}
PINNED_IMAGES = {
    'bridge': {'amd64': '4be2cf6e84a9c0412f313721dedf432d227a2a1f1b17197639c0c1c6f7d4a418',
               'arm64': '0e9f87f7d240afbdcbe7f5f3b2506d5e097ccf1dc092e34ed67968e1466eb02c'},
    'bootstrap': {'amd64': 'b4224628aa2263036ea2c116c901cf8f1c598cf6610319d7b48b38727605d178',
                  'arm64': 'f80236f444b2464db311e9020cbf5574e33d21cd69f78290eb867fc73758cbca'},
    'old_base': {'amd64': 'd5f8def30efb9af232c602fbd5315a730016717296cb8dd22bbc2a4687c9373a',
                 'arm64': '7deaa2ab5080d006ca50aa7c77cd1d91dbf921a191fa97ee9ad894a6dba17adf'}}
PREVIEW_ASSIGNMENT = 'fluxo/internal/bridge.PreviewBridgeAdmissionTag'
OPTIONAL_V1 = frozenset(('imagick-3.8.1-im6-q16-composer-startup-v1', 'nginx-conditional-v1'))
OPTIONAL_V2 = OPTIONAL_V1 | {'nginx-conditional-v2'}
OPTIONAL_MANUAL = OPTIONAL_V2 | {'certbot-manual-nohooks-v1'}
ASSETS = frozenset(('fluxo-linux-amd64', 'fluxo-linux-arm64', 'install.sh',
    'BRIDGE-CONTRACT.json', 'TOOLCHAIN-VERSIONS', 'SHA256SUMS',
    'fluxo-release-attestation.json', 'LICENSE'))
SIGNED = ('fluxo-linux-amd64', 'fluxo-linux-arm64', 'install.sh', 'BRIDGE-CONTRACT.json')
TOOLCHAIN_LABELS = ('Node.js', 'Node.js amd64 SHA-256', 'Node.js arm64 SHA-256',
    'Corepack', 'Corepack integrity', 'pnpm', 'pnpm integrity', 'Yarn', 'Yarn integrity',
    'Bun', 'Bun amd64 SHA-256', 'Bun amd64 baseline SHA-256', 'Bun arm64 SHA-256',
    'uv', 'uv amd64 SHA-256', 'uv arm64 SHA-256', 'Composer', 'Composer SHA-256',
    'WP-CLI', 'Attestation verifier (temporary GitHub CLI)')
CHECKSUMMED = ASSETS - {'SHA256SUMS', 'fluxo-release-attestation.json'}
CASES = {'amd64-bridge': ('amd64', 'bridge'), 'arm64-bridge': ('arm64', 'bridge'),
         'amd64-base': ('amd64', 'old_base')}
# Exact official assignment names, checked against each release's OWN pin file.
PIN_NAMES = {
    'nodetoolchain.PinnedNodeVersion': 'NODE_TOOLCHAIN_VERSION',
    'nodetoolchain.PinnedNodeAMD64SHA256': 'NODE_AMD64_SHA256',
    'nodetoolchain.PinnedNodeARM64SHA256': 'NODE_ARM64_SHA256',
    'nodetoolchain.PinnedCorepackVersion': 'COREPACK_VERSION',
    'nodetoolchain.PinnedCorepackIntegrity': 'COREPACK_INTEGRITY',
    'nodetoolchain.PinnedPNPMVersion': 'PNPM_VERSION',
    'nodetoolchain.PinnedPNPMIntegrity': 'PNPM_INTEGRITY',
    'nodetoolchain.PinnedYarnVersion': 'YARN_VERSION',
    'nodetoolchain.PinnedYarnIntegrity': 'YARN_INTEGRITY',
    'nodetoolchain.PinnedBunVersion': 'BUN_VERSION',
    'nodetoolchain.PinnedBunAMD64SHA256': 'BUN_AMD64_SHA256',
    'nodetoolchain.PinnedBunAMD64BaselineSHA256': 'BUN_AMD64_BASELINE_SHA256',
    'nodetoolchain.PinnedBunARM64SHA256': 'BUN_ARM64_SHA256',
    'pythontoolchain.PinnedUVVersion': 'UV_VERSION',
    'pythontoolchain.PinnedUVAMD64SHA256': 'UV_AMD64_SHA256',
    'pythontoolchain.PinnedUVARM64SHA256': 'UV_ARM64_SHA256',
    'releaseinfo.ComposerVersion': 'COMPOSER_VERSION',
    'releaseinfo.ComposerSHA256': 'COMPOSER_SHA256',
    'releaseinfo.WPCLIVersion': 'WP_CLI_VERSION',
}
require = base.require
_METADATA_TOKEN = None  # Transport-only memory; never inherited by a child.


def public_fixture_download_budget(name, argv, timeout):
    """Narrow cold-stock-index allowance, never an installer/trust override."""
    if name != 'stock-source-refresh':
        return argv, timeout
    expected = ['apt-get', '-o', 'APT::Update::Error-Mode=any',
                '-o', 'Acquire::http::No-Cache=true', '-o', 'Acquire::https::No-Cache=true', 'update']
    require(argv == expected and timeout == 180, 'Unknown stock APT fixture command')
    return [*argv[:-1], '-o', 'Acquire::Languages=none', '-o', 'Acquire::Retries=1',
            '-o', 'Acquire::http::Timeout=45', '-o', 'Acquire::https::Timeout=45', 'update'], 420


def exact_keys(value, keys, label):
    require(isinstance(value, dict) and set(value) == set(keys), 'Unexpected ' + label + ' fields')


def load_subject(path):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, 'Duplicate subject key')
            result[key] = value
        return result
    require(path.stat().st_size <= 32768 and not path.is_symlink(), 'Unsafe or oversized subject')
    value = json.loads(path.read_text(), object_pairs_hook=unique)
    exact_keys(value, ('schema_version', 'releases'), 'subject')
    require(type(value['schema_version']) is int and value['schema_version'] == 1, 'Unknown subject schema')
    exact_keys(value['releases'], TAGS, 'release set')
    for role, release in value['releases'].items():
        exact_keys(release, ('tag', 'source', 'release_id', 'assets'), 'release')
        require(release['tag'] == TAGS[role], 'Wrong exact acceptance tag')
        require(isinstance(release['source'], str) and re.fullmatch('[0-9a-f]{40}', release['source']), 'Invalid source')
        require(role not in PINNED_SOURCES or release['source'] == PINNED_SOURCES[role], 'Wrong exact predecessor source')
        require(type(release['release_id']) is int and release['release_id'] > 0, 'Invalid release id')
        exact_keys(release['assets'], ASSETS, 'asset set')
        ids = set()
        for asset in release['assets'].values():
            exact_keys(asset, ('id', 'size', 'sha256'), 'asset')
            require(type(asset['id']) is int and asset['id'] > 0 and asset['id'] not in ids, 'Invalid/duplicate asset id')
            ids.add(asset['id'])
            require(type(asset['size']) is int and 0 < asset['size'] <= 128 << 20, 'Invalid asset size')
            require(isinstance(asset['sha256'], str) and re.fullmatch('[0-9a-f]{64}', asset['sha256'])
                    and len(set(asset['sha256'])) > 1, 'Invalid asset digest')
        if role in PINNED_IMAGES:
            require(all(release['assets']['fluxo-linux-' + arch]['sha256'] == digest
                        for arch, digest in PINNED_IMAGES[role].items()), 'Wrong exact predecessor image')
    return value


def hosted_profile(case):
    expected = {'FLUXO_BASE_RELEASE_ACCEPTANCE': '1', 'GITHUB_ACTIONS': 'true',
                'RUNNER_ENVIRONMENT': 'github-hosted', 'GITHUB_WORKFLOW': WORKFLOW,
                'GITHUB_REF': BRANCH, 'GITHUB_REPOSITORY': REPO}
    require(os.geteuid() == 0 and all(os.environ.get(k) == v for k, v in expected.items()),
            'Explicit root isolated-branch disposable GitHub-hosted opt-in required')
    arch = {'x86_64': 'amd64', 'aarch64': 'arm64'}.get(platform.machine())
    require(arch == CASES[case][0] and os.environ.get('RUNNER_ARCH') == {'amd64': 'X64', 'arm64': 'ARM64'}[arch],
            'Native case architecture required')
    require(base.run(['dpkg', '--print-architecture'])['stdout'] == arch, 'Native dpkg architecture differs')
    release = dict(line.split('=', 1) for line in Path('/usr/lib/os-release').read_text().splitlines() if '=' in line)
    require(release.get('ID', '').strip('"') == 'ubuntu' and release.get('VERSION_ID', '').strip('"') == '24.04',
            'Only native Ubuntu 24.04 is supported')
    require(socket.gethostname() != 'fluxo-php-patch-test', 'Named VM is not a hosted fixture')
    require(all(re.fullmatch(r'[A-Za-z0-9_.-]{1,100}', os.environ.get(key, ''))
                for key in ('ImageOS', 'ImageVersion')), 'Exact hosted image metadata required')
    return arch


def require_named_fixture(arch):
    # HostedFixture changes the hostname after platform.machine() has populated
    # Python's uname cache. Read the kernel hostname for this exact-host guard.
    require(socket.gethostname() == base.HOSTS[arch, '24.04', 'cron'],
            'Original named native fixture host required')


def git(*args):
    return subprocess.check_output(['git', '-c', 'safe.directory=' + os.getcwd(), *args], timeout=30)


def source_file(source, path):
    return git('show', source + ':' + path)


def take_metadata_token():
    token = os.environ.pop('FLUXO_METADATA_TOKEN', None)
    require(not token or len(token) <= 4096 and all('!' <= char <= '~' for char in token),
            'Invalid read-only GitHub authentication environment')
    return token


class RejectMetadataRedirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, target):
        # Never copy Authorization to a redirected request, including a
        # same-host redirect. Public asset downloads use their unchanged path.
        if response is not None:
            response.close()
        raise urllib.error.HTTPError(request.full_url, code, 'GitHub metadata redirects are refused', headers, None)


def public_json(suffix):
    require(suffix == 'releases/latest' or suffix in {'releases/tags/' + tag for tag in TAGS.values()},
            'Only fixed public release metadata endpoints are allowed')
    headers = {'Accept': 'application/vnd.github+json', 'User-Agent': 'fluxo-public-base-acceptance'}
    if _METADATA_TOKEN:
        headers['Authorization'] = 'Bearer ' + _METADATA_TOKEN
    request = urllib.request.Request('https://api.github.com/repos/' + REPO + '/' + suffix,
        headers=headers)
    opener = urllib.request.build_opener(RejectMetadataRedirects())
    for attempt in range(2):
        try:
            with opener.open(request, timeout=60) as response:
                raw = response.read(2 << 20)
            break
        except urllib.error.HTTPError as error:
            if attempt or error.code not in (429, 500, 502, 503, 504):
                raise
            time.sleep(2)
    require(len(raw) < 2 << 20, 'Oversized public release metadata')
    return json.loads(raw)


def public_api_diagnostic(error):
    prefix = 'https://api.github.com/repos/' + REPO + '/'
    if not isinstance(error, urllib.error.HTTPError) or not error.url.startswith(prefix):
        return None
    result = {'http_status': error.code}
    headers = error.headers or {}
    for source, field in (('X-RateLimit-Remaining', 'rate_limit_remaining'), ('X-RateLimit-Reset', 'rate_limit_reset')):
        value = headers.get(source, '')
        if re.fullmatch(r'[0-9]{1,20}', value):
            result[field] = value
    resource = headers.get('X-RateLimit-Resource', '')
    if resource in ('core', 'search', 'graphql', 'code_search', 'integration_manifest'):
        result['rate_limit_resource'] = resource
    return result


def verify_metadata(subject, release):
    require(release.get('id') == subject['release_id'] and release.get('tag_name') == subject['tag']
            and release.get('draft') is False and release.get('immutable') is True
            and release.get('prerelease') is ('-' in subject['tag']), 'Public immutable release identity differs')
    assets = release.get('assets', [])
    require(len(assets) == 8 and {a['name'] for a in assets} == ASSETS, 'Public eight-asset set differs')
    for asset in assets:
        expected = subject['assets'][asset['name']]
        require(asset.get('id') == expected['id'] and asset.get('size') == expected['size']
                and asset.get('digest') == 'sha256:' + expected['sha256'] and asset.get('state') == 'uploaded'
                and asset.get('browser_download_url') == 'https://github.com/' + REPO + '/releases/download/'
                    + subject['tag'] + '/' + asset['name'], 'Public asset identity differs')


def parse_pins(raw):
    pins = {}
    for line in raw.decode().splitlines():
        if not line or line.startswith('#'):
            continue
        name, value = line.split('=', 1)
        values = shlex.split(value)
        require(name not in pins and len(values) == 1, 'Malformed official pin file')
        pins[name] = values[0]
    require(len(PIN_NAMES) == 19 and all(key in pins for key in PIN_NAMES.values())
            and 'ATTESTATION_GH_VERSION' in pins, 'Missing official pins')
    return pins


def official_pins(raw):
    pins = parse_pins(raw)
    return {('fluxo/internal/' + name if name.startswith('releaseinfo.') else
             'fluxo/internal/services/' + name): pins[key] for name, key in PIN_NAMES.items()}


def verify_binary(path, arch, subject, assignments, go, expected_go='1.26.3'):
    with path.open('rb') as stream:
        header = stream.read(64)
    require(len(header) == 64 and header[:6] == b'\x7fELF\x02\x01'
            and int.from_bytes(header[18:20], 'little') == {'amd64': 62, 'arm64': 183}[arch], 'Wrong Linux ELF architecture')
    metadata = base.run([go, 'version', '-m', str(path)], 30)
    require(metadata['exit_code'] == 0, 'Cannot read actual Go build information')
    require(metadata['stdout'].splitlines()[0].split()[-1] == 'go' + expected_go,
            'Public binary Go toolchain differs from its release source')
    settings = {}
    for line in metadata['stdout'].splitlines():
        if line.strip().startswith('build\t'):
            key, value = line.strip().split('\t', 1)[1].split('=', 1)
            require(key not in settings, 'Duplicate Go build setting')
            settings[key] = value
    require(settings.get('CGO_ENABLED') == '0' and settings.get('GOOS') == 'linux'
            and settings.get('GOARCH') == arch and settings.get('vcs.revision') == subject['source']
            and settings.get('vcs.modified') in ('true', 'false'), 'Actual binary source/CGo/architecture metadata differs')
    flags = shlex.split(settings.get('-ldflags', '').strip('"'))
    actual = {}
    for index, flag in enumerate(flags):
        if flag == '-X':
            key, value = flags[index + 1].split('=', 1)
            require(key not in actual, 'Duplicate release assignment')
            actual[key] = value
    expected_assignments = dict(assignments, **{'main.version': subject['tag'][1:]})
    if subject['tag'] == 'v0.4.54-base-test.1':
        expected_assignments[PREVIEW_ASSIGNMENT] = subject['tag']
    require(actual == expected_assignments, 'Exact official pin/version/preview assignments differ')
    # The signed official build writes generated root files before Go compilation;
    # report its real modified flag rather than requiring a development build's false.
    return {'sha256': base.digest(path), 'goarch': arch, 'cgo_enabled': False,
            'source': settings['vcs.revision'], 'vcs_modified': settings['vcs.modified'],
            'official_pin_assignments_checked': len(assignments)}


def pinned_verifier(subject, private, command):
    source = source_file(subject['source'], 'install.sh').decode()
    arch = {'x86_64': 'amd64', 'aarch64': 'arm64'}[platform.machine()]
    def assignment(name, pattern):
        matches = re.findall(r'^' + name + r'="(' + pattern + r')"$', source, re.M)
        require(len(matches) == 1, 'Missing/ambiguous source-pinned verifier assignment')
        return matches[0]
    version = assignment('ATTESTATION_GH_VERSION', r'[0-9]+\.[0-9]+\.[0-9]+')
    expected = assignment('ATTESTATION_GH_' + arch.upper() + '_SHA256', '[0-9a-f]{64}')
    directory = private / ('verifier-' + subject['tag'])
    directory.mkdir(mode=0o700)
    archive_name = 'gh_' + version + '_linux_' + arch
    archive = directory / (archive_name + '.tar.gz')
    url = 'https://github.com/cli/cli/releases/download/v' + version + '/' + archive.name
    started = time.monotonic()
    with urllib.request.urlopen(url, timeout=30) as response, archive.open('xb') as stream:
        total = 0
        while True:
            block = response.read(1 << 20)
            if not block:
                break
            total += len(block)
            require(total <= 64 << 20 and time.monotonic() - started < 180, 'Verifier download exceeds bounds')
            stream.write(block)
    require(base.digest(archive) == expected, 'Source-pinned verifier archive digest differs')
    path = directory / 'gh'
    with tarfile.open(archive, 'r:gz') as bundle:
        members = [m for m in bundle if m.name == archive_name + '/bin/gh']
        require(len(members) == 1 and members[0].isfile() and members[0].size <= 128 << 20,
                'Unsafe pinned verifier executable')
        with path.open('xb') as stream:
            stream.write(bundle.extractfile(members[0]).read())
    path.chmod(0o700)
    actual = command(subject['tag'] + '-verifier-version', [str(path), '--version'], 30)
    require(actual.startswith('gh version ' + version + ' ('), 'Pinned verifier version differs')
    return path, {'version': version, 'archive_sha256': expected, 'executable_sha256': base.digest(path)}


def verify_release(role, subject, private, command, go):
    require(git('rev-parse', 'refs/tags/' + subject['tag'] + '^{commit}').decode().strip() == subject['source'],
            'Exact fetched public tag source differs')
    metadata = public_json('releases/tags/' + subject['tag'])
    verify_metadata(subject, metadata)
    directory = private / role
    directory.mkdir(mode=0o700)
    for name, asset in subject['assets'].items():
        target = directory / name
        download_public_subject({'public_tag': subject['tag']}, name, target, asset['sha256'])
        require(target.stat().st_size == asset['size'], 'Downloaded asset size differs')
    verifier, verifier_proof = pinned_verifier(subject, private, command)
    for name in SIGNED:
        command(role + '-attest-' + name.replace('.', '-'), [str(verifier), 'attestation', 'verify', str(directory / name),
            '--repo', REPO, '--bundle', str(directory / 'fluxo-release-attestation.json'),
            '--signer-workflow', REPO + '/.github/workflows/release.yml', '--source-ref', 'refs/tags/' + subject['tag'],
            '--source-digest', subject['source'], '--deny-self-hosted-runners'], 120)
    checksums = {}
    for line in (directory / 'SHA256SUMS').read_text().splitlines():
        match = re.fullmatch(r'([0-9a-f]{64})  ([A-Za-z0-9_.-]+)', line)
        require(match and match[2] not in checksums, 'Malformed/duplicate release checksum')
        checksums[match[2]] = match[1]
    require(set(checksums) == CHECKSUMMED and all(checksums[n] == subject['assets'][n]['sha256'] for n in checksums),
            'Release checksum manifest differs')
    for name in ('install.sh', 'LICENSE'):
        require((directory / name).read_bytes() == source_file(subject['source'], name), 'Release asset differs from exact source: ' + name)
    require((directory / 'install.sh').read_bytes() == source_file(subject['source'], 'ui-site/public/install.sh'),
            'Published installer copies differ')
    pin_source = source_file(subject['source'], '.github/release-tool-versions.conf')
    pins = parse_pins(pin_source)
    expected_toolchain = '\n'.join(label + ' ' + pins[key] for label, key in
        zip(TOOLCHAIN_LABELS, (*PIN_NAMES.values(), 'ATTESTATION_GH_VERSION'))) + '\n'
    require((directory / 'TOOLCHAIN-VERSIONS').read_text() == expected_toolchain,
            'Public toolchain manifest differs from official source pins')
    assignments = official_pins(pin_source)
    expected_go = next(line.split()[1] for line in source_file(subject['source'], 'go.mod').decode().splitlines()
                       if line.startswith('go '))
    binaries = {arch: verify_binary(directory / ('fluxo-linux-' + arch), arch, subject, assignments, go, expected_go)
                for arch in ('amd64', 'arm64')}
    # Byte identity is not inferred from metadata or SHA256SUMS alone: signed
    # subjects were authenticated above, and all eight pinned asset bytes checked.
    return {'tag': subject['tag'], 'source': subject['source'], 'release_id': subject['release_id'],
            'immutable': True, 'assets': subject['assets'], 'signed_subjects_verified': list(SIGNED),
            'source_identical_installer': True, 'pinned_verifier': verifier_proof, 'binaries': binaries}


def base_release_contract(contract, tag):
    require(isinstance(contract, dict) and contract.get('role') == 'base', 'Signed base role differs')
    if tag == 'v0.4.54-base-test.1':
        expected = {'base_tag': tag, 'bridge_tag': 'v0.4.54-bridge-test.7',
            'bridge_source': 'bec82832c07a5a7dbd64a318a965b3831edb00d5',
            'amd64_sha256': 'd3b10d64f15ee64326c9c63528c08749531d229ea967014210bb531d23cb00cd',
            'arm64_sha256': '0cb7b04e266848dd2c64976280cb6c7755e62aa37b175d8a02dae2cc444862a0'}
        require(contract.get('bridge_release') == 'v0.4.53' and contract.get('preview_bridge_admission') == expected,
                'Historical base-test.1 signed contract differs')
    else:
        require(tag in (TAGS['base'], TAGS['old_base']) and contract.get('bridge_release') == 'v0.4.55'
                and 'preview_bridge_admission' not in contract, 'Current stable-only bridge contract differs')


def export_public_assets(directory, output, subject, proof, fixture_source):
    """Atomic allowlist relay only after every public-byte/provenance check.

    Private verifier tools, logs, credentials and native snapshots are never
    copied. The result is independent of native success and binds its own source.
    """
    require(re.fullmatch('[0-9a-f]{40}', fixture_source), 'Invalid relay fixture source')
    exact_keys(proof, ('tag', 'source', 'release_id', 'immutable', 'assets', 'signed_subjects_verified',
        'source_identical_installer', 'pinned_verifier', 'binaries'), 'verified public proof')
    require(proof['tag'] == subject['tag'] and proof['source'] == subject['source']
            and proof['release_id'] == subject['release_id'] and proof['immutable'] is True
            and proof['assets'] == subject['assets'] and proof['source_identical_installer'] is True
            and proof['signed_subjects_verified'] == list(SIGNED)
            and set(proof['binaries']) == {'amd64', 'arm64'}
            and all(proof['binaries'][arch].get('sha256') == subject['assets']['fluxo-linux-' + arch]['sha256']
                    for arch in ('amd64', 'arm64')), 'Incomplete public relay verification')
    exact_keys(proof['pinned_verifier'], ('version', 'archive_sha256', 'executable_sha256'), 'public verifier proof')
    require(isinstance(proof['pinned_verifier']['version'], str)
            and re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+', proof['pinned_verifier']['version'])
            and all(re.fullmatch('[0-9a-f]{64}', proof['pinned_verifier'][key])
                    for key in ('archive_sha256', 'executable_sha256')), 'Invalid public verifier proof')
    for arch, binary in proof['binaries'].items():
        exact_keys(binary, ('sha256', 'goarch', 'cgo_enabled', 'source', 'vcs_modified',
                           'official_pin_assignments_checked'), 'public binary proof')
        require(binary['goarch'] == arch and binary['cgo_enabled'] is False
                and binary['source'] == subject['source'] and binary['vcs_modified'] in ('true', 'false')
                and binary['official_pin_assignments_checked'] == 19, 'Incomplete public binary proof')
    pending, target = output / '.public-assets.pending', output / 'public-assets'
    require(not pending.exists() and not target.exists(), 'Public relay already exists')
    pending.mkdir(mode=0o755)
    try:
        for name in sorted(ASSETS):
            path = directory / name
            info = path.lstat()
            require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1
                    and info.st_size == subject['assets'][name]['size']
                    and base.digest(path) == subject['assets'][name]['sha256'], 'Public relay bytes changed')
            destination = pending / name
            with path.open('rb') as source, destination.open('xb') as stream:
                shutil.copyfileobj(source, stream)
            require(base.digest(destination) == subject['assets'][name]['sha256'], 'Public relay copy differs')
            destination.chmod(0o644)
        result = {'schema_version': 1, 'fixture_source': fixture_source,
                  'public_assets_only': True, 'release': proof}
        verified = pending / 'verified-public-release.json'
        verified.write_text(json.dumps(result, indent=2) + '\n')
        verified.chmod(0o644)
        pending.rename(target)
    except BaseException:
        shutil.rmtree(pending)
        raise
    return {'verified_before_native_execution': True, 'asset_count': len(ASSETS),
            'both_architectures': True, 'private_files_exported': False}


def optional_profiles(capabilities, tag):
    expected = {'v0.4.52-base-test.2': frozenset(), 'v0.4.53': OPTIONAL_V1,
                'v0.4.53-base-test.1': OPTIONAL_V1,
                'v0.4.54-bridge-test.1': OPTIONAL_V2,
                'v0.4.54-bridge-test.2': OPTIONAL_MANUAL,
                'v0.4.54-bridge-test.6': OPTIONAL_MANUAL,
                'v0.4.54-bridge-test.7': OPTIONAL_MANUAL,
                'v0.4.54': OPTIONAL_MANUAL, 'v0.4.54-base-test.1': OPTIONAL_MANUAL,
                'v0.4.54-base-test.2': OPTIONAL_MANUAL, 'v0.4.54-base-test.4': OPTIONAL_MANUAL, 'v0.4.55': OPTIONAL_MANUAL,
                'v0.4.55-base-test.1': OPTIONAL_MANUAL, 'v0.4.55-base-test.2': OPTIONAL_MANUAL, 'v0.4.55-base-test.3': OPTIONAL_MANUAL,
                'v0.4.56': OPTIONAL_MANUAL}
    require(tag in expected and isinstance(capabilities, dict)
            and type(capabilities.get('format')) is int and capabilities['format'] == 1,
            'Exact public optional-capability subject required')
    roles = capabilities.get('role_contracts')
    require(roles is None or isinstance(roles, dict), 'Invalid public role contracts')
    features = (roles or {}).get('system-task-optional-profile', [])
    require(isinstance(features, list) and all(isinstance(v, str) for v in features)
            and len(features) == len(set(features)) and set(features) == expected[tag],
            'Public helper optional capabilities differ from their exact release')
    return sorted(features)


def installed(subject, arch, env, observer, directory):
    image = subject['assets']['fluxo-linux-' + arch]['sha256']
    result = base.verify_installed(image, env)
    version = base.run(['/usr/local/bin/fluxo', '--version'], 15, env)
    require(version['exit_code'] == 0 and version['stdout'] == 'fluxo version ' + subject['tag'][1:], 'Native release version differs')
    contract = base.run(['/usr/local/bin/fluxo', '--bridge-contract'], 15, env)
    require(contract['exit_code'] == 0 and json.loads(contract['stdout']) == json.loads((directory / 'BRIDGE-CONTRACT.json').read_text()),
            'Native release contract differs from signed subject')
    if subject['tag'] in (TAGS['base'], TAGS['old_base']):
        base_release_contract(json.loads(contract['stdout']), subject['tag'])
    capabilities = base.run(['/usr/local/bin/fluxo', '--execution-capabilities'], 30, env)
    require(capabilities['exit_code'] == 0, 'Native release capabilities unavailable')
    features = optional_profiles(json.loads(capabilities['stdout']), subject['tag'])
    api = observer.API('https://127.0.0.1:9595')
    require(api.request('GET', '/health') == {'status': 'ok'}
            and api.request('GET', '/version') == {'version': subject['tag'][1:]}, 'Healthy native TLS API/version required')
    # The served frontend must come from the exact signed installed image.
    with api.open.open('https://127.0.0.1:9595/', timeout=15) as response:
        page = response.read(1 << 20)
        require(response.status == 200 and b'<div id="app"' in page and b'/assets/' in page, 'Embedded UI shell missing')
    result.update(native_version=True, signed_contract_matches=True, healthy_tls_api=True,
                  embedded_ui_served=True, optional_profile_features=features)
    return result


def ubuntu_key_fixture(private, report, command):
    # Reuse the authenticated runtime fixture oracle without changing its guard
    # or borrowing that suite's opt-in. This caller has its OWN strict guard.
    path = Path('/usr/share/keyrings/ubuntu-archive-keyring.gpg')
    for parent in path.parents:
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o022, 'Unsafe key ancestor')
    before = path.lstat()
    raw = path.read_bytes()
    sha = hashlib.sha256(raw).hexdigest()
    version = command('ubuntu-keyring-version', ['dpkg-query', '-W', '-f=${Version}', 'ubuntu-keyring']).strip()
    change = packaged_ubuntu_key_needs_stock_mode(before, sha, version)
    (private / 'ubuntu-keyring.original').write_bytes(raw)
    report['ubuntu_key_fixture'] = {'sha256': sha, 'version': version, 'changed_mode_only': change,
                                    'original_mode': oct(stat.S_IMODE(before.st_mode)), 'restored': False}
    if change:
        path.chmod(0o644)
    def restore():
        now = path.lstat()
        require((now.st_dev, now.st_ino, now.st_uid, now.st_gid, now.st_nlink)
                == (before.st_dev, before.st_ino, 0, 0, 1) and base.digest(path) == sha,
                'Ubuntu key changed; refuse cleanup overwrite')
        path.chmod(stat.S_IMODE(before.st_mode))
        os.utime(path, ns=(before.st_atime_ns, before.st_mtime_ns))
        report['ubuntu_key_fixture']['restored'] = True
    return restore


def hosted_javascript_fixture(private, report):
    """Archive ambient hosted shims; never adopt them as managed payloads."""
    before = {}
    for name in node.HOSTED_SHIMS:
        path = Path('/usr/local/bin') / name
        if not os.path.lexists(path):
            continue
        info = path.lstat()
        require(stat.S_ISREG(info.st_mode) or stat.S_ISLNK(info.st_mode), 'Unsafe hosted JavaScript shim type')
        before[name] = {'uid': info.st_uid, 'gid': info.st_gid, 'mode': info.st_mode,
            'inode': info.st_ino, 'mtime_ns': info.st_mtime_ns,
            'link': os.readlink(path) if stat.S_ISLNK(info.st_mode) else None,
            'sha256': base.digest(path) if stat.S_ISREG(info.st_mode) else None}
    report['hosted_javascript_shims_archived'] = sorted(before)
    def restore():
        node.restore_runtime_fixture(private, report)
        for name, expected in before.items():
            path = Path('/usr/local/bin') / name
            info = path.lstat()
            actual = {'uid': info.st_uid, 'gid': info.st_gid, 'mode': info.st_mode,
                'inode': info.st_ino, 'mtime_ns': info.st_mtime_ns,
                'link': os.readlink(path) if stat.S_ISLNK(info.st_mode) else None,
                'sha256': base.digest(path) if stat.S_ISREG(info.st_mode) else None}
            require(actual == expected, 'Hosted JavaScript shim restoration differs')
        report['hosted_javascript_originals_restored'] = True
    try:
        for name in before:
            (Path('/usr/local/bin') / name).rename(private / ('hosted-shim-' + name))
    except BaseException:
        restore()
        raise
    return restore


def predecessor_installer_path(raw, tag):
    if 'Fluxo ' + tag[1:] + ' installed successfully!' in raw:
        require_public_install(raw)
        return 'normal_signed_full_predecessor_install'
    require('Installation compatibility setup is complete.' in raw,
            'Normal predecessor compatibility incomplete')
    return 'normal_same_version_compatibility_shortcut'


def existing_public_predecessor(private, subjects, arch, env, report, command):
    """Authentic existing installation, not the public cold-install route.

    The cold case remains the unchanged installer, including its normal Redis
    dependency. Here an existing stock PHP8.4 runtime has no optional Redis
    extension. The old normal installer alone creates execution authority/ACKs.
    """
    subject = subjects['bootstrap']
    directory = private / 'bootstrap'
    require(not os.path.lexists('/etc/systemd/system/fluxo.service'), 'Existing Fluxo service fixture')
    command('bootstrap-user', ['useradd', '--create-home', '--shell', '/bin/bash', 'fluxo'])
    image = directory / ('fluxo-linux-' + arch)
    with image.open('rb') as source, Path('/usr/local/bin/fluxo').open('xb') as target:
        shutil.copyfileobj(source, target)
    Path('/usr/local/bin/fluxo').chmod(0o755)
    require(base.digest('/usr/local/bin/fluxo') == subject['assets'][image.name]['sha256'],
            'Authentic existing fixture image differs')
    command('bootstrap-managed-repository', ['/usr/local/bin/fluxo', 'php-repository-setup'], 300)
    command('bootstrap-vendor-index', ['apt-get', '-o', 'APT::Update::Error-Mode=any', 'update'], 180)
    packages = ['nginx', 'certbot', 'ufw', 'fail2ban', 'git', 'curl', 'gnupg', 'ca-certificates', 'util-linux']
    packages += ['php8.4-' + component for component in
        ('fpm', 'cli', 'mysql', 'pgsql', 'sqlite3', 'curl', 'mbstring', 'xml', 'gd', 'zip', 'bcmath', 'intl')]
    command('bootstrap-vendor-runtime', ['apt-get', 'install', '-y', *packages], 360)
    command('bootstrap-cli-selection', ['update-alternatives', '--set', 'php', '/usr/bin/php8.4'])
    unit = Path('/etc/systemd/system/fluxo.service')
    with unit.open('x') as stream:
        stream.write('[Unit]\nDescription=Fluxo Management Daemon\nAfter=network.target\n\n'
            '[Service]\nExecStart=/usr/local/bin/fluxo\nRestart=always\nUser=root\n'
            'Environment=FLUXO_ENV=prod\nEnvironment=FLUXO_PORT=9595\nPrivateTmp=true\n'
            'RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX AF_NETLINK\n\n'
            '[Install]\nWantedBy=multi-user.target\n')
    unit.chmod(0o644)
    command('bootstrap-unit-reload', ['systemctl', 'daemon-reload'])
    command('bootstrap-start', ['systemctl', 'enable', '--now', 'fluxo.service'])
    end = time.monotonic() + 60
    while True:
        try:
            # Initial loopback version observation only; all later API reads
            # use the fixture's pinned protected panel certificate.
            with urllib.request.urlopen('https://127.0.0.1:9595/api/v1/version',
                    context=ssl._create_unverified_context(), timeout=2) as response:
                if json.load(response) == {'version': subject['tag'][1:]}:
                    break
        except (OSError, ValueError):
            pass
        require(time.monotonic() < end, 'Authentic existing predecessor failed healthy startup')
        time.sleep(.2)
    await_predecessor_checks(Path('/var/lib/fluxo/fluxo.db'), report)
    prior = env.get('FLUXO_VERSION')
    env['FLUXO_VERSION'] = subject['tag']
    try:
        raw = command('bootstrap-normal-current-compatibility', installer_command(directory / 'install.sh'), 700)
        installer_path = predecessor_installer_path(raw, subject['tag'])
        require(base.digest('/usr/local/bin/fluxo') == subject['assets'][image.name]['sha256'],
                'Normal predecessor installer changed the authenticated image')
    finally:
        if prior is None:
            env.pop('FLUXO_VERSION')
        else:
            env['FLUXO_VERSION'] = prior
    report['existing_predecessor_fixture'] = {'tag': subject['tag'], 'source': subject['source'],
        'image_sha256': subject['assets'][image.name]['sha256'],
        'all_eight_assets_and_four_attestations_verified': True,
        'stock_vendor_php84_without_optional_redis_extension': True,
        'normal_predecessor_installer_path': installer_path,
        'normal_same_version_installer_compatibility': installer_path == 'normal_same_version_compatibility_shortcut',
        'ordinary_public_fresh_install_claimed': False, 'manual_authority_or_ack_edits': False}


def node_snapshot(observer, subject):
    state = node.protected_state()
    return {'selection': node.selection(state), 'payload': node.payload_snapshot(),
        'versions': node.node_versions(observer, {'official_pin_assignments': official_pins(
            source_file(subject['source'], '.github/release-tool-versions.conf'))}),
        'receipt': node.receipt() if state.get('integrity') is not None else None}


def verified_node_preservation(raw, before, after, allow_enrollment):
    enrolled = before['receipt'] is None
    require(not enrolled or allow_enrollment, 'Ordinary update unexpectedly lost Node proof')
    node.reused_output(raw, enrolled=enrolled)
    for key in ('selection', 'payload', 'versions'):
        require(before[key] == after[key], 'Node preservation changed ' + key)
    require(after['receipt'] is not None, 'Node preservation produced no protected proof')
    if not enrolled:
        require(before['receipt'] == after['receipt'], 'Proven Node reuse changed its receipt')
    return {'actual_preserve_path': True, 'authenticated_enrollment': enrolled,
        'payload_bytes_metadata_links_and_versions_preserved': True,
        'protected_receipt_preserved': not enrolled, 'registry_install_prepare_invoked': False,
        'observation_method': 'signed preserve path; required artifact authentication only when enrolling; no repair output; independent payload/proof oracle, no packet trace',
        'versions': after['versions'], 'receipt_sha256': after['receipt']['sha256']}


# Fast, repeatable application fixture. Every normal cron invocation writes its
# own evidence; no once-only failure, synthetic ACK, manual helper or job deletion.
PHP = r'''<?php
if ($argc !== 2 || $argv[1] !== 'schedule:run') exit(90);
$group=trim(file_get_contents('/proc/self/cgroup'));
if (!preg_match('~0::/system.slice/(fluxo-scheduler-[0-9a-f-]{36}\.service)~', $group, $m)) exit(91);
$v=['unit'=>$m[1], 'status'=>file_get_contents('/proc/self/status'), 'exe'=>readlink('/proc/self/exe')];
$p=__DIR__.'/.public-base-observer/'.$m[1].'.json';
$f=fopen($p,'x'); if (!$f) exit(92);
fwrite($f,json_encode($v)); fflush($f); fsync($f); fclose($f);
usleep(3000000); // Bounded benign work permits inspection of the real worker.
'''


def seed_scheduler(observer, command):
    api = observer.API('https://127.0.0.1:9595')
    api.login(False, 'base-owner')
    site = api.request('POST', '/sites', {'domain': 'base-scheduler.invalid', 'app_type': 'laravel',
        'php_version': '8.4', 'web_root': '/public', 'deployment_strategy': 'standard', 'install_composer': False})
    app = Path('/home/fluxo/base-scheduler.invalid')
    require(site['path'] == str(app), 'Scheduler fixture site path differs')
    command('scheduler-payload', ['runuser', '-u', 'fluxo', '--', '/usr/bin/python3', '-c',
        'import pathlib,sys; p=pathlib.Path(sys.argv[1]); (p/".public-base-observer").mkdir(mode=0o700); '
        '(p/"artisan").write_text(sys.argv[2])', str(app), PHP])
    api.request('POST', '/sites/%d/crons' % site['id'], {'name': 'Retained public base Scheduler',
        'expression': '* * * * *', 'command': 'artisan schedule:run', 'user': 'fluxo'})
    rows = api.request('GET', '/sites/%d/crons' % site['id'])
    require(len(rows) == 1, 'Unique retained Scheduler cron missing')
    return {'site_id': site['id'], 'cron_id': rows[0]['id'], 'app': app, 'observed': set()}


def scheduler_image_proof(invocation, current, first, worker, helper, dispatcher):
    # Retained registrations may keep their original signed public helper.
    # Neither a version string nor any other historical image authorizes it.
    approved = {current, first} if first is not None else {current}
    expected = invocation['helper_sha256']
    require(expected in approved and invocation['helper'] == '/var/lib/fluxo-scheduler/helpers/' + expected
            and worker == helper == expected and dispatcher in approved,
            'Scheduler worker/helper/dispatcher differs from authenticated public images')
    return {'helper_sha256': expected, 'dispatcher_sha256': dispatcher,
            'current_panel_sha256': current, 'predecessor_helper_retained': expected != current}


def scheduler_execution(observer, seed, image):
    host = socket.gethostname()
    require(os.geteuid() == 0 and host in observer.HOSTS
            and os.environ.get('FLUXO_PHP_UPDATE_VM_TEST') == '1', 'Original exact-host root Scheduler observer guard required')
    trigger = Path('/etc/cron.d/fluxo-cron-%d' % seed['cron_id'])
    account = pwd.getpwnam('fluxo')
    known = {p.stem for p in (seed['app'] / '.public-base-observer').glob('*.json')}
    image_proof = {}
    def completed():
        for path in sorted((seed['app'] / '.public-base-observer').glob('*.json')):
            if path.stem in known:
                continue
            value = json.loads(observer.payload_read(path, account.pw_uid))
            group = Path('/sys/fs/cgroup/system.slice') / value['unit']
            core = observer.core()
            invocation = core['invocations'].get(value['unit'])
            if invocation is None:
                history = Path('/var/lib/fluxo-scheduler/history') / (value['unit'] + '.json')
                invocation = json.loads(observer.protected_read(history)) if history.exists() else None
            require(invocation is not None and invocation['entry']['id'] == seed['cron_id'] and invocation['started']
                    and invocation['guarded'] and invocation['adapter'] == observer.HOSTS[host]
                    and invocation['execution_contract'] == 'scheduler-bootstrap-drop-exec-v1', 'Missing actual Scheduler execution contract')
            seal = invocation['bootstrap_seal']
            require(seal['phase'] == 'consumed' and seal['entry_identity'] == invocation['entry']['identity'], 'Scheduler one-use seal differs')
            worker = int(observer.show(value['unit'])['MainPID'])
            require(worker > 1 and worker == seal['pid'] and observer.start_key(worker) == seal['start'],
                    'Scheduler worker is not live or does not match its consumed seal')
            image_proof.update(scheduler_image_proof(invocation, image, seed.get('first_helper_image'),
                base.digest('/proc/%d/exe' % worker), base.digest(invocation['helper']),
                base.digest('/var/lib/fluxo-scheduler/dispatcher')))
            threads = list(Path('/proc/%d/task' % worker).iterdir())
            require(threads, 'Actual Scheduler worker threads missing')
            for thread in threads:
                observer.credentials(observer.status(worker, int(thread.name)), True)
            ledger = json.loads(observer.protected_read('/var/lib/fluxo-scheduler/loader-epochs.json'))
            epoch = ledger['epochs'][ledger['current']]
            target = epoch['targets'][str(seed['cron_id'])]
            require(not ledger['pending'] and ledger['current'] == seal['loader']['epoch_id']
                    and epoch['phase'] == 'published' and target['received']
                    and target['nonce'] == seal['loader']['nonce'] and target['binding'] == seal['loader']['binding']
                    and target['trigger_hash'] == base.digest(trigger), 'Genuine provider ACK differs')
            observer.credentials(dict(line.split(':', 1) for line in value['status'].splitlines() if ':' in line), False)
            require(value['exe'] == '/usr/bin/php8.4', 'Scheduler native interpreter differs')
            observer.wait_for(lambda: not group.exists() or ('populated 0' in (group / 'cgroup.events').read_text()
                and int((group / 'pids.current').read_text()) == 0), 20, 'retained Scheduler natural drainage')
            seed['observed'].add(value['unit'])
            return True
        return False
    observer.wait_for(completed, 190, 'retained real cron Scheduler ACK, payload and natural drainage')
    seed.setdefault('first_helper_image', image_proof['helper_sha256'])
    return {'genuine_cron': True, 'genuine_loader_ack': True, 'consumed_bootstrap_seal': True,
            'authenticated_public_dispatcher_helper_and_proc_worker': True, **image_proof,
            'unprivileged_php': True, 'natural_drain': True,
            'registered_job_retained': True}


def preservation_snapshot(seed, previous=None):
    value = base.capture_invariants(seed)
    # Additive migrations may add columns, but may never change an original
    # identity, credential, site, cron or deployment column's value.
    if previous is not None:
        original = base.installation_invariants(previous['columns'])
        # Project database columns only. The complete snapshot also carries
        # history receipts, which the lower-level installation helper does
        # not collect; replacing its retained map would lose that evidence.
        for key in ('columns', 'users', 'sites', 'crons'):
            value[key] = original[key]
    with sqlite3.connect('file:/var/lib/fluxo/fluxo.db?mode=ro', uri=True) as db:
        columns = [row[1] for row in db.execute('PRAGMA table_info(deployments)')]
        if previous is not None:
            require(set(previous['deployment_columns']).issubset(columns), 'Original deployment column lost')
            for name in ('started_at', 'finished_at'):
                if name in columns and name not in previous['deployment_columns']:
                    require(db.execute('SELECT COUNT(*) FROM deployments WHERE ' + name + ' IS NOT NULL').fetchone() == (0,),
                            'Installer invented historical deployment timing')
            columns = previous['deployment_columns']
        value['deployment_columns'] = columns
        projection = ','.join('"' + name.replace('"', '""') + '"' for name in columns)
        value['deployments'] = hashlib.sha256(repr(db.execute('SELECT ' + projection + ' FROM deployments ORDER BY id').fetchall()).encode()).hexdigest()
    value['fluxo_identity'] = [pwd.getpwnam('fluxo').pw_uid, pwd.getpwnam('fluxo').pw_gid]
    value['bootstrap_credentials'] = base.digest('/var/lib/fluxo/.fluxo_credentials')
    # Scheduler live invocations are legitimately swept after natural drainage;
    # immutable loader archives, retained helpers and activation history persist.
    for path in Path('/var/lib/fluxo-scheduler/loader-history').rglob('*'):
        if path.is_file() and not path.is_symlink():
            value['retained'][str(path)] = base.digest(path)
    return value


def compare_preservation(before, after, report, label):
    # Digest-only diagnostics from protected execution state; do not publish
    # database rows, profiles, credential bytes, or private installer output.
    changes = []
    for category in ('retained', 'root_history'):
        for path, expected in before[category].items():
            actual = after[category].get(path)
            if actual != expected:
                changes.append({'category': category, 'path': path,
                                'before_sha256': expected, 'after_sha256': actual})
    if changes:
        report[label + '_retained_mismatches'] = changes[:100]
        report[label + '_retained_mismatch_count'] = len(changes)
    return base.compare_invariants(before, after)


def installer_command(installer, reinstall=False, managed_node=False, remembered_node=False):
    require(not (managed_node and remembered_node), 'Ambiguous fixture Node selection intent')
    flags = ['--node' if flag == '--no-node' and managed_node else flag
             for flag in base.FLAGS if not (remembered_node and flag == '--no-node')]
    return ['bash', str(installer), *(['--reinstall'] if reinstall else []), *flags]


def require_public_install(raw):
    require('Fluxo release provenance verified' in raw and 'Skipping release attestation' not in raw
            and 'Release attestation verification was explicitly disabled' not in raw
            and 'already up to date' not in raw and ' installed successfully!' in raw,
            'Full normal signed public installer path required')


def retention_selection(state, site_id):
    site = next((row for row in state['sites'] if row['site_id'] == site_id), None)
    require(type(state.get('master_enabled')) is bool and site is not None
            and type(site.get('enabled')) is bool, 'Missing actual collection-selection evidence')
    return {'master_enabled': state['master_enabled'], 'site_enabled': site['enabled']}


def release_retention(observer, seed, role, previous):
    api = observer.API('https://127.0.0.1:9595')
    api.login(False, 'base-owner')
    if previous is not None:
        state = api.request('GET', '/traffic-analytics')
        selection = retention_selection(state, seed['site_id'])
        expected = {key: previous.get(key, True) for key in ('master_enabled', 'site_enabled')}
        require(state['retention_days'] == previous['retention_days'] and selection == expected,
                'Upgrade lost the old retention or collection selection')
        # Read the inherited aggregate before ANY origin request can repopulate
        # it. An intentionally disabled producer stays disabled; do not repair
        # the fixture's earlier explicit choice into an enabled state.
        data = api.request('GET', '/sites/%d/traffic-analytics?range=today' % seed['site_id'])
        require(data['totals']['requests'] >= previous['retained_requests'], 'Upgrade lost old analytics aggregates')
        if not selection['master_enabled'] or not selection['site_enabled']:
            return dict(previous, retained_requests=data['totals']['requests'], **selection,
                        manual_policy_survived_update=True, collection_state_preserved=True,
                        new_traffic_requested=False)
        result = base.retention_check(observer, seed) if previous['retention_days'] == 30 else \
            base.retention_check(observer, seed, previous)
    else:
        result = base.retention_check(observer, seed, previous)
    # The shared probe deliberately drains/disables collection after checking
    # an inherited policy. Snapshot that actual final choice for the NEXT update.
    state = api.request('GET', '/traffic-analytics')
    require(state['retention_days'] == result['retention_days'], 'Final retained policy observation differs')
    return dict(result, **retention_selection(state, seed['site_id']))


def release_application_checks(observer, seed, role, previous, command):
    # Preserve the inherited aggregate before any origin request can replace a
    # lost count. Panel API/health calls above use port 9595 and do not log site traffic.
    retention = release_retention(observer, seed, role, previous) if role != 'bridge' else previous
    application = base.application_checks(observer, seed, command, role)
    return retention, application


def installer_boundaries(version):
    from node_preservation_candidate import INSTALLER_BOUNDARIES
    # Only fixed labels/timestamps leave the private log. No xtrace or shell
    # wrapper is used, and variable output is never added to the report.
    return (('public_download', ('Downloading and validating Fluxo ',)),
        ('provenance_start', ('Verifying cryptographic build provenance for Fluxo ',)),
        ('provenance_verified', ('Fluxo release provenance verified against ',)),
        *[(name, prefixes) for name, prefixes in INSTALLER_BOUNDARIES if name != 'success'],
        ('scheduled_setup', ('Checking scheduled jobs...',)),
        ('success', ('Fluxo ' + version + ' installed successfully!',)))


class AptDiagnosticCompleted(Exception):
    pass


def apt_trace_diagnostics(directory, prefix='bridge-apt-trace'):
    result = []
    require(prefix in ('bridge-apt-trace', 'predecessor-apt-trace'), 'Unrecognized private APT trace prefix')
    for path in sorted(directory.glob(prefix + '.*')):
        if path.stat().st_size > 32 << 20:
            continue
        raw = path.read_text(errors='replace')
        if not re.search(r'execve\("/usr/bin/apt-get",', raw):
            continue
        chunks = []
        for line in raw.splitlines():
            match = re.search(r'write\(2, ("(?:[^"\\]|\\.)*"),', line)
            if not match:
                continue
            try:
                value = ast.literal_eval(match[1])
            except (ValueError, SyntaxError):
                continue
            chunks.append(value)
        for message in ''.join(chunks).splitlines():
            if not message.startswith(('E:', 'W:', 'Err:')):
                continue
            # Export no URL credentials, paths, queries or raw traces.
            # Only these known public repository hosts may remain visible.
            def redact_url(match):
                for host in ('packages.sury.org', 'ports.ubuntu.com', 'archive.ubuntu.com',
                             'security.ubuntu.com', 'ppa.launchpadcontent.net'):
                    if re.match(r'https?://' + re.escape(host) + r'(?=[:/]|$)', match[0]):
                        return '<repository:' + host + '>'
                return '<repository-url>'
            message = re.sub(r'https?://[^\s\'"<>]+', redact_url, message)
            message = base.sanitized_diagnostic(message)[:1000]
            if message and message not in result:
                result.append(message)
    return result[:20]


def predecessor_apt_observer(name, argv, private):
    """Observe the unchanged normal predecessor installer, never its inputs.

    Raw traces stay in the root-only disposable runner directory. Only bounded
    APT error/warning lines are eligible for the report, and only after failure.
    """
    if name != 'old_base-public-install':
        return argv, False
    require(os.access('/usr/bin/strace', os.X_OK), 'Private predecessor APT observer unavailable')
    return ['/usr/bin/strace', '-ff', '-qq', '-s', '4096', '-e', 'trace=execve,write',
            '-o', str(private / 'predecessor-apt-trace'), *argv], True


def stable_journal_observer(observer, pause=time.sleep):
    """Retry only typed snapshot churn in the three live atomic journals.

    Each attempt reopens the protected path and repeats all original checks.
    Immutable history, unsafe metadata, corruption and oversized reads refuse.
    """
    original = observer.protected_read
    journals = frozenset(('/var/lib/fluxo-system-tasks/state.json',
        '/var/lib/fluxo-scheduler/state.json', '/var/lib/fluxo-scheduler/loader-epochs.json'))
    def read(path, limit=32*1024*1024):
        for attempt in range(5):
            try:
                return original(path, limit)
            except observer.ProtectedSnapshotChanged:
                if str(path) not in journals or attempt == 4:
                    raise
                pause(.025)
    observer.protected_read = read


def command_environment(private):
    # Explicit allowlist; authentication is never inherited by verifier,
    # installer, fixture, or runtime subprocesses.
    return {'PATH': '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin', 'HOME': '/root',
        'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8', 'DEBIAN_FRONTEND': 'noninteractive', 'NEEDRESTART_MODE': 'l',
        'FLUXO_ENV': 'prod', 'FLUXO_DATA_DIR': '/var/lib/fluxo', 'FLUXO_PHP_UPDATE_VM_TEST': '1',
        'GH_CONFIG_DIR': str(private / 'gh-config'), 'GH_PROMPT_DISABLED': '1'}


def main():
    global _METADATA_TOKEN
    # Remove the dedicated secret before hosted_profile's first dpkg child,
    # any verifier/download process, or the explicit installer environment.
    _METADATA_TOKEN = take_metadata_token()
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--subject', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--case', choices=CASES, required=True)
    parser.add_argument('--go', required=True, help='Hosted exact Go executable for reading public build metadata only')
    parser.add_argument('--presentation-only', action='store_true', help='Verify and relay public assets only; no installation or host preparation')
    parser.add_argument('--apt-diagnostic', action='store_true', help='Private external tracing; not public base acceptance')
    args = parser.parse_args()
    arch = hosted_profile(args.case)
    require(not args.apt_diagnostic, 'No additional APT diagnostic is authorized in this update-only fixture')
    subjects = load_subject(args.subject)['releases']
    require(not git('status', '--porcelain', '--untracked-files=all').strip(), 'Entire fixture checkout must be committed and clean')
    require(git('rev-parse', 'HEAD').decode().strip() == os.environ.get('GITHUB_SHA'), 'Fixture workflow source differs')
    require(not any(os.path.lexists(p) for p in ('/var/lib/fluxo', '/usr/local/bin/fluxo', '/home/fluxo',
        '/var/lib/fluxo-scheduler', '/var/lib/fluxo-system-tasks', '/opt/fluxo')), 'Refuse existing installation')
    require(not list(Path('/etc/letsencrypt/renewal').glob('*.conf')), 'Refuse live certificates')
    args.output.mkdir(mode=0o755, parents=True, exist_ok=False)
    private = Path(tempfile.mkdtemp(prefix='fluxo-public-base-', dir='/root'))
    private.chmod(0o700)
    report = {'passed': False, 'case': args.case, 'architecture': arch, 'ubuntu': '24.04',
        'fixture_source': os.environ['GITHUB_SHA'], 'subject_sha256': base.digest(args.subject),
        'runner_image': {k: os.environ[k] for k in ('ImageOS', 'ImageVersion')},
        'production_approved': False, 'promotion_performed': False, 'steps': {}, 'releases': {},
        'scope': f"authentic public {TAGS[CASES[args.case][1]]} -> {TAGS['base']} -> ordinary base update",
        'node_preservation_required': args.case == 'amd64-bridge'}
    env = command_environment(private)
    diagnostic_secrets = (_METADATA_TOKEN,) if _METADATA_TOKEN else ()
    def command(name, argv, timeout=120, cwd=None):
        argv, timeout = public_fixture_download_budget(name, argv, timeout)
        report['stage'] = name
        print(json.dumps({'stage': name, 'status': 'started'}), flush=True)
        start, boundaries = time.monotonic(), []
        log = private / (name + '.log')
        if args.apt_diagnostic and name == 'bridge-public-install':
            require(os.access('/usr/bin/strace', os.X_OK), 'Diagnostic tracer unavailable')
            argv = ['/usr/bin/strace', '-ff', '-qq', '-s', '4096', '-e', 'trace=execve,write',
                    '-o', str(private / 'bridge-apt-trace'), *argv]
        argv, predecessor_observed = predecessor_apt_observer(name, argv, private)
        code = logged_process(argv, env, cwd, log, timeout, start, boundaries,
            installer_boundaries(env.get('FLUXO_VERSION', '')[1:]))
        duration = round(time.monotonic() - start, 6)
        points = [{'boundary': 'start', 'seconds': 0}, *boundaries, {'boundary': 'exit', 'seconds': duration}]
        measured_installer = 'install' in name or 'update' in name
        report['steps'][name] = {'exit_code': code, 'duration_seconds': duration,
            'stdout_phase_observations': points if measured_installer else [],
            'stdout_phase_spans': [{'from': left['boundary'], 'to': right['boundary'],
                'duration_seconds': round(right['seconds'] - left['seconds'], 6)}
                for left, right in zip(points, points[1:])] if measured_installer else [],
            'timing_method': 'external monotonic stdout arrival; unchanged installer, no command/function instrumentation'}
        if predecessor_observed:
            report['steps'][name]['timing_method'] += '; private external syscall observer adds overhead'
            report['predecessor_preparation_observer'] = {
                'external_only': True, 'installer_inputs_unchanged': True,
                'raw_trace_exported': False, 'stderr_exported_only_on_failure': True}
            if code != 0:
                report['predecessor_apt_stderr_diagnostic'] = apt_trace_diagnostics(private, 'predecessor-apt-trace')
        require(code == 0, 'Command failed: ' + name + '; exit=' + str(code))
        return log.read_text(errors='replace')
    fixture = base.HostedFixture(private, report, env, command, arch, '24.04', 'cron')
    key_restore, javascript_restore = None, None
    def deadline(_signum, _frame):
        raise TimeoutError('Public acceptance time budget exhausted')
    signal.signal(signal.SIGALRM, deadline)
    signal.setitimer(signal.ITIMER_REAL, 2700)
    try:
        require(public_json('releases/latest')['tag_name'] != TAGS['base'], 'Opt-in base must remain off latest')
        predecessor = CASES[args.case][1]
        managed_node = args.case == 'amd64-bridge'
        verification_roles = ['base'] if args.presentation_only else (['base', 'bootstrap', 'bridge'] if predecessor == 'bridge' else ['base', 'old_base'])
        for role in verification_roles:
            subject = subjects[role]
            report['releases'][role] = verify_release(role, subject, private, command, args.go)
            if role == 'base':
                base_release_contract(json.loads((private / 'base/BRIDGE-CONTRACT.json').read_text()), subject['tag'])
                report['new_public_feature_markers'] = {a: feature_checks.public_ui_markers(private / 'base' / ('fluxo-linux-' + a)) for a in ('amd64', 'arm64')}
                report['public_asset_relay'] = export_public_assets(private / 'base', args.output,
                    subject, report['releases']['base'], os.environ['GITHUB_SHA'])
        offmain = subprocess.run(['git', '-c', 'safe.directory=' + os.getcwd(), 'merge-base', '--is-ancestor',
            subjects['base']['source'], 'refs/remotes/origin/main'], capture_output=True, timeout=30)
        require(offmain.returncode == 1, 'Opt-in base source must remain off main')
        report['bridge_admission'] = {'exact_source_and_images': True,
            'experimental_predecessors_admitted': False, 'stable_bridge_policy': 'v0.4.55'}
        if args.presentation_only:
            report.update(passed=True, public_verification_only=True, native_fixture_created=False,
                scope='Public-byte/provenance verification and relay only; no installer or host preparation',
                node_preservation_required=False)
            return 0
        fixture.prepare()
        key_restore = ubuntu_key_fixture(private, report, command)
        if predecessor == 'bridge':
            javascript_restore = hosted_javascript_fixture(private, report)
            optional_runtime_fixture(private, report)
            existing_public_predecessor(private, subjects, arch, env, report, command)
        os.environ['FLUXO_PHP_UPDATE_VM_TEST'] = '1'  # Original observer guard, under the hosted root opt-in.
        observer = base.observer_module()
        stable_journal_observer(observer)
        require_named_fixture(arch)
        original_api = observer.API
        class FixtureAPI(original_api):
            def request(self, method, path, body=None):
                # Fixture-generated routes contain no credentials or tickets.
                report['stage'] = 'api ' + method + ' ' + path
                return super().request(method, path, body)
        observer.API = FixtureAPI
        seed, scheduler, before, retention, node_before = None, None, None, None, None
        new_features = None
        require(shutil.which('strace'), 'Read-only command observer is required on the hosted fixture')
        roles = [predecessor, 'base'] if predecessor else ['base']
        for role in roles:
            subject = subjects[role]
            env['FLUXO_VERSION'] = subject['tag']
            if role == 'base' and predecessor == 'old_base':
                incoming = base.run(['/usr/local/bin/fluxo', 'base-install', 'inspect'], 110, env)
                require(incoming['exit_code'] == 0 and json.loads(incoming['stdout'])['mode'] == 'base',
                        'Prior completed base must enter directly as base')
                report['old_base_admitted_directly'] = True
            raw = command(role + '-public-install', installer_command(private / role / 'install.sh',
                managed_node=managed_node), 1200)
            require_public_install(raw)
            if args.apt_diagnostic and role == 'bridge':
                raise AptDiagnosticCompleted()
            report[role + '_installed'] = installed(subject, arch, env, observer, private / role)
            if role == 'base' and node_before is not None:
                report['incoming_node_preservation'] = verified_node_preservation(raw,
                    node_before, node_snapshot(observer, subject), allow_enrollment=True)
            if before is not None:
                report['predecessor_preserved'] = compare_preservation(before, preservation_snapshot(seed, before), report, 'predecessor')
                if predecessor == 'bridge':
                    require('Completed bridge verified. Continuing to the base automatically.' in raw, 'Automatic supported bridge handoff missing')
                else:
                    require('Completed bridge verified. Continuing to the base automatically.' not in raw,
                            'Completed base was incorrectly routed through a bridge handoff')
            command(role + '-nginx-start', ['systemctl', 'start', 'nginx.service'])
            if seed is None:
                seed = base.seed_application(observer, command, private)
                scheduler = seed_scheduler(observer, command)
            # Retention must be observed before application checks hit port80.
            if role != 'bridge' and retention is None:
                report['retention_checks_start'] = role
            retention, report[role + '_application'] = release_application_checks(observer, seed, role, retention, command)
            if role != 'bridge':
                report[role + '_retention'] = retention
            report[role + '_root_execution'] = base.root_execution(observer, seed)
            report[role + '_scheduler'] = scheduler_execution(observer, scheduler, subject['assets']['fluxo-linux-' + arch]['sha256'])
            report[role + '_deployment'] = base.deployment_check(observer, seed, role != 'bridge')
            await_predecessor_checks(Path('/var/lib/fluxo/fluxo.db'), report)
            if retention is not None:
                api = observer.API('https://127.0.0.1:9595')
                api.login(False, 'base-owner')
                totals = api.request('GET', '/sites/%d/traffic-analytics?range=today' % seed['site_id'])['totals']
                require(totals['requests'] >= retention['retained_requests'], 'Pre-upgrade analytics already lost history')
                retention = dict(retention, retained_requests=totals['requests'])
                report[role + '_retention_snapshot'] = retention
            if role == 'base':
                new_features = feature_checks.new_base_observations(observer, seed, private, check_nginx=True)
                report['new_feature_observations'] = new_features
            before = preservation_snapshot(seed)
            if managed_node:
                node_before = node_snapshot(observer, subject)
        subject = subjects['base']
        admission = base.run(['/usr/local/bin/fluxo', 'base-install', 'inspect'], 110, env)
        require(admission['exit_code'] == 0 and json.loads(admission['stdout'])['mode'] == 'base', 'Ordinary update must start in admitted base mode')
        raw = command('ordinary-base-public-update', installer_command(private / 'base/install.sh',
            reinstall=True, remembered_node=managed_node), 1200)
        require_public_install(raw)
        report['reinstall_installed'] = installed(subject, arch, env, observer, private / 'base')
        report['reinstall_preserved'] = compare_preservation(before, preservation_snapshot(seed, before), report, 'reinstall')
        if managed_node:
            report['ordinary_base_node_preservation'] = verified_node_preservation(raw,
                node_before, node_snapshot(observer, subject), allow_enrollment=False)
        report['reinstall_retention'] = release_retention(observer, seed, 'base', retention)
        report['reinstall_application'] = base.application_checks(observer, seed, command, 'reinstall')
        report['reinstall_root_execution'] = base.root_execution(observer, seed)
        report['reinstall_scheduler'] = scheduler_execution(observer, scheduler, subject['assets']['fluxo-linux-' + arch]['sha256'])
        report['reinstall_deployment'] = base.deployment_check(observer, seed, True)
        report['new_feature_preservation'] = feature_checks.new_base_observations(observer, seed, private, previous=new_features)
        report['busy_preserve_scope'] = 'unchanged coordinator previously qualified by exact public base-test.1; not repeated here'
        report['public_base_delivery'] = True
        report['passed'] = True
    except AptDiagnosticCompleted:
        report['diagnostic_bridge_install_completed'] = True
    except Exception as error:
        report['error_type'] = type(error).__name__
        if isinstance(error, urllib.error.HTTPError):
            report['public_http_status'] = error.code
            if diagnostic := public_api_diagnostic(error):
                report['github_api_diagnostic'] = diagnostic
        # Raw logs, payloads, profiles and credentials remain PRIVATE.
        # Only bounded redacted diagnostics and source locations are public.
        report['error_frames'] = [{'file': Path(f.filename).name, 'function': f.name, 'line': f.lineno}
                                  for f in traceback.extract_tb(error.__traceback__)]
        if isinstance(error, RuntimeError):
            report['refusal'] = base.sanitized_diagnostic(str(error), secrets=diagnostic_secrets)
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        failed_log = private / (report.get('stage', '') + '.log')
        if not report['passed'] and failed_log.is_file():
            report['failure_diagnostic'] = base.sanitized_diagnostic(
                failed_log.read_text(errors='replace'), secrets=diagnostic_secrets)[-16000:]
        cleanup_errors = []
        for cleanup in ([] if args.presentation_only else [fixture.cleanup]) + ([key_restore] if key_restore else []) + ([javascript_restore] if javascript_restore else []):
            try:
                cleanup()
            except Exception as error:
                cleanup_errors.append(type(error).__name__)
        if args.presentation_only:
            shutil.rmtree(private)
            report['private_verification_workspace_removed'] = True
        report['cleanup_error_types'] = cleanup_errors
        report['passed'] = report['passed'] and not cleanup_errors
        if args.apt_diagnostic:
            report['diagnostic_only'] = True
            report['public_base_delivery'] = False
            report['apt_stderr_diagnostic'] = apt_trace_diagnostics(private)
        report['timing_caveats'] = ['Output arrival approximates existing phase boundaries; silent work is grouped.',
            'Public downloads/signature verification included; later phases use warm caches.',
            'The selected Node path is covered by the stable-bridge route; direct old-base qualification uses no optional Node/Python/database/Redis; no runtime migration or universal speedup claim.']
        (args.output / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
        print(json.dumps({'passed': report['passed'], 'stage': report.get('stage'), 'error_type': report.get('error_type')}))
    return 0 if report['passed'] or (report.get('diagnostic_bridge_install_completed') and not cleanup_errors) else 1


if __name__ == '__main__':
    raise SystemExit(main())
