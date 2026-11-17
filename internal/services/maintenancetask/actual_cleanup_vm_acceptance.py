#!/usr/bin/python3
"""Actual installed cleanup caller, only on the named disposable acceptance VMs.

Preserves cache fixtures, package state, APT configuration and timer state. Uses
the production router, retained image, genuine receipts and default adapters.
No journal edits, receipt fabrication, selector override or package installation.
"""
import argparse
import contextlib
import fcntl
import hashlib
import importlib.util
import json
import os
import pathlib
import shutil
import socket
import stat
import subprocess
import time
import uuid

HOSTS = {'fluxo-bridge-jammy-20261101': ('systemd-persistent-v1', 'internal'),
         'fluxo-bridge-noble-final-20261102': ('systemd-cgroup-exit-v1', 'internal'),
         'fluxo-bridge-noble-delivery-20261117': ('systemd-cgroup-exit-v1', 'internal'),
         'fluxo-bridge-resolute-20261101': ('systemd-cgroup-exit-v1', '3.0'),
         'fluxo-bridge-jammy-final-20261117': ('systemd-persistent-v1', 'internal'),
         'fluxo-bridge-resolute-final-20261117': ('systemd-cgroup-exit-v1', '3.0'),
         'fluxo-bridge-jammy-fresh-eb4-20261117': ('systemd-persistent-v1', 'internal'),
         'fluxo-bridge-resolute-fresh-eb4-20261117': ('systemd-cgroup-exit-v1', '3.0')}
ap = argparse.ArgumentParser()
ap.add_argument('--expected-sha256', required=True)
args = ap.parse_args()
assert os.geteuid() == 0 and os.environ.get('FLUXO_PHP_UPDATE_VM_TEST') == '1'
assert socket.gethostname() in HOSTS
adapter, solver = HOSTS[socket.gethostname()]
candidate = pathlib.Path('/usr/local/bin/fluxo')
assert hashlib.sha256(candidate.read_bytes()).hexdigest() == args.expected_sha256
observer_path = pathlib.Path('/home/ubuntu/actual_candidate_vm_acceptance.py')
assert observer_path.is_file() and not observer_path.is_symlink()
spec = importlib.util.spec_from_file_location('observer', observer_path)
observer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(observer)
run_id = str(uuid.uuid4())
root = pathlib.Path('/root/fluxo-cleanup-caller-'+run_id)
root.mkdir(mode=0o700)
env = observer.CONTROL_ENV
core_path = pathlib.Path('/var/lib/fluxo-system-tasks/state.json')
cache = pathlib.Path('/var/cache/apt')
backup = root/'cache'
timers = []
created = None
removed_snippet = False
cache_restored = False
original_cache = None
report = None
active_units = []
snippet = pathlib.Path('/etc/apt/apt.conf.d/20archive')


def retain(name, raw):
    fd = os.open(root/name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as out:
        out.write(raw); out.flush(); os.fsync(out.fileno())


def command(name, argv, timeout=90):
    p = subprocess.run(argv, env=env, capture_output=True, timeout=timeout)
    retain(name+'.stdout', p.stdout); retain(name+'.stderr', p.stderr)
    return p


def core():
    return json.loads(observer.protected_read(core_path))


def wait(test, message, seconds=150):
    return observer.wait_for(test, seconds, message)


@contextlib.contextmanager
def locks():
    # Same flock order and native POSIX record locks as packages.NativeDPKG.
    with contextlib.ExitStack() as stack:
        for path in ['/run/lock/fluxo-php-lifecycle.lock', '/run/lock/fluxo-packages.lock',
                     '/var/lib/dpkg/lock-frontend', '/var/lib/dpkg/lock']:
            fd = os.open(path, os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC)
            stack.callback(os.close, fd)
            s = os.fstat(fd)
            assert stat.S_ISREG(s.st_mode) and s.st_uid == 0 and s.st_nlink == 1 and not s.st_mode & 0o022
            fn = fcntl.flock if path.startswith('/run/') else fcntl.lockf
            until = time.monotonic()+60
            while True:
                try:
                    fn(fd, fcntl.LOCK_EX | fcntl.LOCK_NB); break
                except BlockingIOError:
                    assert time.monotonic() < until, 'package admission did not become idle'
                    time.sleep(0.1)
        yield


def tree(base):
    result = {}
    for p in [base, *sorted(base.rglob('*'))]:
        s = p.lstat()
        assert stat.S_ISREG(s.st_mode) or stat.S_ISDIR(s.st_mode), 'unexpected cache/config file type'
        assert not stat.S_ISREG(s.st_mode) or s.st_nlink == 1
        result[str(p.relative_to(base))] = {'mode': s.st_mode, 'uid': s.st_uid,
            'gid': s.st_gid, 'mtime': s.st_mtime_ns, 'atime': s.st_atime_ns,
            'inode': s.st_ino, 'device': s.st_dev,
            'sha256': hashlib.sha256(p.read_bytes()).hexdigest() if p.is_file() else None}
    return result


def protected_inputs():
    return {p: hashlib.sha256(observer.protected_read(p, 128*1024*1024)).hexdigest()
            for p in ['/var/lib/dpkg/status', '/var/lib/apt/extended_states']}


def same_content(before, after):
    return set(before) == set(after) and all(
        all(before[k][field] == after[k][field] for field in ('mode', 'uid', 'gid', 'sha256'))
        for k in before)


def invocation(unit):
    live = core()['invocations'].get(unit)
    if live is not None:
        return live
    archive = core_path.parent/'history'/(unit+'.json')
    return json.loads(observer.protected_read(archive)) if archive.exists() else None


def drained(unit):
    p = pathlib.Path('/sys/fs/cgroup/system.slice')/unit/'cgroup.events'
    return not p.exists() or 'populated 0' in p.read_text()


def submit(api):
    before = set(core()['invocations'])
    api.request('POST', f'/crons/{created}/run')
    def submitted():
        new = set(core()['invocations'])-before
        assert len(new) <= 1
        return next(iter(new)) if new else None
    return wait(submitted, 'durable production submission')


def restore_cache():
    global cache_restored
    with locks():
        assert protected_inputs() == original_inputs
        assert same_content(original_config, tree(pathlib.Path('/etc/apt')))
        current_cache = tree(cache)
        assert not set(current_cache)-set(original_cache), 'unexpected cache additions'
        deleted = []
        for name, original in original_cache.items():
            target = cache/name
            if name not in current_cache:
                assert stat.S_ISREG(original['mode']), 'cache directory removed'
                assert name.startswith('archives/') and name.endswith('.deb'), 'unexpected cache deletion'
                shutil.copy2(backup/name, target)
                os.chown(target, original['uid'], original['gid'])
                deleted.append(name)
            else:
                assert all(current_cache[name][field] == original[field] for field in ('mode', 'uid', 'gid', 'sha256', 'inode', 'device')), 'cache entry replaced or modified'
        for name, original in original_cache.items():
            os.utime(cache/name, ns=(original['atime'], original['mtime']), follow_symlinks=False)
        assert same_content(original_cache, tree(cache))
        cache_restored = True
        return deleted


try:
    # Timer changes are runtime-only; preserve their original activation state.
    for unit in ['apt-daily.timer', 'apt-daily-upgrade.timer']:
        active = subprocess.run(['systemctl', 'is-active', '--quiet', unit]).returncode == 0
        timers.append((unit, active))
        subprocess.run(['systemctl', 'stop', unit], check=True, timeout=30)
    for unit in ['apt-daily.service', 'apt-daily-upgrade.service']:
        wait(lambda: subprocess.run(['systemctl', 'is-active', '--quiet', unit]).returncode != 0,
             'natural APT service completion', 120)
    panel = observer.show('fluxo.service')
    assert int(panel['MainPID']) > 1 and observer.hash_file(f"/proc/{panel['MainPID']}/exe") == args.expected_sha256
    with locks():
        original_cache, original_config, original_inputs = tree(cache), tree(pathlib.Path('/etc/apt')), protected_inputs()
        shutil.copytree(cache, backup, copy_function=shutil.copy2)
        retain('cache-manifest.json', json.dumps(original_cache, indent=2).encode())
    # Preflight the actual empty plan without changing package state.
    fixed = ['-o', 'Debug::pkgAcqArchive::NoQueue=true', '--no-download', '-o', 'APT::Solver='+solver, '-o', 'APT::Planner=internal']
    p = command('empty-plan', ['/usr/bin/apt-get', *fixed, '--simulate', 'autoremove', '-y'])
    assert p.returncode == 0 and b'0 upgraded, 0 newly installed, 0 to remove' in p.stdout
    assert not any(line.startswith((b'Remv ', b'Purg ', b'Inst ', b'Conf ')) for line in p.stdout.splitlines())
    api = observer.API('https://127.0.0.1:9595')
    api.login(False, run_id)
    contract = '/usr/bin/flock -w 120 /run/lock/fluxo-packages.lock /usr/bin/apt-get autoremove -y && /usr/bin/flock -w 120 /run/lock/fluxo-packages.lock /usr/bin/apt-get autoclean'
    created = api.request('POST', '/crons', {'name': 'cleanup-acceptance-'+run_id,
        'expression': '0 0 1 1 *', 'command': contract, 'user': 'root'})['id']
    entry = core()['entries'][str(created)]
    assert entry['helper'].endswith(args.expected_sha256)
    if not entry['received']:
        previous = set(core()['invocations'])
        try:
            api.request('POST', f'/crons/{created}/run')
            raise AssertionError('unacknowledged registration admitted')
        except RuntimeError as e:
            assert 'status 409' in str(e)
        assert set(core()['invocations']) == previous
    wait(lambda: core()['entries'][str(created)]['received'], 'genuine cron receipt')
    # Hold only package admission: dispatcher can submit, helper cannot execute.
    fd = os.open('/run/lock/fluxo-packages.lock', os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        refused_unit = submit(api)
        active_units.append(refused_unit)
        wait(lambda: observer.show(refused_unit).get('MainPID', '0') != '0', 'queued production helper')
        assert invocation(refused_unit)['status'] == 'submitted'
        # Optional exact approved input: absence remains qualified but differs.
        assert snippet.is_file() and not snippet.is_symlink()
        os.rename(snippet, root/'20archive.original'); removed_snippet = True
    finally:
        os.close(fd)
    wait(lambda: drained(refused_unit), 'profile-drift helper exit')
    refused = invocation(refused_unit)
    assert refused['status'] == 'blocked' and not refused.get('result_profile'), 'drift consumed execution authority'
    log = pathlib.Path(f'/var/log/fluxo/cron-{created}.log').read_bytes()
    assert b'System-task execution profile changed after submission' in log
    os.rename(root/'20archive.original', snippet); removed_snippet = False
    p = command('reconcile-drift', [str(candidate), 'system-task-reconcile'])
    assert p.returncode == 0 and invocation(refused_unit)['status'] in ('blocked', 'interrupted')
    unit = submit(api)
    active_units.append(unit)
    wait(lambda: invocation(unit)['status'] in ('completed', 'failed', 'blocked'), 'actual cleanup completion')
    completed = invocation(unit)
    retain('completed-invocation.json', json.dumps(completed, indent=2).encode())
    assert completed['status'] == 'completed' and not completed.get('error')
    assert completed['adapter'] == adapter and completed.get('result_profile')
    assert completed['profile']['external']['apt-execution-profile'] == 'apt-a0-noqueue-v3'
    assert completed['profile']['external']['apt-fixed-solver'] == solver
    wait(lambda: drained(unit), 'cleanup domain drainage')
    replay = command('consumed-replay-refusal', [entry['helper'], 'system-task-execute', unit])
    assert replay.returncode != 0 and b'System-task launch authority is absent or already consumed' in replay.stderr
    assert invocation(unit) == completed
    api.request('DELETE', f'/crons/{created}')
    created = None
    deleted = restore_cache()
    report = {'passed': True, 'host': socket.gethostname(), 'candidate_sha256': args.expected_sha256,
              'adapter': adapter, 'solver': solver, 'unit': unit, 'profile_drift_refused_before_consumption': True,
              'consumed_replay_refused': True, 'genuine_cron_receipt': True, 'package_and_config_unchanged': True,
              'cache_deletions_restored': deleted, 'evidence': str(root)}
finally:
    try:
        if removed_snippet:
            assert not snippet.exists()
            os.rename(root/'20archive.original', snippet)
        if original_cache is not None and not cache_restored:
            if all(drained(unit) for unit in active_units):
                restore_cache()
            else:
                print(json.dumps({'passed': False, 'restoration_pending': True, 'evidence': str(root)}), flush=True)
    finally:
        for unit, active in timers:
            if active:
                subprocess.run(['systemctl', 'start', unit], check=True, timeout=30)
        # On failure retain the exact registration, journal, backups and state.
        if created is not None:
            print(json.dumps({'passed': False, 'cron_id': created, 'evidence': str(root)}), flush=True)

if report is not None:
    assert cache_restored
    report['timers_restored'] = True
    retain('result.json', json.dumps(report, indent=2).encode())
    print(json.dumps(report), flush=True)
