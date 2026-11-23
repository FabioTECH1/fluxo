"""Bounded authenticated base feature observations, never a new updater or repair.

Only for the existing public-release disposable fixture. No clocks/due records,
package configuration, service activation, floor, nonce or receipt is edited.
Only small sanitized booleans/package facts/digests are returned for publication.
"""
import ast
from datetime import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import sqlite3
import subprocess
import time

MARKERS = ('Automatic security updates', 'Password Protection', 'Allow wildcard subdomains',
           'Nginx update instructions', 'Reload Nginx', 'Last successful check', 'Run these commands over SSH.',
           'Traffic history range', 'All retained history', 'Running kernel', 'Earlier dates are unavailable.',
           'Server restart status', 'Dismiss completed restart status')


def require(value, reason):
    if not value:
        raise RuntimeError(reason)


def public_ui_markers(path):
    raw = Path(path).read_bytes()
    require(all(text.encode() in raw for text in MARKERS), 'New public base feature marker missing')
    return {'system_network_nginx_markers': list(MARKERS), 'all_present': True}


def trace_commands(raw):
    result = []
    for line in raw.splitlines():
        if not re.search(r'\bexecve\(', line):
            continue
        match = re.search(r'execve\(("(?:[^"\\]|\\.)*"), (\[(?:"(?:[^"\\]|\\.)*"(?:, )?)*\])', line)
        require(match is not None, 'Unparsed exec observation; do not assume no mutation')
        name, args = ast.literal_eval(match[1]), ast.literal_eval(match[2])
        # Only successful execs prove a command ran. Failed PATH probes are
        # still parsed but cannot be counted as package/service operations.
        if not line.endswith('= 0'):
            continue
        base = Path(name).name
        if base in ('apt', 'apt-get'):
            require('update' in args and not any(v in args for v in ('install', 'upgrade', 'dist-upgrade', 'full-upgrade', 'remove', 'autoremove', 'purge')), 'Nginx discovery launched a package mutation')
        if base in ('dpkg', 'dpkg-query'):
            require(not any(v in args for v in ('-i', '--install', '-r', '--remove', '-P', '--purge', '--configure')), 'Nginx discovery changed dpkg packages')
        if base in ('systemctl', 'service', 'invoke-rc.d', 'deb-systemd-invoke'):
            require(not any(v in args for v in ('start', 'stop', 'restart', 'reload', 'try-restart', 'reload-or-restart', 'enable', 'disable', 'reboot')), 'Nginx discovery activated or changed a service')
        if base == 'nginx':
            require('-s' not in args, 'Nginx discovery signalled Nginx')
        if base in ('reboot', 'shutdown', 'poweroff', 'kill'):
            raise RuntimeError('Nginx discovery attempted power control')
        result.append({'executable': base, 'apt_metadata_refresh': base in ('apt', 'apt-get') and 'update' in args})
    return result


def check_discovery_truth(result, saved, now):
    require(result.get('running_adoption') == 'not_verified' and type(result.get('stale')) is bool,
            'Nginx observation invented running adoption/currentness')
    require(saved.get('format') == 1 and type(saved.get('next_check')) is int, 'Daily discovery schedule was not saved')
    require(0 < saved['next_check'] - now <= 86410, 'Saved next check is not bounded in the future')
    snapshot = result.get('snapshot')
    if result.get('error'):
        require(result['stale'] and result.get('notice') is False, 'Metadata failure was presented as current')
        return {'fresh_success': False, 'error_truth_preserved': True,
                'error': result['error'][:500], 'scheduled_retry_seconds': max(0, saved['next_check'] - now)}
    require(snapshot is not None and not result['stale'] and result['last_successful_check'] > 0,
            'Successful check did not return fresh package facts')
    require(snapshot.get('state') in ('current', 'update_available', 'held', 'unavailable', 'unsupported'), 'Unknown Nginx candidate state')
    require(saved['next_check'] - result['last_successful_check'] == 86400,
            'Successful manual check did not save actual24hour due time')
    return {'fresh_success': True, 'candidate_state': snapshot['state'], 'daily_interval_seconds': 86400,
            'package': snapshot.get('package'), 'architecture': snapshot.get('architecture'),
            'installed_version': snapshot.get('installed_version'), 'candidate_version': snapshot.get('candidate_version')}


def network_digest(state):
    require(state.get('managed') is True and state.get('pending') is False, 'New base Network is not confirmed managed')
    view = {key: state[key] for key in ('managed', 'protection', 'redirects', 'wildcards')}
    return hashlib.sha256(json.dumps(view, sort_keys=True).encode()).hexdigest()


def process_identity(pid):
    require(type(pid) is int and pid > 1, 'Missing actual service process')
    raw = Path('/proc/%d/stat' % pid).read_text()
    return [pid, raw.rsplit(')', 1)[1].split()[19]]


def nginx_check(api, private):
    before = api.request('GET', '/server/nginx/updates')
    end = time.monotonic() + 100
    while before.get('checking'):
        require(time.monotonic() < end, 'Existing Nginx discovery did not settle within its normal bound')
        time.sleep(1)
        before = api.request('GET', '/server/nginx/updates')
    pid = int(subprocess.check_output(['systemctl', 'show', 'fluxo.service', '-p', 'MainPID', '--value'], text=True))
    nginx_pid = int(subprocess.check_output(['systemctl', 'show', 'nginx.service', '-p', 'MainPID', '--value'], text=True))
    nginx_before = process_identity(nginx_pid)
    package_before = hashlib.sha256(Path('/var/lib/dpkg/status').read_bytes()).hexdigest()
    trace_prefix = Path(private) / 'nginx-check.exec'
    log = open(Path(private) / 'nginx-check.trace-diagnostic', 'wb')
    tracer = subprocess.Popen(['/usr/bin/strace', '-ff', '-s', '131072', '-e', 'trace=execve',
                               '-o', str(trace_prefix), '-p', str(pid)], stdout=log, stderr=log)
    result = None
    try:
        time.sleep(.3)
        require(tracer.poll() is None, 'Read-only command observer could not attach')
        result = api.request('POST', '/server/nginx/updates/check', {})
    finally:
        tracer.send_signal(signal.SIGINT) if tracer.poll() is None else None
        tracer.wait(timeout=10)
        log.close()
    traces = list(Path(private).glob('nginx-check.exec*'))
    require(traces and len(traces) <= 512, 'Missing/bounded command observation required')
    commands = []
    for path in traces:
        require(path.stat().st_size <= 4 << 20, 'Command observation too large')
        commands.extend(trace_commands(path.read_text(errors='replace')))
    require(package_before == hashlib.sha256(Path('/var/lib/dpkg/status').read_bytes()).hexdigest(), 'Installed package records changed during metadata-only check')
    require(process_identity(nginx_pid) == nginx_before, 'Nginx process changed during metadata-only check')
    info = api.request('GET', '/server/nginx/info')
    require(info.get('version_scope') == 'on_disk_binary' and info.get('running_adoption') == 'not_verified', 'Disk version was presented as running adoption')
    with sqlite3.connect('file:/var/lib/fluxo/fluxo.db?mode=ro', uri=True) as db:
        row = db.execute("SELECT value FROM system_metadata WHERE key='nginx_update_discovery'").fetchone()
    require(row is not None, 'No real persisted Nginx discovery')
    saved = json.loads(row[0])
    proof = check_discovery_truth(result, saved, int(time.time()))
    if proof['fresh_success']:
        require(any(row['apt_metadata_refresh'] for row in commands), 'Fresh discovery has no observed real APT metadata command')
    proof['saved_discovery_sha256'] = hashlib.sha256(json.dumps(saved, sort_keys=True).encode()).hexdigest()
    proof.update(authenticated_get_and_manual_check=True, due_record_edited=False,
                 package_records_unchanged=True, nginx_process_unchanged=True,
                 command_observer='external execve only; no production code instrumentation; tracer detached without stopping daemon',
                 commands=commands, no_observed_install_or_service_commands=True, running_adoption='not_verified')
    if proof['fresh_success'] and result['snapshot']['state'] != 'unsupported':
        binary = os.path.realpath(info['binary'])
        output = subprocess.check_output(['/usr/bin/dpkg-query', '--search', binary], text=True).strip()
        matches = [line.split(': ', 1)[0] for line in output.splitlines() if line.endswith(': ' + binary)]
        require(len(matches) == 1, 'Cannot independently identify binary owner')
        owner = matches[0]
        actual = subprocess.check_output(['/usr/bin/dpkg-query', '--show', '--showformat=${Version}', owner], text=True)
        require(result['snapshot']['package'] == owner.split(':')[0] and result['snapshot']['installed_version'] == actual,
                'Discovered installed package revision differs from actual dpkg owner')
        proof['owning_package_revision_independently_matched'] = True
    # Repeat GET is observational; it cannot advance attempted/success/due facts.
    repeated = api.request('GET', '/server/nginx/updates')
    require(all(repeated[key] == result[key] for key in ('last_successful_check', 'attempted_at', 'snapshot')),
            'GET unexpectedly refreshed discovery')
    return proof


def traffic_history_observations(api, site_id):
    """Read actual retained fixtures through authenticated API; never seed old clocks/rows."""
    state = api.request('GET', '/traffic-analytics')
    policy = state['retention_days']
    require(type(policy) is int and policy in (0, 30, 60, 90, 180, 365), 'Unknown actual retention policy')
    proof = {'retention_days': policy, 'read_only': True, 'ranges': {}}
    for period in ('today', '90d', '365d', 'all'):
        result = api.request('GET', '/sites/%d/traffic-analytics?range=%s' % (site_id, period))
        require(result.get('range') == period and result.get('retention_days') == policy,
                'Public history range/retention differs')
        step = result.get('bucket_seconds')
        require(type(step) is int and step >= 3600 and step % 3600 == 0, 'Unbounded/invalid public period')
        require(datetime.fromisoformat(result['from'].replace('Z', '+00:00')) <=
                datetime.fromisoformat(result['to'].replace('Z', '+00:00')), 'Invalid UTC bounds')
        counts = {}
        for name, report in (('overview', result), ('api', result['api'])):
            buckets = report.get('buckets')
            require(isinstance(buckets, list) and len(buckets) <= 720, 'Public history exceeded chart bound')
            require(sum((bucket.get('metrics') or {}).get('requests', 0) for bucket in buckets)
                    == report['totals']['requests'], 'Public history totals differ from periods')
            require(all(bucket.get('metrics') is None for bucket in buckets if bucket['collection'] == 'unknown'),
                    'Unavailable history was fabricated as quiet traffic')
            counts[name] = {'periods': len(buckets), 'requests': report['totals']['requests'],
                            'unavailable_periods': sum(bucket['metrics'] is None for bucket in buckets)}
        require(period != '90d' or step == 86400, '90-day history is not daily')
        require(period != '365d' or step == 604800, '365-day history is not weekly')
        proof['ranges'][period] = {'bucket_seconds': step, **counts}
    repeated = api.request('GET', '/traffic-analytics')
    require(repeated['retention_days'] == policy and repeated['master_enabled'] == state['master_enabled'],
            'Range reads changed collection/retention')
    return proof


def new_base_observations(observer, seed, private, previous=None, check_nginx=False):
    api = observer.API('https://127.0.0.1:9595')
    api.login(False, 'base-owner')
    system = api.request('GET', '/server/system')
    require(system.get('boot_id') and system.get('updates', {}).get('state')
            and system.get('restart_required', {}).get('state'), 'System status is incomplete')
    require(system.get('running_kernel') == os.uname().release, 'Public kernel display differs from actual running kernel')
    state = api.request('GET', '/sites/%d/network' % seed['site_id'])
    if previous is None:
        require(not state['protection']['enabled'] and not state['wildcards'], 'Ordinary new defaults were unexpectedly activated')
        state = api.request('POST', '/sites/%d/network/redirects' % seed['site_id'],
                            {'expected_revision': state['revision'], 'source': '/base-release-preservation', 'destination': '/new', 'status': 302})
    digest = network_digest(state)
    if previous is not None:
        require(digest == previous['network_settings_sha256'], 'Ordinary public update lost new Network settings')
    domains = api.request('GET', '/sites/%d/domains' % seed['site_id'])
    require(domains and all('wildcard_ssl_state' in row for row in domains), 'New domain SSL scope status missing')
    result = {'authenticated_system_network_domain_reads': True, 'system_update_state': system['updates']['state'],
              'restart_required_state': system['restart_required']['state'], 'system_mutations_performed': False,
              'network_settings_sha256': digest, 'redirect_count': len(state['redirects']),
              'new_network_setting_preserved': previous is not None, 'no_reboot_or_tls_mutation': True,
              'running_kernel_matches_native': True,
              'traffic_history': traffic_history_observations(api, seed['site_id'])}
    if check_nginx:
        result['nginx_discovery'] = nginx_check(api, private)
    elif previous and 'nginx_discovery' in previous:
        cached = api.request('GET', '/server/nginx/updates')
        with sqlite3.connect('file:/var/lib/fluxo/fluxo.db?mode=ro', uri=True) as db:
            stored = json.loads(db.execute("SELECT value FROM system_metadata WHERE key='nginx_update_discovery'").fetchone()[0])
        sha = hashlib.sha256(json.dumps(stored, sort_keys=True).encode()).hexdigest()
        if previous['nginx_discovery']['fresh_success']:
            require(sha == previous['nginx_discovery']['saved_discovery_sha256'], 'Ordinary update rewrote saved daily discovery or its dismissal')
        result['nginx_saved_state_after_update'] = {'last_successful_check': cached['last_successful_check'], 'stale': cached['stale'],
            'daily_schedule_preserved': previous['nginx_discovery']['fresh_success'] and sha == previous['nginx_discovery']['saved_discovery_sha256'],
            'saved_discovery_sha256': sha}
    return result
