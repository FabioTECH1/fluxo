#!/usr/bin/env python3
"""Focused real WordPress checks for an explicitly disposable hosted fixture.

All credentials, wp-config contents and process output remain root-private.
Only allowlisted observations go into report.json. Never run on a user server.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import pwd
import re
import selectors
import shutil
import subprocess
import tempfile
import time
import traceback
import urllib.request

import base_candidate as base


def require(value, message):
    if not value:
        raise RuntimeError(message)


def wait_for(test, seconds, label):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        result = test()
        if result:
            return result
        time.sleep(.25)
    raise RuntimeError('Bounded wait failed: ' + label)


def wp(root, *args, stdin=None):
    result = subprocess.run(['runuser', '-u', 'fluxo', '--', '/usr/bin/php8.4',
        '/usr/local/bin/wp', '--path=' + str(root), *args], input=stdin,
        text=True, capture_output=True, timeout=90)
    require(result.returncode == 0, 'Private fixture WP-CLI command failed: ' + args[0])
    return result.stdout.strip()


def marker(path):
    try:
        return json.loads(path.read_text())
    except (FileNotFoundError, json.JSONDecodeError):
        return None


def begin(api, private, command):
    command('wordpress-db-packages', ['apt-get', 'install', '-y', 'mariadb-server'], 300)
    command('wordpress-db-start', ['systemctl', 'start', 'mariadb.service'], 90)
    db = api.request('POST', '/databases', {'name': 'fluxo_wp_native', 'engine': 'mysql', 'username': 'wp_native'})
    site = api.request('POST', '/sites', {'domain': 'wordpress-native.invalid', 'app_type': 'wordpress',
        'deployment_strategy': 'standard', 'php_version': '8.4', 'web_root': '/public',
        'database_name': db['name'], 'database_user': db['username'],
        'database_password': db['password'], 'db_engine': 'mysql'})
    root = Path(site['path']) / 'public'
    hosts = Path('/etc/hosts')
    (private / 'wordpress-hosts.original').write_bytes(hosts.read_bytes())
    with hosts.open('a') as stream:
        stream.write('\n127.0.0.1 wordpress-native.invalid\n')
    password = os.urandom(24).hex()
    wp(root, 'core', 'install', '--url=http://wordpress-native.invalid', '--title=Native WordPress fixture',
       '--admin_user=fixture_owner', '--admin_email=fixture@example.invalid', '--skip-email',
       '--prompt=admin_password', stdin=password + '\n')
    before = (root / 'wp-config.php').read_bytes()
    require(b'DISABLE_WP_CRON' not in before, 'New WordPress site unexpectedly disables visitor scheduling')
    path = '/sites/%d/wordpress-cron' % site['id']
    initial = api.request('GET', path)
    require(initial['enabled'] is False and initial['phase'] == 'off', 'WordPress Cron must default off')
    result = Path(site['path']) / '.native-event.json'
    runs = Path(site['path']) / '.native-cron-runs.json'
    plugin = root / 'wp-content/mu-plugins/fluxo-native-observer.php'
    plugin.parent.mkdir(exist_ok=True)
    plugin.write_text("<?php\n"
        "if (basename($_SERVER['SCRIPT_FILENAME'] ?? '') === 'wp-cron.php') {\n"
        " $p = " + json.dumps(str(runs)) + "; $v = json_decode(@file_get_contents($p),true) ?: ['count'=>0];\n"
        " $v['count']++; $v['sapi']=PHP_SAPI; $v['uid']=posix_geteuid(); $v['php']=PHP_VERSION;\n"
        " file_put_contents($p,json_encode($v),LOCK_EX); }\n"
        "add_action('fluxo_native_tick', function($label) {\n"
        " $p = " + json.dumps(str(result)) + ";\n"
        " file_put_contents($p,json_encode(['label'=>$label,'phase'=>'started','time'=>microtime(true)]),LOCK_EX);\n"
        " if ($label === 'guarded') sleep(8);\n"
        " file_put_contents($p,json_encode(['label'=>$label,'phase'=>'finished','time'=>microtime(true),'sapi'=>PHP_SAPI,'uid'=>posix_geteuid()]),LOCK_EX);\n"
        "});\n")
    owner = pwd.getpwnam('fluxo')
    os.chown(plugin.parent, owner.pw_uid, owner.pw_gid)
    os.chown(plugin, owner.pw_uid, owner.pw_gid)
    wp(root, 'eval', "wp_schedule_single_event(time()-2, 'fluxo_native_tick', ['guarded']);")
    enabled = api.request('PUT', path, {'enabled': True})
    require(enabled['enabled'] and enabled['phase'] == 'enabled', 'WordPress Cron did not enable')
    again = api.request('PUT', path, {'enabled': True})
    require(again['cron_id'] == enabled['cron_id'], 'Repeated enable created a duplicate job')
    wait_for(lambda: (v if (v := marker(result)) and v['phase'] == 'started' else None), 95,
             'genuine cron starts WordPress event')
    return {'path': path, 'root': root, 'site_id': site['id'], 'cron_id': enabled['cron_id'],
        'before': before, 'result': result, 'runs': runs, 'version': wp(root, 'core', 'version')}


def maintenance(state):
    environment = {**os.environ, 'FLUXO_ENV': 'prod', 'FLUXO_DATA_DIR': '/var/lib/fluxo'}
    process = subprocess.Popen(['/usr/local/bin/fluxo', 'scheduler-installer-lease', str(os.getpid())],
        stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True, env=environment)
    lease = None
    try:
        selector = selectors.DefaultSelector()
        selector.register(process.stdout, selectors.EVENT_READ)
        require(selector.select(115), 'Installer maintenance lease did not report readiness')
        line = process.stdout.readline().strip()
        require(re.fullmatch(r'READY [0-9a-f-]{36}', line), 'Real installer maintenance refused')
        lease = line.split()[1]
        completed = marker(state['result'])
        require(completed and completed['phase'] == 'finished', 'Maintenance did not wait for natural WordPress completion')
        require(completed['sapi'] == 'cli' and completed['uid'] == pwd.getpwnam('fluxo').pw_uid,
                'Guarded event did not execute as unprivileged CLI')
        before = marker(state['runs'])['count']
        time.sleep(65)  # Cross a genuine every-minute cron boundary under the real lease.
        require(process.poll() is None and marker(state['runs'])['count'] == before,
                'WordPress payload launched during actual maintenance exclusion')
    finally:
        if lease:
            released = subprocess.run(['/usr/local/bin/fluxo', 'scheduler-installer-release', lease],
                env=environment, capture_output=True, timeout=30)
            require(released.returncode == 0, 'Maintenance release failed')
            process.wait(timeout=30)
    require(process.returncode == 0, 'Maintenance lease exited unsuccessfully')
    wait_for(lambda: marker(state['runs'])['count'] > before, 85, 'WordPress cron resumes after lease release')
    return {'natural_event_drain': True, 'actual_installer_lease': True,
        'no_payload_during_65_second_hold': True, 'genuine_cron_resumed': True}


def finish(api, state):
    status = api.request('GET', state['path'])
    require(status['enabled'] and status['phase'] == 'enabled' and status['cron_id'] == state['cron_id'],
            'Ordinary update lost the enabled WordPress job or changed its identity')
    old_runs = marker(state['runs'])['count']
    wait_for(lambda: marker(state['runs'])['count'] > old_runs, 85, 'genuine cron after ordinary update')
    disabled = api.request('PUT', state['path'], {'enabled': False})
    require(not disabled['enabled'] and disabled['phase'] == 'off', 'WordPress Cron did not disable')
    require((state['root'] / 'wp-config.php').read_bytes() == state['before'],
            'Disabling did not restore exact original config bytes')
    state['result'].unlink()
    wp(state['root'], 'eval', "wp_schedule_single_event(time()-2, 'fluxo_native_tick', ['visitor']);")
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open('http://wordpress-native.invalid/', timeout=20) as response:
        require(response.status == 200, 'Visitor request did not reach WordPress')
        response.read(65536)
    event = wait_for(lambda: (v if (v := marker(state['result'])) and v['phase'] == 'finished' else None),
                     40, 'visitor-triggered WordPress event after disabling')
    require(event['label'] == 'visitor' and event['sapi'] == 'fpm-fcgi',
            'Visitor scheduling was not restored through the actual HTTP/PHP-FPM path')
    return {'ordinary_update_enabled_state_preserved': True, 'genuine_cron_after_update': True,
        'disable_exact_config_restoration': True, 'actual_visitor_trigger_restored': True,
        'wordpress_version': state['version']}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--build-dir', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    require(os.geteuid() == 0 and os.environ.get('FLUXO_WORDPRESS_CRON_ACCEPTANCE') == '1'
        and os.environ.get('GITHUB_ACTIONS') == 'true'
        and os.environ.get('RUNNER_ENVIRONMENT') == 'github-hosted'
        and os.environ.get('GITHUB_REF') == 'refs/heads/test/wordpress-cron-release-acceptance'
        and platform.machine() == 'x86_64', 'Explicit disposable hosted WordPress acceptance only')
    require(not any(Path(p).exists() for p in ('/var/lib/fluxo', '/usr/local/bin/fluxo', '/home/fluxo')),
            'Existing installation is forbidden')
    require('VERSION_ID="24.04"' in Path('/etc/os-release').read_text(), 'Ubuntu 24.04 required')
    manifest = base.verify_build(args.build_dir, 'amd64')
    private = Path(tempfile.mkdtemp(prefix='fluxo-wp-native-', dir='/root'))
    private.chmod(0o700)
    args.output.mkdir(parents=True, exist_ok=False)
    report = {'passed': False, 'candidate': manifest, 'steps': {}, 'scope': 'Real WordPress source candidate; not exact-public release acceptance'}
    env = {'PATH': '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin', 'HOME': '/root',
        'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8', 'DEBIAN_FRONTEND': 'noninteractive', 'NEEDRESTART_MODE': 'l',
        'FLUXO_ENV': 'prod', 'FLUXO_DATA_DIR': '/var/lib/fluxo', 'FLUXO_PHP_UPDATE_VM_TEST': '1',
        'FLUXO_LOCAL_BINARY_SHA256': manifest['binary_sha256']}
    def command(name, argv, timeout=120, cwd=None):
        report['stage'] = name
        print(json.dumps({'stage': name}), flush=True)
        with (private / (name + '.log')).open('w') as stream:
            result = subprocess.run(argv, env=env, cwd=cwd, stdout=stream, stderr=subprocess.STDOUT, timeout=timeout)
        report['steps'][name] = result.returncode
        require(result.returncode == 0, 'Private fixture command failed: ' + name)
    fixture = base.HostedFixture(private, report, env, command, 'amd64', '24.04', 'cron')
    try:
        fixture.prepare()
        binary = private / 'fluxo'
        shutil.copyfile(args.build_dir / 'fluxo', binary)
        binary.chmod(0o700)
        installer = private / 'install.sh'
        shutil.copyfile('install.sh', installer)
        install_args = ['bash', str(installer), '--local-binary=' + str(binary), *base.FLAGS]
        command('candidate-install', install_args, 1200)
        base.verify_installed(manifest['binary_sha256'], env)
        command('native-nginx-start', ['systemctl', 'start', 'nginx.service'])
        observer = base.observer_module()
        api = observer.API('https://127.0.0.1:9595')
        api.login(False, 'wordpress-native-owner')
        state = begin(api, private, command)
        report['maintenance'] = maintenance(state)
        command('ordinary-candidate-update', install_args, 1200)
        base.verify_installed(manifest['binary_sha256'], env)
        api = observer.API('https://127.0.0.1:9595')
        api.login(False, 'wordpress-native-owner')
        report['wordpress'] = finish(api, state)
        report['passed'] = True
    except Exception as error:
        report['error_type'] = type(error).__name__
        report['error'] = str(error)[:500]
        report['frames'] = [{'file': Path(f.filename).name, 'line': f.lineno, 'function': f.name}
                            for f in traceback.extract_tb(error.__traceback__)]
        failed_log = private / (report.get('stage', '') + '.log')
        if failed_log.is_file():
            report['failure_diagnostic'] = base.sanitized_diagnostic(failed_log.read_text(errors='replace'))[-4000:]
    finally:
        if (private / 'wordpress-hosts.original').exists():
            Path('/etc/hosts').write_bytes((private / 'wordpress-hosts.original').read_bytes())
        try:
            fixture.cleanup()
        except Exception as error:
            report['cleanup_error_type'] = type(error).__name__
            report['passed'] = False
        (args.output / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    return 0 if report['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
