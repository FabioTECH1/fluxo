"""Disposable Noble amd64 old-producer acceptance; no journal/ACK fabrication.

All credentials and raw output stay in a root-private directory. Export only
allowlisted milestones, hashes and selected identity/format fields. This script
never runs on local reusable VMs or customer servers.
"""
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import signal
import ssl
import stat
import subprocess
import tempfile
import time
import urllib.request

BRANCH = 'refs/heads/test/amd64-v040-cronie-upgrade-20261118'
OLD_INSTALLER = 'c5b93156a66f768ad6e0998c49f17adfcad273755ed36cb88f87f9ee453aa308'
CORE = Path('/var/lib/fluxo-system-tasks/state.json')
PANEL = Path('/usr/local/bin/fluxo')
INTENT = Path('/var/lib/fluxo/execution-installer.json')
BASE = 'https://127.0.0.1:9595/api/v1'


def sha(path):
    h = hashlib.sha256()
    with open(path, 'rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            h.update(block)
    return h.hexdigest()


def state(path=CORE):
    return json.loads(path.read_bytes()) if path.is_file() else {}


def wait_for(check, seconds, label):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        try:
            value = check()
            if value:
                return value
        except (FileNotFoundError, ConnectionError, OSError):
            pass
        time.sleep(.2)
    raise RuntimeError('Timed out: ' + label)


def identity():
    group = Path('/sys/fs/cgroup/system.slice/cronie.service')
    pid = int(subprocess.check_output(['systemctl', 'show', 'cronie.service', '-p', 'MainPID', '--value']))
    fields = Path(f'/proc/{pid}/stat').read_text().rsplit(')', 1)[1].split()
    return {'boot': Path('/proc/sys/kernel/random/boot_id').read_text().strip(), 'pid': pid,
            'start': fields[19], 'group_inode': group.stat().st_ino,
            'pids.current': int((group / 'pids.current').read_text())}


def api(method, path, data=None, bearer=None):
    headers = {'Content-Type': 'application/json'}
    if bearer:
        headers['Authorization'] = 'Bearer ' + bearer
    req = urllib.request.Request(BASE + path, method=method, headers=headers,
        data=json.dumps(data).encode() if data is not None else None)
    with urllib.request.urlopen(req, context=ssl._create_unverified_context(), timeout=90) as response:
        raw = response.read()
        return json.loads(raw) if raw else None


def main():
    required = {'FLUXO_AMD64_V040_NATIVE': '1', 'GITHUB_ACTIONS': 'true',
        'GITHUB_REF': BRANCH, 'RUNNER_ARCH': 'X64', 'RUNNER_ENVIRONMENT': 'github-hosted'}
    if os.geteuid() != 0 or platform.machine() != 'x86_64' or any(os.environ.get(k) != v for k, v in required.items()):
        raise SystemExit('Requires isolated named GitHub-hosted amd64 workflow')
    if PANEL.exists() or Path('/var/lib/fluxo').exists():
        raise SystemExit('Refuse any existing Fluxo baseline')
    scenario = os.environ.get('FLUXO_NATIVE_SCENARIO', 'full')
    if scenario not in ('full', 'uncertain', 'node-full'):
        raise SystemExit('Unknown isolated native scenario')
    output = Path(os.environ['FLUXO_NATIVE_OUTPUT']); output.mkdir(mode=0o755)
    private = Path(tempfile.mkdtemp(prefix='fluxo-amd64-v040-', dir='/root')); private.chmod(0o700)
    report = {'node_enabled': scenario == 'node-full', 'scenario': scenario, 'source': os.environ['GITHUB_SHA'], 'base': '71c35f4e7810005f3e06cc25339e86631ab9bf2b',
        'predecessor': 'v0.4.40', 'passed': False, 'public_candidate_delivery': False,
        'native': True, 'architecture': 'amd64', 'fixtures_restored': False,
        'cleanup_inventory': {'persistent_vms_created': [], 'runner': 'ephemeral GitHub ubuntu-24.04',
            'private_evidence': str(private)}}
    env = {'PATH': '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin', 'HOME': '/root',
        'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8', 'DEBIAN_FRONTEND': 'noninteractive',
        'NEEDRESTART_MODE': 'l', 'FLUXO_ENV': 'prod', 'FLUXO_DATA_DIR': '/var/lib/fluxo'}
    backups = []
    modes = []
    timers = {}
    survivor = Path('/etc/cron.d/fluxo-amd64-native-survivor')
    release = private / 'survivor-release'
    processes = []

    def save():
        (output / 'report.json').write_text(json.dumps(report, indent=2, sort_keys=True) + '\n')

    def command(label, args, timeout=180, must=True, extra=None):
        report['stage'] = label
        save()
        print(json.dumps({'stage': label, 'state': 'starting'}), flush=True)
        log = private / (label + '.log')
        trace_path = private / (label + '.trace')
        trace = trace_path.open('xb') if label == 'published-v040-install' else None
        if trace:
            trace_path.chmod(0o600)
        try:
            with log.open('xb') as stream:
                log.chmod(0o600)
                command_env=dict(env, **(extra or {}))
                if trace:
                    command_env['BASH_XTRACEFD']=str(trace.fileno())
                proc = subprocess.run(args, stdin=subprocess.DEVNULL, stdout=stream, stderr=subprocess.STDOUT,
                    env=command_env, timeout=timeout, pass_fds=(trace.fileno(),) if trace else ())
        finally:
            if trace:
                trace.close()
        raw = log.read_text(errors='replace')
        # Do not export arbitrary command output, argv, environment or journals.
        known = ['Fluxo release provenance verified', 'Scheduler cron execution domain changed',
            'Could not obtain a safe PHP/Scheduler upgrade lease', 'Creating a pre-upgrade rollback snapshot',
            'Previous Fluxo release restored', 'installation complete', 'Installation complete',
            'historical installer retirement requires', 'wait for System-task', 'Waiting for cron',
            'Reader setup could not finish', 'Could not finish execution', 'An unfinished installer']
        report[label] = {'exit_code': proc.returncode, 'private_log_sha256': sha(log),
            'milestones': [s for s in known if s in raw]}
        # These diagnostics contain implementation errors, never registration fields.
        if not proc.returncode and args[-1:] == ['system-task-diagnostics']:
            try:
                report[label]['diagnostics'] = json.loads(raw)
            except ValueError:
                pass
        if proc.returncode:
            safe_lines=[]
            for line in raw.splitlines():
                line=re.sub(r'\x1b\[[0-9;]*m','',line)
                if not re.search(r'password|token|credential|secret|authorization|nonce|ticket|private.key|bootstrap',line,re.I) and (line.startswith(('ERROR:', 'E:', 'RuntimeError:', 'Scheduler ', 'historical ', 'Unsupported ', 'Packaged ', 'Unsafe ', 'System-task ', 'Fluxo ', 'The ', 'PHP ', 'curl:', 'dpkg:', 'gpg:', 'WARNING:', 'Traceback ', 'ValueError:', 'subprocess.', 'Exception:', 'ConnectionError:', 'download ', 'verify ', 'select ', 'inspect ', 'resolve ', 'create ', 'managed ', 'node ', 'npm ', 'bun ', 'Bun ', 'pnpm ', 'Corepack ', 'corepack ', 'yarn ', 'Yarn ', 'Node.js ', 'Fluxo-managed '))):
                    safe_lines.append(re.sub(r'[0-9a-f]{8}-[0-9a-f-]{27}|[0-9a-f]{32,}', '[identity-redacted]', line)[:500])
            report[label]['diagnostic_lines']=safe_lines[-20:]
            # Xtrace stays private. Export command NAMES only before the EXIT
            # cleanup, never arguments, variable assignments or command output.
            traced=trace_path.read_text(errors='replace').split('+ cleanup_installer',1)[0] if trace_path.exists() else ''
            command_names=[]
            for trace in traced.splitlines():
                found=re.match(r'^\++ ([A-Za-z_][A-Za-z0-9_-]{0,63})(?: |$)',trace)
                if found:command_names.append(found[1])
            report[label]['pre_cleanup_command_names']=command_names[-60:]
            if trace_path.exists():report[label]['private_trace_sha256']=sha(trace_path)
        save()
        print(json.dumps({'stage': label, 'exit_code': proc.returncode}), flush=True)
        if must and proc.returncode:
            raise RuntimeError(label + ' failed; see selected diagnostics/private log digest')
        return proc.returncode, raw

    def replace(path, content):
        path = Path(path)
        backup = private / ('fixture-' + str(len(backups)))
        if path.exists():
            shutil.copy2(path, backup)
        else:
            backup = None
        backups.append((path, backup))
        path.write_bytes(content); path.chmod(0o644)

    def lease_refusal(image, label, expected=None):
        before = sha(CORE)
        log = private / (label + '.log')
        with log.open('xb') as stream:
            log.chmod(0o600)
            proc = subprocess.Popen([str(image), 'scheduler-installer-lease', str(os.getpid())],
                env=env, stdout=stream, stderr=subprocess.STDOUT, start_new_session=True)
            processes.append(proc)
            try:
                proc.wait(timeout=15)
            except subprocess.TimeoutExpired:
                raw = log.read_text()
                ready = re.search(r'^READY ([0-9a-f-]{36})$', raw, re.M)
                if ready:
                    subprocess.run([str(image), 'scheduler-installer-release', ready[1]], env=env,
                        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)
                proc.wait(timeout=10)
                raise RuntimeError(label + ' unexpectedly acquired a lease')
        raw = log.read_text(errors='replace')
        if proc.returncode == 0 or 'READY ' in raw or (expected and expected not in raw):
            raise RuntimeError(label + ' did not reproduce expected refusal')
        if sha(CORE) != before:
            raise RuntimeError(label + ' changed original task authority')
        report[label] = {'refused': True, 'original_task_bytes_unchanged': True,
            'exact_expected_error': expected is not None, 'private_log_sha256': sha(log)}
        save()

    try:
        # Stock-profile preparation applies only to this disposable hosted image.
        for name in ['/usr', '/usr/share', '/usr/share/keyrings', '/usr/local', '/usr/local/bin',
                     '/usr/local/sbin', '/etc', '/etc/apt', '/etc/apt/sources.list.d', '/etc/apt/preferences.d']:
            path = Path(name)
            if path.is_dir():
                info = path.lstat()
                if info.st_uid != 0 or stat.S_ISLNK(info.st_mode):
                    raise RuntimeError('Unsafe system fixture ancestry')
                modes.append((path, stat.S_IMODE(info.st_mode)))
                path.chmod(stat.S_IMODE(info.st_mode) & ~0o022)
        for unit in ['apt-daily.timer', 'apt-daily-upgrade.timer']:
            timers[unit] = subprocess.run(['systemctl', 'is-active', '--quiet', unit]).returncode == 0
            command('pause-' + unit, ['systemctl', 'stop', unit])
        for name in ['microsoft-prod.list', 'microsoft-prod.sources']:
            path = Path('/etc/apt/sources.list.d') / name
            if path.exists():
                target = private / name; path.rename(target); backups.append((path, target))
        pam = Path('/etc/pam.d/common-session-noninteractive'); raw = pam.read_bytes()
        if sha(pam) == 'ed47c9b6671f3977d679102cb4d836ef2982613aaa47647d851fe7d695eaaead':
            suffix = b'session required pam_limits.so\n'
            assert raw.endswith(suffix)
            replace(pam, raw[:-len(suffix)])
        if sha(pam) != '1bd1ef2b6241fc8b3c97bbaf57e5c6038c96c275e30c62ebee6eec15701e2e07':
            raise RuntimeError('Unrecognized noninteractive PAM fixture')
        replace('/etc/environment', b'PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/games:/usr/local/games:/snap/bin"\n')
        command('apt-refresh', ['apt-get', 'update'], 240)
        command('cronie-install', ['apt-get', 'install', '-y', 'cronie=1.7.1-1'], 240)
        command('cronie-start', ['systemctl', 'start', 'cronie.service'])
        # Third-party hosted Python packages are not part of the supported vendor profile.
        local = Path('/usr/local/lib/python3.12/dist-packages')
        if local.is_dir() and any(local.iterdir()):
            archived = private / 'python-local.original'; local.rename(archived); local.mkdir(mode=0o755)
            backups.append((local, archived))
        installer = private / 'v040-install.sh'
        command('download-old-installer', ['curl', '-fsSL', '--retry', '3', '-o', str(installer),
            'https://raw.githubusercontent.com/FabioTECH1/fluxo/v0.4.40/install.sh'])
        if sha(installer) != OLD_INSTALLER:
            raise RuntimeError('Old installer differs from exact published source')
        report['old_installer_sha256'] = sha(installer)
        flags = ['--db-engine=mysql', '--redis', '--node' if scenario == 'node-full' else '--no-node', '--no-python', '--no-harden-ssh']
        _, old_log = command('published-v040-install', ['bash', '-x', str(installer), *flags], 1000,
            extra={'FLUXO_VERSION': 'v0.4.40'})
        if 'Fluxo release provenance verified' not in old_log:
            raise RuntimeError('Old public delivery provenance was not verified')
        old_binary = private / 'fluxo-v040'; shutil.copy2(PANEL, old_binary)
        if subprocess.check_output([str(old_binary), '--version'], env=env).decode().strip() != 'fluxo version 0.4.40':
            raise RuntimeError('Wrong old producer version')
        report['old_binary_sha256'] = sha(old_binary)
        command('nginx-start', ['systemctl', 'start', 'nginx.service'])
        credentials = Path('/var/lib/fluxo/.fluxo_credentials').read_text()
        match = re.search(r'^Fluxo bootstrap token[^:]*:\s*([0-9a-f]{32})\s*$', credentials, re.M)
        if not match:
            raise RuntimeError('Private old bootstrap credential unavailable')
        bearer = api('POST', '/auth/login', {'username': 'amd64-native-owner', 'password': match[1]})['token']
        command('old-producer-diagnostics', [str(old_binary), 'system-task-diagnostics'])
        job = api('POST', '/crons', {'name': 'Native old producer renewal', 'expression': '* * * * *',
            'command': 'certbot renew --quiet', 'user': 'root'}, bearer)['id']
        old_core = wait_for(lambda: state() if str(job) in state().get('entries', {}) else None,
            40, 'v0.4.40 guarded registration')
        if old_core['schema'] != 1:
            raise RuntimeError('Old producer did not emit schema1')
        wait_for(lambda: state()['entries'][str(job)]['received'], 150, 'genuine old cron acknowledgement')
        def completed_job():
            return [v for v in state().get('invocations', {}).values()
                    if v['registration']['contract']['id'] == job and v['status'] == 'completed']
        wait_for(completed_job, 180, 'actual old Cronie-scheduled task completion')
        command('old-panel-stop', ['systemctl', 'stop', 'fluxo.service'])
        wait_for(lambda: identity()['pids.current'] == 1, 45, 'original Cronie natural idle')
        original_bytes = CORE.read_bytes(); original = json.loads(original_bytes)
        (private / 'original-task-core.json').write_bytes(original_bytes)
        old_helpers = {r['helper']: sha(r['helper']) for r in original['entries'].values()}
        candidate = private / 'fluxo-candidate'
        shutil.copy2(Path(os.environ['FLUXO_NATIVE_BUILD']) / 'fluxo', candidate)
        candidate.chmod(0o700)
        report['candidate_sha256'] = sha(candidate)
        if scenario == 'uncertain':
            # Create real authority through the old published helper; stop and
            # kill only its actual guard after Started is durable. No JSON edit.
            known = set(state().get('invocations', {}))
            old_entry = state()['entries'][str(job)]
            log = private / 'real-uncertain-dispatch.log'
            with log.open('xb') as stream:
                log.chmod(0o600)
                dispatch = subprocess.Popen([old_entry['helper'], 'system-task-dispatch', str(job), old_entry['identity']],
                    env=env, stdin=subprocess.DEVNULL, stdout=stream, stderr=subprocess.STDOUT)
                def started_unit():
                    for key, invocation in state().get('invocations', {}).items():
                        if key not in known and invocation['registration']['contract']['id'] == job and invocation['status'] == 'started':
                            return invocation['unit']
                unit = wait_for(started_unit, 30, 'real old guard Started authority')
                if not re.fullmatch(r'fluxo-system-task-[0-9a-f-]{36}\.service', unit):
                    raise RuntimeError('Unexpected real task unit identity')
                command('stop-real-task-guard', ['systemctl', 'kill', '--kill-whom=main', '--signal=STOP', unit])
                if state()['invocations'][unit]['status'] != 'started':
                    raise RuntimeError('Old task finished before uncertainty injection')
                command('crash-real-task-guard', ['systemctl', 'kill', '--kill-whom=main', '--signal=KILL', unit])
                dispatch.wait(timeout=30)
            domain = Path('/sys/fs/cgroup/system.slice') / unit
            wait_for(lambda: not domain.exists() or int((domain / 'pids.current').read_text()) == 0,
                45, 'remaining real application descendants natural exit')
            uncertain = state()['invocations'][unit]
            if uncertain['status'] != 'started':
                raise RuntimeError('Expected genuinely unresolved Started authority')
            lease_refusal(candidate, 'uncertain-authority-refused', 'historical task execution authority is active or uncertain')
            report['uncertain_result'] = {'actual_old_dispatch': True, 'guard_crashed_after_started': True,
                'application_domain_naturally_empty': True, 'unresolved_authority_preserved': True,
                'normal_installer_lease_refused': True, 'normal_upgrade_attempted': False}
            report['passed'] = True
            return 0
        saved = identity()
        assert all(r['backend']['group_inode'] == saved['group_inode'] and r['backend']['pid'] == saved['pid']
            for r in original['entries'].values() if r['enabled'])
        report['old_producer'] = {'task_schema': original['schema'], 'entries': len(original['entries']),
            'received': sum(r['received'] for r in original['entries'].values()),
            'old_scheduled_job_completed': True, 'identity': saved, 'task_sha256': sha(CORE)}
        # A genuine Cronie child survives KillMode=process. No task journal is edited.
        started = private / 'survivor-started'
        survivor.write_text(f"* * * * * root /bin/sh -c 'echo started > {started}; i=0; while ! test -e {release} && test $i -lt 240; do sleep 1; i=$((i+1)); done'\n")
        survivor.chmod(0o644)
        wait_for(started.exists, 90, 'real Cronie surviving-child fixture')
        survivor.unlink()
        command('generation-with-live-child', ['systemctl', 'restart', 'cronie.service'])
        if identity()['pids.current'] <= 1:
            raise RuntimeError('Surviving-child fixture was not live')
        lease_refusal(candidate, 'surviving-child-refused')
        release.touch()
        wait_for(lambda: identity()['pids.current'] == 1, 45, 'surviving child natural completion')
        command('old-empty-domain-stop', ['systemctl', 'stop', 'cronie.service'])
        wait_for(lambda: not Path('/sys/fs/cgroup/system.slice/cronie.service').exists(), 20, 'old empty domain disappearance')
        command('new-empty-generation-start', ['systemctl', 'start', 'cronie.service'])
        current = identity()
        if current['boot'] != saved['boot'] or current['group_inode'] == saved['group_inode']:
            raise RuntimeError('Expected a genuine same-boot replaced domain')
        report['replacement_identity'] = current
        published47 = private / 'fluxo-v047'
        command('download-public47', ['curl', '-fsSL', '--retry', '3', '-o', str(published47),
            'https://github.com/FabioTECH1/fluxo/releases/download/v0.4.47/fluxo-linux-amd64'])
        checks = private / 'v047-shas'
        command('download-public47-checksums', ['curl', '-fsSL', '--retry', '3', '-o', str(checks),
            'https://github.com/FabioTECH1/fluxo/releases/download/v0.4.47/SHA256SUMS'])
        expected = next(line.split()[0] for line in checks.read_text().splitlines() if line.split()[-1] == 'fluxo-linux-amd64')
        if sha(published47) != expected:
            raise RuntimeError('Public47 checksum mismatch')
        published47.chmod(0o700)
        lease_refusal(published47, 'published47-exact-failure', 'Scheduler cron execution domain changed')
        command('old-panel-start', ['systemctl', 'start', 'fluxo.service'])
        wait_for(lambda: api('GET', '/version')['version'] == '0.4.40', 45, 'old panel health')
        env['FLUXO_LOCAL_BINARY_SHA256'] = sha(candidate)
        args = ['bash', 'install.sh', '--local-binary=' + str(candidate), *flags]
        # Crash only this fixture installer group after its exact activation
        # operation is durable. A later ordinary installer must adopt that image.
        crash_log = private / 'interrupted-normal-installer.log'
        with crash_log.open('xb') as stream:
            crash_log.chmod(0o600)
            proc = subprocess.Popen(args, env=env, stdin=subprocess.DEVNULL, stdout=stream,
                stderr=subprocess.STDOUT, start_new_session=True)
            processes.append(proc)
            end = time.monotonic() + 1000
            interrupted = False
            while time.monotonic() < end and proc.poll() is None:
                intent = state(INTENT)
                if intent.get('phase') == 'pending' and intent.get('operation_id'):
                    os.killpg(proc.pid, signal.SIGSTOP)
                    frozen = state(INTENT)
                    report['interruption'] = {'operation_durable': bool(frozen.get('operation_id')),
                        'intent_phase': frozen.get('phase'), 'task_schema': state().get('schema'),
                        'intent_sha256': sha(INTENT)}
                    os.killpg(proc.pid, signal.SIGKILL)
                    proc.wait(timeout=20); interrupted = True
                    break
                time.sleep(.01)
            if not interrupted:
                if proc.poll() is None:
                    os.killpg(proc.pid, signal.SIGKILL); proc.wait(timeout=20)
                report['first_candidate_install'] = {'exit_code': proc.returncode,
                    'private_log_sha256': sha(crash_log)}
                # Export safely selected diagnostics using the same allowlist.
                raw=crash_log.read_text(errors='replace')
                safe=[]
                for line in raw.splitlines():
                    line=re.sub(r'\x1b\[[0-9;]*m','',line)
                    if line.startswith(('ERROR:','Scheduler ','System-task ','historical ','Unsafe ','Unsupported ')) and not re.search(r'password|token|credential|secret|nonce',line,re.I):safe.append(re.sub(r'[0-9a-f]{32,}|[0-9a-f]{8}-[0-9a-f-]{27}','[identity-redacted]',line)[:500])
                report['first_candidate_install']['diagnostic_lines']=safe[-15:]
                raise RuntimeError('Did not observe the durable interruption boundary')
        operation = state(INTENT)['operation_id']
        command('normal-installer-recovery', args, 1000)
        if sha(PANEL) != sha(candidate) or state(INTENT).get('phase') != 'complete':
            raise RuntimeError('Normal installer recovery did not finish exact image')
        floor = state(Path('/var/lib/fluxo/execution-readers.json'))
        if floor.get('phase') != 'committed' or floor.get('activation_id') != operation:
            raise RuntimeError('Recovery changed the original operation')
        archive = Path('/var/lib/fluxo/execution-activations') / operation
        plan = state(archive / 'plan.json')
        if not plan.get('installer_historical_task_retirements'):
            raise RuntimeError('Missing archived retirement identities')
        core_index = next(i for i, change in enumerate(plan['files']) if change['path'] == str(CORE))
        if (archive / f'before-{core_index}').read_bytes() != original_bytes:
            raise RuntimeError('Original authority was not archived exactly')
        if any(sha(path) != digest for path, digest in old_helpers.items()):
            raise RuntimeError('Original helpers were not retained')
        new = state()
        if new['schema'] != 3:
            raise RuntimeError('Expected current task format')
        for key, r in original['entries'].items():
            target = new['entries'][key]
            if target['identity'] != r['identity'] or target['nonce'] == r['nonce'] or target['helper'] == r['helper']:
                raise RuntimeError('Conversion lost identity or reused old helper/nonce')
        wait_for(lambda: all(r['received'] for r in state()['entries'].values() if r['enabled']),
            180, 'genuine new Cronie acknowledgements')
        target = state()['entries'][str(job)]
        rc, _ = command('manual-new-ack-refused', [target['helper'], 'system-task-receipt', str(job), target['nonce']], must=False)
        if rc == 0:
            raise RuntimeError('Manual ACK accepted')
        prior = set(state()['invocations'])
        def continued():
            return [v for key, v in state()['invocations'].items() if key not in prior
                and v['registration']['contract']['id'] == job and v['status'] == 'completed']
        wait_for(continued, 180, 'continued genuine Cronie-scheduled job completion')
        report['result'] = {'task_schema': 3, 'operation_recovered_exactly': True,
            'old_core_archived_exactly': True, 'old_helpers_retained': True,
            'fresh_nonces': True, 'genuine_new_acknowledgements': True,
            'manual_ack_refused': True, 'continued_cron_execution': True,
            'live_acme_tested': False, 'archive_kind': plan['kind']}
        report['passed'] = True
    except Exception as error:
        report['error'] = str(error) if isinstance(error, (RuntimeError, AssertionError)) else type(error).__name__
    finally:
        release.touch(exist_ok=True)
        survivor.unlink(missing_ok=True)
        for proc in processes:
            if proc.poll() is None:
                os.killpg(proc.pid, signal.SIGKILL)
                proc.wait(timeout=20)
        subprocess.run(['systemctl', 'stop', 'fluxo.service'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        for path, backup in reversed(backups):
            if path.is_dir():
                shutil.rmtree(path)
            elif path.exists():
                path.unlink()
            if backup is not None:
                backup.rename(path)
        for path, mode in reversed(modes):
            path.chmod(mode)
        for timer, active in timers.items():
            if active:
                subprocess.run(['systemctl', 'start', timer], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        report['fixtures_restored'] = True
        save()
        print(json.dumps({'passed': report['passed'], 'error': report.get('error'), 'report': str(output / 'report.json')}))
    return 0 if report['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
