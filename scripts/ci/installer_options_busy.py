import os, json, subprocess, hashlib, time, select, re, datetime
from pathlib import Path

assert os.geteuid() == 0 and os.environ.get('FLUXO_ENV') == 'prod'
assert subprocess.check_output(['hostname'], text=True).strip() == 'fluxo-bridge-prod-20261101'
os.umask(0o077)
p = Path(os.environ['FLUXO_OPTIONS_NATIVE_PRIVATE'])
assert str(p).startswith('/root/fluxo-installer-options-') and p.is_dir()
binary = p/'fluxo-candidate'
sha = lambda f: hashlib.sha256(Path(f).read_bytes()).hexdigest()
assert sha(binary) == os.environ['FLUXO_OPTIONS_NATIVE_SHA256']
assert json.loads((p/'full-install-result.json').read_text())['returncode'] == 0
assert sha('/usr/local/bin/fluxo') == sha(binary)
env = dict(os.environ, FLUXO_ENV='prod', FLUXO_DATA_DIR='/var/lib/fluxo')
floor = sha('/var/lib/fluxo/execution-readers.json')
def tools():
    result = {}
    for name in ['node', 'npm', 'corepack', 'pnpm', 'yarn', 'bun']:
        path = subprocess.check_output(['which', name], text=True).strip()
        args = [path, '--version']
        probe_env = dict(env)
        if name in ['pnpm', 'yarn']:
            # Match the production packageManagerVersion probe. A version
            # check must not resolve a project's spec or download into /root.
            probe_env.update(HOME='/home/fluxo', COREPACK_HOME='/home/fluxo/.cache/node/corepack',
                COREPACK_ENABLE_NETWORK='0', COREPACK_DEFAULT_TO_LATEST='0',
                COREPACK_ENABLE_DOWNLOAD_PROMPT='0', COREPACK_ENABLE_PROJECT_SPEC='0', COREPACK_ENV_FILE='0')
            args = ['runuser', '-u', 'fluxo', '--', *args]
        result[name] = {'path': path, 'target': str(Path(path).resolve()), 'sha256': sha(Path(path).resolve()), 'version': subprocess.check_output(args, env=probe_env, cwd='/home/fluxo', text=True, timeout=30).strip()}
    return result
before = tools()
(p/'node-tools-before-busy.json').write_text(json.dumps(before, indent=2)+'\n')
cron = Path('/etc/cron.d/fluxo-options-natural-probe')
probe = p/'natural-probe.sh'
start_marker, end_marker = p/'cron-start.json', p/'cron-end.json'
assert not any(x.exists() for x in [cron, probe, start_marker, end_marker])
probe.write_text('#!/bin/sh\nset -eu\n/usr/bin/printf \'{"pid":%s,"start_ns":%s}\\n\' "$$" "$(/usr/bin/date +%s%N)" > '+str(start_marker)+'\n/usr/bin/sleep 18\n/usr/bin/printf \'{"end_ns":%s}\\n\' "$(/usr/bin/date +%s%N)" > '+str(end_marker)+'\n')
probe.chmod(0o700)
child = None
lease = None
result = {'passed': False, 'actual_normal_Node_CLI': True, 'real_calendar_cron_child': True, 'manual_authority_edits': False}
try:
    with (p/'busy-lease-private.log').open('wb') as stderr:
        child = subprocess.Popen([str(binary), 'scheduler-installer-lease', str(os.getpid())], env=env, stdout=subprocess.PIPE, stderr=stderr, text=True, start_new_session=True)
        assert select.select([child.stdout], [], [], 110)[0], 'real lease did not become ready'
        line = child.stdout.readline().strip()
        assert re.fullmatch(r'READY [0-9a-f-]{36}', line), 'real lease refused'
        lease = line.split()[1]
        j = json.loads(Path('/var/lib/fluxo-scheduler/state.json').read_text())
        assert j['upgrade']['id'] == lease and j['upgrade']['pid'] == child.pid
        assert j['holds'] and all(v == lease for v in j['holds'].values())
        # Calendar fields bind the probe to ONE next minute. No manual process
        # pretending to be cron, fake provider/cgroup, or journal/receipt writes.
        when = datetime.datetime.now() + datetime.timedelta(minutes=1)
        cron.write_text(f'{when.minute} {when.hour} {when.day} {when.month} * root {probe} >/dev/null 2>&1\n')
        cron.chmod(0o644)
        deadline = time.monotonic()+95
        while not start_marker.exists():
            assert child.poll() is None and time.monotonic()<deadline, 'real cron launch missing'
            time.sleep(.1)
        cron.unlink()
        marker = json.loads(start_marker.read_text())
        assert not end_marker.exists()
        info = {}
        for provider in ['cron', 'cronie']:
            raw = subprocess.check_output(['systemctl', 'show', provider+'.service', '-p', 'ActiveState', '-p', 'MainPID', '-p', 'ControlGroup'], text=True)
            state = dict(x.split('=', 1) for x in raw.splitlines() if '=' in x)
            if state.get('ActiveState') == 'active':
                assert not info, 'multiple providers'
                info = state | {'provider': provider}
        assert info
        cg = info['ControlGroup']
        assert any(x.endswith(':'+cg) or (':'+cg+'/') in x for x in Path(f'/proc/{marker["pid"]}/cgroup').read_text().splitlines())
        count = int(Path('/sys/fs/cgroup'+cg+'/pids.current').read_text())
        assert count > 1
        result.update(cron_hierarchical_processes_at_entry=count, provider=info['provider'], installer_child_umask='0022')
        begin_ns = time.time_ns()
        begin = time.monotonic()
        with (p/'busy-node-private.log').open('wb') as log:
            # The normal installer sets umask 0022. Keep private test logs at
            # 0077, but do not accidentally provision tool directories at 0700.
            r = subprocess.run([str(binary), 'node-toolchain', 'install', '--installer-lease', lease], env=env, cwd='/', stdout=log, stderr=subprocess.STDOUT, timeout=240, preexec_fn=lambda: os.umask(0o022))
        text = (p/'busy-node-private.log').read_text()
        result['node_CLI_returncode'] = r.returncode
        result['node_CLI_log_sha256'] = sha(p/'busy-node-private.log')
        assert r.returncode == 0, 'actual Node CLI failed; retain private log'
        assert text.index('Waiting for earlier cron workers') < text.index('Waiting for exclusive access to the Node.js toolchain')
        assert 'Earlier cron workers finished; continuing the upgrade.' in text
        assert 'Node.js toolchain ready:' in text
        ended = json.loads(end_marker.read_text())
        assert begin_ns < ended['end_ns'] <= time.time_ns()
        assert child.poll() is None
        verify = subprocess.run([str(binary), 'scheduler-installer-verify-upgrade', lease], env=env, capture_output=True, timeout=100)
        assert verify.returncode == 0
        assert tools() == before, 'warm Node changed versions/command payloads'
        invalid = subprocess.run([str(binary), 'node-toolchain', 'install', '--installer-lease', '00000000-0000-0000-0000-000000000000'], env=env, cwd='/', capture_output=True, text=True, timeout=15, preexec_fn=lambda: os.umask(0o022))
        (p/'invalid-lease-private.log').write_text(invalid.stdout+invalid.stderr)
        assert invalid.returncode != 0 and 'Waiting for exclusive access to the Node.js toolchain' not in invalid.stdout
        assert tools() == before
        result.update({'passed': True, 'seconds_one_observation': time.monotonic()-begin, 'cron_hierarchical_processes_at_entry': count, 'provider': info['provider'], 'wait_preceded_toolchain_mutation': True, 'natural_cron_completion': True, 'lease_verified_after_Node': True, 'all_six_versions_and_command_payloads_preserved': True, 'invalid_lease_refused_before_install': True})
finally:
    cron.unlink(missing_ok=True)
    if lease and child and child.poll() is None:
        r = subprocess.run([str(binary), 'scheduler-installer-release', lease], env=env, capture_output=True, timeout=30)
        (p/'busy-lease-release.log').write_bytes(r.stdout+r.stderr)
        assert r.returncode == 0
        assert child.wait(timeout=20) == 0
    deadline = time.monotonic()+25
    while start_marker.exists() and not end_marker.exists() and time.monotonic()<deadline:
        time.sleep(.1)
    if start_marker.exists():
        assert end_marker.exists(), 'probe did not finish naturally; preserve state'
    j = json.loads(Path('/var/lib/fluxo-scheduler/state.json').read_text())
    result['no_upgrade_or_holds_after_release'] = not j.get('upgrade') and not j['holds']
    result['reader_floor_bytes_preserved'] = sha('/var/lib/fluxo/execution-readers.json') == floor
    (p/'busy-node-result.json').write_text(json.dumps(result, indent=2)+'\n')
assert result['passed'] and result['no_upgrade_or_holds_after_release'] and result['reader_floor_bytes_preserved']
print(json.dumps(result), flush=True)
