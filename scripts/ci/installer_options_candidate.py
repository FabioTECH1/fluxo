"""One disposable hosted selected-options case; never customer deployment.

Real normal install/reinstall and actual leased Node CLI. Logs, credentials,
databases and authority stay root-private. Export only hashes/booleans and
redacted failure diagnostics. No receipt, nonce, invocation or ACK edits.
"""
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import sqlite3
import stat
import subprocess
import sys
import tarfile
import tempfile
import time
import ssl
import urllib.request


def digest(path):
    with open(path, 'rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def existing_public_predecessor(private, build, env, report, command, await_checks):
    """A healthy existing-install fixture; NOT a normal fresh-install claim.

    The public fresh route remains failed on Sury's missing PHP Redis archive.
    An existing supported PHP runtime need not have that optional extension.
    Use authentic public old bytes, stock vendor packages and the old normal
    same-version installer to create genuine readers/receipts. Never synthesize
    execution journals, registration, nonces, authority or acknowledgements.
    """
    tag = 'v0.4.54-bridge-test.6'
    source = '8d14ea5344b6d61d5572127075f5171d518c3329'
    binary = private/'predecessor-fluxo'
    installer = private/'predecessor-install.sh'
    bundle = private/'predecessor-attestation.json'
    values = [('fluxo-linux-amd64', binary, 'b4224628aa2263036ea2c116c901cf8f1c598cf6610319d7b48b38727605d178'),
              ('install.sh', installer, '1bab7eef4d04ff4b8a76515fb7e19da366c3dd44277bdaf851d1e5e0da5d2e8d'),
              ('fluxo-release-attestation.json', bundle, None)]
    for name, target, expected in values:
        command('predecessor-fetch-'+name, ['curl', '-fsSL', '--retry', '2', '--connect-timeout', '10', '--max-time', '180',
                '-o', str(target), 'https://github.com/FabioTECH1/fluxo/releases/download/'+tag+'/'+name], 570)
        if expected and digest(target) != expected:
            raise RuntimeError('Authentic predecessor asset differs')
    archive = build.parent/'options-public/tools/gh.tar.gz'
    if digest(archive) != 'a757f1ba6db18f4de8cbadb244843a5f89bc75b5e7c6fc127d2bd77fbd12ed62':
        raise RuntimeError('Pinned verifier archive differs')
    gh = private/'gh-pinned'
    with tarfile.open(archive) as tar:
        member = tar.getmember('gh_2.94.0_linux_amd64/bin/gh')
        if not member.isfile() or member.size > 64*1024*1024:
            raise RuntimeError('Unexpected pinned verifier payload')
        gh.write_bytes(tar.extractfile(member).read())
    gh.chmod(0o700)
    for name, target, expected in values[:2]:
        command('predecessor-attest-'+name, [str(gh), 'attestation', 'verify', str(target), '--repo', 'FabioTECH1/fluxo',
            '--bundle', str(bundle), '--signer-workflow', 'FabioTECH1/fluxo/.github/workflows/release.yml',
            '--source-ref', 'refs/tags/'+tag, '--source-digest', source, '--deny-self-hosted-runners'], 180)
    command('fixture-user', ['useradd', '--create-home', '--shell', '/bin/bash', 'fluxo'])
    shutil.copyfile(binary, '/usr/local/bin/fluxo')
    Path('/usr/local/bin/fluxo').chmod(0o755)
    command('predecessor-managed-repository', ['/usr/local/bin/fluxo', 'php-repository-setup'], 300)
    command('predecessor-vendor-index', ['apt-get', '-o', 'APT::Update::Error-Mode=any', 'update'], 180)
    # Explicit existing-install profile: all normal PHP modules except Redis.
    # No package/source/pin is rewritten or downgraded to disguise the 404.
    packages = ['nginx', 'certbot', 'ufw', 'fail2ban', 'git', 'curl', 'gnupg', 'ca-certificates', 'util-linux']
    packages += ['php8.4-'+x for x in ['fpm', 'cli', 'mysql', 'pgsql', 'sqlite3', 'curl', 'mbstring', 'xml', 'gd', 'zip', 'bcmath', 'intl']]
    command('predecessor-vendor-runtime', ['apt-get', 'install', '-y', *packages], 300)
    command('predecessor-cli-selection', ['update-alternatives', '--set', 'php', '/usr/bin/php8.4'])
    unit = Path('/etc/systemd/system/fluxo.service')
    unit.write_text('[Unit]\nDescription=Fluxo Management Daemon\nAfter=network.target\n\n[Service]\nExecStart=/usr/local/bin/fluxo\nRestart=always\nUser=root\nEnvironment=FLUXO_ENV=prod\nEnvironment=FLUXO_PORT=9595\nPrivateTmp=true\nRestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX AF_NETLINK\n\n[Install]\nWantedBy=multi-user.target\n')
    unit.chmod(0o644)
    command('predecessor-unit-reload', ['systemctl', 'daemon-reload'])
    command('predecessor-start', ['systemctl', 'enable', '--now', 'fluxo.service'])
    end = time.monotonic()+60
    while True:
        try:
            with urllib.request.urlopen('https://127.0.0.1:9595/api/v1/version', context=ssl._create_unverified_context(), timeout=2) as response:
                if json.load(response)['version'] == tag[1:]:
                    break
        except Exception:
            pass
        if time.monotonic() >= end:
            raise RuntimeError('Authentic predecessor fixture did not become healthy')
        time.sleep(.2)
    await_checks(Path('/var/lib/fluxo/fluxo.db'), report)
    selected = env['FLUXO_VERSION']
    env['FLUXO_VERSION'] = tag
    try:
        command('predecessor-normal-current-compatibility', ['bash', str(installer), '--no-node', '--no-python', '--db-engine=none', '--no-redis', '--no-harden-ssh'], 700)
    finally:
        env['FLUXO_VERSION'] = selected
    report['existing_predecessor_fixture'] = {'tag': tag, 'source': source, 'binary_sha256': digest(binary),
        'authentic_old_binary_and_installer_attestations': True, 'stock_vendor_PHP84_without_optional_redis_extension': True,
        'normal_same_version_installer_compatibility': True, 'ordinary_public_fresh_install_claimed': False,
        'manual_execution_authority_or_ack_edits': False}


def main():
    required = {'FLUXO_OPTIONS_HOSTED_TEST': '1', 'GITHUB_ACTIONS': 'true',
                'GITHUB_REF': 'refs/heads/test/installer-component-options',
                'RUNNER_ENVIRONMENT': 'github-hosted', 'RUNNER_ARCH': 'X64'}
    if os.geteuid() != 0 or platform.machine() != 'x86_64' or any(os.environ.get(k) != v for k, v in required.items()):
        raise SystemExit('Explicit isolated hosted AMD64 options case only')
    if Path('/usr/local/bin/fluxo').exists() or Path('/var/lib/fluxo/fluxo.db').exists():
        raise SystemExit('Refuse an existing installation')
    build, fixture_modules, output = map(Path, sys.argv[1:])
    sys.path.insert(0, str(fixture_modules))
    from base_candidate import HOSTS, HostedFixture, sanitized_diagnostic
    from arm64_candidate import optional_runtime_fixture, await_predecessor_checks, installation_invariants
    subject_path = Path('scripts/ci/installer_options_subject.json')
    subject = json.loads(subject_path.read_text()) if subject_path.exists() else None
    engine = subject.get('engine', 'cron') if subject else 'cron'
    if engine not in ('cron', 'cronie'):
        raise SystemExit('Unsupported explicit fixture engine')
    # The same already-qualified disposable AMD64 host name also supports the
    # native Noble Cronie provider; no production allowlist is changed.
    HOSTS['amd64', '24.04', 'cronie'] = HOSTS['amd64', '24.04', 'cron']
    report = {'passed': False, 'source': os.environ['GITHUB_SHA'], 'harness_source': os.environ['GITHUB_SHA'], 'arch': 'amd64', 'engine': engine,
              'scope': 'normal selected-options source installer + ordinary reinstall + actual busy leased Node CLI',
              'public_download_attestation': False, 'authority_edits': False}
    output.mkdir(mode=0o755, parents=True, exist_ok=False)
    os.umask(0o077)
    private = Path(tempfile.mkdtemp(prefix='fluxo-installer-options-', dir='/root'))
    env = {'PATH': '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin',
           'HOME': '/root', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8',
           'DEBIAN_FRONTEND': 'noninteractive', 'NEEDRESTART_MODE': 'l',
           'FLUXO_ENV': 'prod', 'FLUXO_DATA_DIR': '/var/lib/fluxo'}

    def command(name, args, timeout=120, **kw):
        log = private / (name + '.log')
        begin = time.monotonic()
        print(json.dumps({'phase': name, 'state': 'started'}), flush=True)
        with log.open('xb') as stream:
            r = subprocess.run(args, env=env, stdin=subprocess.DEVNULL, stdout=stream,
                               stderr=subprocess.STDOUT, timeout=timeout, **kw)
        report[name] = {'returncode': r.returncode, 'seconds': time.monotonic()-begin, 'log_sha256': digest(log)}
        print(json.dumps({'phase': name, 'returncode': r.returncode}), flush=True)
        if r.returncode:
            failure = output/'failure-sanitized.log'
            details = log.read_text(errors='replace')
            if name == 'busy-node':
                inner = private/'busy-node-private.log'
                if inner.is_file():
                    details += '\nActual Node CLI output:\n'+inner.read_text(errors='replace')[-1048576:]
                observed = private/'busy-node-result.json'
                if observed.is_file():
                    report['busy_node'] = json.loads(observed.read_text())
            failure.write_text(sanitized_diagnostic(details))
            failure.chmod(0o644)
            raise RuntimeError(name + ' refused (exit ' + str(r.returncode) + ')')
        return True

    fixture = HostedFixture(private, report, env, command, 'amd64', '24.04', engine)
    shims = []
    try:
        fixture.prepare()
        # Quarantine disposable hosted JS entry points regardless of their
        # recorded owner; do not execute or authorize them as Fluxo tools. Exact
        # bytes/links + owner/mode are preserved in the root-private archive.
        inventory = []
        for name in ('node', 'npm', 'npx', 'corepack', 'pnpm', 'pnpx', 'yarn', 'yarnpkg', 'bun', 'bunx', 'pm2'):
            path = Path('/usr/local/bin') / name
            if not os.path.lexists(path):
                continue
            info = path.lstat()
            if not (stat.S_ISREG(info.st_mode) or stat.S_ISLNK(info.st_mode)):
                raise RuntimeError('Unexpected hosted JS entry type')
            item = {'name': name, 'uid': info.st_uid, 'gid': info.st_gid, 'mode': stat.S_IMODE(info.st_mode)}
            item['link' if path.is_symlink() else 'sha256'] = os.readlink(path) if path.is_symlink() else digest(path)
            archived = private / ('prior-js-' + name)
            path.rename(archived)
            shims.append((path, archived, item))
            inventory.append(item)
        report['hosted_JS_entry_inventory'] = inventory
        optional_runtime_fixture(private, report)
        candidate = private/'fluxo-candidate'
        installer = build/'install.sh'
        if subject:
            if subject.get('tag') != 'v0.4.54-bridge-test.7' or subject.get('public_artifact_integrity_passed') is not True:
                raise RuntimeError('Unexpected or unverified public test subject')
            for key, length in [('source', 40), ('amd64_sha256', 64), ('installer_sha256', 64)]:
                if not re.fullmatch('[0-9a-f]{'+str(length)+'}', subject.get(key, '')):
                    raise RuntimeError('Invalid public subject identity')
            subprocess.run(['git', '-c', 'safe.directory='+os.getcwd(), 'diff', '--exit-code', subject['source'], 'HEAD', '--',
                            'cmd', 'internal', 'ui/src', 'ui/embed.go', 'go.mod', 'go.sum', 'install.sh',
                            '.github/release-tool-versions.conf'], check=True, stdout=subprocess.DEVNULL)
            installer = private/'public-install.sh'
            command('public-installer-download', ['curl', '-fsSL', '--retry', '2', '--connect-timeout', '10', '--max-time', '180',
                    '-o', str(installer), 'https://github.com/FabioTECH1/fluxo/releases/download/'+subject['tag']+'/install.sh'], 570)
            if digest(installer) != subject['installer_sha256']:
                raise RuntimeError('Public installer digest differs')
            image = subject['amd64_sha256']
            report.update(source=subject['source'], public_artifact_selected=True, public_tag=subject['tag'])
            env['FLUXO_VERSION'] = subject['tag']
        else:
            shutil.copyfile(build/'fluxo', candidate)
            candidate.chmod(0o700)
            image = digest(candidate)
            env.update(FLUXO_LOCAL_BINARY_SHA256=image, FLUXO_VERSION='v0.4.54-bridge-test.7')
        report['binary_sha256'] = image
        report['installer_sha256'] = digest(installer)
        flags = ['--node', '--python', '--db-engine=none', '--no-redis', '--no-harden-ssh']
        if not subject:
            flags.insert(0, '--local-binary='+str(candidate))
        previous = None
        if subject and subject.get('existing_install_fixture') is True:
            existing_public_predecessor(private, build, env, report, command, await_predecessor_checks)
            previous = installation_invariants()
        command('normal-selected-install', ['bash', str(installer), *flags], 1000)
        if digest('/usr/local/bin/fluxo') != image:
            raise RuntimeError('Installed subject digest differs')
        if subject:
            shutil.copyfile('/usr/local/bin/fluxo', candidate)
            candidate.chmod(0o700)
        if previous is not None:
            after_upgrade = installation_invariants(previous['columns'])
            fields = ['users', 'sites', 'crons', 'encryption.key', 'tls/cert.pem', 'tls/key.pem']
            if any(previous[k] != after_upgrade[k] for k in fields) or any(after_upgrade['retained'].get(k) != v for k, v in previous['retained'].items()):
                raise RuntimeError('Public predecessor upgrade changed retained records/credentials/keys/helpers')
            report['public_predecessor_records_credentials_keys_TLS_helpers_archives_preserved'] = True

        def preserved():
            # Existing accepted observer authenticates/decrypts encrypted user
            # fields before hashing: randomized ciphertext is not a credential
            # identity. Every other user column remains part of the comparison.
            normalized = installation_invariants()
            with sqlite3.connect('file:/var/lib/fluxo/fluxo.db?mode=ro', uri=True) as db:
                daemons = db.execute('SELECT * FROM daemons ORDER BY id').fetchall()
            files = {str(p): digest(p) for p in Path('/var/lib/fluxo').rglob('*')
                     if p.is_file() and p.name in ('encryption.key', 'cert.pem', 'key.pem')}
            return {**{k: normalized[k] for k in ['users', 'sites', 'crons', 'columns']},
                    'daemons': hashlib.sha256(repr(daemons).encode()).hexdigest(), 'files': files}

        await_predecessor_checks(Path('/var/lib/fluxo/fluxo.db'), report)
        before = preserved()
        (private/'invariants-before.json').write_text(json.dumps(before, indent=2)+'\n')
        command('normal-selected-reinstall', ['bash', str(installer), *flags, '--reinstall'], 1000)
        if subject and not all('Fluxo release provenance verified' in (private/(name+'.log')).read_text()
                               for name in ['normal-selected-install', 'normal-selected-reinstall']):
            raise RuntimeError('Actual public installer provenance verification missing')
        if subject:
            report['public_download_attestation'] = True
        await_predecessor_checks(Path('/var/lib/fluxo/fluxo.db'), report)
        after = preserved()
        (private/'invariants-after.json').write_text(json.dumps(after, indent=2)+'\n')
        report['preservation_fields'] = {k: {'equal': before[k] == after[k],
            'before_sha256': hashlib.sha256(json.dumps(before[k], sort_keys=True).encode()).hexdigest(),
            'after_sha256': hashlib.sha256(json.dumps(after[k], sort_keys=True).encode()).hexdigest()} for k in before}
        if before != after:
            raise RuntimeError('Selected reinstall preservation changed: '+', '.join(k for k in before if before[k] != after[k]))
        if digest('/usr/local/bin/fluxo') != image:
            raise RuntimeError('Selected reinstall installed image differs')
        pid = int(subprocess.check_output(['systemctl', 'show', 'fluxo.service', '-p', 'MainPID', '--value'], text=True))
        if pid <= 1 or digest('/proc/'+str(pid)+'/exe') != image:
            raise RuntimeError('Running image differs from tested candidate')
        report['selected_reinstall_records_credentials_keys_TLS_preserved'] = True
        report['installed_and_running_image_match'] = True
        # The same exact direct-CLI reproduction used on the named VM, with
        # only the explicit disposable-host path and exact image inputs adapted.
        env['FLUXO_OPTIONS_NATIVE_SHA256'] = image
        env['FLUXO_OPTIONS_NATIVE_PRIVATE'] = str(private)
        (private/'full-install-result.json').write_text(json.dumps({'returncode': 0})+'\n')
        command('busy-node', ['python3', 'scripts/ci/installer_options_busy.py'], 400)
        report['busy_node'] = json.loads((private/'busy-node-result.json').read_text())
        report['passed'] = report['busy_node']['passed'] is True
    except Exception as error:
        report['error'] = sanitized_diagnostic(str(error))
    finally:
        try:
            for path, archived, item in shims:
                if os.path.lexists(path):
                    path.unlink()
                archived.rename(path)
                info = path.lstat()
                assert (info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)) == (item['uid'], item['gid'], item['mode'])
                assert (os.readlink(path) if path.is_symlink() else digest(path)) == item.get('link', item.get('sha256'))
            if 'home_default_acl_before_hex' in report:
                os.setxattr('/home', 'system.posix_acl_default', bytes.fromhex(report['home_default_acl_before_hex']))
            fixture.cleanup()
            report['hosted_JS_entries_and_home_ACL_restored'] = True
        except Exception as error:
            report['cleanup_error'] = sanitized_diagnostic(str(error))
            report['passed'] = False
        result = output/'report.json'
        result.write_text(json.dumps(report, indent=2)+'\n')
        result.chmod(0o644)
        print(json.dumps({'passed': report['passed'], 'error': report.get('error'), 'cleanup': report.get('fixture_cleanup_passed')}), flush=True)
    if not report['passed']:
        raise SystemExit(1)


if __name__ == '__main__':
    main()
