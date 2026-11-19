#!/usr/bin/env python3
"""Opt-in functional/resource QA for analytics on the named local Ubuntu VM.

Run only after ui then CGO_ENABLED=0 Go build. Copy candidate and this script
with multipass transfer; invoke as root with FLUXO_PHP_UPDATE_VM_TEST=1.
No existing site is modified. Credentials/raw logs remain in the root-private
output directory. This is candidate evidence, never public-release acceptance.
"""
import argparse
import concurrent.futures
import hashlib
import http.client
import json
import os
from pathlib import Path
import shutil
import sqlite3
import ssl
import statistics
import subprocess
import threading
import time
import traceback

HOST = 'fluxo-php-patch-test'
DATA = Path('/var/lib/fluxo')
BIN = Path('/usr/local/bin/fluxo')
CONF = Path('/etc/nginx')
DOMAINS = ('traffic-v1-native-one.test', 'traffic-v1-native-two.test')


def require(value, message):
    if not value:
        raise RuntimeError(message)


def digest(path):
    h = hashlib.sha256()
    with open(path, 'rb') as stream:
        for block in iter(lambda: stream.read(1 << 20), b''):
            h.update(block)
    return h.hexdigest()


def command(argv, timeout=30):
    proc = subprocess.run(argv, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                          timeout=timeout, text=True)
    require(proc.returncode == 0, 'Command failed: ' + ' '.join(argv[:3]) + ': ' + proc.stdout[-1500:])
    return proc.stdout.strip()


def write_json(path, data):
    path.write_text(json.dumps(data, indent=2, sort_keys=True) + '\n')
    path.chmod(0o600)


class API:
    def __init__(self):
        self.token = ''
        self.tls = ssl._create_unverified_context()  # local loopback fixture certificate only

    def request(self, method, path, body=None, auth=True, expected=(200,)):
        connection = http.client.HTTPSConnection('127.0.0.1', 9595, timeout=35, context=self.tls)
        headers = {'Content-Type': 'application/json'}
        if auth and self.token:
            headers['Authorization'] = 'Bearer ' + self.token
        try:
            connection.request(method, '/api/v1' + path, json.dumps(body) if body is not None else None, headers)
            response = connection.getresponse()
            raw = response.read(2 << 20)
            require(response.status in expected,
                    'API ' + method + ' ' + path + ' returned ' + str(response.status) + ': ' + raw.decode(errors='replace')[:500])
            value = json.loads(raw) if response.headers.get('Content-Type', '').startswith('application/json') else raw.decode()
            return response.status, value
        finally:
            connection.close()

    def await_health(self, seconds=35):
        end = time.monotonic() + seconds
        while time.monotonic() < end:
            try:
                if self.request('GET', '/health', auth=False)[1].get('status') == 'ok':
                    return
            except Exception:
                pass
            time.sleep(.25)
        raise RuntimeError('Panel health deadline reached')

    def login(self):
        with sqlite3.connect('file:' + str(DATA / 'fluxo.db') + '?mode=ro', uri=True) as db:
            users = db.execute("SELECT username FROM users WHERE username!='__bootstrap__'").fetchall()
        require(len(users) == 1, 'Expected one existing fixture administrator')
        raw = (DATA / '.fluxo_credentials').read_text()
        lines = [line.split(':', 1)[1].strip() for line in raw.splitlines() if line.startswith('Fluxo bootstrap token:')]
        require(len(lines) == 1 and lines[0], 'Fixture credential is unavailable')
        self.token = self.request('POST', '/auth/login', {'username': users[0][0], 'password': lines[0]}, auth=False)[1]['token']

    def state(self):
        return self.request('GET', '/traffic-analytics')[1]

    def settled(self, timeout=40):
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            value = self.state()
            if not value['pending']:
                return value
            time.sleep(.25)
        raise RuntimeError('Analytics transition did not settle')

    def toggle(self, site, enabled, settle=True):
        path = '/traffic-analytics' if site is None else '/sites/%d/traffic-analytics' % site
        code, value = self.request('PUT', path, {'enabled': enabled}, expected=(200, 202))
        return self.settled() if settle else (code, value)

    def report(self, site):
        return self.request('GET', '/sites/%d/traffic-analytics?range=today' % site)[1]

    def await_count(self, site, minimum, timeout=100):
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            value = self.report(site)
            if value['totals']['requests'] >= minimum:
                return value
            time.sleep(.5)
        raise RuntimeError('Collector request count deadline reached')


def website(domain, path='/', user_agent='Traffic acceptance browser', referrer=None, accept='text/html'):
    conn = http.client.HTTPConnection('127.0.0.1', 80, timeout=15)
    headers = {'Host': domain, 'User-Agent': user_agent, 'Accept': accept}
    if referrer:
        headers['Referer'] = referrer
    begin = time.monotonic()
    try:
        conn.request('GET', path, headers=headers)
        response = conn.getresponse()
        body = response.read()
        return response.status, len(body), (time.monotonic() - begin) * 1000
    finally:
        conn.close()


def original_inventory():
    result = {'tables': {}, 'files': {}}
    with sqlite3.connect('file:' + str(DATA / 'fluxo.db') + '?mode=ro', uri=True) as db:
        require(db.execute('PRAGMA quick_check').fetchone() == ('ok',), 'Core SQLite check failed')
        for table in ('users', 'sites', 'github_accounts', 'ssh_keys', 'crons', 'commands', 'deployments', 'certificates', 'backup_plans', 'backup_destinations'):
            rows = db.execute('SELECT * FROM ' + table + ' ORDER BY 1').fetchall()
            result['tables'][table] = {str(row[0]): hashlib.sha256(json.dumps(row).encode()).hexdigest() for row in rows}
    for path in (DATA / 'encryption.key', DATA / '.fluxo_credentials', DATA / 'tls/cert.pem', DATA / 'tls/key.pem', Path('/home/fluxo/.ssh/authorized_keys')):
        if path.is_file():
            result['files'][str(path)] = digest(path)
    for directory in ('execution-activations',):
        for path in (DATA / directory).rglob('*'):
            if path.is_file() and not path.is_symlink():
                result['files'][str(path)] = digest(path)
    for directory in ('/var/lib/fluxo-scheduler/helpers', '/var/lib/fluxo-scheduler/history', '/var/lib/fluxo-system-tasks/helpers', '/var/lib/fluxo-system-tasks/history'):
        for path in Path(directory).rglob('*'):
            if path.is_file() and not path.is_symlink():
                result['files'][str(path)] = digest(path)
    return result


def check_preservation(before):
    after = original_inventory()
    require(before['tables'] == after['tables'], 'Original application row changed')
    require(all(after['files'].get(path) == sha for path, sha in before['files'].items()), 'Original key/helper/history/archive file changed')
    return {'application_rows': sum(len(rows) for rows in before['tables'].values()), 'original_files': len(before['files']), 'preserved': True}


def process_metrics(pid):
    try:
        proc = Path('/proc') / str(pid)
        stat = (proc / 'stat').read_text().rsplit(')', 1)[1].split()
        status = dict((line.split(':', 1)[0], line.split(':', 1)[1].strip()) for line in (proc / 'status').read_text().splitlines() if ':' in line)
        io = dict((line.split(':')[0], int(line.split(':')[1])) for line in (proc / 'io').read_text().splitlines())
        return {'cpu_seconds': (int(stat[11]) + int(stat[12])) / os.sysconf('SC_CLK_TCK'), 'rss_bytes': int(status['VmRSS'].split()[0]) * 1024, 'write_bytes': io['write_bytes'], 'read_bytes': io['read_bytes']}
    except (FileNotFoundError, ProcessLookupError, KeyError):
        return None


def service_pids(unit):
    group = Path('/sys/fs/cgroup/system.slice') / unit
    if (group / 'cgroup.procs').exists():
        return {int(pid) for pid in (group / 'cgroup.procs').read_text().split()}
    pid = int(command(['systemctl', 'show', unit, '--property=MainPID', '--value']))
    pids = {pid} if pid else set()
    changed = True
    while changed:
        changed = False
        for entry in Path('/proc').iterdir():
            if not entry.name.isdigit() or int(entry.name) in pids:
                continue
            try:
                ppid = int((entry / 'stat').read_text().rsplit(')', 1)[1].split()[1])
                if ppid in pids:
                    pids.add(int(entry.name))
                    changed = True
            except (FileNotFoundError, ProcessLookupError, ValueError):
                pass
    return pids


def cgroup_metrics(unit):
    group = Path('/sys/fs/cgroup/system.slice') / unit
    cpu = dict(line.split() for line in (group / 'cpu.stat').read_text().splitlines())
    writes = None
    if (group / 'io.stat').exists():
        writes = 0
        for line in (group / 'io.stat').read_text().splitlines():
            values = dict(field.split('=', 1) for field in line.split()[1:])
            writes += int(values.get('wbytes', '0'))
    return {'cpu_seconds': int(cpu['usage_usec']) / 1_000_000, 'write_bytes': writes}


def disk_metrics():
    result = {'database_bytes': 0, 'wal_bytes': 0, 'log_bytes': 0, 'total_bytes': 0, 'files': 0}
    directory = DATA / 'traffic'
    if not directory.exists():
        return result
    for path in directory.rglob('*'):
        if path.is_file() and not path.is_symlink():
            size = path.stat().st_size
            result['files'] += 1
            result['total_bytes'] += size
            if path.name == 'analytics.db':
                result['database_bytes'] += size
            elif path.name.endswith('-wal'):
                result['wal_bytes'] += size
            elif path.name.endswith('.jsonl') or path.name.endswith('.jsonl.1'):
                result['log_bytes'] += size
    return result


def phase(label, domain, duration=35, requests=0, unique=False, bot=False):
    groups = {'panel': service_pids('fluxo.service'), 'nginx': service_pids('nginx.service')}
    before = {name: {pid: process_metrics(pid) for pid in pids} for name, pids in groups.items()}
    before_cgroup = {name: cgroup_metrics('fluxo.service' if name == 'panel' else 'nginx.service') for name in groups}
    begin = time.monotonic()
    samples = []
    latencies = []
    errors = []
    lock = threading.Lock()

    def send(i):
        path = '/burst-%d' % i if unique else '/'
        try:
            status, _, elapsed = website(domain, path, user_agent='KnownTestBot crawler' if bot else 'Traffic acceptance browser')
            with lock:
                latencies.append(elapsed)
                if status != (404 if unique else 200):
                    errors.append(status)
        except Exception as exc:
            with lock:
                errors.append(type(exc).__name__)

    def worker(slot):
        for i in range(slot, requests, 8):
            send(i)

    with concurrent.futures.ThreadPoolExecutor(max_workers=8) as executor:
        futures = [executor.submit(worker, slot) for slot in range(8)]
        while time.monotonic() - begin < duration or any(not future.done() for future in futures):
            require(time.monotonic() - begin < max(120, duration + 60), 'Traffic phase deadline reached')
            sample = {'t': round(time.monotonic() - begin, 3), 'disk': disk_metrics()}
            for name in groups:
                pids = service_pids('fluxo.service' if name == 'panel' else 'nginx.service')
                values = [process_metrics(pid) for pid in pids]
                values = [v for v in values if v]
                sample[name] = {'rss_bytes': sum(v['rss_bytes'] for v in values), 'cpu_seconds': sum(v['cpu_seconds'] for v in values)}
            samples.append(sample)
            time.sleep(.5)
        for future in futures:
            future.result()
    seconds = time.monotonic() - begin
    result = {'label': label, 'seconds': round(seconds, 3), 'requests_sent': requests, 'request_errors': len(errors), 'disk_end': disk_metrics(), 'samples': samples}
    for name, pids in groups.items():
        cumulative = cgroup_metrics('fluxo.service' if name == 'panel' else 'nginx.service')
        cpu = cumulative['cpu_seconds'] - before_cgroup[name]['cpu_seconds']
        result[name] = {'cpu_seconds': round(cpu, 4), 'one_core_cpu_percent': round(cpu / seconds * 100, 3),
                        'rss_start_bytes': sum(v['rss_bytes'] for v in before[name].values() if v),
                        'rss_peak_bytes': max(sample[name]['rss_bytes'] for sample in samples),
                        'kernel_write_bytes': (cumulative['write_bytes'] - before_cgroup[name]['write_bytes']) if cumulative['write_bytes'] is not None and before_cgroup[name]['write_bytes'] is not None else None,
                        'cpu_io_source': 'cgroup-v2 cumulative counters, including exited workers; null IO means controller unavailable; disk footprint still measured'}
    if latencies:
        ordered = sorted(latencies)
        result['latency_ms'] = {'median': round(statistics.median(ordered), 4), 'p95': round(ordered[int((len(ordered) - 1) * .95)], 4), 'mean': round(statistics.mean(ordered), 4)}
    require(not errors, 'Website errors during ' + label)
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--candidate', required=True, type=Path)
    parser.add_argument('--sha256', required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--installer', type=Path)
    parser.add_argument('--installer-sha256')
    parser.add_argument('--source', default='uncommitted candidate; identify by exact binary/installer digests')
    args = parser.parse_args()
    require(os.geteuid() == 0 and os.uname().nodename == HOST and os.getenv('FLUXO_PHP_UPDATE_VM_TEST') == '1', 'Exact root named-VM opt-in required')
    require(digest(args.candidate) == args.sha256, 'Candidate hash mismatch')
    if args.installer:
        require(args.installer_sha256 and digest(args.installer) == args.installer_sha256, 'Installer hash mismatch')
    require(not args.output.exists(), 'Output must be a fresh private directory')
    args.output.mkdir(mode=0o700)
    require(not (DATA / 'traffic').exists() and not (CONF / '.fluxo-traffic').exists(), 'Pristine analytics OFF fixture required')
    original = original_inventory()
    timers = {unit: subprocess.run(['systemctl', 'is-active', '--quiet', unit]).returncode == 0
              for unit in ('apt-daily.timer', 'apt-daily-upgrade.timer')}
    choices = Path('/var/lib/fluxo-installer/preferences.json')
    choices_before = choices.read_bytes() if choices.exists() else None
    shutil.copy2(BIN, args.output / 'original-fluxo')
    backup = args.output / 'nginx-original'
    shutil.copytree(CONF, backup, symlinks=True)
    with sqlite3.connect('file:' + str(DATA / 'fluxo.db') + '?mode=ro', uri=True) as source, sqlite3.connect(args.output / 'core-before.db') as target:
        source.backup(target)
    (args.output / 'core-before.db').chmod(0o600)
    api = API()
    api.await_health()
    api.login()
    original_sha = digest(BIN)
    original_active = command(['systemctl', 'is-active', 'fluxo.service']) == 'active'
    result = {'candidate_sha256': args.sha256, 'source': args.source, 'original_sha256': original_sha, 'hostname': HOST, 'arch': os.uname().machine,
              'method': 'candidate binary; real authenticated HTTP API and Nginx origin traffic; same 2-vCPU/2-GiB named Ubuntu VM; no public candidate transport',
              'phases': [], 'checks': {}, 'limitations': ['One observation per condition; warm caches and existing fixture background jobs introduce variance.', 'RSS is process resident sum (shared pages may be counted in multiple Nginx processes), not incremental PSS.', 'CPU percentages use one core as 100%; latency is local origin HTTP, not internet/browser performance.']}
    created = []
    error = None
    try:
        for unit in timers:
            command(['systemctl', 'stop', unit])
        require(all(subprocess.run(['systemctl', 'is-active', '--quiet', unit]).returncode != 0
                    for unit in ('apt-daily.service', 'apt-daily-upgrade.service')),
                'An existing APT transaction is active; wait naturally before this measurement')
        command(['systemctl', 'stop', 'fluxo.service'])
        staging = BIN.with_name('.fluxo-traffic-candidate')
        shutil.copy2(args.candidate, staging)
        staging.chmod(0o755)
        staging.replace(BIN)
        command(['systemctl', 'start', 'fluxo.service'])
        api.await_health()
        require(digest(BIN) == args.sha256, 'Installed candidate differs')
        api.login()
        require(api.state()['master_enabled'] is False, 'Analytics must start disabled')
        require(not (DATA / 'traffic').exists(), 'Disabled startup created analytics state')
        for domain in DOMAINS:
            _, value = api.request('POST', '/sites', {'domain': domain, 'app_type': 'html', 'web_root': 'public', 'db_engine': 'none', 'www_redirect': 'none'}, expected=(200, 201, 202))
            site_id = value.get('id') or value.get('site', {}).get('id')
            require(isinstance(site_id, int), 'Site creation did not return an ID')
            created.append(site_id)
            root = Path('/home/fluxo') / domain / 'public'
            root.mkdir(exist_ok=True)
            for name, payload in (('index.html', b'<html>Traffic fixture</html>'), ('asset.css', b'body{}'), ('disabled.html', b'<html>disabled interval</html>')):
                (root / name).write_bytes(payload)
                (root / name).chmod(0o644)
            require(website(domain)[0] == 200, 'Temporary website unavailable')
        first, second = created
        # Scoped test-site custom configuration exercises explicit status codes
        # and one naturally slow in-flight request without touching existing sites.
        _, vhost = api.request('GET', '/sites/%d/vhost' % first)
        locations = '\n    location = /other-4xx { return 403; }\n    location = /server-error { return 503; }\n    location = /slow.bin { limit_rate 64k; }\n'
        changed = vhost['config'].replace('server {', 'server {' + locations, 1)
        require(changed != vhost['config'], 'Test vhost server block was not found')
        api.request('PUT', '/sites/%d/vhost' % first, {'config': changed, 'expected_revision': vhost['revision']})
        (Path('/home/fluxo') / DOMAINS[0] / 'public/slow.bin').write_bytes(b'x' * (768 << 10))
        api.request('GET', '/sites/%d/traffic-analytics' % first, auth=False, expected=(401,))
        api.request('GET', '/sites/999999999/traffic-analytics', expected=(404,))
        api.request('GET', '/sites/%d/traffic-analytics?range=invalid' % first, expected=(400,))
        result['checks']['auth_site_range'] = True
        result['phases'].append(phase('off_idle', DOMAINS[0], 35))
        result['phases'].append(phase('off_ordinary', DOMAINS[0], 10, 1000))
        require(not (DATA / 'traffic').exists(), 'OFF read/traffic created an analytics database')
        api.toggle(None, True)
        api.toggle(first, True)
        require(api.report(second)['totals']['requests'] == 0, 'Other site received wrong analytics')
        result['phases'].append(phase('enabled_idle', DOMAINS[0], 35))
        initial = api.report(first)['totals']['requests']
        expected_bytes = 0
        for i in range(6):
            expected_bytes += website(DOMAINS[0], '/?secret-query-marker=%d' % i, referrer='https://Example.TEST/private?referrer-secret-marker=1')[1]
        for _ in range(2):
            expected_bytes += website(DOMAINS[0], '/', user_agent='KnownTestBot crawler')[1]
            expected_bytes += website(DOMAINS[0], '/asset.css')[1]
        for i in range(3):
            expected_bytes += website(DOMAINS[0], '/missing-%d' % i)[1]
        for path in ('/other-4xx', '/server-error'):
            expected_bytes += website(DOMAINS[0], path)[1]
        accuracy = api.await_count(first, initial + 15)
        require(accuracy['totals']['requests'] == initial + 15, 'Request aggregate mismatch')
        require(accuracy['totals']['page_views'] == 6, 'Estimated page view filter mismatch')
        require(accuracy['totals']['not_found'] == 3, '404 aggregate mismatch')
        require(accuracy['totals']['other_4xx'] == 1 and accuracy['totals']['server_errors'] == 1, 'Other response error aggregate mismatch')
        require(accuracy['totals']['response_bytes'] == expected_bytes, 'Origin response byte aggregate mismatch')
        require(any(row['value'] == 'example.test' for row in accuracy['referrers']), 'Referrer hostname missing')
        for path in (DATA / 'traffic').rglob('*'):
            if path.is_file():
                raw = path.read_bytes()
                require(b'secret-query-marker' not in raw and b'referrer-secret-marker' not in raw, 'Sensitive URL detail retained in analytics storage')
        result['checks']['metrics_privacy'] = True
        invalid = CONF / 'conf.d/fluxo-traffic-qa-invalid.conf'
        invalid.write_text('this_is_not_a_valid_nginx_directive;\n')
        try:
            api.request('PUT', '/sites/%d/traffic-analytics' % second, {'enabled': True}, expected=(503,))
            require(not next(site for site in api.state()['sites'] if site['site_id'] == second)['enabled'], 'Failed validation committed a toggle')
            require(website(DOMAINS[0])[0] == 200, 'Failed validation interrupted existing website')
        finally:
            invalid.unlink(missing_ok=True)
        result['checks']['validation_failure_preserves_previous'] = True
        api.toggle(second, True)
        api.toggle(second, False)
        slow = http.client.HTTPConnection('127.0.0.1', 80, timeout=25)
        slow.request('GET', '/slow.bin', headers={'Host': DOMAINS[0]})
        response = slow.getresponse()
        require(response.status == 200 and response.read(1) == b'x', 'Slow in-flight request did not start')
        status, disabling = api.toggle(None, False, settle=False)
        require(status == 202 and disabling['pending'], 'OFF reported completion before in-flight worker retirement')
        require(len(response.read()) == (768 << 10) - 1, 'Disabling terminated an in-flight website response')
        slow.close()
        api.settled()
        result['checks']['inflight_graceful_disable'] = True
        disabled_before = api.report(first)['totals']['requests']
        for _ in range(5):
            website(DOMAINS[0], '/disabled.html')
        time.sleep(1)
        require(api.report(first)['totals']['requests'] == disabled_before, 'Disabled interval ingested traffic')
        api.toggle(None, True)
        website(DOMAINS[0], '/')
        resumed = api.await_count(first, disabled_before + 1)
        require(not any(row['value'] == '/disabled.html' for row in resumed['top_paths']), 'Re-enable backfilled disabled traffic')
        require(resumed['gaps'], 'Disabled gap absent')
        result['checks']['master_site_disabled_no_backfill'] = True
        result['phases'].append(phase('enabled_ordinary', DOMAINS[0], 35, 1000))
        api.await_count(first, resumed['totals']['requests'] + 1000)
        if args.installer:
            before_update = api.report(first)['totals']['requests']
            node_exists = Path('/usr/bin/node').exists()
            flags = ['--local-binary=' + str(args.candidate), '--no-python', '--no-harden-ssh', '--db-engine=none', '--no-redis', '--node' if node_exists else '--no-node']
            env = dict(os.environ, FLUXO_LOCAL_BINARY_SHA256=args.sha256, FLUXO_ENV='prod', FLUXO_DATA_DIR=str(DATA), DEBIAN_FRONTEND='noninteractive', NEEDRESTART_MODE='l')
            with (args.output / 'private-ordinary-update.log').open('wb') as stream:
                proc = subprocess.run(['bash', str(args.installer), *flags], stdout=stream, stderr=subprocess.STDOUT, env=env, timeout=240)
            require(proc.returncode == 0, 'Ordinary base installer failed (root-private log retained)')
            api.await_health()
            api.login()
            api.settled()
            require(digest(BIN) == args.sha256, 'Ordinary update installed wrong image')
            require(api.report(first)['totals']['requests'] == before_update, 'Ordinary update lost or duplicated analytics')
            result['checks']['ordinary_update_preserves_analytics'] = True
            result['installer_sha256'] = args.installer_sha256
        before_restart = api.report(first)['totals']['requests']
        command(['systemctl', 'restart', 'fluxo.service'])
        api.await_health()
        api.login()
        api.settled()
        time.sleep(2)
        require(api.report(first)['totals']['requests'] == before_restart, 'Restart duplicated aggregate requests')
        result['checks']['restart_no_duplicates'] = True
        result['phases'].append(phase('burst_bot_high_cardinality', DOMAINS[0], 40, 25000, unique=True, bot=True))
        high = api.report(first)
        require(len(high['top_paths']) <= 20 and high['site']['incomplete'], 'Cardinality/backlog pressure is not reported')
        with sqlite3.connect('file:' + str(DATA / 'traffic/analytics.db') + '?mode=ro', uri=True) as db:
            dimensions = db.execute('SELECT COUNT(*) FROM dimensions WHERE site=?', (first,)).fetchone()[0]
            require(dimensions <= 100, 'Hourly dimensions are unbounded')
            result['checks']['bounded_dimension_rows'] = dimensions
        result['checks']['rotation_observed'] = any((DATA / 'traffic/logs').rglob('*.1'))
        require(result['checks']['rotation_observed'], 'Production log rotation was not reached')
        current_logs = list((DATA / 'traffic/logs' / str(first)).glob('*.jsonl'))
        require(len(current_logs) == 1, 'Expected exactly one current source generation')
        current_log = current_logs[0]
        rotated_log = Path(str(current_log) + '.1')
        require(rotated_log.exists(), 'Expected matching rotation file')
        current_size = current_log.stat().st_size
        old_size = rotated_log.stat().st_size
        for _ in range(5):
            website(DOMAINS[0], '/')
        time.sleep(2)
        require(current_log.stat().st_size > current_size, 'Nginx did not write to the reopened current log')
        require(rotated_log.stat().st_size == old_size, 'Old rotated log still receives new traffic')
        result['checks']['rotation_new_writer_old_quiet'] = True
        api.request('DELETE', '/sites/%d/traffic-analytics' % first)
        require(api.report(first)['totals']['requests'] == 0, 'Explicit history deletion did not clear totals')
        time.sleep(2)
        require(api.report(first)['totals']['requests'] == 0, 'Deleted backlog resurrected history')
        website(DOMAINS[0], '/')
        api.await_count(first, 1)
        result['checks']['delete_active_no_resurrection'] = True
        api.toggle(None, False)
        result['checks']['final_off'] = True
        result['passed'] = True
    except Exception as exc:
        error = exc
        result['passed'] = False
        result['failure'] = type(exc).__name__ + ': ' + str(exc)
        (args.output / 'private-traceback.txt').write_text(traceback.format_exc())
    finally:
        cleanup = {}
        try:
            api.login()
            api.toggle(None, False)
            for site in reversed(created):
                api.request('DELETE', '/sites/%d' % site, expected=(200, 204))
            cleanup['temporary_sites_removed'] = True
        except Exception as exc:
            cleanup['api_cleanup_error'] = str(exc)
        try:
            command(['systemctl', 'stop', 'fluxo.service'])
            # Restore exact original Nginx configuration without touching logs.
            for path in (CONF / 'conf.d/fluxo-traffic.conf', CONF / 'conf.d/fluxo-traffic-qa-invalid.conf'):
                path.unlink(missing_ok=True)
            if (CONF / '.fluxo-traffic').exists():
                shutil.rmtree(CONF / '.fluxo-traffic')
            for sub in ('sites-available', 'sites-enabled', 'conf.d'):
                target, saved = CONF / sub, backup / sub
                for path in target.iterdir():
                    if path.name not in {p.name for p in saved.iterdir()}:
                        if path.is_dir() and not path.is_symlink():
                            shutil.rmtree(path)
                        else:
                            path.unlink()
                for path in saved.iterdir():
                    dest = target / path.name
                    if dest.is_symlink() or dest.exists():
                        if dest.is_dir() and not dest.is_symlink():
                            continue
                        dest.unlink()
                    if path.is_symlink():
                        dest.symlink_to(os.readlink(path))
                    elif path.is_file():
                        shutil.copy2(path, dest)
            command(['nginx', '-t'])
            command(['systemctl', 'reload', 'nginx.service'])
            if (DATA / 'traffic').exists():
                shutil.move(str(DATA / 'traffic'), str(args.output / 'traffic-state'))
            staging = BIN.with_name('.fluxo-traffic-restore')
            shutil.copy2(args.output / 'original-fluxo', staging)
            staging.replace(BIN)
            if original_active:
                command(['systemctl', 'start', 'fluxo.service'])
                api.await_health()
            require(digest(BIN) == original_sha, 'Original binary restoration failed')
            cleanup['binary_nginx_service_restored'] = True
            if choices_before is None:
                choices.unlink(missing_ok=True)
            else:
                choices.write_bytes(choices_before)
                choices.chmod(0o600)
            if 'api_cleanup_error' in cleanup:
                api.login()
                for site in reversed(created):
                    api.request('DELETE', '/sites/%d' % site, expected=(200, 204, 404))
                del cleanup['api_cleanup_error']
                cleanup['temporary_sites_removed'] = True
            cleanup['preservation'] = check_preservation(original)
        except Exception as exc:
            cleanup['restore_error'] = str(exc)
        result['cleanup'] = cleanup
        for unit, was_active in timers.items():
            try:
                command(['systemctl', 'start' if was_active else 'stop', unit])
            except Exception as exc:
                cleanup['timer_restore_error'] = str(exc)
        cleanup['apt_timer_states_restored'] = 'timer_restore_error' not in cleanup
        if any(key in cleanup for key in ('restore_error', 'api_cleanup_error', 'timer_restore_error')):
            result['passed'] = False
        write_json(args.output / 'result.json', result)
        print(json.dumps({k: result.get(k) for k in ('passed', 'failure', 'checks', 'cleanup')}))
    if error:
        raise error
    require(not any(key in result['cleanup'] for key in ('restore_error', 'api_cleanup_error', 'timer_restore_error')), 'Fixture cleanup incomplete')


if __name__ == '__main__':
    main()
