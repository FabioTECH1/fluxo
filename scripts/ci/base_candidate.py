#!/usr/bin/env python3
"""Disposable hosted FINAL-base acceptance; never public base delivery acceptance.

Build ui first, then use build_release_candidate.py DEST 0.0.0-base.dev.
Run with sudo and the explicit hosted opt-in shown in base-candidate.yml:
  python3 scripts/ci/base_candidate.py --build-dir DIR --output DIR \
      --mode fresh --engine cron
Modes: fresh, bridge (authentic public v0.4.51). Both perform an ordinary
same-candidate base update. Raw logs/credentials/DBs remain root-private. Only
allowlisted results and candidate build artifacts may be uploaded. This does
not run the previously user-skipped early-interruption scenario.
"""
import argparse
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import platform
import pwd
import re
import shutil
import sqlite3
import stat
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.request

from arm64_candidate import (
    await_predecessor_checks, digest, download_public_subject,
    installation_invariants, stock_profile_files, stock_ubuntu_sources,
)
from arm64_release_probe import isolate_ci_microsoft_source, run, runner_system_paths, sanitized, state_summary
from build_release_candidate import source_identity

VERSION = '0.0.0-base.dev'
BRANCHES = ('refs/heads/test/base-native-acceptance', 'refs/heads/baseline/clean-base')
FLAGS = ['--db-engine=none', '--no-redis', '--no-node', '--no-python', '--no-harden-ssh']
HOSTS = {
    ('amd64', '24.04', 'cron'): 'fluxo-bridge-prod-20261101',
    ('arm64', '22.04', 'cron'): 'fluxo-arm64-2204-acceptance',
    ('arm64', '24.04', 'cron'): 'fluxo-arm64-2404-acceptance',
    ('arm64', '26.04', 'cron'): 'fluxo-arm64-2604-acceptance',
    ('arm64', '24.04', 'cronie'): 'fluxo-arm64-2404-cronie-acceptance',
}


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def sanitized_diagnostic(raw, secrets=()):
    """Retain actionable diagnostics without raw credentials or bearer data."""
    # Credentials are collected only in memory; neither their file nor values
    # are exported. Also redact unlabeled JWTs/bootstrap tokens/private blocks.
    values = set(secrets)
    path = Path('/var/lib/fluxo/.fluxo_credentials')
    if path.is_file():
        for line in path.read_text().splitlines():
            if ':' in line:
                value = line.split(':', 1)[1].strip()
                if value:
                    values.add(value)
    for value in sorted(values, key=len, reverse=True):
        raw = raw.replace(value, '[redacted]')
    raw = re.sub(r'-----BEGIN [^-]*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----',
                 '[redacted private key]', raw, flags=re.S)
    raw = re.sub(r'\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b', '[redacted JWT]', raw)
    raw = re.sub(r'\b[0-9a-fA-F]{32}\b', '[redacted possible bootstrap value]', raw)
    return sanitized(raw)


def public_bridge(path=Path('scripts/ci/release_subject.json')):
    # Deliberately NOT arm64_candidate.release_subject(): that helper requires
    # application source equality with the bridge, which the base cannot have.
    subject = json.loads(path.read_text())
    require(subject.get('public_tag') == 'v0.4.51', 'Expected exact supported public bridge')
    for key in ('source', 'installer_sha256', 'amd64_sha256', 'arm64_sha256'):
        require(re.fullmatch('[0-9a-f]{%d}' % (40 if key == 'source' else 64),
                             subject.get(key, '')), 'Invalid pinned bridge identity')
    return subject


def hosted_profile():
    expected = {'FLUXO_BASE_CANDIDATE': '1', 'GITHUB_ACTIONS': 'true',
                'RUNNER_ENVIRONMENT': 'github-hosted'}
    require(os.geteuid() == 0 and all(os.environ.get(k) == v for k, v in expected.items()),
            'Explicit root disposable GitHub-hosted opt-in required')
    require(os.environ.get('GITHUB_REF') in BRANCHES, 'Investigation branch required')
    arch = {'x86_64': 'amd64', 'aarch64': 'arm64'}.get(platform.machine())
    require(arch is not None and os.environ.get('RUNNER_ARCH') == {'amd64': 'X64', 'arm64': 'ARM64'}[arch],
            'Native runner architecture required')
    require(run(['dpkg', '--print-architecture'])['stdout'] == arch, 'Native dpkg architecture differs')
    release = dict(line.split('=', 1) for line in Path('/usr/lib/os-release').read_text().splitlines()
                   if '=' in line)
    require(release.get('ID', '').strip('"') == 'ubuntu', 'Ubuntu fixture required')
    return arch, release['VERSION_ID'].strip('"')


def observer_module():
    spec = importlib.util.spec_from_file_location('base_execution_observer',
              Path('internal/services/scheduler/actual_candidate_vm_acceptance.py'))
    observer = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(observer)
    return observer


def atomic_fixture(path, raw):
    with tempfile.NamedTemporaryFile(dir=path.parent, delete=False) as stream:
        os.fchmod(stream.fileno(), 0o644)
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
        staged = Path(stream.name)
    staged.replace(path)


class HostedFixture:
    """Same stock-profile preparation/cleanup as arm64_candidate, no guard edits."""
    def __init__(self, private, report, env, command, arch, release, engine):
        self.private, self.report, self.env, self.command = private, report, env, command
        self.arch, self.release, self.engine = arch, release, engine
        self.moved = []
        self.timers = {u: run(['systemctl', 'is-active', '--quiet', u])['exit_code'] == 0
                       for u in ('apt-daily.timer', 'apt-daily-upgrade.timer')}
        self.nginx_active = run(['systemctl', 'is-active', '--quiet', 'nginx.service'])['exit_code'] == 0
        self.hostname = platform.node()

    def prepare(self):
        p, r, command = self.private, self.report, self.command
        self.umask = os.umask(0o022)
        r['fixture_directories'] = runner_system_paths()
        r['nginx_fixture_permissions'] = []
        root = Path('/usr/share/nginx')
        for path in ([root, *root.rglob('*')] if root.is_dir() and not root.is_symlink() else []):
            info = path.lstat()
            if info.st_uid == 0 and (stat.S_ISDIR(info.st_mode) or stat.S_ISREG(info.st_mode)) and info.st_mode & 0o022:
                r['nginx_fixture_permissions'].append((str(path), stat.S_IMODE(info.st_mode)))
                path.chmod(stat.S_IMODE(info.st_mode) & ~0o022)
        isolate_ci_microsoft_source(p, self.moved)
        for timer in self.timers:
            command('pause-' + timer, ['systemctl', 'stop', timer])
        command('hostname', ['hostnamectl', 'set-hostname', HOSTS[self.arch, self.release, self.engine]])
        pam = Path('/etc/pam.d/common-session-noninteractive')
        raw = pam.read_bytes()
        if digest(pam) == 'ed47c9b6671f3977d679102cb4d836ef2982613aaa47647d851fe7d695eaaead':
            require(raw.endswith(b'session required pam_limits.so\n'), 'Unexpected CI PAM suffix')
            stock = raw[:-len(b'session required pam_limits.so\n')]
            require(hashlib.sha256(stock).hexdigest() == '1bd1ef2b6241fc8b3c97bbaf57e5c6038c96c275e30c62ebee6eec15701e2e07',
                    'Stock PAM reconstruction disagrees')
            shutil.copy2(pam, p / 'common-session-noninteractive.original')
            atomic_fixture(pam, stock)
        fallback = Path('/etc/pam.d/common-session')
        raw = fallback.read_bytes()
        if raw.endswith(b'session required pam_limits.so\n'):
            shutil.copy2(fallback, p / 'common-session.original')
            atomic_fixture(fallback, raw[:-len(b'session required pam_limits.so\n')])
        environment = Path('/etc/environment')
        shutil.copy2(environment, p / 'environment.original')
        atomic_fixture(environment, b'PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/games:/usr/local/games:/snap/bin"\n')
        command('apt-refresh', ['apt-get', '-o', 'APT::Update::Error-Mode=any', 'update'], 180)
        apt = {'22.04': '2.4.15ubuntu0.1', '24.04': '2.8.4ubuntu0.1', '26.04': '3.2.1ubuntu0.1'}[self.release]
        command('apt-security-baseline', ['apt-get', 'install', '-y', 'apt=' + apt], 180)
        if self.release == '24.04':
            command('libc-security-baseline', ['apt-get', 'install', '-y',
                *[name + '=2.39-0ubuntu8.12' for name in ('libc6', 'libc-bin', 'libc6-dev', 'libc-dev-bin')]], 240)
        # Restart only an empty fixture provider before installing Fluxo.
        until = time.monotonic() + 90
        while True:
            pid = run(['systemctl', 'show', 'cron.service', '--property=MainPID', '--value'])['stdout']
            group = Path('/sys/fs/cgroup/system.slice/cron.service/cgroup.procs')
            pids = group.read_text().split() if group.exists() else []
            if not pids or pids == [pid]:
                break
            require(time.monotonic() < until, 'Fixture cron failed natural drainage')
            time.sleep(.5)
        if self.engine == 'cronie':
            command('cronie-install', ['apt-get', 'install', '-y', 'cronie=1.7.1-1'], 180)
        command('cron-restart', ['systemctl', 'restart', self.engine + '.service'])
        # The shared helper's vendor archive glob is ARM-specific. On AMD64
        # restore that same authenticated stock limits file before calling it.
        limits = Path('/etc/security/limits.conf')
        wanted = ('efe8446a3c499818e7c5736c9494f222cb9098e8733aee91508b3631ad053b27'
                  if self.release == '22.04' else '0adf64deecea56e9ed5c2229db357900218b10b76f7e69830c9d209347896206')
        if self.arch == 'amd64' and digest(limits) != wanted:
            info = limits.lstat()
            require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1
                    and not info.st_mode & 0o022, 'Unsafe limits fixture metadata')
            version = run(['dpkg-query', '-W', '-f=${Version}', 'libpam-modules'])['stdout']
            command('vendor-pam-download', ['apt-get', 'download', 'libpam-modules=' + version], 120, cwd=p)
            archives = list(p.glob('libpam-modules_*_amd64.deb'))
            require(len(archives) == 1, 'Ambiguous vendor PAM archive')
            raw = subprocess.check_output(['dpkg-deb', '--fsys-tarfile', str(archives[0])], timeout=20)
            with tarfile.open(fileobj=io.BytesIO(raw), mode='r:') as archive:
                member = archive.getmember('./etc/security/limits.conf')
                require(member.isfile() and member.size <= 1 << 20, 'Unexpected PAM archive input')
                stock = archive.extractfile(member).read()
            require(hashlib.sha256(stock).hexdigest() == wanted, 'Packaged stock limits differs')
            shutil.copy2(limits, p / 'limits.conf.original')
            atomic_fixture(limits, stock)
            r['limits_vendor_archive_sha256'] = digest(archives[0])
        stock_profile_files(p, self.release, self.env, r)
        if self.arch == 'arm64':
            stock_ubuntu_sources(p, r)
        else:
            r['source_fixtures'] = []
            for name in ('/etc/apt/sources.list', '/etc/apt/sources.list.d/ubuntu.sources'):
                path = Path(name)
                if not path.exists():
                    continue
                info = path.lstat()
                require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1
                        and not info.st_mode & 0o022, 'Unsafe Ubuntu source metadata')
                raw = path.read_bytes()
                stock = raw.replace(b'mirror+file:/etc/apt/apt-mirrors.txt', b'https://archive.ubuntu.com/ubuntu')
                stock = stock.replace(b'mirror+file:/etc/apt/apt-mirrors-security.txt', b'https://security.ubuntu.com/ubuntu')
                if raw != stock:
                    shutil.copy2(path, p / (path.name + '.source.original'))
                    atomic_fixture(path, stock)
                    r['source_fixtures'].append({'path': name, 'before_sha256': hashlib.sha256(raw).hexdigest()})
        command('stock-source-refresh', ['apt-get', '-o', 'APT::Update::Error-Mode=any',
            '-o', 'Acquire::http::No-Cache=true', '-o', 'Acquire::https::No-Cache=true', 'update'], 180)
        python_local = Path('/usr/local/lib/python%d.%d/dist-packages' % sys.version_info[:2])
        if python_local.exists() and any(python_local.iterdir()):
            info = python_local.lstat()
            require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o022,
                    'Unsafe local Python fixture')
            python_local.rename(p / 'python-local.original')
            python_local.mkdir(mode=0o755)
            r['python_local_fixture'] = str(python_local)

    def cleanup(self):
        p, r = self.private, self.report
        run(['systemctl', 'stop', 'fluxo.service'], 60)
        if not self.nginx_active:
            run(['systemctl', 'stop', 'nginx.service'], 60)
        for name in ('/etc/environment', '/etc/pam.d/common-session-noninteractive',
                     '/etc/pam.d/common-session', '/etc/security/limits.conf'):
            target = Path(name)
            archive = p / (target.name + '.original')
            if archive.exists():
                shutil.copy2(archive, target)
                require(digest(archive) == digest(target), 'Fixture bytes failed restoration')
        if (p / 'apt.conf.d.original').exists():
            Path('/etc/apt/apt.conf.d').rename(p / 'apt.conf.d.tested')
            (p / 'apt.conf.d.original').rename('/etc/apt/apt.conf.d')
        for item in r.get('source_fixtures', []):
            path = Path(item['path'])
            shutil.copy2(p / (path.name + '.source.original'), path)
            require(digest(path) == item['before_sha256'], 'Source fixture failed restoration')
        if r.get('python_local_fixture'):
            path = Path(r['python_local_fixture'])
            path.rename(p / 'python-local.tested')
            (p / 'python-local.original').rename(path)
        for original, archive in self.moved:
            require(not original.exists(), 'CI tool source appeared during fixture')
            archive.rename(original)
        for unit, active in self.timers.items():
            run(['systemctl', 'start' if active else 'stop', unit])
        require(all((run(['systemctl', 'is-active', '--quiet', unit])['exit_code'] == 0) == active
                    for unit, active in self.timers.items()), 'APT timers failed restoration')
        require(run(['hostnamectl', 'set-hostname', self.hostname])['exit_code'] == 0, 'Hostname failed restoration')
        for path, mode in r.get('nginx_fixture_permissions', []):
            Path(path).chmod(mode)
        for item in r.get('fixture_directories', []):
            if item.get('fixture_permissions_hardened'):
                Path(item['path']).chmod(int(item['mode_before'], 8))
        if hasattr(self, 'umask'):
            os.umask(self.umask)
        r['fixture_cleanup_passed'] = True


def capture_invariants(seed):
    # Reuse credential decryption/normalization and exact original-column
    # comparison. Only hashes/booleans leave this root process.
    result = installation_invariants()
    with sqlite3.connect('file:/var/lib/fluxo/fluxo.db?mode=ro', uri=True) as db:
        require(db.execute('PRAGMA quick_check').fetchone() == ('ok',), 'SQLite integrity failed')
        for table in ('ssh_keys', 'commands', 'system_metadata'):
            rows = db.execute('SELECT * FROM ' + table + ' ORDER BY 1').fetchall()
            result[table] = hashlib.sha256(repr(rows).encode()).hexdigest()
    for name in ('/home/fluxo/.ssh/authorized_keys', seed['html_file']):
        result[name] = digest(name)
    for root in ('/var/lib/fluxo-scheduler/history', '/var/lib/fluxo-system-tasks/history'):
        for path in Path(root).glob('*.json'):
            result['retained'][str(path)] = digest(path)
    # Completed invocations may legitimately move from the live core to
    # immutable history during the next install. Compare their content across
    # either location rather than assuming they were already compacted.
    task_root = Path('/var/lib/fluxo-system-tasks')
    core = json.loads((task_root / 'state.json').read_text())
    invocations = dict(core['invocations'])
    for path in (task_root / 'history').glob('*.json'):
        value = json.loads(path.read_text())
        if value['unit'] in invocations:
            require(invocations[value['unit']] == value, 'Task live/history content differs')
        invocations[value['unit']] = value
    result['root_history'] = {unit: hashlib.sha256(json.dumps(value, sort_keys=True).encode()).hexdigest()
        for unit, value in invocations.items() if value['registration']['contract']['id'] == seed['root_cron_id']}
    require(result['root_history'], 'Root fixture has no retained invocation history')
    return result


def compare_invariants(before, after):
    for key, value in before.items():
        if key in ('retained', 'root_history'):
            require(all(after[key].get(path) == sha for path, sha in value.items()),
                    'Retained execution archive/helper changed or disappeared')
        else:
            require(after.get(key) == value, 'Installation invariant changed: ' + key)
    return True


def seed_application(observer, command, private):
    api = observer.API('https://127.0.0.1:9595')
    # Claim a named configured admin using its real bootstrap secret. Keeping
    # this password allows the unchanged actual-candidate observer to login;
    # its stored authentication hash is checked throughout every upgrade.
    api.login(False, 'base-owner')
    api.request('PUT', '/settings', {'admin_email': 'base-owner@example.invalid', 'default_php': '8.4'})
    site = api.request('POST', '/sites', {'domain': 'base-fixture.invalid', 'app_type': 'html',
                        'web_root': '/public', 'deployment_strategy': 'standard'})
    require(site['path'] == '/home/fluxo/base-fixture.invalid', 'HTML fixture path differs')
    target = Path(site['path']) / 'public' / 'base-acceptance.html'
    command('seed-html', ['runuser', '-u', 'fluxo', '--', 'python3', '-c',
        'import pathlib,sys; pathlib.Path(sys.argv[1]).write_text("base-native-html-ok")', str(target)])
    key = private / 'acceptance-key'
    command('seed-key', ['ssh-keygen', '-q', '-t', 'ed25519', '-N', '', '-C', 'base-native-fixture', '-f', str(key)])
    api.request('POST', '/ssh-keys', {'name': 'base-native-fixture', 'public_key': key.with_suffix('.pub').read_text()})
    history = api.request('POST', '/sites/%d/commands' % site['id'], {'command': '/usr/bin/id -un'})
    require(history['status'] == 'success' and history['output'].strip() == 'fluxo', 'Command fixture did not execute unprivileged')
    require(not list(Path('/etc/letsencrypt/renewal').glob('*.conf')), 'Root renewal fixture requires no live certificates')
    root = api.request('POST', '/crons', {'name': 'Base retained root contract', 'expression': '0 0 1 1 *',
                       'command': 'certbot renew --quiet', 'user': 'root'})
    return {'site_id': site['id'], 'html_file': str(target), 'root_cron_id': root['id'], 'ssh_key': key}


def application_checks(observer, seed, command, stage):
    api = observer.API('https://127.0.0.1:9595')
    api.login(False, 'base-owner')
    sites = api.request('GET', '/sites')
    require(any(s['id'] == seed['site_id'] and s['app_type'] == 'html' for s in sites), 'HTML site not retained')
    settings = api.request('GET', '/settings')
    require(settings['admin_email'] == 'base-owner@example.invalid', 'Admin settings changed')
    request = urllib.request.Request('http://127.0.0.1/base-acceptance.html', headers={'Host': 'base-fixture.invalid'})
    with urllib.request.urlopen(request, timeout=10) as response:
        require(response.status == 200 and response.read() == b'base-native-html-ok', 'HTML native HTTP failed')
    # The new private key never appears in args, environment output or artifacts.
    command(stage + '-ssh', ['ssh', '-i', str(seed['ssh_key']), '-o', 'BatchMode=yes',
        '-o', 'IdentitiesOnly=yes', '-o', 'StrictHostKeyChecking=accept-new',
        '-o', 'UserKnownHostsFile=' + str(seed['ssh_key'].parent / 'known_hosts'),
        '-o', 'ConnectTimeout=10', 'fluxo@127.0.0.1', '/usr/bin/test "$(id -un)" = fluxo'])
    return {'named_admin_login': True, 'admin_settings': True, 'html_http': True, 'ssh_public_key_login': True}


def root_execution(observer, seed):
    """Existing real certificate-renewal probe, retaining its yearly cron."""
    require(not list(Path('/etc/letsencrypt/renewal').glob('*.conf')), 'Live certificate fixture refused')
    api = observer.API('https://127.0.0.1:9595')
    api.login(False, 'base-owner')
    path = Path('/var/lib/fluxo-system-tasks/state.json')
    def core():
        return json.loads(observer.protected_read(path))
    cron_id = seed['root_cron_id']
    def received():
        entry = core()['entries'].get(str(cron_id))
        return entry if entry and entry['received'] and entry['enabled'] else None
    entry = observer.wait_for(received, 190, 'genuine retained root cron acknowledgement')
    require(entry['contract']['kind'] == 'certificate-renewal', 'Unexpected root contract')
    helper_sha = digest(entry['helper'])
    require(entry['helper'].endswith(helper_sha), 'Root retained helper image differs')
    def invocations():
        values = dict(core()['invocations'])
        for item in (path.parent / 'history').glob('*.json'):
            value = json.loads(observer.protected_read(item))
            values[value['unit']] = value
        return values
    known = set(invocations())
    api.request('POST', '/crons/%d/run' % cron_id)
    def completed():
        matches = [v for unit, v in invocations().items() if unit not in known
                   and v['registration']['contract']['id'] == cron_id]
        require(len(matches) <= 1, 'Duplicate root submission')
        return matches[0] if matches and matches[0]['status'] in ('completed', 'failed', 'blocked') else None
    value = observer.wait_for(completed, 100, 'root contract terminal evidence')
    require(value['status'] == 'completed' and value['result_profile'], 'Root contract execution failed')
    group = Path('/sys/fs/cgroup/system.slice') / value['unit']
    observer.wait_for(lambda: not group.exists() or ('populated 0' in (group / 'cgroup.events').read_text()
        and int((group / 'pids.current').read_text()) == 0), 30, 'root task natural drainage')
    require(observer.command([entry['helper'], 'system-task-execute', value['unit']], 60).returncode != 0,
            'Consumed root execution authority replayed')
    return {'passed': True, 'genuine_receipt': True, 'completed_result_profile': True,
            'natural_drain': True, 'consumed_replay_refused': True, 'yearly_cron_retained': True,
            'helper_sha256': helper_sha, 'live_acme': False}


def verify_installed(image, env):
    require(digest('/usr/local/bin/fluxo') == image, 'Installed binary SHA differs')
    pid = run(['systemctl', 'show', 'fluxo.service', '-p', 'MainPID', '--value'])['stdout']
    require(pid.isdigit() and int(pid) > 1 and digest('/proc/' + pid + '/exe') == image,
            'Native running panel image differs')
    result = run(['/usr/local/bin/fluxo', 'execution-compatibility', 'inspect'], 90, env)
    require(result['exit_code'] == 0 and json.loads(result['stdout'])['compatible'], 'Installed execution compatibility failed')
    return {'installed_sha256': image, 'running_proc_exe_sha256': image, 'execution_compatible': True}


def native_test(command, name, binary, test):
    # Exit zero with a Go SKIP is not native coverage.
    raw = command(name, [str(binary), '-test.run=^' + test + '$', '-test.v', '-test.timeout=4m'], 260)
    require('--- PASS: ' + test in raw and '--- SKIP:' not in raw, 'Native test skipped or did not run: ' + test)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--build-dir', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--mode', choices=('fresh', 'bridge'), required=True)
    parser.add_argument('--engine', choices=('cron', 'cronie'), default='cron')
    args = parser.parse_args()
    arch, release = hosted_profile()
    require((arch, release, args.engine) in HOSTS and (args.engine != 'cronie' or args.mode == 'bridge'), 'Unsupported native profile')
    require(not any(os.path.lexists(p) for p in ('/var/lib/fluxo', '/usr/local/bin/fluxo', '/home/fluxo',
                '/var/lib/fluxo-scheduler', '/var/lib/fluxo-system-tasks')), 'Refuse existing installation')
    args.output.mkdir(mode=0o755, parents=True, exist_ok=False)
    private = Path(tempfile.mkdtemp(prefix='fluxo-base-candidate-', dir='/root'))
    private.chmod(0o700)
    report = {'passed': False, 'mode': args.mode, 'architecture': arch, 'ubuntu': release, 'engine': args.engine,
              'internal_version': VERSION, 'public_base_delivery': False, 'public_base_release': False,
              'production_approved': False, 'transport': 'explicit-local-sha256', 'steps': {}}
    env = {'PATH': '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin', 'HOME': '/root',
           'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8', 'DEBIAN_FRONTEND': 'noninteractive', 'NEEDRESTART_MODE': 'l',
           'FLUXO_ENV': 'prod', 'FLUXO_DATA_DIR': '/var/lib/fluxo', 'FLUXO_PHP_UPDATE_VM_TEST': '1'}
    def command(name, argv, timeout=120, cwd=None):
        report['stage'] = name
        log = private / (name + '.log')
        began = time.monotonic()
        with log.open('x') as stream:
            log.chmod(0o600)
            try:
                result = subprocess.run(argv, env=env, cwd=cwd, stdin=subprocess.DEVNULL,
                                        stdout=stream, stderr=subprocess.STDOUT, timeout=timeout)
                code = result.returncode
            except subprocess.TimeoutExpired:
                code = 124
        report['steps'][name] = {'exit_code': code, 'private_log_sha256': digest(log),
                                 'duration_seconds': round(time.monotonic() - began, 3)}
        raw = log.read_text(errors='replace')
        (args.output / (name + '-sanitized.log')).write_text(sanitized_diagnostic(raw) + '\n')
        require(code == 0, 'Command failed: ' + name)
        return raw
    fixture = HostedFixture(private, report, env, command, arch, release, args.engine)
    try:
        # Builder sidecar verification is deliberately performed BEFORE fixture
        # preparation. Source/installer and exact native artifact must all agree.
        manifest = verify_build(args.build_dir, arch)
        report['candidate'] = manifest
        for name in ('fluxo', 'scheduler.test', 'maintenance.test', 'executiondomain.test'):
            shutil.copyfile(args.build_dir / name, private / name)
            (private / name).chmod(0o700)
        image = digest(private / 'fluxo')
        require(image == manifest['binary_sha256'], 'Protected candidate copy differs')
        fixture.prepare()
        observer = observer_module()
        seed = None
        if args.mode == 'bridge':
            subject = public_bridge()
            report['public_bridge'] = subject
            installer = private / 'bridge-install.sh'
            download_public_subject(subject, 'install.sh', installer, subject['installer_sha256'])
            download_public_subject(subject, 'fluxo-linux-' + arch, private / 'public-bridge', subject[arch + '_sha256'])
            # Ordinary public installer downloads/verifies its own release
            # assets/provenance. No local override or attestation bypass.
            env['FLUXO_VERSION'] = subject['public_tag']
            raw = command('public-bridge-install', ['bash', str(installer), *FLAGS], 1200)
            require('Fluxo release provenance verified' in raw and 'Skipping release attestation' not in raw,
                    'Bridge installer did not authenticate public provenance')
            verify_installed(subject[arch + '_sha256'], env)
            command('bridge-nginx-start', ['systemctl', 'start', 'nginx.service'])
            seed = seed_application(observer, command, private)
            report['bridge_application'] = application_checks(observer, seed, command, 'bridge')
            report['bridge_root_execution'] = root_execution(observer, seed)
            await_predecessor_checks(Path('/var/lib/fluxo/fluxo.db'), report)
            before = capture_invariants(seed)
            env.pop('FLUXO_VERSION')
        env['FLUXO_LOCAL_BINARY_SHA256'] = image
        # Install an exact protected copy of the FINAL-source installer.
        installer = private / 'base-install.sh'
        shutil.copyfile('install.sh', installer)
        require(digest(installer) == manifest['installer_sha256'], 'Staged base installer differs')
        raw = command('base-install', ['bash', str(installer), '--local-binary=' + str(private / 'fluxo'), *FLAGS], 1200)
        report['base_native'] = verify_installed(image, env)
        if args.mode == 'bridge':
            require('Completed bridge verified. Continuing to the base automatically.' in raw,
                    'Expected automatic public bridge handoff')
            report['bridge_preservation'] = compare_invariants(before, capture_invariants(seed))
        else:
            floor = state_summary(Path('/var/lib/fluxo/execution-readers.json'))
            require(floor.get('phase') == 'committed' and floor.get('scheduler_schema') == 5
                    and floor.get('system_task_schema') == 3, 'Fresh direct current floor not committed')
            report['fresh_direct_schema5_task3'] = True
            command('base-nginx-start', ['systemctl', 'start', 'nginx.service'])
            seed = seed_application(observer, command, private)
        report['base_application'] = application_checks(observer, seed, command, 'base')
        report['base_root_execution'] = root_execution(observer, seed)
        await_predecessor_checks(Path('/var/lib/fluxo/fluxo.db'), report)
        before = capture_invariants(seed)
        admission = run([str(private / 'fluxo'), 'base-install', 'inspect'], 110, env)
        require(admission['exit_code'] == 0 and json.loads(admission['stdout'])['mode'] == 'base',
                'Ordinary update must start in base mode')
        command('ordinary-base-update', ['bash', str(installer), '--local-binary=' + str(private / 'fluxo'), *FLAGS], 1200)
        report['update_native'] = verify_installed(image, env)
        report['update_preservation'] = compare_invariants(before, capture_invariants(seed))
        report['update_application'] = application_checks(observer, seed, command, 'update')
        report['update_root_execution'] = root_execution(observer, seed)
        # Native stock-provider/profile checks retain all original exact-host
        # selectors. No test binaries may silently skip at a mismatched host.
        native_test(command, 'native-apt-fences', private / 'maintenance.test', 'TestVMAPTNoQueueArchiveFence')
        if release == '26.04':
            native_test(command, 'native-apt-solver3-fences', private / 'maintenance.test', 'TestVMAPTNoQueueStockSolver3')
        if arch == 'arm64':
            native_test(command, 'native-loader', private / 'scheduler.test', 'TestVMARM64LoaderClosure')
            native_test(command, 'native-loader-refusals', private / 'scheduler.test', 'TestVMARM64LoaderRefusals')
            native_test(command, 'native-maintenance', private / 'maintenance.test', 'TestVMARM64MaintenanceProfiles')
        native_test(command, 'native-bootstrap', private / 'executiondomain.test', 'TestVMNonLeaderSchedulerBootstrapABI')
        if release == '22.04':
            native_test(command, 'native-domain-lifetime', private / 'executiondomain.test', 'TestVMPersistent249DomainLifetime')
        previous = set(Path('/var/lib').glob('fluxo-candidate-cli-*/result.json'))
        command('guarded-scheduler', ['python3', 'internal/services/scheduler/actual_candidate_vm_acceptance.py',
                                    '--expected-sha256', image], 420)
        results = set(Path('/var/lib').glob('fluxo-candidate-cli-*/result.json')) - previous
        require(len(results) == 1, 'Missing unique genuine Scheduler result')
        observed = json.loads(results.pop().read_text())
        keys = ('passed', 'genuine_cron', 'same_image_dispatcher_bootstrap_worker', 'natural_empty',
                'late_registration_launch_refused', 'no_duplicate_application',
                'active_payload_excludes_maintenance_and_natural_drain_releases')
        require(all(observed.get(key) is True for key in keys), 'Genuine native Scheduler execution failed')
        report['guarded_scheduler'] = {key: True for key in keys}
        # All yearly acceptance fixtures remain registered. Never remove a job
        # to gain upgrade quiescence; the runner itself is disposable.
        report['execution_state'] = {name: state_summary(Path(path)) for name, path in {
            'floor': '/var/lib/fluxo/execution-readers.json', 'scheduler': '/var/lib/fluxo-scheduler/state.json',
            'tasks': '/var/lib/fluxo-system-tasks/state.json'}.items()}
        report['passed'] = True
    except Exception as error:
        report['error_type'] = type(error).__name__
        report['error'] = sanitized_diagnostic(str(error))
    finally:
        report['installation_timing'] = {
            'method': 'time.monotonic around existing installer subprocesses; no extra restart or requests',
            'samples_seconds': {name: report['steps'][name]['duration_seconds'] for name in
                ('public-bridge-install', 'base-install', 'ordinary-base-update') if name in report['steps']},
            'base_install_kind': 'fresh' if args.mode == 'fresh' else 'bridge-to-base',
            'gate': False,
            'caveats': ['Public bridge downloads and provenance verification differ from local-SHA transport.',
                        'Later installs use warm package/download caches on the same fixture.',
                        'These end-to-end case timings do not establish a universal speedup.'],
        }
        try:
            fixture.cleanup()
        except Exception as error:
            report['passed'] = False
            report['cleanup_error_type'] = type(error).__name__
            report['cleanup_error'] = sanitized_diagnostic(str(error))
        (args.output / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
        print(json.dumps({'passed': report['passed'], 'stage': report.get('stage'),
                          'error_type': report.get('error_type'), 'cleanup_passed': report.get('fixture_cleanup_passed', False)}))
    return 0 if report['passed'] else 1


def verify_build(build, arch):
    """Check the builder's sidecar and native execution, never a public claim."""
    manifest = json.loads((build / 'fluxo.manifest.json').read_text())
    # Sidecar interface shared with build_release_candidate.py.
    identity = source_identity()
    require(identity['source'] == os.environ.get('GITHUB_SHA'), 'Checkout differs from workflow source')
    for key, value in identity.items():
        require(manifest.get(key) == value, 'Candidate source identity differs: ' + key)
    require(manifest['binary_sha256'] == digest(build / 'fluxo'), 'Candidate binary differs')
    require(manifest['goarch'] == arch and manifest['goos'] == 'linux' and manifest['cgo_enabled'] is False,
            'Native CGo-free Linux build required')
    require(manifest['version'] == VERSION, 'Internal dev build required')
    assignments = manifest['official_pin_assignments']
    require(len(assignments) == 19 and all(key + '=' + value in manifest['build_metadata']
            for key, value in assignments.items()), 'Missing official pin build assignments')
    metadata = run([str((build / 'fluxo').resolve()), '--version'])
    require(metadata['exit_code'] == 0 and metadata['stdout'] == 'fluxo version ' + VERSION, 'Native internal dev version required')
    # Safe allowlist: never copy ambient environment or arbitrary manifest fields.
    return {key: manifest[key] for key in ('source', 'source_tree', 'installer_sha256', 'binary_sha256',
            'tool_pins_sha256', 'goarch', 'go_version', 'version')}


if __name__ == '__main__':
    raise SystemExit(main())
