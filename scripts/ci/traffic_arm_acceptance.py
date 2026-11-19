#!/usr/bin/env python3
"""Bounded origin-analytics QA on explicitly opted-in disposable native runners.

This composes the existing fresh-base stock-profile fixture without changing its
guards or launching any historical interruption scenario. Raw credentials,
databases and command logs remain private under /root. Only sanitized evidence
and exact checksummed candidate artifacts are uploaded; no release is created.
"""
import argparse
import concurrent.futures
import json
import os
from pathlib import Path
import platform
import re
import shutil
import sqlite3
import ssl
import subprocess
import tempfile
import time
import traceback

import base_candidate as base
import traffic_native_acceptance as traffic


BRANCH = 'refs/heads/test/traffic-native-acceptance'
DATA = Path('/var/lib/fluxo')


def hosted_profile():
    required = {'GITHUB_ACTIONS': 'true', 'RUNNER_ENVIRONMENT': 'github-hosted',
                'GITHUB_REF': BRANCH}
    base.require(os.geteuid() == 0
                 and all(os.environ.get(k) == v for k, v in required.items()),
                 'Exact disposable native hosted context required')
    arch = {'aarch64': 'arm64', 'x86_64': 'amd64'}.get(platform.machine())
    native_flag = {'arm64': 'FLUXO_TRAFFIC_ARM_CANDIDATE', 'amd64': 'FLUXO_TRAFFIC_AMD_CANDIDATE'}.get(arch)
    other_flag = 'FLUXO_TRAFFIC_AMD_CANDIDATE' if arch == 'arm64' else 'FLUXO_TRAFFIC_ARM_CANDIDATE'
    base.require(native_flag is not None and os.getenv(native_flag) == '1' and os.getenv(other_flag) != '1'
                 and os.getenv('RUNNER_ARCH') == {'arm64': 'ARM64', 'amd64': 'X64'}[arch],
                 'Separate exact native architecture opt-in required')
    source = subprocess.check_output(['git', '-c', 'safe.directory=' + os.getcwd(),
                                     'rev-parse', 'HEAD'], text=True).strip()
    base.require(re.fullmatch('[0-9a-f]{40}', source) and source == os.getenv('GITHUB_SHA'),
                 'Hosted source must match the exact workflow commit')
    base.require(base.run(['dpkg', '--print-architecture'])['stdout'] == arch,
                 'Native dpkg architecture required')
    release = dict(line.split('=', 1) for line in Path('/usr/lib/os-release').read_text().splitlines()
                   if '=' in line)
    base.require(release.get('ID', '').strip('"') == 'ubuntu', 'Ubuntu runner required')
    version = release['VERSION_ID'].strip('"')
    base.require((arch == 'arm64' and version in ('22.04', '24.04', '26.04'))
                 or (arch == 'amd64' and version == '24.04'), 'Unsupported native Ubuntu runner')
    base.require(not any(os.path.lexists(p) for p in ('/var/lib/fluxo', '/usr/local/bin/fluxo',
                 '/home/fluxo', '/var/lib/fluxo-scheduler', '/var/lib/fluxo-system-tasks')),
                 'Refuse existing installation')
    return arch, version


class API(traffic.API):
    def __init__(self, observer):
        super().__init__()
        self.observer = observer
        self.tls = ssl.create_default_context(cadata=(DATA / 'tls/cert.pem').read_text())

    def login(self):
        identity = self.observer.API('https://127.0.0.1:9595')
        identity.login(False, 'traffic-arm')
        self.token = identity.token

    def report(self, site):
        # Keep this multi-minute test stable across a UTC midnight boundary;
        # the Today API is also checked independently below.
        return self.request('GET', '/sites/%d/traffic-analytics?range=7d' % site)[1]


def await_predicate(check, timeout, label):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        result = check()
        if result:
            return result
        time.sleep(.25)
    raise RuntimeError('Timed out: ' + label)


def analytics_snapshot(site):
    with sqlite3.connect('file:' + str(DATA / 'traffic/analytics.db') + '?mode=ro', uri=True) as db:
        base.require(db.execute('PRAGMA quick_check').fetchone() == ('ok',), 'Analytics SQLite integrity failed')
        source = db.execute('SELECT generation,path,start,dev,ino,offset FROM sources WHERE site=?', (site,)).fetchone()
        rows = db.execute('SELECT hour,requests,pageviews,not_found,other_4xx,server_errors,bytes,time_ms FROM hourly WHERE site=? ORDER BY hour', (site,)).fetchall()
        # This private projection includes the source pathname, never any user
        # credential or request URL. Only its digest leaves the process.
        return {'generation': source[0], 'cursor': tuple(source[1:]), 'aggregates': rows}


def no_nginx_writer(path):
    expected = path.stat()
    for pid in traffic.service_pids('nginx.service'):
        try:
            for descriptor in (Path('/proc') / str(pid) / 'fd').iterdir():
                try:
                    held = descriptor.stat()
                    if held.st_dev == expected.st_dev and held.st_ino == expected.st_ino:
                        return False
                except FileNotFoundError:
                    pass
        except FileNotFoundError:
            pass
    return True


def process_start(pid):
    try:
        return (Path('/proc') / str(pid) / 'stat').read_text().rsplit(')', 1)[1].split()[19]
    except FileNotFoundError:
        return None


def nginx_children():
    master = int(traffic.command(['systemctl', 'show', 'nginx.service', '--property=MainPID', '--value']))
    return {pid: process_start(pid) for pid in traffic.service_pids('nginx.service') if pid != master}


def children_retired(children):
    return all(process_start(pid) != start for pid, start in children.items())


def analytics_checks(api, seed, observer, command, private, report, prefix, image, env):
    first = seed['site_id']
    domain = 'base-fixture.invalid'
    state = api.state()
    base.require(not state['master_enabled'] and all(not s['enabled'] for s in state['sites']),
                 'Fresh analytics must be OFF by default')
    base.require(not (DATA / 'traffic').exists(), 'OFF startup or reads created analytics storage')
    traffic.await_website(domain, '/base-acceptance.html')
    api.request('GET', '/sites/%d/traffic-analytics' % first, auth=False, expected=(401,))
    api.request('GET', '/sites/999999999/traffic-analytics', expected=(404,))
    api.request('GET', '/sites/%d/traffic-analytics?range=invalid' % first, expected=(400,))
    api.report(first)
    today = api.request('GET', '/sites/%d/traffic-analytics?range=today' % first)[1]
    base.require(today['range'] == 'today' and today['from'].endswith('T00:00:00Z'),
                 'Today must use a UTC calendar boundary')
    base.require(not (DATA / 'traffic').exists(), 'OFF report or origin traffic created analytics storage')
    report['checks']['default_off_auth_scope_range'] = True

    _, second = api.request('POST', '/sites', {'domain': 'traffic-arm-second.invalid', 'app_type': 'html',
                             'web_root': '/public', 'deployment_strategy': 'standard'}, expected=(200, 201, 202))
    second = second.get('id') or second.get('site', {}).get('id')
    base.require(isinstance(second, int), 'Second site ID missing')
    command('seed-traffic-files', ['runuser', '-u', 'fluxo', '--', 'python3', '-c',
        'import pathlib; p=pathlib.Path("/home/fluxo/base-fixture.invalid/public"); '
        '(p/"asset.css").write_text("body{}"); (p/"disabled.html").write_text("disabled")'])
    api.toggle(None, True)
    api.toggle(first, True)
    initial = api.report(first)['totals']['requests']
    body_bytes = 0
    body_bytes += traffic.website(domain, '/base-acceptance.html?private-query-marker=1',
                                  referrer='https://Example.TEST/private?referrer-secret-marker=1')[1]
    body_bytes += traffic.website(domain, '/base-acceptance.html', user_agent='KnownTestBot crawler')[1]
    body_bytes += traffic.website(domain, '/asset.css')[1]
    body_bytes += traffic.website(domain, '/missing-native-traffic')[1]
    accuracy = api.await_count(first, initial + 4)
    totals = accuracy['totals']
    base.require(totals['requests'] == initial + 4 and totals['page_views'] == 1
                 and totals['not_found'] == 1 and totals['response_bytes'] == body_bytes,
                 'Origin request/page-view/status/byte aggregates differ')
    base.require(any(row['value'] == 'example.test' for row in accuracy['referrers']),
                 'Domain-only referrer missing')
    base.require(api.report(second)['totals']['requests'] == 0, 'Traffic crossed site boundaries')
    for file in (DATA / 'traffic').rglob('*'):
        if file.is_file():
            raw = file.read_bytes()
            base.require(b'private-query-marker' not in raw and b'referrer-secret-marker' not in raw,
                         'Sensitive query/referrer detail retained')
    report['checks']['metrics_privacy_site_isolation'] = True

    # Preserve the last working producer when a genuine nginx -t failure occurs.
    invalid = Path('/etc/nginx/conf.d/fluxo-traffic-arm-invalid.conf')
    invalid.write_text('this_is_not_a_valid_nginx_directive;\n')
    try:
        api.request('PUT', '/sites/%d/traffic-analytics' % second, {'enabled': True}, expected=(503,))
        base.require(not next(site for site in api.state()['sites'] if site['site_id'] == second)['enabled'],
                     'Failed Nginx publication committed its toggle')
        base.require(traffic.website(domain, '/base-acceptance.html')[0] == 200,
                     'Validation failure interrupted the existing website')
    finally:
        invalid.unlink(missing_ok=True)
    api.toggle(second, True)
    api.toggle(second, False)
    api.await_count(first, initial + 5)
    report['checks']['validation_rollback'] = True

    api.toggle(None, False)
    disabled = api.report(first)['totals']['requests']
    for _ in range(5):
        traffic.website(domain, '/disabled.html')
    base.require(api.report(first)['totals']['requests'] == disabled, 'OFF ingested traffic')
    api.toggle(None, True)
    traffic.website(domain, '/base-acceptance.html')
    resumed = api.await_count(first, disabled + 1)
    base.require(not any(row['value'] == '/disabled.html' for row in resumed['top_paths']) and resumed['gaps'],
                 'Re-enable backfilled a disabled interval or hid its gap')
    report['checks']['disable_reenable_gap_no_backfill'] = True

    command('panel-restart-active-analytics', ['systemctl', 'restart', 'fluxo.service'])
    api.await_health(); api.login(); api.settled()
    base.require(api.report(first)['totals']['requests'] == resumed['totals']['requests'],
                 'Panel restart duplicated or lost aggregates')
    report['checks']['restart_checkpoint_dedupe'] = True
    # Exercise real maintenance under the changed Nginx profile. No internal
    # certification/repair is used to admit the installer or the job.
    report['active_analytics_root_execution'] = base.root_execution(observer, seed)
    base.await_predecessor_checks(DATA / 'fluxo.db', report)
    before = base.capture_invariants(seed)
    analytics_before = analytics_snapshot(first)
    command('ordinary-base-update-active-analytics', [*prefix, *base.FLAGS], 900)
    report['update_native'] = base.verify_installed(image, env)
    api.await_health(); api.login(); api.settled()
    report['update_preservation'] = base.compare_invariants(before, base.capture_invariants(seed))
    analytics_after = analytics_snapshot(first)
    base.require(analytics_before == analytics_after, 'Ordinary update changed analytics aggregates/cursor/generation')
    report['checks']['ordinary_update_analytics_preserved'] = True
    report['update_application'] = base.application_checks(observer, seed, command, 'update')
    report['update_root_execution'] = base.root_execution(observer, seed)
    # The application HTTP check above legitimately contributes one request.
    api.await_count(first, resumed['totals']['requests'] + 1)

    # Real Nginx output, rather than generated records or journal manipulation,
    # grows the current source beyond the production 4MiB rotation target.
    _, vhost = api.request('GET', '/sites/%d/vhost' % first)
    config = vhost['config'].replace('server {', 'server {\n    location ^~ /arm-burst- { default_type text/html; return 200 "burst"; }', 1)
    base.require(config != vhost['config'], 'Burst fixture server block not found')
    previous_workers = nginx_children()
    api.request('PUT', '/sites/%d/vhost' % first, {'config': config, 'expected_revision': vhost['revision']})
    # This existing general vhost API returns after systemd accepts reload; it
    # does not promise every old listener has already retired. Do not start a
    # parallel new-location test on a single lucky new-worker HTTP response.
    await_predicate(lambda: children_retired(previous_workers), 15, 'test-vhost old-worker retirement')
    traffic.await_website(domain, '/arm-burst-ready')
    def send(i):
        response = traffic.website(domain, '/arm-burst-%d' % i,
            user_agent='KnownTestBot crawler' if i % 2 == 0 else 'Traffic acceptance browser',
            referrer='https://r-%d.example.test/private?ignored=1' % i)
        base.require(response[0] == 200, 'Burst origin returned HTTP %d at request %d' % (response[0], i))
    with concurrent.futures.ThreadPoolExecutor(max_workers=8) as executor:
        list(executor.map(send, range(25000)))
    folder = DATA / 'traffic/logs' / str(first)
    rotated = await_predicate(lambda: next(iter(folder.glob('*.jsonl.1')), None), 30, 'managed rotation')
    current = Path(str(rotated)[:-2])
    await_predicate(lambda: no_nginx_writer(rotated), 30, 'old Nginx source retirement')
    current_size, old_size = current.stat().st_size, rotated.stat().st_size
    for _ in range(5):
        traffic.website(domain, '/base-acceptance.html')
    await_predicate(lambda: current.stat().st_size > current_size, 10, 'new current log writer')
    time.sleep(1)
    base.require(rotated.stat().st_size == old_size, 'Old rotated inode still receives new requests')
    capped = await_predicate(lambda: api.report(first) if api.report(first)['site']['incomplete'] else None,
                             90, 'visible cardinality/backlog cap')
    with sqlite3.connect('file:' + str(DATA / 'traffic/analytics.db') + '?mode=ro', uri=True) as db:
        dimensions = db.execute('SELECT count(*) FROM dimensions WHERE site=?', (first,)).fetchone()[0]
    base.require(dimensions <= 200 and len(capped['top_paths']) <= 20,
                 'Dimensions exceeded the bounded hourly representation')
    report['checks']['real_rotation_current_writer_old_quiet'] = True
    report['checks']['bounded_dimensions_and_visible_incomplete'] = dimensions
    api.request('DELETE', '/sites/%d/traffic-analytics' % first)
    base.require(api.report(first)['totals']['requests'] == 0, 'Delete history retained requests')
    time.sleep(1)
    base.require(api.report(first)['totals']['requests'] == 0, 'Old backlog resurrected deleted history')
    traffic.website(domain, '/base-acceptance.html')
    api.await_count(first, 1)
    report['checks']['delete_active_no_resurrection'] = True
    api.toggle(None, False)
    report['checks']['final_off'] = True


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--build-dir', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    arch, release = hosted_profile()
    args.output.mkdir(mode=0o755, parents=True, exist_ok=False)
    private = Path(tempfile.mkdtemp(prefix='fluxo-traffic-arm-', dir='/root'))
    private.chmod(0o700)
    report = {'passed': False, 'architecture': arch, 'ubuntu': release, 'engine': 'cron',
              'fresh_install': True, 'public_delivery': False, 'production_approved': False,
              'checks': {}, 'steps': {}, 'test_scope': 'proportional analytics/ordinary-update native QA; prior bridge handoff evidence remains separate'}
    env = {'PATH': '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin', 'HOME': '/root',
           'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8', 'DEBIAN_FRONTEND': 'noninteractive', 'NEEDRESTART_MODE': 'l',
           'FLUXO_ENV': 'prod', 'FLUXO_DATA_DIR': str(DATA), 'FLUXO_PHP_UPDATE_VM_TEST': '1'}
    def command(name, argv, timeout=120, cwd=None):
        report['stage'] = name
        print(json.dumps({'stage': name, 'status': 'started'}), flush=True)
        log = private / (name + '.log')
        begin = time.monotonic()
        with log.open('x') as stream:
            log.chmod(0o600)
            try:
                result = subprocess.run(argv, env=env, cwd=cwd, stdin=subprocess.DEVNULL,
                                        stdout=stream, stderr=subprocess.STDOUT, timeout=timeout)
                code = result.returncode
            except subprocess.TimeoutExpired:
                code = 124
        report['steps'][name] = {'exit_code': code, 'seconds': round(time.monotonic() - begin, 3),
                                 'private_log_sha256': base.digest(log)}
        (args.output / (name + '-sanitized.log')).write_text(base.sanitized_diagnostic(log.read_text(errors='replace')) + '\n')
        base.require(code == 0, 'Command failed: ' + name)
        return log.read_text(errors='replace')
    fixture = base.HostedFixture(private, report, env, command, arch, release, 'cron')
    api = None
    try:
        manifest = base.verify_build(args.build_dir, arch)
        report['candidate'] = manifest
        binary, installer = private / 'fluxo', private / 'install.sh'
        shutil.copyfile(args.build_dir / 'fluxo', binary); binary.chmod(0o700)
        shutil.copyfile('install.sh', installer)
        image = base.digest(binary)
        base.require(image == manifest['binary_sha256'] and base.digest(installer) == manifest['installer_sha256'],
                     'Staged candidate/installer differs')
        env['FLUXO_LOCAL_BINARY_SHA256'] = image
        fixture.prepare()
        prefix = ['bash', str(installer), '--local-binary=' + str(binary)]
        command('fresh-base-install', [*prefix, *base.FLAGS], 900)
        report['fresh_native'] = base.verify_installed(image, env)
        command('base-nginx-start', ['systemctl', 'start', 'nginx.service'])
        observer = base.observer_module()
        seed = base.seed_application(observer, command, private)
        traffic.await_website('base-fixture.invalid', '/base-acceptance.html')
        report['fresh_application'] = base.application_checks(observer, seed, command, 'fresh')
        report['fresh_root_execution'] = base.root_execution(observer, seed)
        base.await_predecessor_checks(DATA / 'fluxo.db', report)
        api = API(observer); api.login()
        analytics_checks(api, seed, observer, command, private, report, prefix, image, env)
        report['passed'] = True
    except Exception as exc:
        report['failure'] = base.sanitized_diagnostic(type(exc).__name__ + ': ' + str(exc))
        (private / 'traceback.txt').write_text(traceback.format_exc())
    finally:
        if api is not None:
            try:
                api.login(); api.toggle(None, False)
                report['final_analytics_disabled'] = True
            except Exception as exc:
                report['final_analytics_disable_error'] = type(exc).__name__
        try:
            fixture.cleanup()
        except Exception as exc:
            report['fixture_cleanup_error'] = type(exc).__name__ + ': ' + str(exc)
            report['passed'] = False
        (args.output / 'report.json').write_text(json.dumps(report, indent=2, sort_keys=True) + '\n')
    print(json.dumps({'passed': report['passed'], 'architecture': arch, 'ubuntu': release, 'source': os.getenv('GITHUB_SHA'),
                      'report': str(args.output / 'report.json')}), flush=True)
    return 0 if report['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
