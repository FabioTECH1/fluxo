#!/usr/bin/python3
"""Private-VM actual-candidate observer. No helper, selector or provider override.

Creates a unique Laravel-typed site and Scheduler cron through the installed panel HTTP
API. The benign artisan-named PHP file proves the mechanism, not Laravel. All
execution/receipt/seal writes belong to the installed candidate. Keep this
script, its protected evidence directory and the created site after acceptance.
Only its own cron is retired, after natural completion. No forced process kill.
"""
import argparse
import hashlib
import json
import os
import pathlib
import pwd
import re
import socket
import sqlite3
import ssl
import stat
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

HOSTS = {
    'fluxo-bridge-jammy-20261101': 'systemd-persistent-v1',
    'fluxo-bridge-prod-20261101': 'systemd-cgroup-exit-v1',
    'fluxo-bridge-noble-final-20261102': 'systemd-cgroup-exit-v1',
    'fluxo-bridge-resolute-20261101': 'systemd-cgroup-exit-v1',
    'fluxo-bridge-jammy-final-20261117': 'systemd-persistent-v1',
    'fluxo-bridge-resolute-final-20261117': 'systemd-cgroup-exit-v1',
    'fluxo-bridge-jammy-fresh-eb4-20261117': 'systemd-persistent-v1',
    'fluxo-bridge-resolute-fresh-eb4-20261117': 'systemd-cgroup-exit-v1',
}
CANDIDATE = '/usr/local/bin/fluxo'
CORE = '/var/lib/fluxo-scheduler/state.json'
CONTROL_ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'HOME': '/root',
               'USER': 'root', 'LOGNAME': 'root', 'LANG': 'C.UTF-8',
               'FLUXO_ENV': 'prod', 'FLUXO_DATA_DIR': '/var/lib/fluxo'}
STAGE = 'guard'


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def protected_read(path, limit=32*1024*1024):
    path = pathlib.Path(path)
    for parent in path.parents:
        s = parent.lstat()
        require(stat.S_ISDIR(s.st_mode) and s.st_uid == 0 and not s.st_mode & 0o022,
                'unsafe protected ancestor: '+str(parent))
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK)
    try:
        s = os.fstat(fd)
        require(stat.S_ISREG(s.st_mode) and s.st_uid == 0 and s.st_nlink == 1
                and not s.st_mode & 0o022 and s.st_size <= limit,
                'unsafe or oversized protected file: '+str(path))
        data = os.read(fd, limit+1)
        after = os.fstat(fd)
        require(len(data) <= limit and (s.st_dev, s.st_ino, s.st_size, s.st_mtime_ns, s.st_ctime_ns)
                == (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns),
                'protected file changed during read')
        return data
    finally:
        os.close(fd)


def payload_read(path, owner, limit=1024*1024):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        require(stat.S_ISREG(before.st_mode) and before.st_uid == owner and before.st_nlink == 1
                and before.st_size <= limit, 'unsafe payload observation file')
        raw = os.read(fd, limit+1)
        after = os.fstat(fd)
        require(len(raw) <= limit and before.st_size == after.st_size and before.st_mtime_ns == after.st_mtime_ns,
                'payload observation changed while read')
        return raw
    finally:
        os.close(fd)


def release_payload(directory, expected, run_id):
    fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        actual = os.fstat(fd)
        require((actual.st_dev, actual.st_ino) == expected, 'own handshake directory was replaced')
        try:
            release = os.open('release', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o644, dir_fd=fd)
        except FileExistsError:
            release = os.open('release', os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=fd)
            try:
                st = os.fstat(release)
                require(stat.S_ISREG(st.st_mode) and st.st_uid == 0 and st.st_nlink == 1
                        and os.read(release, 100).decode() == run_id, 'release handshake differs; refusing overwrite')
            finally:
                os.close(release)
        else:
            try:
                os.write(release, run_id.encode()); os.fsync(release)
            finally:
                os.close(release)
            os.fsync(fd)
    finally:
        os.close(fd)


def core():
    return json.loads(protected_read(CORE))


def command(args, timeout=20):
    return subprocess.run(args, env=CONTROL_ENV, stdin=subprocess.DEVNULL,
                          capture_output=True, timeout=timeout)


def show(unit):
    props = 'LoadState,ActiveState,SubState,MainPID,ControlPID,InvocationID,ControlGroup,ExecStart,CapabilityBoundingSet,NoNewPrivileges,Type,ExitType,KillMode,RemainAfterExit'
    out = command(['/usr/bin/systemctl', 'show', unit, '--property='+props])
    values = dict(line.split('=', 1) for line in out.stdout.decode().splitlines() if '=' in line)
    require(out.returncode == 0 or out.returncode == 4 and values.get('LoadState') == 'not-found', 'PID1 unit inspection failed')
    return values


def status(pid, tid=None):
    path = f'/proc/{pid}/status' if tid is None else f'/proc/{pid}/task/{tid}/status'
    return dict(line.split(':', 1) for line in pathlib.Path(path).read_text().splitlines() if ':' in line)


def credentials(s, worker):
    expected = 0xc4 if worker else 0
    for key in ['CapPrm', 'CapEff']:
        require(int(s[key].strip(), 16) == expected, 'wrong '+key)
    for key in ['CapInh', 'CapAmb']:
        require(int(s[key].strip(), 16) == 0, 'wrong '+key)
    require(int(s['CapBnd'].strip(), 16) == 0xc4 and s['NoNewPrivs'].strip() == '1',
            'bounding capabilities or NoNewPrivs differ')
    account = pwd.getpwnam('fluxo')
    uid, gid = (0, 0) if worker else (account.pw_uid, account.pw_gid)
    require([int(x) for x in s['Uid'].split()] == [uid]*4, 'wrong UIDs')
    require([int(x) for x in s['Gid'].split()] == [gid]*4, 'wrong GIDs')


def start_key(pid):
    raw = pathlib.Path(f'/proc/{pid}/stat').read_text()
    return raw[raw.rfind(')')+2:].split()[19]


def hash_file(path):
    h = hashlib.sha256()
    with open(path, 'rb') as source:
        for block in iter(lambda: source.read(1024*1024), b''):
            h.update(block)
    return h.hexdigest()


def wait_for(test, timeout, description):
    until = time.monotonic()+timeout
    while time.monotonic() < until:
        answer = test()
        if answer:
            return answer
        time.sleep(0.2)
    raise RuntimeError('bounded wait failed: '+description)


class API:
    def __init__(self, base):
        parsed = urllib.parse.urlsplit(base)
        require(parsed.scheme in ('https', 'http') and parsed.hostname in ('127.0.0.1', '::1', 'localhost')
                and not parsed.username and not parsed.password and not parsed.query and not parsed.fragment
                and parsed.path in ('', '/'), 'HTTP observer requires an explicit loopback panel URL')
        self.base, self.token = base.rstrip('/'), None
        require(parsed.scheme == 'https', 'installed acceptance requires TLS')
        # Trust this installation's protected certificate, retaining hostname
        # and certificate validation for every request. No ambient proxy.
        context = ssl.create_default_context(cadata=protected_read('/var/lib/fluxo/tls/cert.pem').decode())
        self.open = urllib.request.build_opener(urllib.request.ProxyHandler({}),
                         urllib.request.HTTPSHandler(context=context))

    def request(self, method, path, body=None):
        data = None if body is None else json.dumps(body).encode()
        headers = {'Content-Type': 'application/json'}
        if self.token:
            headers['Authorization'] = 'Bearer '+self.token
        req = urllib.request.Request(self.base+'/api/v1'+path, data=data, headers=headers, method=method)
        try:
            with self.open.open(req, timeout=120) as reply:
                raw = reply.read(1024*1024)
                return None if not raw else json.loads(raw)
        except urllib.error.HTTPError as error:
            # Never print response/payload/headers: login success contains JWT,
            # and provisioning diagnostics may contain configuration material.
            raise RuntimeError(f'HTTP {method} {path} returned status {error.code}') from None

    def login(self, allow_reset, run_id):
        db = sqlite3.connect('file:/var/lib/fluxo/fluxo.db?mode=ro', uri=True)
        try:
            row = db.execute('SELECT username FROM users ORDER BY id LIMIT 1').fetchone()
        finally:
            db.close()
        require(row is not None, 'installed panel has no initialized admin record')
        username = row[0]
        if username == '__bootstrap__':
            username = 'bridge_observer_'+run_id.replace('-', '')[:20]
        try:
            raw = protected_read('/var/lib/fluxo/.fluxo_credentials', 64*1024).decode()
        except FileNotFoundError:
            raw = ''
        tokens = re.findall(r'^Fluxo bootstrap token(?: \(reset\))?:\s*(\S+)\s*$', raw, re.M)
        password = tokens[-1] if tokens else None
        if password:
            try:
                self.token = self.request('POST', '/auth/login', {'username': username, 'password': password})['token']
                return
            except RuntimeError:
                pass
        require(allow_reset, 'no usable retained login secret; explicit --reset-token-if-needed required for this private fixture')
        reset = command([CANDIDATE, '--reset-token'], timeout=60)
        # Reset output and recovery secrets remain only in this process memory.
        require(reset.returncode == 0, 'private fixture admin-token recovery failed')
        tokens = re.findall(r'^New token:\s*(\S+)\s*$', reset.stdout.decode(), re.M)
        require(len(tokens) == 1, 'private fixture recovery did not return exactly one token')
        self.token = self.request('POST', '/auth/login', {'username': username, 'password': tokens[0]})['token']


PHP = r'''<?php
// Mechanism fixture: deliberately not a Laravel application.
$d = __DIR__ . '/.candidate-observer';
if ($argc !== 2 || $argv[1] !== 'schedule:run') { exit(90); }
$f = @fopen($d.'/once', 'x');
if ($f === false) { file_put_contents($d.'/duplicate', (string)getmypid()); exit(97); }
fwrite($f, 'RUN_ID'); fflush($f); fsync($f); fclose($f);
$fd = [];
foreach (glob('/proc/self/fd/*') as $p) { $v = @readlink($p); if ($v !== false) {$fd[basename($p)]=$v;} }
$ready = ['run_id'=>'RUN_ID','pid'=>getmypid(),'status'=>file_get_contents('/proc/self/status'),
          'cgroup'=>trim(file_get_contents('/proc/self/cgroup')),'exe'=>readlink('/proc/self/exe'),'fds'=>(object)$fd];
$f=fopen($d.'/ready.json','x'); fwrite($f,json_encode($ready)); fflush($f); fsync($f); fclose($f);
$until = microtime(true)+180;
while (!is_file($d.'/release') && microtime(true)<$until) {usleep(50000);}
if (!is_file($d.'/release')) {file_put_contents($d.'/timed-out','1');exit(98);}
$f=fopen($d.'/completed','x');fwrite($f,'RUN_ID');fflush($f);fsync($f);fclose($f);
'''


def main():
    global STAGE
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--expected-sha256', required=True)
    ap.add_argument('--url', default='https://127.0.0.1:9595')
    ap.add_argument('--reset-token-if-needed', action='store_true')
    args = ap.parse_args()
    host = socket.gethostname()
    require(os.geteuid() == 0 and host in HOSTS and os.environ.get('FLUXO_PHP_UPDATE_VM_TEST') == '1',
            'root, exact dedicated VM hostname and VM_TEST=1 are required')
    require(re.fullmatch('[0-9a-f]{64}', args.expected_sha256), 'invalid expected image SHA')
    image = hashlib.sha256(protected_read(CANDIDATE, 128*1024*1024)).hexdigest()
    require(image == args.expected_sha256, 'installed candidate differs from expected image')
    caps = command([CANDIDATE, '--execution-capabilities'])
    require(caps.returncode == 0, 'candidate pure capabilities unavailable')
    caps = json.loads(caps.stdout)
    require(caps['scheduler_support'] in (6, 7) and 5 in caps['scheduler_schemas'],
            'candidate does not contain a reviewed schema5 reader')
    floor = json.loads(protected_read('/var/lib/fluxo/execution-readers.json', 64*1024))
    require(floor['phase'] == 'committed' and floor['scheduler_schema'] == 5 and floor['system_task_schema'] == 3
            and floor['scheduler_support'] in (6, 7)
            and floor['scheduler_support'] <= caps['scheduler_support'],
            'reviewed final format activation must complete first')
    compatibility = command([CANDIDATE, 'execution-compatibility', 'inspect'])
    require(compatibility.returncode == 0 and json.loads(compatibility.stdout)['compatible'],
            'installed candidate is incompatible with retained activation evidence')
    panel = show('fluxo.service')
    pid = int(panel['MainPID'])
    require(pid > 1 and hash_file(f'/proc/{pid}/exe') == image, 'running panel is not the actual installed candidate')
    run_id = str(uuid.uuid4())
    evidence = pathlib.Path('/var/lib/fluxo-candidate-cli-'+run_id)
    evidence.mkdir(mode=0o700)
    account = pwd.getpwnam('fluxo')
    domain = 'bridge-cli-'+run_id+'.invalid'
    site_id, cron_id, handshakes, handshake_identity = None, None, None, None
    report = {'run_id': run_id, 'host': host, 'candidate_sha256': image, 'adapter': HOSTS[host],
              'activation_sha256': floor['binary_sha256'], 'activation_id': floor['activation_id'],
              'script_sha256': hash_file(__file__), 'mechanism': 'actual PHP fixture, not full Laravel',
              'evidence': str(evidence), 'passed': False}
    def save(name, value):
        path = evidence/name
        with path.open('w') as out:
            os.chmod(path, 0o600)
            json.dump(value, out, indent=2); out.write('\n'); out.flush(); os.fsync(out.fileno())
    api = API(args.url)
    try:
        STAGE = 'login'
        api.login(args.reset_token_if_needed, run_id)
        STAGE = 'HTTP site creation'
        site = api.request('POST', '/sites', {'domain': domain, 'app_type': 'laravel', 'php_version': '8.4',
                             'web_root': '/public', 'deployment_strategy': 'standard', 'install_composer': False})
        site_id = int(site['id'])
        app = pathlib.Path('/home/fluxo')/domain
        require(site['path'] == str(app) and app.is_dir() and not app.is_symlink(), 'created site path differs')
        require(not (app/'artisan').exists() and not (app/'.candidate-observer').exists(), 'unique fixture path was already occupied')
        handshakes = app/'.candidate-observer'
        # Application paths can be changed by their owner. Perform all fixture
        # creation as that owner, never through a privileged Python writer.
        provision = subprocess.run(['/usr/bin/python3', '-c',
            'import os,pathlib,sys; p=pathlib.Path(sys.argv[1]); '
            '(p/".candidate-observer").mkdir(mode=0o700); '
            'fd=os.open(p/"artisan",os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o644); '
            'out=os.fdopen(fd,"w"); out.write(sys.stdin.read()); out.flush(); os.fsync(out.fileno()); out.close()',
            str(app)], input=PHP.replace('RUN_ID', run_id), text=True,
            user=account.pw_uid, group=account.pw_gid, extra_groups=[],
            env={'PATH':'/usr/bin:/bin','HOME':account.pw_dir,'LANG':'C.UTF-8'},
            capture_output=True, timeout=20)
        require(provision.returncode == 0, 'unprivileged fixture provisioning failed')
        st = handshakes.lstat()
        require(stat.S_ISDIR(st.st_mode) and st.st_uid == account.pw_uid, 'unsafe handshake directory')
        handshake_identity = (st.st_dev, st.st_ino)
        STAGE = 'HTTP Scheduler cron creation'
        name = 'candidate-cli-'+run_id
        api.request('POST', f'/sites/{site_id}/crons', {'name': name, 'expression': '* * * * *',
                      'command': 'artisan schedule:run', 'user': 'fluxo'})
        rows = api.request('GET', f'/sites/{site_id}/crons')
        owned = [row for row in rows if row['name'] == name]
        require(len(owned) == 1, 'HTTP cron response did not identify exactly our registration')
        cron_id = int(owned[0]['id'])
        trigger = pathlib.Path(f'/etc/cron.d/fluxo-cron-{cron_id}')
        trigger_bytes = protected_read(trigger, 1024*1024)
        require(b'/var/lib/fluxo-scheduler/dispatcher scheduler-dispatch ' in trigger_bytes,
                'created cron is not an actual candidate Scheduler wrapper')
        save('registration.json', {'site_id': site_id, 'cron_id': cron_id, 'domain': domain,
                                  'trigger_sha256': hashlib.sha256(trigger_bytes).hexdigest()})
        save('candidate-capabilities.json', caps)
        STAGE = 'genuine cron / candidate bootstrap / actual PHP'
        ready_path = handshakes/'ready.json'
        wait_for(lambda: ready_path.exists(), 190, 'real cron receipt and PHP readiness')
        ready = json.loads(payload_read(ready_path, account.pw_uid))
        require(ready['run_id'] == run_id, 'PHP nonce differs')
        match = re.fullmatch(r'0::/system.slice/(fluxo-scheduler-[0-9a-f-]{36}\.service)', ready['cgroup'])
        require(match is not None, 'PHP did not execute inside its recorded Scheduler domain')
        unit = match[1]
        journal = core()
        v = journal['invocations'].get(unit)
        require(v is not None and v['entry']['id'] == cron_id and v['adapter'] == HOSTS[host]
                and v['execution_contract'] == 'scheduler-bootstrap-drop-exec-v1' and v['started'],
                'actual invocation lacks the expected saved contract/Started authority')
        seal = v['bootstrap_seal']
        require(seal['phase'] == 'consumed' and seal['entry_identity'] == v['entry']['identity']
                and seal['loader']['nonce'] == v['entry']['loader_nonce'], 'one-use seal is not exactly consumed')
        require(v['helper_sha256'] == image and v['helper'] == '/var/lib/fluxo-scheduler/helpers/'+image
                and hashlib.sha256(protected_read(v['helper'], 128*1024*1024)).hexdigest() == image,
                'execution helper is not the actual candidate image')
        require(hashlib.sha256(protected_read('/var/lib/fluxo-scheduler/dispatcher',128*1024*1024)).hexdigest() == image,
                'cron dispatcher is not the actual candidate image')
        ledger = json.loads(protected_read('/var/lib/fluxo-scheduler/loader-epochs.json'))
        require(not ledger['pending'] and ledger['current'] == seal['loader']['epoch_id'], 'current loader epoch differs from consumed seal')
        epoch = ledger['epochs'][ledger['current']]
        target = epoch['targets'][str(cron_id)]
        require(epoch['phase'] == 'published' and target['received'] and target['binding'] == seal['loader']['binding']
                and target['nonce'] == seal['loader']['nonce'] and target['trigger_hash'] == hashlib.sha256(trigger_bytes).hexdigest(),
                'genuine current provider acknowledgement is missing or differs')
        save('loader-acknowledgement.json', {'epoch_id': epoch['id'], 'provider': epoch['provider'], 'target': target})
        unit_props = show(unit)
        worker = int(unit_props['MainPID'])
        require(worker == seal['pid'] and start_key(worker) == seal['start'], 'worker TGID/start differs from bootstrap seal')
        require(hash_file(f'/proc/{worker}/exe') == image, 'actual worker executable differs')
        threads = {}
        for thread in pathlib.Path(f'/proc/{worker}/task').iterdir():
            s = status(worker, int(thread.name)); credentials(s, True)
            threads[thread.name] = {k: s[k].strip() for k in ['Uid','Gid','CapPrm','CapEff','CapBnd','CapInh','CapAmb','NoNewPrivs']}
        require(threads, 'worker has no observed threads')
        php_status = dict(line.split(':', 1) for line in ready['status'].splitlines() if ':' in line)
        credentials(php_status, False)
        require(ready['exe'] == '/usr/bin/php8.4' and int(ready['pid']) != worker, 'PHP interpreter/process identity differs')
        for value in ready['fds'].values():
            require('fluxo-php-lifecycle.lock' not in value and '/fluxo-scheduler/helpers/' not in value,
                    'application inherited a private lifecycle/image descriptor')
        fd = seal['lease']['fd']
        info = pathlib.Path(f'/proc/{worker}/fdinfo/{fd}').read_text()
        flags = int(re.search(r'^flags:\s*([0-7]+)', info, re.M)[1], 8)
        require(flags & os.O_CLOEXEC and 'FLOCK' in info and 'OFDLCK' in info, 'worker did not restore/retain actual lifecycle OFD')
        require(set(unit_props['CapabilityBoundingSet'].split()) == {'cap_dac_read_search','cap_setgid','cap_setuid','cap_setpcap','cap_sys_ptrace'}
                and unit_props['NoNewPrivileges'] == 'yes', 'configured bootstrap unit is not the reviewed five-cap policy')
        require(unit_props['InvocationID'] == seal['service']['invocation_id'], 'PID1 generation differs from seal')
        group = pathlib.Path('/sys/fs/cgroup/system.slice')/unit
        gs = group.stat()
        require(gs.st_ino == seal['service']['cgroup_inode'] and gs.st_dev == seal['service']['cgroup_device'], 'kernel domain differs from seal')
        save('execution.json', {'invocation': v, 'unit': unit_props, 'worker_threads': threads, 'php': ready})
        STAGE = 'natural completion'
        release_payload(handshakes, handshake_identity, run_id)
        wait_for(lambda: (handshakes/'completed').exists(), 15, 'PHP completion')
        def empty():
            p = show(unit)
            if p.get('MainPID') not in ('0','') or p.get('ControlPID') not in ('0',''):
                return False
            if not group.exists():
                return True
            events = dict(line.split() for line in (group/'cgroup.events').read_text().splitlines())
            return int((group/'pids.current').read_text()) == 0 and events['populated'] == '0'
        wait_for(empty, 20, 'actual hierarchical natural emptiness')
        STAGE = 'retire only own trigger after natural drain'
        current_trigger = protected_read(trigger, 1024*1024)
        require(current_trigger == trigger_bytes, 'own trigger changed unexpectedly; refusing cleanup')
        api.request('DELETE', f'/sites/{site_id}/crons/{cron_id}')
        require(not trigger.exists() and not trigger.is_symlink(), 'HTTP retirement left own trigger')
        # Exact retired cron nonce must not regain candidate dispatch authority.
        late = command([CANDIDATE, 'scheduler-dispatch', str(cron_id), v['entry']['loader_nonce']])
        require(late.returncode != 0, 'retired candidate CLI accepted delayed launch authority')
        save('late-refusal.json', {'returncode': late.returncode, 'no_output_recorded': True})
        STAGE = 'next real minute boundary'
        # Cron may have cached a previously read line. Observe a full next
        # minute; entry/receipt retirement must prevent another application.
        until = time.monotonic()+65
        while time.monotonic() < until:
            require(not (handshakes/'duplicate').exists() and not (handshakes/'timed-out').exists(), 'duplicate/expired PHP launch observed')
            time.sleep(0.2)
        require(payload_read(handshakes/'completed', account.pw_uid).decode() == run_id and empty(), 'completed fixture did not remain naturally empty')
        require(hashlib.sha256(protected_read(CANDIDATE,128*1024*1024)).hexdigest() == image,
                'candidate changed during acceptance')
        report.update(passed=True, site_id=site_id, cron_id=cron_id, unit=unit,
                      actual_candidate_HTTP_and_CLI=True, genuine_cron=True,
                      same_image_dispatcher_bootstrap_worker=True, natural_empty=True,
                      late_registration_launch_refused=True, no_duplicate_application=True)
    except Exception as error:
        report.update(stage=STAGE, error=str(error), site_id=site_id, cron_id=cron_id)
        # Natural release of ONLY our own cooperative payload. Never force-kill,
        # alter existing fixtures or hide a failed acceptance by deleting data.
        if handshakes is not None and handshake_identity is not None:
            release_payload(handshakes, handshake_identity, run_id)
        raise
    finally:
        api.token = None
        save('result.json', report)
        print(json.dumps({'passed': report['passed'], 'stage': STAGE, 'evidence': str(evidence)}), flush=True)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(json.dumps({'passed': False, 'stage': STAGE, 'error': str(error)}), flush=True)
        # Detailed sanitized failure is in protected result.json. Never dump
        # exception locals, credentials, JWT, HTTP response or reset output.
        sys.exit(1)
