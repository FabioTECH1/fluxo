#!/usr/bin/python3
"""Exact installed-image helper-refresh fault acceptance on the private Noble VM.

Only immutable flags on one already guarded fixture trigger and the reader
floor are changed for deterministic write failures; both flags are restored.
Every original byte, helper and activation archive is retained. Never run on a
customer server. Failed prepared operations remain stopped for exact recovery.
"""
import argparse
import hashlib
import json
import os
import pathlib
import socket
import stat
import subprocess
import time
import uuid

ap = argparse.ArgumentParser()
ap.add_argument('--expected-sha256', required=True)
args = ap.parse_args()
assert os.geteuid() == 0 and socket.gethostname() == 'fluxo-bridge-noble-final-20261102'
assert os.environ.get('FLUXO_PHP_UPDATE_VM_TEST') == '1'
binary = pathlib.Path('/usr/local/bin/fluxo')
assert hashlib.sha256(binary.read_bytes()).hexdigest() == args.expected_sha256
assert pathlib.Path('/usr/bin/chattr').is_file()
env = {'PATH': '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin',
       'HOME': '/root', 'LANG': 'C.UTF-8', 'FLUXO_ENV': 'prod', 'FLUXO_DATA_DIR': '/var/lib/fluxo'}
root = pathlib.Path('/root/fluxo-task-helper-refresh-'+str(uuid.uuid4()))
root.mkdir(mode=0o700)


def retain(name, raw):
    fd = os.open(root/name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as out:
        out.write(raw); out.flush(); os.fsync(out.fileno())


def run(name, command):
    result = subprocess.run(command, env=env, capture_output=True, timeout=330)
    retain(name+'.stdout', result.stdout); retain(name+'.stderr', result.stderr)
    return result


def protected(path, limit=128*1024*1024):
    for parent in path.parents:
        s = parent.lstat()
        assert stat.S_ISDIR(s.st_mode) and s.st_uid == 0 and s.st_mode & 0o022 == 0
    before = path.lstat()
    assert stat.S_ISREG(before.st_mode) and before.st_uid == 0 and before.st_nlink == 1 and before.st_mode & 0o022 == 0 and before.st_size <= limit
    raw = path.read_bytes(); after = path.lstat()
    assert (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) == (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns)
    return raw


floor = pathlib.Path('/var/lib/fluxo/execution-readers.json')
core = pathlib.Path('/var/lib/fluxo-system-tasks/state.json')
scheduler = pathlib.Path('/var/lib/fluxo-scheduler/state.json')
ledger = pathlib.Path('/var/lib/fluxo-scheduler/loader-epochs.json')
subprocess.run(['systemctl', 'stop', 'fluxo.service'], check=True, timeout=60)
original_floor, original_core = protected(floor), protected(core)
source_floor, source = json.loads(original_floor), json.loads(original_core)
assert source_floor['phase'] == 'committed' and source_floor['scheduler_schema'] == 5 and source['schema'] == 3
entries = [r for r in source['entries'].values() if r['enabled']]
assert entries and all(r['published'] and r['received'] for r in entries)
entry = next(r for r in entries if r['contract']['site_id'] == 0 and r['contract']['kind'] in ('composer-update', 'certificate-renewal'))
trigger = pathlib.Path('/etc/cron.d/fluxo-cron-'+str(entry['contract']['id']))
trigger_stat, original_trigger = trigger.lstat(), protected(trigger)
assert entry['helper'].encode() in original_trigger and entry['nonce'].encode() in original_trigger
assert 'i' not in subprocess.check_output(['lsattr', '-d', str(trigger)]).decode().split()[0]
assert 'i' not in subprocess.check_output(['lsattr', '-d', str(floor)]).decode().split()[0]
old_helper = pathlib.Path(entry['helper'])
old_image = protected(old_helper)
assert hashlib.sha256(old_image).hexdigest() == old_helper.name
original_scheduler, original_ledger = protected(scheduler), protected(ledger)
original_database = {str(p): hashlib.sha256(protected(p)).hexdigest()
                     for p in [pathlib.Path('/var/lib/fluxo/fluxo.db'), pathlib.Path('/var/lib/fluxo/fluxo.db-wal')] if p.exists()}
retain('source-floor.json', original_floor); retain('source-task-core.json', original_core)
retain('source-trigger', original_trigger)
before_operations = sorted(p.name for p in pathlib.Path('/var/lib/fluxo/execution-activations').iterdir())
wrong = run('wrong-core-refusal', [str(binary), 'execution-compatibility', 'refresh-task-helpers', '0'*64])
assert wrong.returncode != 0 and protected(floor) == original_floor and protected(core) == original_core
assert sorted(p.name for p in pathlib.Path('/var/lib/fluxo/execution-activations').iterdir()) == before_operations
expected_core = hashlib.sha256(original_core).hexdigest()
floor_immutable, trigger_immutable, completed = False, False, False
operation = None
try:
    subprocess.run(['chattr', '+i', str(floor)], check=True); floor_immutable = True
    subprocess.run(['chattr', '+i', str(trigger)], check=True); trigger_immutable = True
    interrupted = run('before-prepared-floor-fault', [str(binary), 'execution-compatibility', 'refresh-task-helpers', expected_core])
    assert interrupted.returncode != 0
    first = json.loads(interrupted.stdout); operation = first['operation_id']
    assert operation and protected(floor) == original_floor and protected(core) == original_core
    directory = pathlib.Path('/var/lib/fluxo/execution-activations')/operation
    plan_bytes = protected(directory/'plan.json')
    plan = json.loads(plan_bytes)
    assert plan['kind'] == 'task-helper-refresh' and hashlib.sha256(plan_bytes).hexdigest()
    subprocess.run(['chattr', '-i', str(floor)], check=True); floor_immutable = False
    interrupted = run('after-prepared-floor-fault', [str(binary), 'execution-compatibility', 'recover', operation])
    assert interrupted.returncode != 0
    prepared = json.loads(protected(floor))
    assert prepared['phase'] == 'prepared' and prepared['activation_id'] == operation and prepared['binary_sha256'] == args.expected_sha256 and prepared['plan_sha256'] == hashlib.sha256(plan_bytes).hexdigest()
    assert protected(trigger) == original_trigger
    pending = protected(core)
    pending_core = json.loads(pending)
    for key, previous in source['entries'].items():
        if previous['enabled']:
            intended = pending_core['entries'][key]
            assert not intended['received'] and intended['helper'].endswith(args.expected_sha256) and intended['nonce'] != previous['nonce']
    for label, executable in [('retained-helper', str(old_helper)), ('current-image', str(binary))]:
        refused = run(label+'-ordinary-refusal', [executable, 'system-task-receipt', str(entry['contract']['id']), entry['nonce']])
        assert refused.returncode != 0 and b'execution compatibility refused before initialization' in refused.stderr and b'activation is incomplete' in refused.stderr
        assert protected(core) == pending
    wrong = run('wrong-recovery-refusal', [str(binary), 'execution-compatibility', 'recover', str(uuid.uuid4())])
    assert wrong.returncode != 0 and protected(core) == pending and json.loads(protected(floor)) == prepared
    assert (trigger.lstat().st_dev, trigger.lstat().st_ino) == (trigger_stat.st_dev, trigger_stat.st_ino)
    subprocess.run(['chattr', '-i', str(trigger)], check=True); trigger_immutable = False
    recovered = run('exact-recovery', [str(binary), 'execution-compatibility', 'recover', operation])
    assert recovered.returncode == 0 and json.loads(recovered.stdout)['format_active']
    committed = json.loads(protected(floor)); assert committed['phase'] == 'committed' and committed['activation_id'] == operation
    next_core = json.loads(protected(core))
    for key in source:
        if key != 'entries': assert next_core[key] == source[key], key
    for key, previous in source['entries'].items():
        current = next_core['entries'][key]
        if not previous['enabled']:
            assert current == previous
        else:
            assert current['identity'] == previous['identity'] and current['contract'] == previous['contract'] and current['database'] == previous['database']
            assert current['helper'] == '/var/lib/fluxo-system-tasks/helpers/'+args.expected_sha256 and current['nonce'] != previous['nonce']
            # A genuine receipt may arrive immediately after commit/release.
            stable_current, stable_intended = dict(current), dict(pending_core['entries'][key])
            stable_current.pop('received'); stable_intended.pop('received')
            assert stable_current == stable_intended
    assert protected(old_helper) == old_image and protected(directory/'plan.json') == plan_bytes
    assert protected(scheduler) == original_scheduler and protected(ledger) == original_ledger
    assert {p: hashlib.sha256(protected(pathlib.Path(p))).hexdigest() for p in original_database} == original_database
    source_operation = pathlib.Path('/var/lib/fluxo/execution-activations')/source_floor['activation_id']
    assert hashlib.sha256(protected(source_operation/'plan.json')).hexdigest() == source_floor['plan_sha256']
    old_receipt = run('old-nonce-refusal', [str(old_helper), 'system-task-receipt', str(entry['contract']['id']), entry['nonce']])
    assert old_receipt.returncode != 0
    again = run('completed-recovery', [str(binary), 'execution-compatibility', 'recover', operation])
    assert again.returncode == 0 and protected(directory/'plan.json') == plan_bytes
    subprocess.run(['systemctl', 'start', 'fluxo.service'], check=True, timeout=60)
    until = time.monotonic()+190
    while time.monotonic() < until:
        latest = json.loads(protected(core))
        if all(r['received'] for r in latest['entries'].values() if r['enabled']):
            break
        time.sleep(0.3)
    else:
        raise RuntimeError('fresh real cron acknowledgements did not arrive')
    assert all(r['helper'].endswith(args.expected_sha256) for r in latest['entries'].values() if r['enabled'])
    completed = True
    report = {'passed': True, 'candidate_sha256': args.expected_sha256, 'operation_id': operation,
              'before_and_after_prepared_floor_faults_recovered': True,
              'retained_helper_preflight_refused': True, 'wrong_core_and_recovery_refused': True,
              'fresh_real_cron_acknowledgements': True, 'original_archives_helpers_history_preserved': True,
              'database_bytes_and_scheduler_state_unchanged': True, 'evidence': str(root)}
    retain('result.json', json.dumps(report, indent=2).encode()); print(json.dumps(report), flush=True)
finally:
    if floor_immutable:
        subprocess.run(['chattr', '-i', str(floor)], check=True)
    if trigger_immutable:
        assert (trigger.lstat().st_dev, trigger.lstat().st_ino) == (trigger_stat.st_dev, trigger_stat.st_ino)
        subprocess.run(['chattr', '-i', str(trigger)], check=True)
    if not completed:
        print(json.dumps({'passed': False, 'operation_id': operation, 'evidence': str(root),
                          'preserve_exact_recovery': True}), flush=True)
