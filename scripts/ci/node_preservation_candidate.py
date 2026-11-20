#!/usr/bin/env python3
"""One disposable native AMD64 or ARM64 Ubuntu 24.04 Node preservation case.

Build the embedded UI, then build_release_candidate.py DIR/fluxo 0.0.0-base.dev.
See node-preservation-candidate.yml for the root hosted-runner opt-ins.
Compose the fresh base helpers here so HostedFixture remains in effect through
all Node updates, without running the unrelated base native test matrix.
No publication, authority repair, synthetic receipt or runtime apply is used.
Raw logs, credentials, database and SSH key stay in a private /root directory.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import selectors
import shutil
import signal
import stat
import subprocess
import tempfile
import threading
import time
import traceback

import base_candidate as base
from arm64_candidate import optional_runtime_fixture


HARNESS_PATHS = ('scripts/ci/node_preservation_candidate.py',
                 '.github/workflows/node-preservation-candidate.yml')
STATE = Path('/var/lib/fluxo/node-toolchain.json')
PAYLOADS = ('/opt/fluxo/node', '/opt/fluxo/node-toolchain', '/home/fluxo/.cache/node/corepack')
COMMANDS = ('node', 'npm', 'npx', 'corepack', 'pnpm', 'pnpx', 'yarn', 'yarnpkg', 'bun', 'bunx')
HOSTED_SHIMS = (*COMMANDS, 'pm2')
REUSED = 'Node.js toolchain already verified; no changes needed.'
BASELINE_COMMIT = '43a1f21aa1de06000911f650ffbea9e90df255a0'
BASELINE_SHA256 = '292c1572ec3fe684c93eb68d073303ef5d3a447e47a1818bc225cc94c0f35aa5'
# Existing stdout boundaries only: neither installer nor runtime is instrumented.
# Prefixes with variable paths/versions deliberately export only a fixed name.
INSTALLER_BOUNDARIES = (
    ('repository', ('Preparing the official PHP package repository...',)),
    ('dependencies', ('Preparing Fluxo dependencies...',)),
    ('firewall', ('Initializing UFW Firewall safely...',)),
    ('binary', ('Installing binary to /usr/local/bin...',)),
    ('upgrade_snapshot', ('Creating a pre-upgrade rollback snapshot at ',)),
    ('composer', ('Installing release-authenticated Composer ',)),
    ('wp_cli', ('Installing release-authenticated WP-CLI ',)),
    ('node_outer_snapshot', ('Creating an independent Node.js toolchain rollback snapshot...',)),
    ('node_toolchain', ('Installing and verifying the Node.js toolchain...',
                        'Verifying the existing Node.js toolchain...')),
    ('node_done', ('Node.js toolchain installed successfully.', 'Node.js toolchain ready.')),
    ('systemd', ('Configuring systemd service...',)),
    ('health_wait', ('Waiting for Fluxo daemon to become ready...',)),
    ('healthy', ('Daemon is responding.',)),
    ('success', ('Fluxo ' + base.VERSION + ' installed successfully!',)),
)
INSTALLER_PHASES = ('preflight', 'php_repository_and_apt_update', 'apt_dependencies',
                    'firewall_user_and_security', 'candidate_staging', 'upgrade_snapshot_and_stop',
                    'composer', 'wp_cli', 'node_outer_snapshot', 'node_toolchain',
                    'binary_activation', 'systemd_and_security', 'dashboard_health_wait',
                    'post_health_commit_and_cleanup', 'completion')


def hosted_profile():
    arch, release = base.hosted_profile()
    base.require(os.environ.get('FLUXO_NODE_PRESERVATION_CANDIDATE') == '1'
                 and arch in ('amd64', 'arm64') and release == '24.04',
                 'Explicit native AMD64/ARM64 Ubuntu 24.04 Node fixture opt-in required')
    base.require(all(re.fullmatch(r'[A-Za-z0-9_.-]{1,100}', os.environ.get(key, ''))
                     for key in ('ImageOS', 'ImageVersion')), 'Exact hosted runner image metadata required')
    return arch, release


def harness_identity():
    changed = subprocess.check_output(['git', '-c', 'safe.directory=' + os.getcwd(),
        'status', '--porcelain', '--untracked-files=all', '--', *HARNESS_PATHS], text=True)
    base.require(not changed, 'Node harness or workflow differs from the recorded source')
    return {path: base.digest(path) for path in HARNESS_PATHS}


def materialize_baseline(private):
    """Exact historical installer; the binary and official pins stay current."""
    result = subprocess.run(['git', '-c', 'safe.directory=' + os.getcwd(),
        'show', BASELINE_COMMIT + ':install.sh'], capture_output=True, timeout=20, check=True)
    base.require(hashlib.sha256(result.stdout).hexdigest() == BASELINE_SHA256,
                 'Pre-optimization installer SHA differs')
    path = private / 'pre-optimization-install.sh'
    with path.open('xb') as stream:
        path.chmod(0o600)
        stream.write(result.stdout)
    base.require(base.digest(path) == BASELINE_SHA256, 'Protected baseline installer differs')
    return path


def installer_timing(boundaries, duration, complete=False):
    keys = [key for key, _ in INSTALLER_BOUNDARIES]
    if complete:
        base.require([item['boundary'] for item in boundaries] == keys,
                     'Installer stdout phase boundaries missing, repeated or out of order')
    points = [{'boundary': 'start', 'seconds': 0.0}, *boundaries,
              {'boundary': 'exit', 'seconds': duration}]
    phases = []
    if [item['boundary'] for item in boundaries] == keys:
        phases = [{'phase': name, 'start_seconds': left['seconds'], 'end_seconds': right['seconds'],
                   'duration_seconds': round(right['seconds'] - left['seconds'], 3)}
                  for name, left, right in zip(INSTALLER_PHASES, points, points[1:])]
    return {'method': 'external time.monotonic at existing combined stdout/stderr line boundaries',
            'total_seconds': duration, 'boundaries': points, 'phases': phases,
            'caveat': 'Output arrival times approximate phase boundaries; APT phases include surrounding repository/guard work.'}


def logged_process(argv, env, cwd, log, timeout, start, boundaries=None, boundary_rules=None):
    """Tee output privately, observing line arrivals without blocking deadlines."""
    with log.open('xb') as stream, selectors.DefaultSelector() as selector:
        log.chmod(0o600)
        process = subprocess.Popen(argv, env=env, cwd=cwd, stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, start_new_session=True)
        pending = b''
        def kill_group():
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
        try:
            os.set_blocking(process.stdout.fileno(), False)
            selector.register(process.stdout, selectors.EVENT_READ)
            while selector.get_map() or process.poll() is None:
                remaining = start + timeout - time.monotonic()
                if remaining <= 0:
                    kill_group()
                    process.wait(timeout=10)
                    return 124
                for key, _ in selector.select(min(0.1, remaining)):
                    chunk = os.read(key.fd, 65536)
                    if not chunk:
                        selector.unregister(key.fileobj)
                        continue
                    stream.write(chunk)
                    if boundaries is not None:
                        pending += chunk
                        lines = pending.split(b'\n')
                        pending = lines.pop()
                        for line in lines:
                            text = line.decode('utf-8', errors='replace').strip()
                            for name, prefixes in (INSTALLER_BOUNDARIES if boundary_rules is None else boundary_rules):
                                if text.startswith(prefixes):
                                    boundaries.append({'boundary': name,
                                        'seconds': round(time.monotonic() - start, 6)})
                                    break
                        # No installer boundary is remotely this long.
                        if len(pending) > 65536:
                            pending = b''
            return process.wait(timeout=10)
        except BaseException:
            # The global deadline may interrupt even after the direct child
            # exits; terminate any foreground descendants still holding stdout.
            kill_group()
            raise
        finally:
            if process.poll() is None:
                kill_group()
                process.wait(timeout=10)
            process.stdout.close()


class DashboardObserver:
    """Read-only TLS health samples for exactly the paired installer commands."""
    interval, timeout = 0.25, 0.7

    def __init__(self, certificate, private):
        # Freeze the protected installed certificate as the only trusted CA.
        self.ca = private / 'dashboard-observer-ca.pem'
        if self.ca.exists():
            base.require(self.ca.read_bytes() == certificate, 'Dashboard observer TLS certificate changed')
        else:
            with self.ca.open('xb') as stream:
                self.ca.chmod(0o600)
                stream.write(certificate)
        self.samples = []
        self.stop_event = threading.Event()

    def sample(self):
        start = time.monotonic()
        healthy = False
        try:
            result = subprocess.run(['/usr/bin/curl', '--silent', '--show-error', '--fail',
                '--noproxy', '*', '--proto', '=https', '--cacert', str(self.ca),
                '--connect-timeout', str(self.timeout), '--max-time', str(self.timeout),
                '--max-filesize', '4096', 'https://127.0.0.1:9595/api/v1/health'],
                capture_output=True, timeout=1.7, stdin=subprocess.DEVNULL,
                env={'PATH': '/usr/bin:/bin', 'LANG': 'C.UTF-8'})
            healthy = result.returncode == 0 and json.loads(result.stdout) == {'status': 'ok'}
        except (OSError, ValueError, subprocess.TimeoutExpired):
            pass  # Fixed numeric/boolean observations only, never raw HTTP or secrets.
        self.samples.append((start, time.monotonic(), healthy))
        return healthy

    def start(self):
        base.require(self.sample(), 'Paired measurement requires initially healthy verified TLS dashboard')
        def poll():
            while not self.stop_event.wait(max(0, self.interval - (time.monotonic() - self.samples[-1][0]))):
                self.sample()
        self.thread = threading.Thread(target=poll, daemon=True)
        self.thread.start()

    def finish(self, origin):
        self.stop_event.set()
        self.thread.join(timeout=3)
        base.require(not self.thread.is_alive(), 'Dashboard health observer exceeded shutdown deadline')
        self.sample()
        windows, opened, previous = [], None, None
        for start, end, healthy in self.samples:
            if not healthy:
                if opened is None:
                    opened = {'preceding_healthy_seconds': None if previous is None else previous[0] - origin,
                              'first_failed_seconds': start - origin}
                opened['last_failed_seconds'] = start - origin
            elif opened is not None:
                opened['next_healthy_seconds'] = start - origin
                windows.append(opened)
                opened = None
            previous = (start, end, healthy)
        if opened is not None:
            opened['next_healthy_seconds'] = None
            windows.append(opened)
        for window in windows:
            left, right = window['preceding_healthy_seconds'], window['next_healthy_seconds']
            window['approximate_unavailable_seconds'] = None if left is None or right is None else round(
                (window['last_failed_seconds'] + right - left - window['first_failed_seconds']) / 2, 3)
            for key, value in window.items():
                if value is not None:
                    window[key] = round(value, 3)
        estimates = [window['approximate_unavailable_seconds'] for window in windows]
        return {'endpoint': 'https://127.0.0.1:9595/api/v1/health', 'verified_tls': True,
                'poll_interval_seconds': self.interval, 'request_timeout_seconds': self.timeout,
                'observer_process_timeout_seconds': 1.7,
                'samples': len(self.samples), 'failed_samples': sum(not item[2] for item in self.samples),
                'initial_healthy': self.samples[0][2], 'final_healthy': self.samples[-1][2],
                'maximum_sample_gap_seconds': round(max((b[0] - a[0] for a, b in
                    zip(self.samples, self.samples[1:])), default=0), 3),
                'approximate_unavailable_seconds': None if None in estimates else round(sum(estimates), 3),
                'failure_windows': windows,
                'caveat': 'Midpoints of healthy/failed request-start brackets; timeouts and polling gaps add uncertainty. '
                          'Includes immediate pre/post probes. Loopback dashboard health, not fleet or browser availability.'}


def protected_state():
    for path in (STATE, *STATE.parents):
        info = path.lstat()
        base.require(info.st_uid == 0 and info.st_gid == 0 and not info.st_mode & 0o022,
                     'Unsafe Node receipt ownership or parent permissions')
        if path == STATE:
            base.require(stat.S_ISREG(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o600
                         and info.st_nlink == 1 and info.st_size <= 65536,
                         'Node receipt must be a private, single-link root-owned regular file')
        else:
            base.require(stat.S_ISDIR(info.st_mode), 'Symlinked Node receipt parent')
    return json.loads(STATE.read_text())


def selection(state):
    return {key: value for key, value in state.items() if key != 'integrity'}


def receipt():
    proof = protected_state().get('integrity')
    base.require(isinstance(proof, dict) and proof.get('schema') == 2, 'Missing Node preservation receipt')
    base.require(all(re.fullmatch('[0-9a-f]{64}', proof.get(key, ''))
                     for key in ('profile', 'selection')), 'Invalid Node receipt identity')
    trees = proof.get('trees', {})
    selected = os.readlink('/opt/fluxo/node/current')
    base.require(set(trees) == {selected, PAYLOADS[1], PAYLOADS[2]+'/lastKnownGood.json',
                     PAYLOADS[2]+'/v1/pnpm/11.20.0', PAYLOADS[2]+'/v1/yarn/4.18.0'}
                 and all(re.fullmatch('[0-9a-f]{64}', value) for value in trees.values()),
                 'Incomplete Node payload receipt')
    return {'sha256': base.digest(STATE), 'root_private': True, 'schema': 2, 'trees': trees}


def payload_snapshot():
    """Independent byte/metadata oracle, including inodes and mtimes, no atimes.

    Walk without following links; include *all* managed versions/caches, not
    just the selected payload. No private file contents enter the report.
    """
    def tree(root):
        h = hashlib.sha256()
        def visit(path):
            info = path.lstat()
            record = [str(path.relative_to(root)), info.st_mode, info.st_uid, info.st_gid,
                      info.st_ino, info.st_mtime_ns]
            if stat.S_ISLNK(info.st_mode):
                record.append(os.readlink(path))
            elif stat.S_ISREG(info.st_mode):
                record.extend([info.st_size, base.digest(path)])
            else:
                base.require(stat.S_ISDIR(info.st_mode), 'Unexpected special Node payload file')
            h.update(json.dumps(record, separators=(',', ':')).encode() + b'\n')
            if stat.S_ISDIR(info.st_mode):
                for child in sorted(path.iterdir()):
                    visit(child)
        visit(root)
        return h.hexdigest()
    links = {}
    for name in COMMANDS:
        path = Path('/usr/local/bin') / name
        info = path.lstat()
        base.require(stat.S_ISLNK(info.st_mode) and info.st_uid == 0, 'Unmanaged Node command link')
        links[name] = [os.readlink(path), info.st_ino, info.st_mtime_ns]
    return {'trees': {path: tree(Path(path)) for path in PAYLOADS}, 'links': links}


def node_versions(observer, manifest):
    api = observer.API('https://127.0.0.1:9595')
    api.login(False, 'base-owner')
    info = api.request('GET', '/server/node/info')
    base.require(info.get('toolchain_ready') and info.get('managed')
                 and info.get('binary') == '/usr/local/bin/node', 'Managed Node API not ready')
    versions = {key: info[key] for key in ('version', 'npm', 'corepack', 'pnpm', 'yarn', 'bun')}
    pins = manifest['official_pin_assignments']
    for key, pin in (('version', 'Node'), ('corepack', 'Corepack'), ('pnpm', 'PNPM'),
                     ('yarn', 'Yarn'), ('bun', 'Bun')):
        base.require(versions[key].removeprefix('v') ==
                     pins['fluxo/internal/services/nodetoolchain.Pinned' + pin + 'Version'],
                     'Node component differs from official pin: ' + key)
    base.require(re.fullmatch(r'\d+\.\d+\.\d+', versions['npm']), 'Invalid npm version')
    return versions


def other_runtime_snapshot():
    """Observe the optional/PHP runtimes; no repair or package changes."""
    result = base.run(['dpkg-query', '-W', '-f=${binary:Package}\t${db:Status-Status}\t${Version}\n'], 30)
    base.require(result['exit_code'] == 0, 'Runtime package observation failed')
    packages = sorted(line for line in result['stdout'].splitlines()
        if re.match(r'^(?:php|libapache2-mod-php|mariadb|mysql|postgresql|redis|python3|libpython3|nodejs|npm)(?:[\d:.\t-]|$)', line))
    return {'packages_sha256': hashlib.sha256('\n'.join(packages).encode()).hexdigest(),
            'php_cli_target': os.path.realpath('/usr/bin/php'),
            'python_state_sha256': base.digest('/var/lib/fluxo/python-toolchain.json')
                if Path('/var/lib/fluxo/python-toolchain.json').exists() else None}


def reused_output(raw, enrolled=False):
    base.require(REUSED in raw, 'Node preservation path was not exercised')
    # Installer-wide dependency preparation is expected. These markers identify
    # the original Node repair phases, which must never run during preservation.
    forbidden = ('with the npm registry', 'Installing Corepack', 'Preparing pnpm', 'Preparing Yarn',
                 'Downloading Node.js', 'Installing Node.js', 'Downloading Bun',
                 'Verifying and installing Bun', 'Checking Node.js system prerequisites',
                 'Creating the Node.js toolchain rollback snapshot', 'restoring the previous Node.js toolchain')
    base.require(not any(marker.lower() in raw.lower() for marker in forbidden),
                 'Node preservation unexpectedly ran installation/repair work')
    if enrolled:
        base.require('Authenticating the installed Node.js release' in raw,
                     'Absent-proof enrollment did not authenticate Node.js')
        for component in ('Corepack', 'pnpm', 'Yarn', 'Bun'):
            base.require('Authenticating existing ' + component in raw,
                         'Absent-proof enrollment did not authenticate ' + component)
    else:
        base.require('Authenticating' not in raw and 'registry.npmjs.org' not in raw,
                     'Proven reuse unexpectedly performed artifact authentication')


def restore_runtime_fixture(private, report):
    # Also handles partial optional_runtime_fixture failures after archiving a
    # shim. Only that helper's explicit hosted shim names can be restored.
    for name in HOSTED_SHIMS:
        archived = private / ('hosted-shim-' + name)
        if os.path.lexists(archived):
            path = Path('/usr/local/bin') / name
            if os.path.lexists(path):
                path.rename(private / ('tested-shim-' + name))
            archived.rename(path)
    if 'home_default_acl_before_hex' in report:
        os.setxattr('/home', 'system.posix_acl_default', bytes.fromhex(report['home_default_acl_before_hex']))
    report['runtime_fixture_cleanup_passed'] = True


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--build-dir', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    arch, release = hosted_profile()
    base.require(not any(os.path.lexists(path) for path in (
        '/var/lib/fluxo', '/usr/local/bin/fluxo', '/home/fluxo', '/opt/fluxo',
        '/var/lib/fluxo-scheduler', '/var/lib/fluxo-system-tasks')), 'Refuse existing installation')
    args.output.mkdir(mode=0o755, parents=True, exist_ok=False)
    private = Path(tempfile.mkdtemp(prefix='fluxo-node-candidate-', dir='/root'))
    private.chmod(0o700)
    report = {'passed': False, 'architecture': arch, 'ubuntu': release, 'mode': 'fresh',
              'public_delivery': False, 'production_approved': False, 'steps': {},
              'runner_image': {key: os.environ.get(key) for key in ('ImageOS', 'ImageVersion')},
              'timing_caveat': 'Local SHA transport, warm caches, no active Node application fleet; no universal speedup claim.'}
    env = {'PATH': '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin', 'HOME': '/root',
           'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8', 'DEBIAN_FRONTEND': 'noninteractive', 'NEEDRESTART_MODE': 'l',
           'FLUXO_ENV': 'prod', 'FLUXO_DATA_DIR': '/var/lib/fluxo'}
    began = time.monotonic()
    deadline = began + 32 * 60

    def command(name, argv, timeout=120, cwd=None, measure_installer=False, observe_dashboard=False, expected_exit=0):
        report['stage'] = name
        base.require(time.monotonic() < deadline, 'Node acceptance time budget exhausted')
        print(json.dumps({'stage': name, 'status': 'started'}), flush=True)
        log = private / (name + '.log')
        monitor = None
        if observe_dashboard:
            base.require(name in ('pre-optimization-node-baseline', 'ordinary-node-warm-update'),
                         'Health polling is restricted to the paired installer commands')
            monitor = DashboardObserver(observer.protected_read('/var/lib/fluxo/tls/cert.pem'), private)
            monitor.start()
        start = time.monotonic()
        boundaries = [] if measure_installer else None
        try:
            code = logged_process(argv, env, cwd, log, min(timeout, deadline - start), start, boundaries)
            duration = round(time.monotonic() - start, 6)
        finally:
            if monitor is not None:
                report.setdefault('dashboard_observations', {})[name] = monitor.finish(start)
        report['steps'][name] = {'exit_code': code, 'expected_exit': expected_exit, 'private_log_sha256': base.digest(log),
                                 'duration_seconds': round(duration, 3)}
        if measure_installer:
            report['steps'][name]['installer_timing'] = installer_timing(boundaries, duration)
        raw = log.read_text(errors='replace')
        (args.output / (name + '-sanitized.log')).write_text(base.sanitized_diagnostic(raw) + '\n')
        base.require(code == expected_exit, 'Command failed: ' + name)
        if measure_installer:
            installer_timing(boundaries, duration, complete=True)
        if monitor is not None:
            base.require(report['dashboard_observations'][name]['final_healthy'],
                         'Paired installer did not restore verified TLS dashboard health')
        return raw

    fixture = base.HostedFixture(private, report, env, command, arch, release, 'cron')
    def budget_exhausted(_signal, _frame):
        raise TimeoutError('Node acceptance time budget exhausted')
    previous_alarm = signal.signal(signal.SIGALRM, budget_exhausted)
    signal.setitimer(signal.ITIMER_REAL, max(0.001, deadline - time.monotonic()))
    try:
        report['candidate'] = base.verify_build(args.build_dir, arch)
        report['harness_sha256'] = harness_identity()
        manifest = json.loads((args.build_dir / 'fluxo.manifest.json').read_text())
        binary, installer = private / 'fluxo', private / 'install.sh'
        shutil.copyfile(args.build_dir / 'fluxo', binary)
        binary.chmod(0o700)
        shutil.copyfile(args.build_dir / 'install.sh', installer)
        installer.chmod(0o600)
        image = report['candidate']['binary_sha256']
        base.require(base.digest(binary) == image and base.digest(installer) ==
                     report['candidate']['installer_sha256'], 'Protected candidate/installer copy differs')
        baseline_installer = materialize_baseline(private)
        report['baseline'] = {'label': 'Pre-optimization installer baseline with current unchanged explicit install implementation '
                                      '(minor integrity-field invalidation addition)',
                              'installer_commit': BASELINE_COMMIT, 'installer_sha256': BASELINE_SHA256,
                              'unmodified_installer': True, 'old_public_release': False,
                              'binary_sha256': image, 'official_pin_assignments': manifest['official_pin_assignments'],
                              'transport': 'same current candidate --local-binary with explicit SHA-256'}
        env['FLUXO_LOCAL_BINARY_SHA256'] = image
        fixture.prepare()
        optional_runtime_fixture(private, report)
        # setup-node lives outside the stock PATH. Quarantine hosted /usr/local
        # JS shims without executing or trusting their contents/ownership. They
        # remain preserved for cleanup; the tested installation must create its
        # own protected runtime. This is disposable runner preparation only.
        parent = Path('/usr/local/bin').lstat()
        base.require(stat.S_ISDIR(parent.st_mode) and parent.st_uid == 0
                     and not parent.st_mode & 0o022, 'Unsafe hosted command parent')
        for name in ('node', 'npm', 'npx'):
            path = Path('/usr/local/bin') / name
            if os.path.lexists(path):
                info = path.lstat()
                report.setdefault('hosted_node_shim_metadata', {})[name] = {
                    'uid': info.st_uid, 'gid': info.st_gid, 'mode': info.st_mode,
                    'nlink': info.st_nlink, 'symlink': stat.S_ISLNK(info.st_mode)}
                base.require(stat.S_ISLNK(info.st_mode) or stat.S_ISREG(info.st_mode),
                             'Unsupported hosted Node command shim type')
                path.rename(private / ('hosted-shim-' + name))
                report['optional_hosted_shims_archived'].append(name)
        base.require(shutil.which('node', path=env['PATH']) is None
                     and shutil.which('npm', path=env['PATH']) is None,
                     'Unexpected distro/external Node runtime in stock fixture PATH')
        observer = base.observer_module()
        prefix = ['bash', str(installer), '--local-binary=' + str(binary)]

        # Same fresh base setup, real seed/receipt and ordinary --no-node update
        # as base_candidate.main; keep its fixture open for the affected path.
        command('base-install-no-node', [*prefix, *base.FLAGS], 900)
        report['fresh_native'] = base.verify_installed(image, env)
        floor = base.state_summary(Path('/var/lib/fluxo/execution-readers.json'))
        base.require(floor.get('phase') == 'committed' and floor.get('scheduler_schema') == 5
                     and floor.get('system_task_schema') == 3, 'Fresh base floor differs')
        command('base-nginx-start', ['systemctl', 'start', 'nginx.service'])
        seed = base.seed_application(observer, command, private)
        report['fresh_application'] = base.application_checks(observer, seed, command, 'fresh')
        report['fresh_root_execution'] = base.root_execution(observer, seed)
        base.await_predecessor_checks(Path('/var/lib/fluxo/fluxo.db'), report)
        before = base.capture_invariants(seed)
        command('ordinary-base-update-no-node', [*prefix, *base.FLAGS], 600)
        report['base_update_native'] = base.verify_installed(image, env)
        report['base_update_preservation'] = base.compare_invariants(before, base.capture_invariants(seed))
        report['base_update_application'] = base.application_checks(observer, seed, command, 'base-update')
        report['base_update_root_execution'] = base.root_execution(observer, seed)
        base.await_predecessor_checks(Path('/var/lib/fluxo/fluxo.db'), report)
        before = base.capture_invariants(seed)

        command('explicit-node-install', ['/usr/local/bin/fluxo', 'node-toolchain', 'install'], 600)
        base.require(protected_state().get('integrity') is None,
                     'Explicit install must leave absent proof for enrollment case')
        report['receipt_absent_after_explicit_install'] = True
        report['versions_before'] = node_versions(observer, manifest)
        selected = selection(protected_state())
        report['node_setup_preservation'] = base.compare_invariants(before, base.capture_invariants(seed))
        flags = ['--node' if flag == '--no-node' else flag for flag in base.FLAGS]

        # One paired BEFORE sample, using the exact prior installer and the
        # current native binary/pins. Its original full install/repair path may
        # rewrite payload metadata; it must preserve versions, selection, data,
        # keys and genuine execution history. Never synthesize a receipt.
        baseline_before = base.capture_invariants(seed)
        runtime_before = other_runtime_snapshot()
        raw = command('pre-optimization-node-baseline', ['bash', str(baseline_installer),
            '--local-binary=' + str(binary), *flags], 600, measure_installer=True, observe_dashboard=True)
        base.require('Installing and verifying the Node.js toolchain...' in raw and REUSED not in raw
                     and 'Checking Node.js system prerequisites...' in raw
                     and 'Creating the Node.js toolchain rollback snapshot...' in raw
                     and 'with the npm registry' in raw,
                     'Pre-optimization baseline did not exercise the full Node install/repair path')
        report['baseline_native'] = base.verify_installed(image, env)
        report['versions_after_baseline'] = node_versions(observer, manifest)
        base.require(report['versions_after_baseline'] == report['versions_before']
                     and selection(protected_state()) == selected
                     and other_runtime_snapshot() == runtime_before,
                     'Pre-optimization baseline unexpectedly changed runtime versions or selection')
        report['baseline']['other_runtimes_unchanged'] = True
        report['baseline_preservation'] = base.compare_invariants(baseline_before, base.capture_invariants(seed))
        report['baseline_application'] = base.application_checks(observer, seed, command, 'baseline')
        report['baseline_root_execution'] = base.root_execution(observer, seed)
        base.await_predecessor_checks(Path('/var/lib/fluxo/fluxo.db'), report)
        base.require(protected_state().get('integrity') is None,
                     'Baseline full installation must leave genuine absent proof for enrollment')
        report['receipt_absent_before_enrollment'] = True
        before = base.capture_invariants(seed)
        # Reset the independent byte/inode/mtime oracle after the intentional
        # baseline repair, before any preservation assessment runs.
        payload = payload_snapshot()
        report['payload_before'] = payload
        report['payload_oracle_reset_after_baseline'] = True
        raw = command('ordinary-node-enrollment-update', [*prefix, *flags], 600, measure_installer=True)
        reused_output(raw, enrolled=True)
        report['receipt_after_enrollment'] = receipt()
        base.require(selection(protected_state()) == selected and payload_snapshot() == payload,
                     'Enrollment changed Node selection or payload bytes/metadata')
        report['enrollment_native'] = base.verify_installed(image, env)
        report['enrollment_preservation'] = base.compare_invariants(before, base.capture_invariants(seed))
        report['versions_after_enrollment'] = node_versions(observer, manifest)
        base.require(report['versions_after_enrollment'] == report['versions_before'], 'Enrollment changed versions')
        report['enrollment_application'] = base.application_checks(observer, seed, command, 'enrollment')
        report['enrollment_root_execution'] = base.root_execution(observer, seed)
        base.await_predecessor_checks(Path('/var/lib/fluxo/fluxo.db'), report)
        before = base.capture_invariants(seed)

        # Keep the normal network namespace and AF_UNIX observability required
        # by the existing DynamicUser/lifecycle guards. Deny IP socket creation
        # only in this verification child and its descendants, using the kernel.
        offline = r'''import ctypes, errno, os, socket, sys
class Comparison(ctypes.Structure):
    _fields_ = [("arg", ctypes.c_uint), ("op", ctypes.c_uint),
                ("a", ctypes.c_uint64), ("b", ctypes.c_uint64)]
lib = ctypes.CDLL("libseccomp.so.2", use_errno=True)
lib.seccomp_init.argtypes = [ctypes.c_uint32]
lib.seccomp_init.restype = ctypes.c_void_p
lib.seccomp_syscall_resolve_name.argtypes = [ctypes.c_char_p]
lib.seccomp_syscall_resolve_name.restype = ctypes.c_int
lib.seccomp_rule_add_array.argtypes = [ctypes.c_void_p, ctypes.c_uint32, ctypes.c_int,
                                      ctypes.c_uint, ctypes.POINTER(Comparison)]
lib.seccomp_load.argtypes = [ctypes.c_void_p]
lib.seccomp_release.argtypes = [ctypes.c_void_p]
context = lib.seccomp_init(0x7fff0000)
assert context
number = lib.seccomp_syscall_resolve_name(b"socket")
assert number >= 0
for family in (socket.AF_INET, socket.AF_INET6):
    comparison = Comparison(0, 4, family, 0)
    assert lib.seccomp_rule_add_array(context, 0x00050000 | errno.EPERM, number,
                                      1, ctypes.byref(comparison)) == 0
assert lib.seccomp_load(context) == 0
lib.seccomp_release(context)
for family in (socket.AF_INET, socket.AF_INET6):
    try:
        socket.socket(family)
    except OSError as error:
        assert error.errno == errno.EPERM
    else:
        raise AssertionError("IP socket denial was not installed")
socket.socket(socket.AF_UNIX).close()
print("verified-IP-sockets-denied-unix-observability-retained", flush=True)
os.execv(sys.argv[1], sys.argv[1:])
'''
        raw = command('proven-node-preserve-offline', ['python3', '-c', offline,
                      '/usr/local/bin/fluxo', 'node-toolchain', 'install', '--preserve'], 180)
        reused_output(raw)
        base.require('verified-IP-sockets-denied-unix-observability-retained' in raw,
                     'Offline socket filter did not run')
        base.require(receipt() == report['receipt_after_enrollment'] and payload_snapshot() == payload,
                     'Local proof verification changed receipt or payload')
        report['proven_repeat_network_free'] = True
        report['offline_preservation'] = base.compare_invariants(before, base.capture_invariants(seed))
        raw = command('ordinary-node-warm-update', [*prefix, *flags], 600,
                      measure_installer=True, observe_dashboard=True)
        reused_output(raw)
        report['warm_native'] = base.verify_installed(image, env)
        report['warm_preservation'] = base.compare_invariants(before, base.capture_invariants(seed))
        report['warm_application'] = base.application_checks(observer, seed, command, 'warm')
        report['warm_root_execution'] = base.root_execution(observer, seed)
        report['versions_after_warm'] = node_versions(observer, manifest)
        report['receipt_after_warm'] = receipt()
        report['payload_after'] = payload_snapshot()
        base.require(report['versions_after_warm'] == report['versions_before']
                     and report['receipt_after_warm'] == report['receipt_after_enrollment']
                     and selection(protected_state()) == selected and report['payload_after'] == payload,
                     'Warm update changed versions, receipt, selection or Node payload')
        report['paired_measurements'] = {
            'before': 'pre-optimization-node-baseline', 'after': 'ordinary-node-warm-update',
            'total_seconds': {name: report['steps'][name]['duration_seconds'] for name in
                ('pre-optimization-node-baseline', 'ordinary-node-warm-update')},
            'phase_timings': 'steps.<command>.installer_timing',
            'dashboard_unavailability': 'dashboard_observations.<command>',
            'same_binary_pins_and_flags': True, 'performance_gate': False,
            'caveats': ['Single sequential pair on one disposable native hosted runner.',
                        'Both follow explicit fresh Node setup; final sample also follows authenticated enrollment and offline reuse.',
                        'Caches and host/network load may differ; no old public release or universal speedup claim.']}
        # One bounded repair regression after the measurements: a missing alias
        # must be refused without repair, then repaired only by the existing
        # explicitly chosen command. No receipt/journal/authority is edited.
        base.await_predecessor_checks(Path('/var/lib/fluxo/fluxo.db'), report)
        alias = Path('/usr/local/bin/pnpx')
        original_alias = private / 'missing-link-test-pnpx.original'
        original_target = os.readlink(alias)
        prior_receipt = receipt()
        prior_data = base.capture_invariants(seed)
        alias.rename(original_alias)
        try:
            raw = command('missing-link-preservation-refusal',
                ['/usr/local/bin/fluxo', 'node-toolchain', 'install', '--preserve'],
                180, expected_exit=1)
            base.require('pnpx' in raw and not os.path.lexists(alias)
                         and receipt() == prior_receipt
                         and not any(marker in raw for marker in
                             ('Installing Corepack', 'Preparing pnpm', 'Preparing Yarn', 'Authenticating')),
                         'Preservation failed to leave the missing-link defect for explicit repair')
            report['missing_link_refused_without_repair'] = True
            command('explicit-missing-link-repair',
                    ['/usr/local/bin/fluxo', 'node-toolchain', 'install'], 600)
            base.require(os.readlink(alias) == original_target
                         and protected_state().get('integrity') is None
                         and node_versions(observer, manifest) == report['versions_before']
                         and selection(protected_state()) == selected,
                         'Explicit repair did not restore the alias and preserve runtime selection')
            report['explicit_repair_preservation'] = base.compare_invariants(
                prior_data, base.capture_invariants(seed))
            raw = command('post-repair-authenticated-assessment',
                ['/usr/local/bin/fluxo', 'node-toolchain', 'install', '--preserve'], 600)
            reused_output(raw, enrolled=True)
            report['receipt_after_explicit_repair'] = receipt()
            report['explicit_repair_verified'] = True
        finally:
            if not os.path.lexists(alias) and os.path.lexists(original_alias):
                original_alias.rename(alias)
        report['passed'] = True
    except Exception as error:
        signal.setitimer(signal.ITIMER_REAL, 0)
        report['error_type'] = type(error).__name__
        report['error'] = base.sanitized_diagnostic(str(error))
        report['error_frames'] = [{'file': Path(frame.filename).name, 'function': frame.name, 'line': frame.lineno}
                                  for frame in traceback.extract_tb(error.__traceback__)]
        for name, argv in (
            ('failure-task-profiles', ['/usr/local/bin/fluxo', 'system-task-diagnostics']),
            ('failure-panel-journal', ['journalctl', '-u', 'fluxo.service', '-n', '100', '--no-pager'])):
            try:
                result = base.run(argv, 45, env)
                (args.output / (name + '-sanitized.log')).write_text(
                    base.sanitized_diagnostic(result['stdout'] + '\n' + result['stderr']) + '\n')
            except Exception:
                pass
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, previous_alarm)
        for cleanup in (fixture.cleanup, lambda: restore_runtime_fixture(private, report)):
            try:
                cleanup()
            except Exception as error:
                report['passed'] = False
                report.setdefault('cleanup_errors', []).append(base.sanitized_diagnostic(str(error)))
        report['duration_seconds'] = round(time.monotonic() - began, 3)
        (args.output / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
        print(json.dumps({'passed': report['passed'], 'stage': report.get('stage'),
                          'error_type': report.get('error_type')}), flush=True)
    return 0 if report['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
