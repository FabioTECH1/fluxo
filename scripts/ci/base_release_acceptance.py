#!/usr/bin/env python3
"""Four bounded PUBLIC base acceptance cases on disposable native Noble runners.

The isolated test/base-public-release-acceptance branch must first commit an
exact, independently reviewed post-publication subject JSON. No subject is
shipped before publication. --subject has schema_version=1 and releases with
exactly base, bridge, old_base. Each release has tag, source, release_id, assets;
each of the eight asset entries has id, size, sha256. Unknown fields are refused.
All identities are checked again against public immutable metadata, downloaded
bytes, source, and signed hosted release-workflow provenance before execution.
Fixture source and signed release source are separate report identities.

Run only via base-release-acceptance.yml. Raw output, credentials, profiles,
private keys and snapshots stay under /root; only report.json is publishable.
No local binary, digest override, attestation bypass, job deletion, authority
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
from arm64_candidate import await_predecessor_checks, download_public_subject
from node_preservation_candidate import logged_process
from runtime_compat_candidate import packaged_ubuntu_key_needs_stock_mode

REPO = 'FabioTECH1/fluxo'
BRANCH = 'refs/heads/test/base-public-release-acceptance'
WORKFLOW = 'Native public base release acceptance'
TAGS = {'base': 'v0.4.53-base-test.1', 'bridge': 'v0.4.53',
        'old_base': 'v0.4.52-base-test.2'}
OLD_SOURCE = '435511bb0240893402c0fe00a5a8a7905b1c0930'
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
CASES = {'amd64-fresh': ('amd64', None), 'amd64-bridge': ('amd64', 'bridge'),
         'arm64-bridge': ('arm64', 'bridge'), 'amd64-old-base': ('amd64', 'old_base')}
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
        require(role != 'old_base' or release['source'] == OLD_SOURCE, 'Wrong admitted old base source')
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


def public_json(suffix):
    request = urllib.request.Request('https://api.github.com/repos/' + REPO + '/' + suffix,
        headers={'Accept': 'application/vnd.github+json', 'User-Agent': 'fluxo-public-base-acceptance'})
    for attempt in range(2):
        try:
            with urllib.request.urlopen(request, timeout=60) as response:
                raw = response.read(2 << 20)
            break
        except urllib.error.HTTPError as error:
            if attempt or error.code not in (429, 500, 502, 503, 504):
                raise
            time.sleep(2)
    require(len(raw) < 2 << 20, 'Oversized public release metadata')
    return json.loads(raw)


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
    require(actual == dict(assignments, **{'main.version': subject['tag'][1:]}), 'Exact 19 official pin assignments/version differ')
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


def installed(subject, arch, env, observer, directory):
    image = subject['assets']['fluxo-linux-' + arch]['sha256']
    result = base.verify_installed(image, env)
    version = base.run(['/usr/local/bin/fluxo', '--version'], 15, env)
    require(version['exit_code'] == 0 and version['stdout'] == 'fluxo version ' + subject['tag'][1:], 'Native release version differs')
    contract = base.run(['/usr/local/bin/fluxo', '--bridge-contract'], 15, env)
    require(contract['exit_code'] == 0 and json.loads(contract['stdout']) == json.loads((directory / 'BRIDGE-CONTRACT.json').read_text()),
            'Native release contract differs from signed subject')
    api = observer.API('https://127.0.0.1:9595')
    require(api.request('GET', '/health') == {'status': 'ok'}
            and api.request('GET', '/version') == {'version': subject['tag'][1:]}, 'Healthy native TLS API/version required')
    # The served frontend must come from the exact signed installed image.
    with api.open.open('https://127.0.0.1:9595/', timeout=15) as response:
        page = response.read(1 << 20)
        require(response.status == 200 and b'<div id="app"' in page and b'/assets/' in page, 'Embedded UI shell missing')
    result.update(native_version=True, signed_contract_matches=True, healthy_tls_api=True, embedded_ui_served=True)
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


def installer_command(installer, reinstall=False):
    return ['bash', str(installer), *(['--reinstall'] if reinstall else []), *base.FLAGS]


def require_public_install(raw):
    require('Fluxo release provenance verified' in raw and 'Skipping release attestation' not in raw
            and 'Release attestation verification was explicitly disabled' not in raw
            and 'already up to date' not in raw and ' installed successfully!' in raw,
            'Full normal signed public installer path required')


def release_retention(observer, seed, role, previous):
    if role == 'old_base':
        # This immutable older base supports enabled/disabled analytics only.
        # Its fixed30-day default and real history must survive before the new
        # base can offer a manual-retention choice.
        return base.retention_check(observer, seed, manual=False)
    if previous is not None:
        api = observer.API('https://127.0.0.1:9595')
        api.login(False, 'base-owner')
        state = api.request('GET', '/traffic-analytics')
        require(state['retention_days'] == previous['retention_days'] and state['master_enabled'] is True and
                any(site['site_id'] == seed['site_id'] and site['enabled'] is True for site in state['sites']),
                'Upgrade lost the old retention or collection selection')
        data = api.request('GET', '/sites/%d/traffic-analytics?range=today' % seed['site_id'])
        require(data['totals']['requests'] >= previous['retained_requests'], 'Upgrade lost old analytics aggregates')
        if previous['retention_days'] == 30:
            return base.retention_check(observer, seed)
    return base.retention_check(observer, seed, previous)


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


def apt_trace_diagnostics(directory):
    result = []
    for path in sorted(directory.glob('bridge-apt-trace.*')):
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


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--subject', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--case', choices=CASES, required=True)
    parser.add_argument('--go', required=True, help='Hosted exact Go executable for reading public build metadata only')
    parser.add_argument('--apt-diagnostic', action='store_true', help='Private external tracing; not public base acceptance')
    args = parser.parse_args()
    arch = hosted_profile(args.case)
    require(not args.apt_diagnostic or args.case == 'arm64-bridge', 'APT diagnostic is restricted to the ARM bridge fixture')
    subjects = load_subject(args.subject)['releases']
    require(not git('status', '--porcelain', '--untracked-files=all').strip(), 'Entire fixture checkout must be committed and clean')
    require(git('rev-parse', 'HEAD').decode().strip() == os.environ.get('GITHUB_SHA'), 'Fixture workflow source differs')
    require(not any(os.path.lexists(p) for p in ('/var/lib/fluxo', '/usr/local/bin/fluxo', '/home/fluxo',
        '/var/lib/fluxo-scheduler', '/var/lib/fluxo-system-tasks')), 'Refuse existing installation')
    require(not list(Path('/etc/letsencrypt/renewal').glob('*.conf')), 'Refuse live certificates')
    args.output.mkdir(mode=0o755, parents=True, exist_ok=False)
    private = Path(tempfile.mkdtemp(prefix='fluxo-public-base-', dir='/root'))
    private.chmod(0o700)
    report = {'passed': False, 'case': args.case, 'architecture': arch, 'ubuntu': '24.04',
        'fixture_source': os.environ['GITHUB_SHA'], 'subject_sha256': base.digest(args.subject),
        'runner_image': {k: os.environ[k] for k in ('ImageOS', 'ImageVersion')},
        'production_approved': False, 'promotion_performed': False, 'steps': {}, 'releases': {}}
    env = {'PATH': '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin', 'HOME': '/root',
        'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8', 'DEBIAN_FRONTEND': 'noninteractive', 'NEEDRESTART_MODE': 'l',
        'FLUXO_ENV': 'prod', 'FLUXO_DATA_DIR': '/var/lib/fluxo', 'FLUXO_PHP_UPDATE_VM_TEST': '1',
        'GH_CONFIG_DIR': str(private / 'gh-config'), 'GH_PROMPT_DISABLED': '1'}
    def command(name, argv, timeout=120, cwd=None):
        report['stage'] = name
        print(json.dumps({'stage': name, 'status': 'started'}), flush=True)
        start, boundaries = time.monotonic(), []
        log = private / (name + '.log')
        if args.apt_diagnostic and name == 'bridge-public-install':
            require(os.access('/usr/bin/strace', os.X_OK), 'Diagnostic tracer unavailable')
            argv = ['/usr/bin/strace', '-ff', '-qq', '-s', '4096', '-e', 'trace=execve,write',
                    '-o', str(private / 'bridge-apt-trace'), *argv]
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
        require(code == 0, 'Command failed: ' + name + '; exit=' + str(code))
        return log.read_text(errors='replace')
    fixture = base.HostedFixture(private, report, env, command, arch, '24.04', 'cron')
    key_restore = None
    def deadline(_signum, _frame):
        raise TimeoutError('Public acceptance time budget exhausted')
    signal.signal(signal.SIGALRM, deadline)
    signal.setitimer(signal.ITIMER_REAL, 3600)
    try:
        require(public_json('releases/latest')['tag_name'] != TAGS['base'], 'Opt-in base must remain off latest')
        for role, subject in subjects.items():
            report['releases'][role] = verify_release(role, subject, private, command, args.go)
        offmain = subprocess.run(['git', '-c', 'safe.directory=' + os.getcwd(), 'merge-base', '--is-ancestor',
            subjects['base']['source'], 'refs/remotes/origin/main'], capture_output=True, timeout=30)
        require(offmain.returncode == 1, 'Opt-in base source must remain off main')
        require(subprocess.run(['git', '-c', 'safe.directory=' + os.getcwd(), 'merge-base', '--is-ancestor',
            subjects['bridge']['source'], 'refs/remotes/origin/main'], capture_output=True, timeout=30).returncode == 0,
            'Supported stable bridge source must be on main')
        fixture.prepare()
        key_restore = ubuntu_key_fixture(private, report, command)
        os.environ['FLUXO_PHP_UPDATE_VM_TEST'] = '1'  # Original observer guard, under the hosted root opt-in.
        observer = base.observer_module()
        require_named_fixture(arch)
        original_api = observer.API
        class FixtureAPI(original_api):
            def request(self, method, path, body=None):
                # Fixture-generated routes contain no credentials or tickets.
                report['stage'] = 'api ' + method + ' ' + path
                return super().request(method, path, body)
        observer.API = FixtureAPI
        seed, scheduler, before, retention = None, None, None, None
        predecessor = CASES[args.case][1]
        roles = [predecessor, 'base'] if predecessor else ['base']
        for role in roles:
            subject = subjects[role]
            env['FLUXO_VERSION'] = subject['tag']
            raw = command(role + '-public-install', installer_command(private / role / 'install.sh'), 1200)
            require_public_install(raw)
            if args.apt_diagnostic and role == 'bridge':
                raise AptDiagnosticCompleted()
            report[role + '_installed'] = installed(subject, arch, env, observer, private / role)
            if before is not None:
                report['predecessor_preserved'] = compare_preservation(before, preservation_snapshot(seed, before), report, 'predecessor')
                if predecessor == 'bridge':
                    require('Completed bridge verified. Continuing to the base automatically.' in raw, 'Automatic supported bridge handoff missing')
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
            report[role + '_deployment'] = base.deployment_check(observer, seed, role == 'base')
            await_predecessor_checks(Path('/var/lib/fluxo/fluxo.db'), report)
            if retention is not None:
                api = observer.API('https://127.0.0.1:9595')
                api.login(False, 'base-owner')
                totals = api.request('GET', '/sites/%d/traffic-analytics?range=today' % seed['site_id'])['totals']
                require(totals['requests'] >= retention['retained_requests'], 'Pre-upgrade analytics already lost history')
                retention = dict(retention, retained_requests=totals['requests'])
                report[role + '_retention_snapshot'] = retention
            before = preservation_snapshot(seed)
        subject = subjects['base']
        admission = base.run(['/usr/local/bin/fluxo', 'base-install', 'inspect'], 110, env)
        require(admission['exit_code'] == 0 and json.loads(admission['stdout'])['mode'] == 'base', 'Ordinary update must start in admitted base mode')
        raw = command('ordinary-base-public-update', installer_command(private / 'base/install.sh', reinstall=True), 1200)
        require_public_install(raw)
        report['reinstall_installed'] = installed(subject, arch, env, observer, private / 'base')
        report['reinstall_preserved'] = compare_preservation(before, preservation_snapshot(seed, before), report, 'reinstall')
        report['reinstall_retention'] = release_retention(observer, seed, 'base', retention)
        report['reinstall_application'] = base.application_checks(observer, seed, command, 'reinstall')
        report['reinstall_root_execution'] = base.root_execution(observer, seed)
        report['reinstall_scheduler'] = scheduler_execution(observer, scheduler, subject['assets']['fluxo-linux-' + arch]['sha256'])
        report['reinstall_deployment'] = base.deployment_check(observer, seed, True)
        report['public_base_delivery'] = True
        report['passed'] = True
    except AptDiagnosticCompleted:
        report['diagnostic_bridge_install_completed'] = True
    except Exception as error:
        report['error_type'] = type(error).__name__
        if isinstance(error, urllib.error.HTTPError):
            report['public_http_status'] = error.code
        # Raw logs, payloads, profiles and credentials remain PRIVATE.
        # Only bounded redacted diagnostics and source locations are public.
        report['error_frames'] = [{'file': Path(f.filename).name, 'function': f.name, 'line': f.lineno}
                                  for f in traceback.extract_tb(error.__traceback__)]
        if isinstance(error, RuntimeError):
            report['refusal'] = base.sanitized_diagnostic(str(error))
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        failed_log = private / (report.get('stage', '') + '.log')
        if not report['passed'] and failed_log.is_file():
            report['failure_diagnostic'] = base.sanitized_diagnostic(failed_log.read_text(errors='replace'))[-16000:]
        cleanup_errors = []
        for cleanup in ([key_restore] if key_restore else []) + [fixture.cleanup]:
            try:
                cleanup()
            except Exception as error:
                cleanup_errors.append(type(error).__name__)
        report['cleanup_error_types'] = cleanup_errors
        report['passed'] = report['passed'] and not cleanup_errors
        if args.apt_diagnostic:
            report['diagnostic_only'] = True
            report['public_base_delivery'] = False
            report['apt_stderr_diagnostic'] = apt_trace_diagnostics(private)
        report['timing_caveats'] = ['Output arrival approximates existing phase boundaries; silent work is grouped.',
            'Public downloads/signature verification included; later phases use warm caches.', 'Not a universal speedup or runtime/Node qualification.']
        (args.output / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
        print(json.dumps({'passed': report['passed'], 'stage': report.get('stage'), 'error_type': report.get('error_type')}))
    return 0 if report['passed'] or (report.get('diagnostic_bridge_install_completed') and not cleanup_errors) else 1


if __name__ == '__main__':
    raise SystemExit(main())
