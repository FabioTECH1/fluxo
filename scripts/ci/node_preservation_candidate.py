#!/usr/bin/env python3
"""One disposable native ARM64 Ubuntu 24.04 Node preservation case.

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
import shutil
import signal
import stat
import subprocess
import tempfile
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


def hosted_profile():
    arch, release = base.hosted_profile()
    base.require(os.environ.get('FLUXO_NODE_PRESERVATION_CANDIDATE') == '1'
                 and (arch, release) == ('arm64', '24.04'),
                 'Explicit native ARM64 Ubuntu 24.04 Node fixture opt-in required')
    base.require(all(re.fullmatch(r'[A-Za-z0-9_.-]{1,100}', os.environ.get(key, ''))
                     for key in ('ImageOS', 'ImageVersion')), 'Exact hosted runner image metadata required')
    return arch, release


def harness_identity():
    changed = subprocess.check_output(['git', '-c', 'safe.directory=' + os.getcwd(),
        'status', '--porcelain', '--untracked-files=all', '--', *HARNESS_PATHS], text=True)
    base.require(not changed, 'Node harness or workflow differs from the recorded source')
    return {path: base.digest(path) for path in HARNESS_PATHS}


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

    def command(name, argv, timeout=120, cwd=None):
        report['stage'] = name
        base.require(time.monotonic() < deadline, 'Node acceptance time budget exhausted')
        print(json.dumps({'stage': name, 'status': 'started'}), flush=True)
        log = private / (name + '.log')
        start = time.monotonic()
        with log.open('x') as stream:
            log.chmod(0o600)
            process = subprocess.Popen(argv, env=env, cwd=cwd, stdin=subprocess.DEVNULL,
                stdout=stream, stderr=subprocess.STDOUT, start_new_session=True)
            try:
                code = process.wait(timeout=min(timeout, max(1, deadline - start)))
            except subprocess.TimeoutExpired:
                # Include foreground installer/Node children, not just bash.
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
                code = 124
        report['steps'][name] = {'exit_code': code, 'private_log_sha256': base.digest(log),
                                 'duration_seconds': round(time.monotonic() - start, 3)}
        raw = log.read_text(errors='replace')
        (args.output / (name + '-sanitized.log')).write_text(base.sanitized_diagnostic(raw) + '\n')
        base.require(code == 0, 'Command failed: ' + name)
        return raw

    fixture = base.HostedFixture(private, report, env, command, arch, release, 'cron')
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
        env['FLUXO_LOCAL_BINARY_SHA256'] = image
        fixture.prepare()
        optional_runtime_fixture(private, report)
        # setup-node lives outside the stock PATH. Archive any additional
        # root-owned /usr/local JS shims using the existing fixture convention;
        # never remove a distro package or silently accept an external runtime.
        for name in ('node', 'npm', 'npx'):
            path = Path('/usr/local/bin') / name
            if os.path.lexists(path):
                info = path.lstat()
                base.require(info.st_uid == 0 and (stat.S_ISLNK(info.st_mode) or
                             (stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and not info.st_mode & 0o022)),
                             'Unsafe hosted Node command shim')
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
        report['receipt_absent_before_enrollment'] = True
        report['versions_before'] = node_versions(observer, manifest)
        selected = selection(protected_state())
        payload = payload_snapshot()
        report['payload_before'] = payload
        report['node_setup_preservation'] = base.compare_invariants(before, base.capture_invariants(seed))
        flags = ['--node' if flag == '--no-node' else flag for flag in base.FLAGS]
        raw = command('ordinary-node-enrollment-update', [*prefix, *flags], 600)
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

        # Only this verification child gets an empty network namespace; panel,
        # cron, systemd and installer authority retain their original context.
        offline = ('import os,socket,sys; '
                   'assert socket.if_nameindex() == [(1,"lo")]; '
                   'assert not open("/proc/net/route").read().splitlines()[1:]; '
                   'print("verified-empty-network-namespace",flush=True); '
                   'os.execv(sys.argv[1],sys.argv[1:])')
        raw = command('proven-node-preserve-offline', ['unshare', '--net', '--', 'python3', '-c', offline,
                      '/usr/local/bin/fluxo', 'node-toolchain', 'install', '--preserve'], 180)
        reused_output(raw)
        base.require('verified-empty-network-namespace' in raw, 'Offline isolation did not run')
        base.require(receipt() == report['receipt_after_enrollment'] and payload_snapshot() == payload,
                     'Local proof verification changed receipt or payload')
        report['proven_repeat_network_free'] = True
        report['offline_preservation'] = base.compare_invariants(before, base.capture_invariants(seed))
        raw = command('ordinary-node-warm-update', [*prefix, *flags], 600)
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
        report['passed'] = True
    except Exception as error:
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
