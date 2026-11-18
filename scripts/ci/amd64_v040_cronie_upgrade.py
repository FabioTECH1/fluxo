"""Disposable Noble amd64 old-producer acceptance; no journal/ACK fabrication.

All credentials and raw output stay in a root-private directory. Export only
allowlisted milestones, hashes and selected identity/format fields. This script
never runs on local reusable VMs or customer servers.
"""
import errno
import hashlib
import io
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
import tarfile
import time
import urllib.request
from amd64_fixture_metadata import PreparedRoots
from amd64_reviewed_apt import ReviewedAPT

BRANCH = 'refs/heads/test/amd64-v040-cronie-upgrade-20261118'
OLD_INSTALLER = 'c5b93156a66f768ad6e0998c49f17adfcad273755ed36cb88f87f9ee453aa308'
CORE = Path('/var/lib/fluxo-system-tasks/state.json')
PANEL = Path('/usr/local/bin/fluxo')
INTENT = Path('/var/lib/fluxo/execution-installer.json')
BASE = 'https://127.0.0.1:9595/api/v1'


def sha(path):
    info=Path(path).stat()
    if not stat.S_ISREG(info.st_mode) or info.st_size>512<<20:
        raise RuntimeError('Refuse unbounded or special evidence file')
    h = hashlib.sha256()
    with open(path, 'rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            h.update(block)
    return h.hexdigest()


def state(path=CORE):
    return json.loads(path.read_bytes()) if path.is_file() else {}


def wait_for(check, seconds, label):
    began=time.monotonic();heartbeat=began
    end = began + seconds
    while time.monotonic() < end:
        try:
            value = check()
            if value:
                return value
        except (FileNotFoundError, ConnectionError, OSError):
            pass
        if time.monotonic()-heartbeat>=15:
            print(json.dumps({'stage':label,'state':'waiting','elapsed_seconds':int(time.monotonic()-began)}),flush=True);heartbeat=time.monotonic()
        time.sleep(.2)
    raise RuntimeError('Timed out: ' + label)


def identity():
    group = Path('/sys/fs/cgroup/system.slice/cronie.service')
    pid = int(subprocess.check_output(['systemctl', 'show', 'cronie.service', '-p', 'MainPID', '--value'],timeout=5))
    fields = Path(f'/proc/{pid}/stat').read_text().rsplit(')', 1)[1].split()
    return {'boot': Path('/proc/sys/kernel/random/boot_id').read_text().strip(), 'pid': pid,
            'start': fields[19], 'group_inode': group.stat().st_ino,
            'pids.current': int((group / 'pids.current').read_text())}



def history_retained(previous, following):
    # v0.4.40 legitimately compacts terminal live entries into immutable
    # protected history files before its next dispatch attempt. Observe and
    # verify those real receipts; never recreate missing evidence.
    history=CORE.parent/'history'
    for key,old in previous.get('invocations',{}).items():
        new=following.get('invocations',{}).get(key)
        if new is None:
            if not re.fullmatch(r'fluxo-system-task-[0-9a-f-]{36}\.service',key):
                return False
            path=history/(key+'.json')
            try:
                info=path.lstat()
                if not stat.S_ISREG(info.st_mode) or info.st_uid!=0 or info.st_mode&0o022:
                    return False
                new=json.loads(path.read_bytes())
            except (OSError,ValueError):
                return False
        if old.get('status') in ('completed','blocked','interrupted'):
            if new!=old:return False
        else:
            mutable={'status','error','result_profile','application_started'}
            if {k:v for k,v in old.items() if k not in mutable}!={k:v for k,v in new.items() if k not in mutable}:
                return False
    return True


def immutable_history():
    root=CORE.parent/'history'
    return {str(p):sha(p) for p in root.glob('*.json')} if root.is_dir() else {}

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
    if scenario not in ('full', 'uncertain', 'node-full', 'probe'):
        raise SystemExit('Unknown isolated native scenario')
    previous_umask=os.umask(0o022)
    output = Path(os.environ['FLUXO_NATIVE_OUTPUT']); output.mkdir(mode=0o755,exist_ok=True)
    private = Path(tempfile.mkdtemp(prefix='fluxo-amd64-v040-', dir='/root')); private.chmod(0o700)
    report = {'fixture_umask_before':oct(previous_umask), 'fixture_umask':'0o22', 'node_enabled': scenario == 'node-full', 'scenario': scenario, 'source': os.environ['GITHUB_SHA'], 'base': '71c35f4e7810005f3e06cc25339e86631ab9bf2b',
        'harness_source':os.environ.get('FLUXO_NATIVE_HARNESS_SOURCE',os.environ['GITHUB_SHA']),
        'predecessor': 'v0.4.40', 'passed': False, 'public_candidate_delivery': False,
        'native': True, 'architecture': 'amd64', 'fixtures_restored': False, 'checkpoints':[], 'finished':False,
        'cleanup_inventory': {'persistent_vms_created': [], 'runner': 'ephemeral GitHub ubuntu-24.04',
            'private_evidence': str(private)}}
    env = {'PATH': '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin', 'HOME': '/root',
        'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8', 'DEBIAN_FRONTEND': 'noninteractive',
        'NEEDRESTART_MODE': 'l', 'FLUXO_ENV': 'prod', 'FLUXO_DATA_DIR': '/var/lib/fluxo'}
    backups = []
    prepared_roots=None
    modes = []
    timers = {}
    survivor = Path('/etc/cron.d/fluxo-amd64-native-survivor')
    release = private / 'survivor-release'
    processes = []

    began=time.monotonic()

    def acl_digest(path,name):
        try:raw=os.getxattr(path,'system.posix_acl_'+name)
        except OSError as error:
            if error.errno not in (errno.ENODATA,errno.ENOTSUP):raise
            return None
        return hashlib.sha256(raw).hexdigest()

    def save():
        report['elapsed_seconds']=int(time.monotonic()-began)
        for name,data in [('report.json',report),('progress.json',{k:report.get(k) for k in ('stage','checkpoints','finished','passed','error','elapsed_seconds','installer_milestones')})]:
            temporary=output/(name+'.tmp');temporary.write_text(json.dumps(data,indent=2,sort_keys=True)+'\n');temporary.replace(output/name)

    def stage(name):
        report['stage']=name;save();print(json.dumps({'stage':name,'state':'starting','elapsed_seconds':int(time.monotonic()-began)}),flush=True)

    def checkpoint(name):
        report['checkpoints'].append(name);save();print(json.dumps({'checkpoint':name,'elapsed_seconds':int(time.monotonic()-began)}),flush=True)

    def installer_substage(trace):
        source=private/'v040-install.sh'
        if not trace.is_file() or not source.is_file():return None
        functions=set(re.findall(r'^([A-Za-z_][A-Za-z_0-9]*)\(\)\s*\{',source.read_text(),re.M))
        with trace.open('rb') as stream:
            stream.seek(max(0,trace.stat().st_size-131072));tail=stream.read().decode(errors='replace')
        names=re.findall(r'^\++ ([A-Za-z_][A-Za-z_0-9]*)(?: |$)',tail,re.M)
        return next((name for name in reversed(names) if name in functions),None)

    def installer_heartbeat(label,log,elapsed):
        with log.open('rb') as stream:
            stream.seek(max(0,log.stat().st_size-131072));tail=stream.read().decode(errors='replace')
        markers={'provenance-verified':'Fluxo release provenance verified',
            'binary-install':'Installing binary to /usr/local/bin',
            'snapshot':'Creating a pre-upgrade rollback snapshot',
            'safe-lease-refused':'Could not obtain a safe PHP/Scheduler upgrade lease',
            'reader-setup':'Reader setup', 'waiting-for-cron':'Waiting for cron',
            'install-complete':'Installation complete'}
        report['installer_milestones']={'stage':label,'source_function':installer_substage(private/(label+'.trace')),'names':[name for name,match in markers.items() if match in tail]}
        save();print(json.dumps({'stage':label,'state':'running','elapsed_seconds':int(elapsed),
            'milestones':report['installer_milestones']['names']}),flush=True)

    def command(label, args, timeout=180, must=True, extra=None, cwd=None):
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
                proc = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=stream, stderr=subprocess.STDOUT,
                    env=command_env, cwd=cwd, pass_fds=(trace.fileno(),) if trace else (),start_new_session=True)
                processes.append(proc);started=time.monotonic();heartbeat=started
                while proc.poll() is None:
                    elapsed=time.monotonic()-started
                    if elapsed>timeout:
                        os.killpg(proc.pid,signal.SIGKILL);proc.wait(timeout=10)
                        report[label]={'timeout_seconds':timeout,'private_log_sha256':sha(log),'source_function':installer_substage(trace_path)}
                        if trace_path.is_file():report[label]['private_trace_sha256']=sha(trace_path)
                        save()
                        raise RuntimeError(label+' timed out after '+str(timeout)+' seconds')
                    if time.monotonic()-heartbeat>=15:
                        installer_heartbeat(label,log,elapsed);heartbeat=time.monotonic()
                    time.sleep(.2)
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
        report[label] = {'exit_code': proc.returncode, 'duration_seconds':int(time.monotonic()-started), 'private_log_sha256': sha(log),
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

    def error_source_locations(raw):
        # Export source locations, never unknown runtime error text or values.
        matches=[]
        for source in Path('internal').rglob('*.go'):
            if source.name.endswith('_test.go'):continue
            content=source.read_text()
            for found in re.finditer(r'"((?:\\.|[^"\\])*)"',content):
                try:literal=json.loads('"'+found[1]+'"')
                except ValueError:continue
                prefix=literal.split('%',1)[0]
                if len(prefix)>=15 and prefix in raw:
                    matches.append(str(source)+':'+str(content.count('\n',0,found.start())+1))
        return matches[:12]

    def lease_refusal(image, label, expected=None, allow_history_progress=False):
        before_bytes = CORE.read_bytes()
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
        expected_errors=[expected] if isinstance(expected,str) else (expected or [])
        matched=next((message for message in expected_errors if message in raw),None)
        if proc.returncode == 0 or 'READY ' in raw or (expected_errors and not matched):
            selected=[]
            for line in raw.splitlines():
                if line.startswith(('historical ','Scheduler ','candidate ','unreviewed ','reviewed ','Unsupported ','System-task ','Cronie ','Packaged ','PAM ','NSS ','invalid ','unsupported ','unsafe ','stat ','lstat ','open ','inspect ','failed ','resolve ','verify ','The saved Cronie provider ','Waiting ','guarded ')) and not re.search(r'nonce|token|password|credential|secret|authorization',line,re.I):
                    selected.append(re.sub(r'[0-9a-f]{8}-[0-9a-f-]{27}|[0-9a-f]{32,}','[identity-redacted]',line)[:500])
            report[label]={'exit_code':proc.returncode,'expected_error_matched':None,
                'selected_diagnostics':selected[-5:],'source_error_locations':error_source_locations(raw),'private_log_sha256':sha(log)};save()
            raise RuntimeError(label + ' did not reproduce expected refusal')
        unchanged = sha(CORE) == before
        if not unchanged:
            previous, following = json.loads(before_bytes), state()
            # Live old helpers can finish real invocations while exclusive
            # admission is being obtained. Keep both observations; do not
            # attribute that ordinary progress to the refusing candidate.
            if not allow_history_progress or {k:v for k,v in previous.items() if k!='invocations'} != {k:v for k,v in following.items() if k!='invocations'} or not history_retained(previous,following):
                raise RuntimeError(label + ' changed or deleted original registration/history authority')
        report[label] = {'refused': True, 'original_task_bytes_unchanged': unchanged,
            'registrations_unchanged': True, 'original_invocations_or_exact_history_receipts_retained': True,
            'before_task_sha256': hashlib.sha256(before_bytes).hexdigest(), 'after_task_sha256': sha(CORE),
            'exact_expected_error': expected is not None, 'expected_error_matched': matched, 'private_log_sha256': sha(log)}
        save()

    def lease_admission(candidate,label):
        stage(label)
        before=sha(CORE);log=private/(label+'.log')
        with log.open('xb') as stream:
            log.chmod(0o600)
            probe=subprocess.Popen([str(candidate),'scheduler-installer-lease',str(os.getpid())],env=env,
                stdout=stream,stderr=subprocess.STDOUT,start_new_session=True)
            processes.append(probe)
            def lease_ready():
                raw=log.read_text(errors='replace')
                found=re.search(r'^READY ([0-9a-f-]{36})$',raw,re.M)
                if found:return found[1]
                if probe.poll() is not None:
                    selected=[line for line in raw.splitlines() if line.startswith(('historical ','Scheduler ','candidate ','unreviewed ','reviewed ','Cronie ','Packaged ','PAM ','NSS ','invalid ','unsupported ','unsafe ','stat ','lstat ','open ','inspect ','failed ','resolve ','verify ','The saved Cronie provider ','Waiting ','guarded ')) and not re.search(r'nonce|token|password|secret',line,re.I)]
                    report[label]={'refused':True,'selected_diagnostics':selected[-3:],'source_error_locations':error_source_locations(raw),'private_log_sha256':sha(log)};save()
                    raise RuntimeError('Candidate safe lease probe refused; see selected diagnostics')
            lease=wait_for(lease_ready,45,label+'-readiness')
            command(label+'-release',[str(candidate),'scheduler-installer-release',lease],timeout=15)
            probe.wait(timeout=15)
        if sha(CORE)!=before:raise RuntimeError('Lease probe changed original task authority')
        report[label]={'admitted':True,'released':True,'original_task_bytes_unchanged':True,'private_log_sha256':sha(log)}

    try:
        if scenario=='node-full':
            stage('prepare-known-hosted-parent-metadata')
            prepared_roots=PreparedRoots(private)
            report['ci_parent_metadata_before']=prepared_roots.before
            report['ci_parent_metadata_prepared']=prepared_roots.prepared
            prepared_roots.prepare()
            save()
        # Stock-profile preparation applies only to this disposable hosted image.
        for name in ['/usr', '/usr/share', '/usr/share/keyrings', '/usr/local', '/usr/local/bin',
                     '/usr/local/sbin', '/opt', '/etc', '/etc/apt', '/etc/apt/sources.list.d', '/etc/apt/preferences.d']:
            path = Path(name)
            if path.is_dir():
                info = path.lstat()
                if info.st_uid != 0 or stat.S_ISLNK(info.st_mode):
                    raise RuntimeError('Unsafe system fixture ancestry')
                modes.append((path, stat.S_IMODE(info.st_mode)))
                path.chmod(stat.S_IMODE(info.st_mode) & ~0o022)
        for unit in ['apt-daily.timer', 'apt-daily-upgrade.timer']:
            timers[unit] = subprocess.run(['systemctl', 'is-active', '--quiet', unit],timeout=10).returncode == 0
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
        stage('prove-and-install-authenticated-historical-tuple')
        apt_work=Path(tempfile.mkdtemp(prefix='fluxo-reviewed-apt-',dir='/var/tmp'));apt_work.chmod(0o755)
        report['cleanup_inventory']['owned_historical_package_cache']=str(apt_work)
        apt_details={};report['reviewed_apt']=apt_details
        def historical_command(label,args,timeout,cwd=None):
            return command('reviewed-apt-'+label,args,timeout=timeout,cwd=cwd)[1]
        reviewed_apt=ReviewedAPT(apt_work,historical_command,apt_details)
        reviewed_apt.prove();reviewed_apt.install()
        command('vendor-pam-archive',['apt-get',*reviewed_apt.opts,'download','libpam-modules=1.5.3-5ubuntu5.7'],120,
            cwd=private)
        archives=list(private.glob('libpam-modules_*_amd64.deb'))
        if len(archives)!=1:raise RuntimeError('Ambiguous authenticated PAM archive')
        payload=subprocess.check_output(['dpkg-deb','--fsys-tarfile',str(archives[0])],env=env,timeout=30)
        with tarfile.open(fileobj=io.BytesIO(payload),mode='r:') as archive:
            member=archive.getmember('./etc/security/limits.conf')
            if not member.isfile() or member.size>1<<20:raise RuntimeError('Unexpected vendor limits input')
            stock=archive.extractfile(member).read()
        if hashlib.sha256(stock).hexdigest()!='0adf64deecea56e9ed5c2229db357900218b10b76f7e69830c9d209347896206':
            raise RuntimeError('Vendor limits differs from the published qualification tuple')
        report['limits_before_sha256']=sha('/etc/security/limits.conf')
        replace('/etc/security/limits.conf',stock)
        report['limits_vendor_archive_sha256']=sha(archives[0])
        report['limits_after_sha256']=sha('/etc/security/limits.conf')
        fallback=Path('/etc/pam.d/common-session');fallback_raw=fallback.read_bytes()
        suffix=b'session required pam_limits.so\n'
        if fallback_raw.endswith(suffix):
            replace(fallback,fallback_raw[:-len(suffix)])
        # Hosted multilib/toolchain loader declarations are outside the
        # published amd64 contract. Preserve them before creating any old
        # authority, and restore the original cache after fixture teardown.
        allowed_loader_dirs={'/usr/local/lib','/usr/local/lib/x86_64-linux-gnu',
            '/lib/x86_64-linux-gnu','/usr/lib/x86_64-linux-gnu','/lib','/usr/lib'}
        report['ci_loader_declarations_archived']=[]
        for path in sorted(Path('/etc/ld.so.conf.d').glob('*.conf')):
            info=path.lstat()
            if not stat.S_ISREG(info.st_mode) or info.st_uid!=0 or info.st_mode&0o022 or info.st_size>1<<20:
                raise RuntimeError('Untrusted hosted loader declaration')
            active=[line.split('#',1)[0].strip() for line in path.read_text().splitlines()]
            active=[line for line in active if line]
            reviewed_fakeroot=(path.name=='fakeroot-x86_64-linux-gnu.conf' and active==['/usr/lib/x86_64-linux-gnu/libfakeroot'])
            if reviewed_fakeroot or all(line in allowed_loader_dirs for line in active):continue
            digest=sha(path)
            archived=private/('ci-loader-'+path.name)
            path.rename(archived);backups.append((path,archived))
            report['ci_loader_declarations_archived'].append({'name':path.name,'sha256':digest})
        command('stock-amd64-loader-cache',['ldconfig'])
        command('cronie-install', ['apt-get', 'install', '-y', 'cronie=1.7.1-1'], 240)
        command('cronie-start', ['systemctl', 'start', 'cronie.service'])
        # Third-party hosted Python packages are not part of the supported vendor profile.
        local = Path('/usr/local/lib/python3.12/dist-packages')
        if local.is_dir() and any(local.iterdir()):
            archived = private / 'python-local.original'; local.rename(archived); local.mkdir(mode=0o755)
            backups.append((local, archived))
        # The hosted image has newer global JS manager shims, unlike the
        # Fluxo-managed predecessor being reproduced. Preserve those CI shims
        # intact; the public installer must install/verify its own pinned tools.
        report['ci_tool_shims_archived'] = []
        if scenario == 'node-full':
            for name in ['corepack', 'pnpm', 'pnpx', 'yarn', 'yarnpkg', 'pm2', 'bun', 'bunx']:
                path = Path('/usr/local/bin') / name
                if not path.exists() and not path.is_symlink():
                    continue
                info=path.lstat()
                if info.st_uid!=0 or not (stat.S_ISREG(info.st_mode) or stat.S_ISLNK(info.st_mode)):
                    raise RuntimeError('Untrusted hosted JS shim metadata')
                archived=private/('ci-shim-'+name)
                path.rename(archived);backups.append((path,archived))
                report['ci_tool_shims_archived'].append(name)
        checkpoint('fixture-prepared')
        if scenario=='node-full':
            # The hosted old install leaves a fresh bin group-writable.
            # Represent an existing secure managed prefix:
            # create only empty0755 directories before the unchanged public
            # producer installs all actual pinned tools and verifies them.
            prefix=Path('/opt/fluxo/node-toolchain')
            stage('prepare-secure-empty-node-prefix')
            if prefix.parent.exists():raise RuntimeError('Refuse preexisting managed Node fixture ancestry')
            (prefix/'bin').mkdir(parents=True,mode=0o755)
            report['node_prefix_inherited_metadata']=[]
            for directory in [prefix.parent,prefix,prefix/'bin']:
                info=directory.lstat()
                if not stat.S_ISDIR(info.st_mode):raise RuntimeError('New Node prefix component is not a directory')
                report['node_prefix_inherited_metadata'].append({'path':str(directory),'type':'directory','uid':info.st_uid,'gid':info.st_gid,'mode':oct(stat.S_IMODE(info.st_mode)),'nlink':info.st_nlink,'access_acl_sha256':acl_digest(directory,'access'),'default_acl_sha256':acl_digest(directory,'default')})
                # Default ACLs on hosted /opt bypass umask for new directories.
                # Remove ACLs only from our new empty prefix, never from /opt.
                for name in ('access','default'):
                    try:os.removexattr(directory,'system.posix_acl_'+name)
                    except OSError as error:
                        if error.errno not in (errno.ENODATA,errno.ENOTSUP):raise
                os.chown(directory,0,0);directory.chmod(0o755)
                if directory.stat().st_uid!=0 or directory.stat().st_gid!=0 or stat.S_IMODE(directory.stat().st_mode)!=0o755:
                    raise RuntimeError('Unsafe empty Node prefix fixture metadata')
            report['secure_empty_node_prefix_precreated']=True
            report['new_prefix_acls_removed']=all(acl_digest(path,name) is None for path in (prefix.parent,prefix,prefix/'bin') for name in ('access','default'))
            if not report['new_prefix_acls_removed']:raise RuntimeError('New Node prefix ACL correction failed')
        installer = private / 'v040-install.sh'
        command('download-old-installer', ['curl', '-fsSL', '--retry', '3', '-o', str(installer),
            'https://raw.githubusercontent.com/FabioTECH1/fluxo/v0.4.40/install.sh'])
        if sha(installer) != OLD_INSTALLER:
            raise RuntimeError('Old installer differs from exact published source')
        report['old_installer_sha256'] = sha(installer)
        flags = ['--db-engine=mysql', '--redis', '--node' if scenario == 'node-full' else '--no-node', '--no-python', '--no-harden-ssh']
        _, old_log = command('published-v040-install', ['bash', '-x', str(installer), *flags], 600,
            extra={'FLUXO_VERSION': 'v0.4.40'})
        if 'Fluxo release provenance verified' not in old_log:
            raise RuntimeError('Old public delivery provenance was not verified')
        checkpoint('old-installed')
        old_binary = private / 'fluxo-v040'; shutil.copy2(PANEL, old_binary)
        if subprocess.check_output([str(old_binary), '--version'], env=env,timeout=10).decode().strip() != 'fluxo version 0.4.40':
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
        stage('old-genuine-registration')
        old_core = wait_for(lambda: state() if str(job) in state().get('entries', {}) else None,
            40, 'v0.4.40 guarded registration')
        if old_core['schema'] != 1:
            raise RuntimeError('Old producer did not emit schema1')
        stage('old-genuine-cron-acknowledgement')
        wait_for(lambda: state()['entries'][str(job)]['received'], 150, 'genuine old cron acknowledgement')
        def completed_job():
            return [v for v in state().get('invocations', {}).values()
                    if v['registration']['contract']['id'] == job and v['status'] == 'completed']
        stage('old-genuine-scheduled-completion')
        wait_for(completed_job, 180, 'actual old Cronie-scheduled task completion')
        command('old-panel-stop', ['systemctl', 'stop', 'fluxo.service'])
        stage('all-genuine-original-cron-acknowledgements')
        def originals_acknowledged():
            core=state()
            summary=[{'id':int(key),'kind':entry['contract']['kind'],'enabled':entry['enabled'],
                'published':entry['published'],'received':entry['received']}
                for key,entry in sorted(core.get('entries',{}).items())]
            if report.get('old_registration_receipts')!=summary:
                report['old_registration_receipts']=summary;save()
            return bool(summary) and all(entry['received'] and entry['published'] for entry in summary if entry['enabled'])
        wait_for(originals_acknowledged,180,'all genuine original Cronie acknowledgements')
        # Retain settled authority from the actual old producer, never infer
        # completion from the daemon PID or repair its journals.
        stage('initial-old-recorded-work-natural-idle')
        def initial_work_settled():
            for invocation in state().get('invocations',{}).values():
                if invocation['status'] not in ('completed','blocked','interrupted'):return False
                group=Path('/sys/fs/cgroup/system.slice')/invocation['unit']
                if group.exists() and int((group/'pids.current').read_text())!=0:return False
            return True
        wait_for(initial_work_settled,60,'initial real old work naturally settled')
        stage('old-cronie-natural-idle')
        wait_for(lambda: identity()['pids.current'] == 1, 45, 'original Cronie natural idle')
        stage('preserve-original-core-and-helpers')
        original_bytes = CORE.read_bytes(); original = json.loads(original_bytes)
        initial_history=immutable_history()
        (private / 'original-task-core.json').write_bytes(original_bytes)
        old_helpers = {r['helper']: sha(r['helper']) for r in original['entries'].values()}
        old_triggers = {f'/etc/cron.d/fluxo-cron-{key}': Path(f'/etc/cron.d/fluxo-cron-{key}').read_bytes() for key,r in original['entries'].items() if r['enabled']}
        checkpoint('old-receipts-preserved')
        stage('stage-exact-candidate')
        candidate = private / 'fluxo-candidate'
        shutil.copy2(Path(os.environ['FLUXO_NATIVE_BUILD']) / 'fluxo', candidate)
        candidate.chmod(0o700)
        report['candidate_sha256'] = sha(candidate)
        if scenario!='uncertain':lease_admission(candidate,'candidate-original-generation-lease')
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
        stage('genuine-cronie-surviving-child')
        wait_for(started.exists, 90, 'real Cronie surviving-child fixture')
        survivor.unlink()
        command('generation-with-live-child', ['systemctl', 'restart', 'cronie.service'])
        if identity()['pids.current'] <= 1:
            raise RuntimeError('Surviving-child fixture was not live')
        lease_refusal(candidate, 'surviving-child-refused', [
            'historical installer retirement requires a replaced same-boot Cronie generation under its unchanged contract',
            'historical task execution authority is active or uncertain',
            'historical Cronie execution hierarchy still exists',
            'The saved Cronie provider is not the sole active scheduling engine. Existing schedules and history are preserved.'], allow_history_progress=True)
        # Separate the unknown surviving Cronie descendant from ordinary old
        # guard progress: all recorded authorities must settle while this real
        # unjournaled child remains alive. The retained inode must still refuse.
        stage('recorded-work-settled-with-surviving-child')
        def recorded_work_settled():
            for invocation in state().get('invocations',{}).values():
                if invocation['status'] not in ('completed','blocked','interrupted'):return False
                group=Path('/sys/fs/cgroup/system.slice')/invocation['unit']
                if group.exists() and int((group/'pids.current').read_text())!=0:return False
            return True
        wait_for(recorded_work_settled,60,'recorded-work-settled-with-surviving-child')
        if identity()['pids.current']<=1 or release.exists():raise RuntimeError('Unknown Cronie descendant did not remain live')
        lease_refusal(candidate,'surviving-unrecorded-child-refused',[
            'historical installer retirement requires a replaced same-boot Cronie generation under its unchanged contract',
            'historical Cronie execution hierarchy still exists',
            'The saved Cronie provider is not the sole active scheduling engine. Existing schedules and history are preserved.'],allow_history_progress=True)
        report['surviving-unrecorded-child-refused']['all_recorded_work_settled']=True
        checkpoint('live-child-refused')
        stage('surviving-child-natural-retirement')
        release.touch()
        wait_for(lambda: identity()['pids.current'] == 1, 45, 'surviving child natural completion')
        command('old-empty-domain-stop', ['systemctl', 'stop', 'cronie.service'])
        wait_for(lambda: not Path('/sys/fs/cgroup/system.slice/cronie.service').exists(), 20, 'old empty domain disappearance')
        command('new-empty-generation-start', ['systemctl', 'start', 'cronie.service'])
        current = identity()
        if current['boot'] != saved['boot'] or current['group_inode'] == saved['group_inode']:
            raise RuntimeError('Expected a genuine same-boot replaced domain')
        report['replacement_identity'] = current
        # The recurring old job may have submitted just before the daemon
        # replacement. Let its genuine producer record completion/blockage and
        # require every known domain empty before fixing the archive baseline.
        def settled_old_authority():
            j=state()
            for invocation in j.get('invocations',{}).values():
                if invocation['status'] not in ('completed','blocked','interrupted'):
                    return False
                group=Path('/sys/fs/cgroup/system.slice')/invocation['unit']
                if group.exists() and int((group/'pids.current').read_text())!=0:
                    return False
            return True
        stage('settled-old-authority')
        wait_for(settled_old_authority,90,'all real old task outcomes and descendant domains settled')
        settled_bytes=CORE.read_bytes()
        if {k:v for k,v in json.loads(settled_bytes).items() if k!='invocations'} != {k:v for k,v in original.items() if k!='invocations'} or not history_retained(original,json.loads(settled_bytes)):
            raise RuntimeError('Old registration or history authority disappeared before conversion')
        report['old_producer']['pre_generation_task_sha256']=hashlib.sha256(original_bytes).hexdigest()
        report['old_producer']['task_sha256']=hashlib.sha256(settled_bytes).hexdigest()
        report['old_producer']['all_old_outcomes_settled']=True
        if any(not Path(path).is_file() or sha(path)!=digest for path,digest in initial_history.items()):
            raise RuntimeError('An original immutable history receipt changed')
        original_bytes=settled_bytes;original=json.loads(settled_bytes)
        (private/'settled-task-core.json').write_bytes(original_bytes)
        original_history=immutable_history()
        report['old_producer']['immutable_history_receipts']=len(original_history)
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
        checkpoint('public47-reproduced')
        if scenario=='probe':
            lease_admission(candidate,'candidate-retired-generation-lease')
            checkpoint('candidate-lease-admitted');report['passed']=True
            return 0
        command('old-panel-start', ['systemctl', 'start', 'fluxo.service'])
        wait_for(lambda: api('GET', '/version')['version'] == '0.4.40', 45, 'old panel health')
        env['FLUXO_LOCAL_BINARY_SHA256'] = sha(candidate)
        args = ['bash', 'install.sh', '--local-binary=' + str(candidate), *flags]
        # Crash only this fixture installer group after its exact activation
        # operation is durable. A later ordinary installer must adopt that image.
        stage('normal-installer-to-durable-operation')
        crash_log = private / 'interrupted-normal-installer.log'
        with crash_log.open('xb') as stream:
            crash_log.chmod(0o600)
            proc = subprocess.Popen(args, env=env, stdin=subprocess.DEVNULL, stdout=stream,
                stderr=subprocess.STDOUT, start_new_session=True)
            processes.append(proc)
            end = time.monotonic() + 360
            interrupted = False;heartbeat=time.monotonic();installer_began=heartbeat
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
                if time.monotonic()-heartbeat>=15:
                    installer_heartbeat('normal-installer-to-durable-operation',crash_log,time.monotonic()-installer_began);heartbeat=time.monotonic()
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
                lease_context=[]
                clean=[re.sub(r'\x1b\[[0-9;]*m','',line) for line in raw.splitlines()]
                for i,line in enumerate(clean):
                    if 'Could not obtain a safe PHP/Scheduler upgrade lease' in line:
                        for cause in clean[max(0,i-3):i]:
                            if not re.search(r'password|token|credential|secret|authorization|nonce|ticket',cause,re.I) and not cause.lstrip().startswith(('{','[')):
                                lease_context.append(re.sub(r'[0-9a-f]{32,}|[0-9a-f]{8}-[0-9a-f-]{27}','[identity-redacted]',cause)[:500])
                report['first_candidate_install']['lease_error_context']=lease_context
                task=state()
                counts={}
                for invocation in task.get('invocations',{}).values():
                    status=invocation.get('status','unknown');counts[status]=counts.get(status,0)+1
                panel_group=Path('/sys/fs/cgroup/system.slice/fluxo.service/pids.current')
                report['first_candidate_install']['identity_snapshot']={
                    'cronie':identity(),'panel_pids.current':int(panel_group.read_text()) if panel_group.exists() else None,
                    'task_schema':task.get('schema'),'task_status_counts':counts,
                    'service_manager_cgroup_namespace':os.path.samestat(os.stat('/proc/self/ns/cgroup'),os.stat('/proc/1/ns/cgroup'))}
                raise RuntimeError('Did not observe the durable interruption boundary')
        checkpoint('candidate-interrupted')
        operation = state(INTENT)['operation_id']
        command('normal-installer-recovery', args, 360)
        checkpoint('installer-recovered')
        stage('verify-exact-original-archive')
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
        for i, change in enumerate(plan['files']):
            if change['path'] in old_triggers and (archive / f'before-{i}').read_bytes() != old_triggers[change['path']]:
                raise RuntimeError('Original cron trigger was not retained exactly')
        if any(not Path(path).is_file() or sha(path)!=digest for path,digest in original_history.items()):
            raise RuntimeError('Immutable original history receipt was not retained exactly')
        if any(sha(path) != digest for path, digest in old_helpers.items()):
            raise RuntimeError('Original helpers were not retained')
        new = state()
        if new['schema'] != 3:
            raise RuntimeError('Expected current task format')
        for key, r in original['entries'].items():
            target = new['entries'][key]
            if target['identity'] != r['identity'] or target['nonce'] == r['nonce'] or target['helper'] == r['helper']:
                raise RuntimeError('Conversion lost identity or reused old helper/nonce')
        stage('genuine-new-cron-acknowledgements')
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
        stage('continued-genuine-cron-completion')
        wait_for(continued, 180, 'continued genuine Cronie-scheduled job completion')
        report['result'] = {'task_schema': 3, 'operation_recovered_exactly': True,
            'old_core_archived_exactly': True, 'old_immutable_history_retained_exactly': True, 'old_triggers_archived_exactly': True, 'old_helpers_retained': True,
            'fresh_nonces': True, 'genuine_new_acknowledgements': True,
            'manual_ack_refused': True, 'continued_cron_execution': True,
            'live_acme_tested': False, 'archive_kind': plan['kind']}
        report['passed'] = True
    except Exception as error:
        report['failed_stage']=report.get('stage')
        report['error'] = str(error) if isinstance(error, (RuntimeError, AssertionError)) else type(error).__name__
        save()
    finally:
        cleanup_errors=[]
        def cleanup(label,action):
            stage(label)
            try:action()
            except Exception as error:
                cleanup_errors.append({'stage':label,'error':type(error).__name__});report['cleanup_errors']=cleanup_errors;save()
        cleanup('cleanup-release-survivor',lambda:release.touch(exist_ok=True))
        cleanup('cleanup-remove-own-trigger',lambda:survivor.unlink(missing_ok=True))
        for proc in processes:
            if proc.poll() is None:
                def terminate(proc=proc):
                    os.killpg(proc.pid,signal.SIGKILL);proc.wait(timeout=10)
                cleanup('cleanup-own-subprocess',terminate)
        def stop_own_panel():
            loaded=subprocess.run(['systemctl','show','fluxo.service','-p','LoadState','--value'],capture_output=True,text=True,timeout=5)
            if loaded.stdout.strip()=='not-found':return
            loaded.check_returncode()
            subprocess.run(['systemctl','stop','fluxo.service'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=20,check=True)
        cleanup('cleanup-stop-own-panel',stop_own_panel)
        for path,backup in reversed(backups):
            def restore(path=path,backup=backup):
                if path.is_dir():shutil.rmtree(path)
                elif path.exists() or path.is_symlink():path.unlink()
                if backup is not None:backup.rename(path)
            cleanup('cleanup-restore-fixture-file',restore)
        cleanup('cleanup-restore-loader-cache',lambda:subprocess.run(['ldconfig'],env=env,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=20,check=True))
        for path,mode in reversed(modes):cleanup('cleanup-restore-mode',lambda path=path,mode=mode:path.chmod(mode))
        for timer,active in timers.items():
            if active:cleanup('cleanup-restore-timer',lambda timer=timer:subprocess.run(['systemctl','start',timer],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=10,check=True))
        if prepared_roots is not None:
            cleanup('cleanup-restore-original-known-parent-metadata',prepared_roots.restore)
            report['ci_parent_metadata_restored']=prepared_roots.restored
            report['ci_original_parent_metadata_preserved']=prepared_roots.preserved
        report['fixtures_restored']=not cleanup_errors
        report['finished']=True
        if cleanup_errors:report['passed']=False
        os.umask(previous_umask)
        save()
        print(json.dumps({'passed': report['passed'], 'error': report.get('error'), 'report': str(output / 'report.json')}))
    return 0 if report['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
