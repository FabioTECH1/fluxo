#!/usr/bin/env python3
"""Opt-in functional/resource QA for analytics on the named local Ubuntu VM.

Run only after ui then CGO_ENABLED=0 Go build. Copy candidate and this script
with multipass transfer; invoke as root with FLUXO_PHP_UPDATE_VM_TEST=1.
No existing site is modified. Credentials/raw logs remain in the root-private
output directory. This is candidate evidence, never public-release acceptance.
"""
import argparse
import base64
import concurrent.futures
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import selectors
import shutil
import signal
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
STARTED = time.monotonic()


def progress(phase, status, **extra):
    print(json.dumps({'phase': phase, 'status': status, 'elapsed_seconds': round(time.monotonic() - STARTED, 3), **extra}), flush=True)


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

    def api_settings(self, site, settings=None):
        path = '/sites/%d/traffic-analytics/api-settings' % site
        return self.request('GET' if settings is None else 'PUT', path, settings)[1]

    def await_api_count(self, site, minimum, timeout=100):
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            value = self.report(site)
            if value['api']['totals']['requests'] >= minimum:
                return value
            time.sleep(.5)
        raise RuntimeError('API collector request count deadline reached')

    def await_count(self, site, minimum, timeout=100):
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            value = self.report(site)
            if value['totals']['requests'] >= minimum:
                return value
            time.sleep(.5)
        raise RuntimeError('Collector request count deadline reached')


def website(domain, path='/', user_agent='Traffic acceptance browser', referrer=None, accept='text/html', method='GET'):
    conn = http.client.HTTPConnection('127.0.0.1', 80, timeout=15)
    headers = {'Host': domain, 'User-Agent': user_agent, 'Accept': accept}
    if referrer:
        headers['Referer'] = referrer
    begin = time.monotonic()
    try:
        conn.request(method, path, headers=headers)
        response = conn.getresponse()
        body = response.read()
        return response.status, len(body), (time.monotonic() - begin) * 1000
    finally:
        conn.close()


def await_website(domain, path='/', expected=200, timeout=8):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            if website(domain, path)[0] == expected:
                return
        except (OSError, http.client.HTTPException):
            pass
        time.sleep(.05)
    raise RuntimeError('New temporary vhost did not become available within its reload deadline')


# These locations belong only to sites created by the acceptance fixture. All
# expected analytics come from HTTP through Nginx, never inserted log/DB records.
API_LOCATIONS = '''
    location = /api/users/slow { limit_rate 64k; }
    location = /api/redirect { return 302 /api/ok; }
    location = /api/forbidden { return 403; }
    location = /api/server-error { return 503; }
    location = /api { default_type application/json; return 200 "api-root"; }
    location ^~ /api/ { default_type application/json; return 200 "api-response"; }
    location = /apiculture { default_type text/html; return 200 "boundary-page"; }
    location ^~ /outside/ { default_type application/json; return 200 "outside-response"; }
'''


def seed_api_files(domain):
    folder = Path('/home/fluxo') / domain / 'public/api/users'
    folder.mkdir(parents=True, exist_ok=True)
    (folder / 'slow').write_bytes(b'x' * (128 << 10))
    (folder / 'slow').chmod(0o644)


def nginx_records(site, paths):
    """Read actual sanitized producer metadata; raw rows never enter evidence."""
    rows = []
    folder = DATA / 'traffic/logs' / str(site)
    for log in sorted(folder.glob('*.jsonl*')):
        if not log.is_file():
            continue
        for line in log.read_bytes().splitlines():
            try:
                row = json.loads(line)
            except (ValueError, UnicodeDecodeError):
                continue  # A currently incomplete write will be retried.
            if row.get('path') in paths:
                rows.append(row)
    return rows


def measured_api_metrics(rows):
    result = {'requests': len(rows), 'response_bytes': sum(row['bytes'] for row in rows),
              **{'status_%dxx' % n: sum(row['status'] // 100 == n for row in rows) for n in range(1, 6)}}
    result['avg_request_time_ms'] = sum(row['request_time'] * 1000 for row in rows) / len(rows) if rows else 0
    return result


def assert_api_metrics(actual, expected, label):
    for name, value in expected.items():
        if name == 'avg_request_time_ms':
            require(abs(actual[name] - value) <= .001, label + ' weighted Nginx request time differs')
        else:
            require(actual[name] == value, label + ' ' + name + ' differs')


def assert_api_contract(value):
    api = value['api']
    require(all(name in api for name in ('settings', 'available_from', 'effective_from', 'totals',
                                        'buckets', 'endpoints', 'gaps', 'notes', 'incomplete')),
            'API report contract missing')
    require(len(api['buckets']) == len(value['buckets']), 'API and overview time buckets differ')
    require(isinstance(api['notes'], list) and isinstance(api['gaps'], list), 'API coverage notes/gaps missing')
    return api


def api_settings_checks(api, first, second, checks):
    path = '/sites/%d/traffic-analytics/api-settings' % first
    api.request('GET', path, auth=False, expected=(401,))
    api.request('PUT', path, {'prefixes': ['/api'], 'routes': []}, auth=False, expected=(401,))
    api.request('GET', '/sites/999999999/traffic-analytics/api-settings', expected=(404,))
    original = api.api_settings(first)
    require(original['settings'] == {'prefixes': ['/api'], 'routes': []}, 'Default API settings differ')
    for invalid in ({'prefixes': ['api'], 'routes': []},
                    {'prefixes': ['/api/*'], 'routes': []},
                    {'prefixes': ['/api'], 'routes': ['/api/users/*']}):
        api.request('PUT', path, invalid, expected=(422,))
    require(api.api_settings(first) == original, 'Invalid settings changed persisted API rules')
    require(api.api_settings(second)['settings'] == original['settings'], 'API settings crossed site boundaries')
    checks['api_settings_auth_scope_validation'] = True


def api_live_checks(api, first, second, domain, checks):
    """Verify default boundary then future-only configurable route metrics."""
    before = assert_api_contract(api.report(first))
    initial = before['totals']['requests']
    baseline_pages = api.report(first)['totals']['page_views']
    for path in ('/api', '/api/default-native?api-query-secret-marker=1', '/api/users/pre-rule', '/apiculture'):
        require(website(domain, path)[0] == 200, 'API boundary fixture HTTP failed')
    require(website(domain, '/outside/before-settings', method='POST')[0] == 200, 'Outside-prefix fixture HTTP failed')
    boundary = api.await_api_count(first, initial + 3)
    require(boundary['api']['totals']['requests'] == initial + 3, 'Literal /api prefix boundary differs')
    require(boundary['totals']['page_views'] == baseline_pages + 1, 'API boundary page-view classification differs')
    require(not any(row['path'] == '/apiculture' for row in boundary['api']['endpoints']),
            '/apiculture was classified as API')
    require(not any(row['path'] == '/outside/before-settings' for row in boundary['api']['endpoints']),
            'Unconfigured outside prefix was classified as API')
    require(api.report(second)['api']['totals']['requests'] == 0, 'API request crossed site boundaries')
    original_settings = api.api_settings(first)
    state_before = api.state()
    settings = {'prefixes': ['/api', '/outside'], 'routes': ['/api/users/:id']}
    saved = api.api_settings(first, settings)
    require(saved['settings'] == settings and saved['effective_from'], 'Configured API settings not returned')
    require(saved['effective_from'] >= (original_settings.get('effective_from') or ''), 'Settings effective time moved backwards')
    require(api.state()['master_enabled'] == state_before['master_enabled']
            and [s['enabled'] for s in api.state()['sites']] == [s['enabled'] for s in state_before['sites']],
            'API rules changed collection toggles')
    second_settings = {'prefixes': ['/api', '/second-api'], 'routes': []}
    api.api_settings(second, second_settings)
    second_report = api.report(second)
    require(not second_report['site']['enabled'] and second_report['api']['available_from'] is None,
            'Saving API settings enabled a disabled site')
    require(not (DATA / 'traffic/logs' / str(second)).exists(), 'Saving OFF API settings created a log source')
    with sqlite3.connect('file:' + str(DATA / 'traffic/analytics.db') + '?mode=ro', uri=True) as db:
        require(db.execute('SELECT count(*) FROM sources WHERE site=?', (second,)).fetchone()[0] == 0,
                'Saving OFF API settings registered a source')
    api_before = api.report(first)['api']['totals']
    requests = [('GET', '/api/users/17', 200), ('GET', '/api/users/29', 200),
                ('GET', '/api/users/slow', 200), ('POST', '/api/users/17', 200),
                ('PUT', '/api/users/29', 200), ('GET', '/api/redirect', 302),
                ('POST', '/api/forbidden', 403), ('PUT', '/api/server-error', 503),
                ('POST', '/outside/action', 200)]
    paths = {path for _, path, _ in requests}
    for method, path, status in requests:
        require(website(domain, path + '?api-query-secret-marker=1', method=method)[0] == status,
                'Live API fixture returned an unexpected status')
    deadline = time.monotonic() + 10
    while True:
        rows = nginx_records(first, paths)
        if len(rows) == len(requests):
            break
        require(time.monotonic() < deadline, 'Actual Nginx metadata count differs')
        time.sleep(.1)
    require(all('?' not in row['path'] for row in rows), 'Nginx retained query detail')
    expected = measured_api_metrics(rows)
    require(expected['avg_request_time_ms'] > 10, 'Timing fixture did not exercise a nonzero weighted request duration')
    measured = api.await_api_count(first, api_before['requests'] + len(rows))['api']
    delta = {key: measured['totals'][key] - api_before[key] for key in expected if key != 'avg_request_time_ms'}
    delta['avg_request_time_ms'] = ((measured['totals']['avg_request_time_ms'] * measured['totals']['requests']
                                     - api_before['avg_request_time_ms'] * api_before['requests']) / len(rows))
    assert_api_metrics(delta, expected, 'API totals')
    endpoints = {(row['method'], row['path']): row['metrics'] for row in measured['endpoints']}
    grouped = {}
    for row in rows:
        path = '/api/users/:id' if row['path'].startswith('/api/users/') else row['path']
        grouped.setdefault((row['method'], path), []).append(row)
    for key, endpoint_rows in grouped.items():
        require(key in endpoints, 'Normalized method + route endpoint missing: ' + str(key))
        assert_api_metrics(endpoints[key], measured_api_metrics(endpoint_rows), 'API endpoint ' + str(key))
    require(not any(path in ('/api/users/17', '/api/users/29', '/api/users/slow') for _, path in endpoints),
            'Configured route did not normalize dynamic path segments')
    require(('GET', '/api/users/pre-rule') in endpoints
            and not any(path == '/outside/before-settings' for _, path in endpoints),
            'Settings change reconstructed or relabeled historical API aggregates')
    for file in (DATA / 'traffic').rglob('*'):
        if file.is_file():
            require(b'api-query-secret-marker' not in file.read_bytes(), 'API query detail retained in analytics storage')
    checks['api_boundary_configured_prefix_route_method_status_bytes_weighted_time_privacy'] = True
    checks['api_1xx'] = 'No terminal 1xx response is generated: HTTP informational responses precede the final response; zero is asserted from actual Nginx metadata.'
    return measured


def original_inventory():
    from cryptography.hazmat.primitives.ciphers.aead import AESGCM
    result = {'tables': {}, 'files': {}}
    cipher = AESGCM((DATA / 'encryption.key').read_bytes())
    with sqlite3.connect('file:' + str(DATA / 'fluxo.db') + '?mode=ro', uri=True) as db:
        require(db.execute('PRAGMA quick_check').fetchone() == ('ok',), 'Core SQLite check failed')
        for table in ('users', 'sites', 'github_accounts', 'ssh_keys', 'crons', 'commands', 'deployments', 'certificates', 'backup_plans', 'backup_destinations'):
            names = [row[1] for row in db.execute('PRAGMA table_info(' + table + ')')]
            rows = db.execute('SELECT * FROM ' + table + ' ORDER BY 1').fetchall()
            if table == 'users':
                authenticated = []
                for row in rows:
                    row = list(row)
                    for name in ('fluxo_db_password', 'fluxo_sudo_password', 'fluxo_mysql_password', 'fluxo_postgres_password', 'github_pat', 'webhook_secret'):
                        if name not in names:
                            continue
                        index = names.index(name)
                        if row[index]:
                            require(row[index].startswith('enc:'), 'Existing credential must remain encrypted')
                            raw = base64.b64decode(row[index][4:])
                            plain = cipher.decrypt(raw[:12], raw[12:], None)
                            row[index] = 'authenticated-plaintext-sha256:' + hashlib.sha256(plain).hexdigest()
                    authenticated.append(row)
                rows = authenticated
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


def image_proof(expected):
    pid = int(command(['systemctl', 'show', 'fluxo.service', '--property=MainPID', '--value']))
    require(pid > 1 and digest(BIN) == digest('/proc/%d/exe' % pid) == expected, 'Installed/running candidate identity differs')
    return {'installed_sha256': expected, 'running_sha256': expected}


def recover_owned_test_lease(private, allowed_images):
    """Use the same guarded acquire/release as install.sh after a QA timeout.

    No serialized authority is edited. The dedicated VM was required to have
    no lease at test start. Refuse a living owner or another installer caller.
    """
    state = Path('/var/lib/fluxo-scheduler/state.json')
    raw = state.read_bytes()
    saved = json.loads(raw).get('upgrade')
    if saved is None:
        return False
    require(digest(BIN) in allowed_images, 'Unexpected image during test lease cleanup')
    owner = Path('/proc') / str(saved['pid'])
    if owner.exists():
        start = (owner / 'stat').read_text().rsplit(')', 1)[1].split()[19]
        require(start != saved['start'], 'A live installer still owns the lease')
    for proc in Path('/proc').iterdir():
        if not proc.name.isdigit():
            continue
        try:
            argv = (proc / 'cmdline').read_bytes().split(b'\0')
        except (FileNotFoundError, PermissionError, ProcessLookupError):
            continue
        require(b'scheduler-installer-lease' not in argv, 'Another installer lease caller remains')
    write_json(private / 'lease-cleanup-before.json', {'dead_owner_verified': True, 'scheduler_sha256': hashlib.sha256(raw).hexdigest()})
    env = dict(os.environ, FLUXO_ENV='prod', FLUXO_DATA_DIR=str(DATA))
    with (private / 'lease-cleanup-private.log').open('wb') as errors:
        lease = subprocess.Popen([str(BIN), 'scheduler-installer-lease', str(os.getpid())], stdout=subprocess.PIPE, stderr=errors, env=env)
        ready = selectors.DefaultSelector()
        ready.register(lease.stdout, selectors.EVENT_READ)
        require(ready.select(timeout=105), 'Test cleanup lease readiness timed out')
        line = lease.stdout.readline().decode().strip()
        require(re.fullmatch(r'READY [0-9a-f-]{36}', line), 'Test cleanup lease refused; retain private log')
        operation = line.split()[1]
        release = subprocess.run([str(BIN), 'scheduler-installer-release', operation], stdout=errors, stderr=errors, env=env, timeout=15)
        require(release.returncode == 0 and lease.wait(timeout=15) == 0, 'Test cleanup lease release refused')
    current = json.loads(state.read_text())
    require(current.get('upgrade') is None and not current.get('holds'), 'Test cleanup left an admission hold')
    write_json(private / 'lease-cleanup-after.json', {'supported_lease_round_trip': True, 'scheduler_sha256': digest(state), 'no_raw_authority_edits': True})
    return True


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


def phase(label, domain, duration=35, requests=0, unique=False, bot=False, api_traffic=False):
    progress(label, 'started')
    groups = {'panel': service_pids('fluxo.service'), 'nginx': service_pids('nginx.service')}
    before = {name: {pid: process_metrics(pid) for pid in pids} for name, pids in groups.items()}
    before_cgroup = {name: cgroup_metrics('fluxo.service' if name == 'panel' else 'nginx.service') for name in groups}
    disk_before = disk_metrics()
    begin = time.monotonic()
    samples = []
    latencies = []
    errors = []
    lock = threading.Lock()

    def send(i):
        path = ('/api/cardinality-%d' % i if unique else '/api/users/%d' % (i % 4)) if api_traffic else ('/burst-%d' % i if unique else '/')
        try:
            status, _, elapsed = website(domain, path,
                user_agent='KnownTestBot crawler' if bot and i % 2 == 0 else 'Traffic acceptance browser',
                referrer='https://r-%d.example.test/private?ignored=1' % i if unique else None,
                method=('GET', 'POST', 'PUT')[i % 3] if api_traffic else 'GET')
            with lock:
                latencies.append(elapsed)
                if status != 200:
                    errors.append(status)
        except Exception as exc:
            with lock:
                errors.append(type(exc).__name__)

    def worker(slot):
        for i in range(slot, requests, 8):
            if not unique and requests:
                # Pair OFF/ON at the same ordinary arrival rate; the separate
                # unique-path phase deliberately sends an unpaced burst.
                delay = begin + i * duration / requests - time.monotonic()
                if delay > 0:
                    time.sleep(delay)
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
    disk_end = disk_metrics()
    result = {'label': label, 'seconds': round(seconds, 3), 'requests_sent': requests,
              'traffic_kind': 'API GET/POST/PUT' if api_traffic else 'website GET',
              'known_bot_fraction': .5 if bot else 0, 'unique_paths_and_referring_domains': unique,
              'request_errors': len(errors), 'disk_start': disk_before, 'disk_end': disk_end,
              'disk_growth_bytes': disk_end['total_bytes'] - disk_before['total_bytes'], 'samples': samples}
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
    progress(label, 'passed', seconds=result['seconds'])
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--candidate', required=True, type=Path)
    parser.add_argument('--sha256', required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--installer', type=Path)
    parser.add_argument('--installer-sha256')
    parser.add_argument('--source', default='uncommitted candidate; identify by exact binary/installer digests')
    parser.add_argument('--mode', choices=('full', 'burst', 'update'), default='full')
    args = parser.parse_args()
    require(os.geteuid() == 0 and os.uname().nodename == HOST and os.getenv('FLUXO_PHP_UPDATE_VM_TEST') == '1', 'Exact root named-VM opt-in required')
    require(digest(args.candidate) == args.sha256, 'Candidate hash mismatch')
    if args.installer:
        require(args.installer_sha256 and digest(args.installer) == args.installer_sha256, 'Installer hash mismatch')
    require(not args.output.exists(), 'Output must be a fresh private directory')
    args.output.mkdir(mode=0o700)
    require(not (DATA / 'traffic').exists() and not (CONF / '.fluxo-traffic').exists(), 'Pristine analytics OFF fixture required')
    require(all(not (Path('/home/fluxo') / domain).exists() for domain in DOMAINS), 'Temporary site paths already exist; preserve them for inspection')
    original = original_inventory()
    require(json.loads(Path('/var/lib/fluxo-scheduler/state.json').read_text()).get('upgrade') is None, 'An existing installer requires recovery before this QA')
    write_json(args.output / 'original-inventory.json', original)
    timers = {unit: subprocess.run(['systemctl', 'is-active', '--quiet', unit]).returncode == 0
              for unit in ('apt-daily.timer', 'apt-daily-upgrade.timer')}
    choices = Path('/var/lib/fluxo-installer/preferences.json')
    choices_before = choices.read_bytes() if choices.exists() else None
    shutil.copy2(BIN, args.output / 'original-fluxo')
    unit = Path(command(['systemctl', 'show', 'fluxo.service', '--property=FragmentPath', '--value']))
    require(unit == Path('/etc/systemd/system/fluxo.service') and unit.is_file(), 'Expected fixture service unit path')
    shutil.copy2(unit, args.output / 'original-fluxo.service')
    original_enabled = subprocess.run(['systemctl', 'is-enabled', '--quiet', 'fluxo.service']).returncode == 0
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
    result = {'mode': args.mode, 'candidate_sha256': args.sha256, 'harness_sha256': digest(__file__), 'source': args.source, 'original_sha256': original_sha, 'hostname': HOST, 'arch': os.uname().machine,
              'method': 'candidate binary; real authenticated HTTP API and Nginx origin traffic; same 2-vCPU/2-GiB named Ubuntu VM; no public candidate transport',
              'phases': [], 'checks': {}, 'limitations': ['One observation per condition; warm caches and existing fixture background jobs introduce variance.', 'RSS is process resident sum (shared pages may be counted in multiple Nginx processes), not incremental PSS.', 'CPU percentages use one core as 100%; latency is local origin HTTP, not internet/browser performance.']}
    created = []
    error = None
    try:
        progress('candidate-start-and-api-sites', 'started')
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
        result['running_image_before'] = image_proof(args.sha256)
        api.login()
        require(api.state()['master_enabled'] is False, 'Analytics must start disabled')
        require(not (DATA / 'traffic').exists(), 'Disabled startup created analytics state')
        for domain in DOMAINS:
            _, value = api.request('POST', '/sites', {'domain': domain, 'app_type': 'html', 'web_root': 'public', 'db_engine': '', 'www_redirect': 'none'}, expected=(200, 201, 202))
            site_id = value.get('id') or value.get('site', {}).get('id')
            require(isinstance(site_id, int), 'Site creation did not return an ID')
            created.append(site_id)
            root = Path('/home/fluxo') / domain / 'public'
            root.mkdir(exist_ok=True)
            for name, payload in (('index.html', b'<html>Traffic fixture</html>'), ('asset.css', b'body{}'), ('disabled.html', b'<html>disabled interval</html>')):
                (root / name).write_bytes(payload)
                (root / name).chmod(0o644)
            await_website(domain)
        first, second = created
        # Scoped test-site custom configuration exercises explicit status codes
        # and one naturally slow in-flight request without touching existing sites.
        _, vhost = api.request('GET', '/sites/%d/vhost' % first)
        locations = '\n    location = /other-4xx { return 403; }\n    location = /server-error { return 503; }\n    location = /slow.bin { limit_rate 64k; }\n    location ^~ /burst- { default_type text/html; return 200 "burst"; }\n' + API_LOCATIONS
        changed = vhost['config'].replace('server {', 'server {' + locations, 1)
        require(changed != vhost['config'], 'Test vhost server block was not found')
        api.request('PUT', '/sites/%d/vhost' % first, {'config': changed, 'expected_revision': vhost['revision']})
        (Path('/home/fluxo') / DOMAINS[0] / 'public/slow.bin').write_bytes(b'x' * (768 << 10))
        seed_api_files(DOMAINS[0])
        await_website(DOMAINS[0], '/other-4xx', 403)
        if args.mode == 'full':
            api.request('GET', '/sites/%d/traffic-analytics' % first, auth=False, expected=(401,))
            api.request('GET', '/sites/999999999/traffic-analytics', expected=(404,))
            api.request('GET', '/sites/%d/traffic-analytics?range=invalid' % first, expected=(400,))
            result['checks']['auth_site_range'] = True
            uncollected = assert_api_contract(api.report(first))
            require(uncollected['available_from'] is None
                    and all(bucket['metrics'] is None for bucket in uncollected['buckets']),
                    'Uncollected API history was presented as observed zeros')
            api_settings_checks(api, first, second, result['checks'])
            # One origin probe over the idle window supplies comparable local
            # website latency without turning it into an ordinary-load phase.
            result['phases'].append(phase('off_idle_one_probe', DOMAINS[0], 35, 1))
            result['phases'].append(phase('off_ordinary_api', DOMAINS[0], 35, 1000, api_traffic=True))
            require(not (DATA / 'traffic').exists(), 'OFF read/traffic created an analytics database')
            api.toggle(None, True)
            api.toggle(first, True)
            require(api.report(second)['totals']['requests'] == 0, 'Other site received wrong analytics')
            result['phases'].append(phase('enabled_idle_one_probe', DOMAINS[0], 35, 1))
            api.await_count(first, 1)
            progress('metrics-and-privacy', 'started')
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
            api_live_checks(api, first, second, DOMAINS[0], result['checks'])
            progress('metrics-and-privacy', 'passed')
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
            api.request('PUT', '/sites/%d/traffic-analytics/api-settings' % first,
                        {'prefixes': ['/api'], 'routes': []}, expected=(409,))
            require(len(response.read()) == (768 << 10) - 1, 'Disabling terminated an in-flight website response')
            slow.close()
            api.settled()
            result['checks']['inflight_graceful_disable'] = True
            disabled_before = api.report(first)['totals']['requests']
            disabled_api = api.report(first)['api']['totals']['requests']
            for _ in range(5):
                website(DOMAINS[0], '/disabled.html')
                website(DOMAINS[0], '/api/disabled')
            time.sleep(1)
            require(api.report(first)['totals']['requests'] == disabled_before, 'Disabled interval ingested traffic')
            require(api.report(first)['api']['totals']['requests'] == disabled_api, 'Disabled interval ingested API traffic')
            api.toggle(None, True)
            website(DOMAINS[0], '/')
            website(DOMAINS[0], '/api/resumed')
            resumed = api.await_count(first, disabled_before + 2)
            resumed_api = api.await_api_count(first, disabled_api + 1)['api']
            require(resumed_api['totals']['requests'] == disabled_api + 1
                    and not any(row['path'] == '/api/disabled' for row in resumed_api['endpoints']),
                    'Re-enable backfilled disabled API traffic')
            require(not any(row['value'] == '/disabled.html' for row in resumed['top_paths']), 'Re-enable backfilled disabled traffic')
            require(resumed['gaps'], 'Disabled gap absent')
            result['checks']['master_site_disabled_no_backfill'] = True
            result['phases'].append(phase('enabled_ordinary_api', DOMAINS[0], 35, 1000, api_traffic=True))
            api.await_count(first, resumed['totals']['requests'] + 1000)
        else:
            api.toggle(None, True)
            api.toggle(first, True)
            for _ in range(20):
                website(DOMAINS[0], '/')
            api.await_count(first, 20)
            result['checks']['seeded_real_origin_data'] = True
        if args.mode in ('full', 'burst'):
            before_restart = api.report(first)['totals']['requests']
            command(['systemctl', 'restart', 'fluxo.service'])
            api.await_health()
            api.login()
            api.settled()
            time.sleep(2)
            require(api.report(first)['totals']['requests'] == before_restart, 'Restart duplicated aggregate requests')
            result['checks']['restart_no_duplicates'] = True
            result['phases'].append(phase('burst_bot_high_cardinality_api', DOMAINS[0], 40, 25000,
                                          unique=True, bot=True, api_traffic=True))
            high = api.report(first)
            require(len(high['top_paths']) <= 20 and high['site']['incomplete'], 'Cardinality/backlog pressure is not reported')
            require(high['api']['incomplete'], 'API endpoint cardinality/backlog pressure is not reported')
            result['burst_collection_snapshot'] = {'input_requests': 25000, 'processed_requests': high['totals']['requests'], 'backlog_bytes': high['site']['backlog_bytes'], 'dropped_bytes': high['site']['dropped_bytes'], 'incomplete': high['site']['incomplete']}
            with sqlite3.connect('file:' + str(DATA / 'traffic/analytics.db') + '?mode=ro', uri=True) as db:
                dimensions = db.execute('SELECT COUNT(*) FROM dimensions WHERE site=?', (first,)).fetchone()[0]
                require(dimensions <= 200, 'Hourly dimensions are unbounded')
                result['checks']['bounded_dimension_rows'] = dimensions
                api_rows = db.execute('SELECT COUNT(*) FROM api_endpoints WHERE site=?', (first,)).fetchone()[0]
                result['checks']['bounded_api_endpoint_rows'] = api_rows
                require(api_rows <= 130, 'API hourly endpoint representation exceeded two bounded hourly budgets')
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
            require(api.report(first)['api']['totals']['requests'] == 0
                    and not api.report(first)['api']['endpoints'], 'History deletion retained API data')
            time.sleep(2)
            require(api.report(first)['totals']['requests'] == 0, 'Deleted backlog resurrected history')
            require(api.report(first)['api']['totals']['requests'] == 0, 'Deleted backlog resurrected API history')
            website(DOMAINS[0], '/')
            website(DOMAINS[0], '/api/after-delete')
            api.await_count(first, 1)
            require(api.await_api_count(first, 1)['api']['totals']['requests'] == 1,
                    'API collection did not resume after explicit history deletion')
            result['checks']['delete_active_no_resurrection'] = True
        if args.installer:
            progress('ordinary-base-installer-preservation', 'started')
            before_update = api.report(first)['totals']['requests']
            api_before_update = api.report(first)['api']['totals']
            node_exists = Path('/usr/bin/node').exists()
            flags = ['--local-binary=' + str(args.candidate), '--no-python', '--no-harden-ssh', '--db-engine=none', '--no-redis', '--node' if node_exists else '--no-node']
            env = dict(os.environ, FLUXO_LOCAL_BINARY_SHA256=args.sha256, FLUXO_ENV='prod', FLUXO_DATA_DIR=str(DATA), DEBIAN_FRONTEND='noninteractive', NEEDRESTART_MODE='l')
            with (args.output / 'private-ordinary-update.log').open('wb') as stream:
                proc = subprocess.Popen(['bash', str(args.installer), *flags], stdout=stream, stderr=subprocess.STDOUT, env=env, start_new_session=True)
                try:
                    code = proc.wait(timeout=240)
                except subprocess.TimeoutExpired:
                    os.killpg(proc.pid, signal.SIGTERM)
                    try:
                        proc.wait(timeout=12)
                    except subprocess.TimeoutExpired:
                        os.killpg(proc.pid, signal.SIGKILL)
                        proc.wait(timeout=5)
                    raise RuntimeError('Ordinary base installer exceeded its 240-second deadline; its owned process group was stopped')
            require(code == 0, 'Ordinary base installer failed (root-private log retained)')
            api.await_health()
            api.login()
            api.settled()
            require(digest(BIN) == args.sha256, 'Ordinary update installed wrong image')
            result['running_image_after_update'] = image_proof(args.sha256)
            require(api.report(first)['totals']['requests'] == before_update, 'Ordinary update lost or duplicated analytics')
            require(api.report(first)['api']['totals'] == api_before_update, 'Ordinary update lost or duplicated API analytics')
            result['checks']['ordinary_update_preserves_analytics'] = True
            progress('ordinary-base-installer-preservation', 'passed')
            result['installer_sha256'] = args.installer_sha256
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
            shutil.copy2(args.output / 'original-fluxo.service', unit)
            command(['systemctl', 'daemon-reload'])
            command(['systemctl', 'enable' if original_enabled else 'disable', 'fluxo.service'])
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
                cleanup['supported_interrupted_test_lease_cleanup'] = recover_owned_test_lease(args.output, {original_sha})
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
