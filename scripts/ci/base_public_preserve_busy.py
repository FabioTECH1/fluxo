#!/usr/bin/env python3
"""One real calendar child and exact-public BASE leased --preserve check.

Only called by the guarded AMD64 public acceptance case. Adapted from the
source/native-qualified installer_options_busy fixture: no journal/ACK edits,
manual process pretending to be cron, forced stop, or application runtime apply.
All scripts, raw output and process observations remain root-private.
"""
import argparse
import datetime
import json
import os
from pathlib import Path
import re
import select
import stat
import subprocess
import time

import base_release_acceptance as acceptance
import node_preservation_candidate as node


def verify_lease_state(state, lease, pid):
    acceptance.require(state['upgrade']['id'] == lease and state['upgrade']['pid'] == pid
        and state['holds'] and all(value == lease for value in state['holds'].values()),
        'Real lease ownership/holds differ')


def verify_natural_preserve_completion(raw, begin_ns, completed_ns, now_ns):
    node.reused_output(raw)
    acceptance.require('Waiting for earlier cron workers' in raw
        and raw.index('Waiting for earlier cron workers') < raw.index(node.REUSED)
        and 'Earlier cron workers finished; continuing the upgrade.' in raw,
        'Actual public preserve did not wait before reusing the toolchain')
    acceptance.require(begin_ns < completed_ns <= now_ns,
        'Calendar work did not finish naturally during preserve')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--private', required=True, type=Path)
    parser.add_argument('--image', required=True)
    args = parser.parse_args()
    acceptance.hosted_profile('amd64-bridge')
    acceptance.require_named_fixture('amd64')
    private = args.private
    acceptance.require(private.parent == Path('/root') and re.fullmatch(r'fluxo-public-base-[a-z0-9_]{8}', private.name),
                       'Exact private public-base fixture required')
    info = private.lstat()
    acceptance.require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and stat.S_IMODE(info.st_mode) == 0o700,
                       'Unsafe private public-base fixture')
    binary = '/usr/local/bin/fluxo'
    acceptance.require(re.fullmatch('[0-9a-f]{64}', args.image)
                       and acceptance.base.digest(binary) == args.image, 'Installed exact public base required')
    os.umask(0o077)
    floor = acceptance.base.digest('/var/lib/fluxo/execution-readers.json')
    before = {'payload': node.payload_snapshot(), 'receipt': node.receipt(),
              'selection': node.selection(node.protected_state())}
    cron = Path('/etc/cron.d/fluxo-public-base-natural-probe')
    probe = private / 'natural-probe.sh'
    started, ended = private / 'cron-start.json', private / 'cron-end.json'
    acceptance.require(not any(os.path.lexists(path) for path in (cron, probe, started, ended)),
                       'Existing natural cron probe refused')
    probe.write_text('#!/bin/sh\nset -eu\n/usr/bin/printf \'{"pid":%s,"start_ns":%s}\\n\' '
        '"$$" "$(/usr/bin/date +%s%N)" > ' + str(started) + '\n/usr/bin/sleep 18\n'
        '/usr/bin/printf \'{"end_ns":%s}\\n\' "$(/usr/bin/date +%s%N)" > ' + str(ended) + '\n')
    probe.chmod(0o700)
    result = {'passed': False, 'actual_public_base_preserve_cli': True,
              'real_calendar_cron_child': True, 'manual_authority_or_ack_edits': False}
    child, lease = None, None
    def check(condition, message):
        acceptance.require(condition, message)
    try:
        with (private / 'busy-lease.log').open('xb') as log:
            child = subprocess.Popen([binary, 'scheduler-installer-lease', str(os.getpid())],
                stdout=subprocess.PIPE, stderr=log, text=True, start_new_session=True)
            check(select.select([child.stdout], [], [], 110)[0], 'Real public installer lease did not become ready')
            line = child.stdout.readline().strip()
            check(re.fullmatch(r'READY [0-9a-f-]{36}', line), 'Real public installer lease refused')
            lease = line.split()[1]
            state = json.loads(Path('/var/lib/fluxo-scheduler/state.json').read_text())
            verify_lease_state(state, lease, child.pid)
            when = datetime.datetime.now() + datetime.timedelta(minutes=1)
            with cron.open('x') as stream:
                stream.write(f'{when.minute} {when.hour} {when.day} {when.month} * root {probe} >/dev/null 2>&1\n')
            cron.chmod(0o644)
            deadline = time.monotonic() + 95
            while not started.exists():
                check(child.poll() is None and time.monotonic() < deadline, 'Real calendar cron launch missing')
                time.sleep(.1)
            cron.unlink()  # One scheduled probe only; its running child drains naturally.
            marker = json.loads(started.read_text())
            check(not ended.exists(), 'Calendar child finished before preserve admission')
            provider = {}
            for name in ('cron', 'cronie'):
                raw = subprocess.check_output(['systemctl', 'show', name + '.service', '-p', 'ActiveState',
                    '-p', 'MainPID', '-p', 'ControlGroup'], text=True, timeout=15)
                current = dict(line.split('=', 1) for line in raw.splitlines() if '=' in line)
                if current.get('ActiveState') == 'active':
                    check(not provider, 'Multiple active cron providers')
                    provider = dict(current, provider=name)
            check(provider and provider['ControlGroup'].startswith('/system.slice/'), 'Unknown real cron provider')
            group = provider['ControlGroup']
            check(any(line.endswith(':' + group) or (':' + group + '/') in line
                      for line in Path('/proc/%d/cgroup' % marker['pid']).read_text().splitlines()),
                  'Calendar probe is outside the actual provider cgroup')
            count = int(Path('/sys/fs/cgroup' + group + '/pids.current').read_text())
            check(count > 1, 'Real provider has no active calendar child')
            begin_ns, begin = time.time_ns(), time.monotonic()
            with (private / 'busy-preserve.log').open('xb') as log:
                run = subprocess.run([binary, 'node-toolchain', 'install', '--preserve', '--installer-lease', lease],
                    cwd='/', stdout=log, stderr=subprocess.STDOUT, timeout=180,
                    preexec_fn=lambda: os.umask(0o022))
            raw = (private / 'busy-preserve.log').read_text(errors='replace')
            check(run.returncode == 0, 'Actual public leased preserve failed')
            completion = json.loads(ended.read_text())
            verify_natural_preserve_completion(raw, begin_ns, completion['end_ns'], time.time_ns())
            check(child.poll() is None, 'Installer lease owner died during preserve')
            verify = subprocess.run([binary, 'scheduler-installer-verify-upgrade', lease],
                capture_output=True, timeout=100)
            check(verify.returncode == 0, 'Real installer lease failed post-preserve verification')
            after = {'payload': node.payload_snapshot(), 'receipt': node.receipt(),
                     'selection': node.selection(node.protected_state())}
            check(after == before, 'Leased preserve changed payload, links, selection or proof')
            invalid = subprocess.run([binary, 'node-toolchain', 'install', '--preserve', '--installer-lease',
                '00000000-0000-0000-0000-000000000000'], cwd='/', capture_output=True, timeout=15,
                preexec_fn=lambda: os.umask(0o022))
            (private / 'invalid-preserve-lease.log').write_bytes(invalid.stdout + invalid.stderr)
            check(invalid.returncode != 0 and node.REUSED.encode() not in invalid.stdout,
                  'Invalid lease reached toolchain preservation')
            check(node.payload_snapshot() == before['payload'] and node.receipt() == before['receipt'],
                  'Invalid lease changed the managed toolchain')
            result.update(passed=True, duration_seconds=round(time.monotonic() - begin, 6),
                provider=provider['provider'], hierarchical_processes_at_entry=count,
                typed_wait_preceded_preserve=True, natural_completion=True,
                lease_verified_after_preserve=True, payload_and_protected_proof_preserved=True,
                invalid_lease_refused_before_preserve=True)
    finally:
        cron.unlink(missing_ok=True)
        if lease and child and child.poll() is None:
            release = subprocess.run([binary, 'scheduler-installer-release', lease], capture_output=True, timeout=30)
            (private / 'busy-lease-release.log').write_bytes(release.stdout + release.stderr)
            check(release.returncode == 0 and child.wait(timeout=20) == 0, 'Real installer lease did not release')
        deadline = time.monotonic() + 25
        while started.exists() and not ended.exists() and time.monotonic() < deadline:
            time.sleep(.1)
        if started.exists():
            check(ended.exists(), 'Probe did not finish naturally; preserve its private evidence')
        state = json.loads(Path('/var/lib/fluxo-scheduler/state.json').read_text())
        result['no_upgrade_or_holds_after_release'] = not state.get('upgrade') and not state['holds']
        result['reader_floor_bytes_preserved'] = acceptance.base.digest('/var/lib/fluxo/execution-readers.json') == floor
        (private / 'busy-preserve-result.json').write_text(json.dumps(result, indent=2) + '\n')
    check(result['passed'] and result['no_upgrade_or_holds_after_release'] and result['reader_floor_bytes_preserved'],
          'Leased preserve did not preserve exact authority and release')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
