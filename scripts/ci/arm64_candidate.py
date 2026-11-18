"""Source-candidate execution on named disposable native ARM runners.

This is explicit local-SHA transport, not public artifact/delivery acceptance.
No execution authority, receipt or recovery journal is edited by this controller.
Only sanitized controller results are exported; private raw logs stay in /root.
"""
import hashlib
import base64
import importlib.util
import io
import json
import os
from pathlib import Path
import platform
import shutil
import sqlite3
import stat
import subprocess
import sys
import tarfile
import tempfile
import time

from arm64_release_probe import runner_system_paths, isolate_ci_microsoft_source, run, sanitized, state_summary
from arm64_release_probe import download, basic_api_checks, TAG, EXPECTED_BINARY


def digest(path):
    # Jammy's native Python is 3.10; hashlib.file_digest arrived in 3.11.
    value = hashlib.sha256()
    with open(path, 'rb') as stream:
        for block in iter(lambda: stream.read(1024*1024), b''):
            value.update(block)
    return value.hexdigest()


def stock_profile_files(private, release, env, report):
    """Restore vendor limits plus a minimal stock APT profile in this fixture.

    This never runs on a customer host. CI additions are archived intact and
    restored by the controller; no production profile allows them implicitly.
    """
    limits=Path('/etc/security/limits.conf')
    info=limits.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_uid!=0 or info.st_nlink!=1 or info.st_mode&0o022:
        raise RuntimeError('Unsafe fixture limits metadata')
    wanted='efe8446a3c499818e7c5736c9494f222cb9098e8733aee91508b3631ad053b27' if release=='22.04' else '0adf64deecea56e9ed5c2229db357900218b10b76f7e69830c9d209347896206'
    report['limits_fixture_before']=digest(limits)
    if report['limits_fixture_before']!=wanted:
        version=run(['dpkg-query','-W','-f=${Version}','libpam-modules'])['stdout']
        fetched=subprocess.run(['apt-get','download','libpam-modules='+version],cwd=private,env=env,capture_output=True,timeout=120)
        if fetched.returncode:raise RuntimeError('Authenticated vendor PAM archive unavailable')
        archives=list(private.glob('libpam-modules_*_arm64.deb'))
        if len(archives)!=1:raise RuntimeError('Ambiguous vendor PAM archive')
        raw=subprocess.check_output(['dpkg-deb','--fsys-tarfile',str(archives[0])],timeout=20)
        with tarfile.open(fileobj=io.BytesIO(raw),mode='r:') as archive:
            member=archive.getmember('./etc/security/limits.conf')
            if not member.isfile() or member.size>1<<20:raise RuntimeError('Unexpected packaged limits input')
            stock=archive.extractfile(member).read()
        if hashlib.sha256(stock).hexdigest()!=wanted:raise RuntimeError('Packaged limits differs from reviewed stock profile')
        shutil.copy2(limits,private/'limits.conf.original')
        with tempfile.NamedTemporaryFile(dir=limits.parent,delete=False) as stream:
            os.fchmod(stream.fileno(),0o644);stream.write(stock);stream.flush();os.fsync(stream.fileno());staged=Path(stream.name)
        staged.replace(limits)
        report['limits_vendor_archive_sha256']=digest(archives[0])
    report['limits_fixture_after']=digest(limits)
    parts=Path('/etc/apt/apt.conf.d')
    info=parts.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid!=0 or info.st_mode&0o022:raise RuntimeError('Unsafe APT fixture directory')
    report['ci_apt_configuration_inventory']={p.name:digest(p) for p in parts.iterdir() if p.is_file() and not p.is_symlink()}
    required={'01-vendor-ubuntu':'364d5eeac5475b7dddfd629899ea88b91ed8d8e8e319c29bb9dbd6772e87ed55',
              '01autoremove':'93e7e6d2fdb36b04cb10127e3b0d1b9d19d822327fd959484639bbbd65cce004' if release=='22.04' else '35b4360d9126e5e5efc1ce42cea8a957ba42921de749ad8502922d536c9f385c'}
    approved={}
    for name,expected in required.items():
        approved[name]=(parts/name).read_bytes()
        if hashlib.sha256(approved[name]).hexdigest()!=expected:raise RuntimeError('Required vendor APT configuration differs: '+name)
    parts.rename(private/'apt.conf.d.original');parts.mkdir(mode=0o755)
    for name,raw in approved.items():
        fd=os.open(parts/name,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o644)
        with os.fdopen(fd,'wb') as stream:stream.write(raw)
    report['apt_fixture_profile']='required-vendor-snippets-only; CI additions archived'


def stock_ubuntu_sources(private, report):
    # CI's mirror+file indirection is outside the existing source profile.
    # Use the ordinary official ports URI in this stock-profile fixture only.
    report['source_fixtures']=[]
    for name in ['/etc/apt/sources.list','/etc/apt/sources.list.d/ubuntu.sources']:
        path=Path(name)
        if not path.exists():continue
        info=path.lstat()
        if not stat.S_ISREG(info.st_mode) or info.st_uid!=0 or info.st_nlink!=1 or info.st_mode&0o022:
            raise RuntimeError('Unsafe Ubuntu source fixture metadata')
        raw=path.read_bytes();stock=raw
        for value in [b'mirror+file:/etc/apt/apt-mirrors.txt',b'mirror+file:/etc/apt/apt-mirrors-security.txt']:
            stock=stock.replace(value,b'https://ports.ubuntu.com/ubuntu-ports')
        if stock==raw:continue
        archive=private/(path.name+'.source.original');shutil.copy2(path,archive)
        with tempfile.NamedTemporaryFile(dir=path.parent,delete=False) as stream:
            os.fchmod(stream.fileno(),0o644);stream.write(stock);stream.flush();os.fsync(stream.fileno());staged=Path(stream.name)
        staged.replace(path)
        report['source_fixtures'].append({'path':str(path),'before_sha256':hashlib.sha256(raw).hexdigest(),'after_sha256':digest(path)})


def root_maintenance_execution(image):
    """Run the real fixed certificate-renewal contract with no live certificates.

    This proves root task submission/execution/drainage, not live ACME renewal.
    Registration, receipts, invocation and result writes belong to Fluxo only.
    """
    if list(Path('/etc/letsencrypt/renewal').glob('*.conf')):
        raise RuntimeError('No-op root fixture requires no live renewal configuration')
    spec=importlib.util.spec_from_file_location('arm_scheduler_observer',Path('internal/services/scheduler/actual_candidate_vm_acceptance.py'))
    observer=importlib.util.module_from_spec(spec);spec.loader.exec_module(observer)
    api=observer.API('https://127.0.0.1:9595');api.login(False,'arm64-root-maintenance')
    core_path=Path('/var/lib/fluxo-system-tasks/state.json')
    def core():return json.loads(observer.protected_read(core_path))
    created=api.request('POST','/crons',{'name':'ARM64 fixed root contract','expression':'0 0 1 1 *','command':'certbot renew --quiet','user':'root'})['id']
    entry=core()['entries'].get(str(created))
    if not entry or entry['contract']['kind']!='certificate-renewal' or not entry['helper'].endswith(image):
        raise RuntimeError('Fixed root contract did not create exact guarded authority')
    observer.wait_for(lambda:core()['entries'][str(created)]['received'],190,'genuine root contract cron acknowledgement')
    known=set(core()['invocations'])
    api.request('POST',f'/crons/{created}/run')
    def completed():
        values=list(core()['invocations'].values())
        history=core_path.parent/'history'
        if history.exists():
            values += [json.loads(observer.protected_read(path)) for path in history.glob('*.json')]
        matches=[v for v in values if v['unit'] not in known and v['registration']['contract']['id']==created]
        if len(matches)>1:raise RuntimeError('Unexpected duplicate root submission')
        return matches[0] if matches and matches[0]['status'] in ('completed','failed','blocked') else None
    result=observer.wait_for(completed,100,'fixed root task terminal evidence')
    if result['status']!='completed' or not result['result_profile']:raise RuntimeError('Fixed root task failed: '+result['status'])
    group=Path('/sys/fs/cgroup/system.slice')/result['unit']
    observer.wait_for(lambda:not group.exists() or ('populated 0' in (group/'cgroup.events').read_text() and int((group/'pids.current').read_text())==0),30,'root task natural drainage')
    replay=run([entry['helper'],'system-task-execute',result['unit']],60)
    if replay['exit_code']==0:raise RuntimeError('Consumed root execution authority replayed')
    api.request('DELETE',f'/crons/{created}')
    return {'passed':True,'unit':result['unit'],'adapter':result['adapter'],'kind':'certificate-renewal','genuine_cron_receipt':True,'result_profile_bound':True,'natural_drain':True,'consumed_replay_refused':True,'live_acme':False}


def installation_invariants():
    from cryptography.hazmat.primitives.ciphers.aead import AESGCM
    cipher=AESGCM(Path('/var/lib/fluxo/encryption.key').read_bytes())
    result={}
    with sqlite3.connect('file:/var/lib/fluxo/fluxo.db?mode=ro',uri=True) as db:
        for table in ['users','sites','crons']:
            rows=db.execute('SELECT * FROM '+table+' ORDER BY id').fetchall()
            if table=='users':
                names=[row[1] for row in db.execute('PRAGMA table_info(users)')]
                normalized=[]
                for row in rows:
                    values=list(row)
                    for name in ['fluxo_db_password','fluxo_sudo_password','fluxo_mysql_password','fluxo_postgres_password','github_pat','webhook_secret']:
                        i=names.index(name);value=values[i]
                        if value in ('',None):continue
                        if not isinstance(value,str) or not value.startswith('enc:'):raise RuntimeError('Unencrypted fixture credential representation')
                        raw=base64.b64decode(value[4:],validate=True)
                        plain=cipher.decrypt(raw[:12],raw[12:],None)
                        values[i]=('authenticated-plaintext-sha256',hashlib.sha256(plain).hexdigest())
                    normalized.append(tuple(values))
                rows=normalized
            result[table]=hashlib.sha256(repr(rows).encode()).hexdigest()
    for name in ['encryption.key','tls/cert.pem','tls/key.pem','execution-readers.json']:
        result[name]=digest(Path('/var/lib/fluxo')/name)
    retained={}
    for base in [Path('/var/lib/fluxo/execution-activations'),Path('/var/lib/fluxo-scheduler/helpers'),Path('/var/lib/fluxo-system-tasks/helpers')]:
        if base.exists():
            for path in base.rglob('*'):
                if path.is_file() and not path.is_symlink():retained[str(path)]=digest(path)
    result['retained']=retained
    return result


def main():
    required = {'FLUXO_ARM64_RELEASE_PROBE':'1','GITHUB_ACTIONS':'true','RUNNER_ARCH':'ARM64',
                'RUNNER_ENVIRONMENT':'github-hosted','GITHUB_REF':'refs/heads/test/arm64-bridge-acceptance'}
    if os.geteuid()!=0 or platform.machine()!='aarch64' or any(os.environ.get(k)!=v for k,v in required.items()):
        raise SystemExit('Dedicated native ARM branch fixture only')
    if Path('/var/lib/fluxo/fluxo.db').exists() or Path('/usr/local/bin/fluxo').exists():
        raise SystemExit('Refuse existing installation')
    release = dict(line.split('=',1) for line in Path('/usr/lib/os-release').read_text().splitlines() if '=' in line)['VERSION_ID'].strip('"')
    hosts = {'22.04':'fluxo-arm64-2204-acceptance','24.04':'fluxo-arm64-2404-acceptance','26.04':'fluxo-arm64-2604-acceptance'}
    if release not in hosts: raise SystemExit('Unsupported fixture release')
    engine=os.environ.get('FLUXO_ARM64_ENGINE','cron')
    if engine not in ('cron','cronie') or (engine=='cronie' and release!='24.04'):raise SystemExit('Unsupported fixture provider')
    if engine=='cronie':hosts[release]='fluxo-arm64-2404-cronie-acceptance'
    predecessor=os.environ.get('FLUXO_ARM64_PREDECESSOR','false')
    if predecessor not in ('false','true') or (predecessor=='true' and (release!='24.04' or engine!='cron')):raise SystemExit('Unsupported predecessor fixture')
    output=Path(os.environ['FLUXO_PROBE_OUTPUT']);output.mkdir(mode=0o755,parents=True,exist_ok=False)
    private=Path(tempfile.mkdtemp(prefix='fluxo-arm64-candidate-',dir='/root'));private.chmod(0o700)
    moved=[]
    timers={u:run(['systemctl','is-active','--quiet',u])['exit_code']==0 for u in ['apt-daily.timer','apt-daily-upgrade.timer']}
    nginx_was_active=run(['systemctl','is-active','--quiet','nginx.service'])['exit_code']==0
    report={'source_commit':os.environ['GITHUB_SHA'],'release':release,'engine':engine,'predecessor':predecessor=='true','native_arm64':True,'public_delivery':False,'production_approved':False,'passed':False}
    env={'PATH':'/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin','HOME':'/root','LANG':'C.UTF-8','LC_ALL':'C.UTF-8',
         'DEBIAN_FRONTEND':'noninteractive','NEEDRESTART_MODE':'l','FLUXO_ENV':'prod','FLUXO_DATA_DIR':'/var/lib/fluxo','FLUXO_PHP_UPDATE_VM_TEST':'1'}
    def command(name,args,timeout=120,required=True):
        log=private/(name+'.log')
        with log.open('x') as stream:
            log.chmod(0o600)
            result=subprocess.run(args,env=env,stdin=subprocess.DEVNULL,stdout=stream,stderr=subprocess.STDOUT,timeout=timeout)
        (output/(name+'-sanitized.log')).write_text(sanitized(log.read_text(errors='replace'))+'\n')
        report[name]={'exit_code':result.returncode,'raw_log_sha256':digest(log)}
        if result.returncode and required: raise RuntimeError(name+' refused; inspect retained sanitized log')
        return result.returncode == 0
    try:
        report['fixture_directories']=runner_system_paths()
        # The hosted Nginx tool fixture also leaves this root-owned vendor
        # subtree writable. Preserve bytes; vendor MD5 checks still authenticate
        # every code/configuration input before maintenance qualification.
        nginx_root=Path('/usr/share/nginx')
        nginx_paths=[nginx_root,*nginx_root.rglob('*')] if nginx_root.is_dir() and not nginx_root.is_symlink() else []
        report['nginx_fixture_permissions']=[]
        for path in nginx_paths:
            info=path.lstat()
            if info.st_uid==0 and (stat.S_ISDIR(info.st_mode) or stat.S_ISREG(info.st_mode)) and info.st_mode&0o022:
                path.chmod(stat.S_IMODE(info.st_mode)&~0o022)
                report['nginx_fixture_permissions'].append({'path':str(path),'before':oct(stat.S_IMODE(info.st_mode))})
        isolate_ci_microsoft_source(private,moved)
        for timer in timers: command('pause-'+timer,['systemctl','stop',timer])
        command('hostname',['hostnamectl','set-hostname',hosts[release]])
        # GitHub appends this exact manual limits rule to a vendor-generated
        # PAM file. Establish the stock profile in this disposable fixture only;
        # preserve the original and demand both known digests, never loose text.
        pam=Path('/etc/pam.d/common-session-noninteractive');raw=pam.read_bytes()
        report['pam_fixture_before']=hashlib.sha256(raw).hexdigest()
        if report['pam_fixture_before']=='ed47c9b6671f3977d679102cb4d836ef2982613aaa47647d851fe7d695eaaead':
            if not raw.endswith(b'session required pam_limits.so\n'): raise RuntimeError('Unexpected CI PAM suffix')
            stock=raw[:-len(b'session required pam_limits.so\n')]
            if hashlib.sha256(stock).hexdigest()!='1bd1ef2b6241fc8b3c97bbaf57e5c6038c96c275e30c62ebee6eec15701e2e07': raise RuntimeError('Stock PAM reconstruction disagrees')
            shutil.copy2(pam,private/'common-session-noninteractive.original')
            with tempfile.NamedTemporaryFile(dir=pam.parent,delete=False) as stream:
                os.fchmod(stream.fileno(),0o644);stream.write(stock);stream.flush();os.fsync(stream.fileno());staged=Path(stream.name)
            staged.replace(pam)
        report['pam_fixture_after']=digest(pam)
        fallback=Path('/etc/pam.d/common-session');fallback_raw=fallback.read_bytes()
        report['pam_fallback_fixture_before']=digest(fallback)
        if fallback_raw.endswith(b'session required pam_limits.so\n'):
            shutil.copy2(fallback,private/'common-session.original')
            with tempfile.NamedTemporaryFile(dir=fallback.parent,delete=False) as stream:
                os.fchmod(stream.fileno(),0o644);stream.write(fallback_raw[:-len(b'session required pam_limits.so\n')]);stream.flush();os.fsync(stream.fileno());staged=Path(stream.name)
            staged.replace(fallback)
        report['pam_fallback_fixture_after']=digest(fallback)
        # Establish the existing stock Ubuntu PATH profile explicitly. Hosted
        # CI images inject additional tool paths; their original bytes remain
        # in the private archive and are never uploaded as environment data.
        environment=Path('/etc/environment')
        report['environment_fixture_before']=digest(environment)
        shutil.copy2(environment,private/'environment.original')
        stock_environment=b'PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/games:/usr/local/games:/snap/bin"\n'
        if hashlib.sha256(stock_environment).hexdigest()!='3d88795b8d42b87bbbf91f8d30dae43b6db9ecd4b1eb93a883062352c902ff5d': raise RuntimeError('Stock environment profile differs')
        with tempfile.NamedTemporaryFile(dir=environment.parent,delete=False) as stream:
            os.fchmod(stream.fileno(),0o644);stream.write(stock_environment);stream.flush();os.fsync(stream.fileno());staged=Path(stream.name)
        staged.replace(environment)
        report['environment_fixture_after']=digest(environment)
        command('apt-refresh',['apt-get','-o','APT::Update::Error-Mode=any','update'],180)
        apt_version={'22.04':'2.4.15ubuntu0.1','24.04':'2.8.4ubuntu0.1','26.04':'3.2.1ubuntu0.1'}[release]
        command('apt-security-baseline',['apt-get','install','-y','apt='+apt_version],180)
        if release=='24.04':
            # Current reviewed security baseline, not the stale runner libc.
            command('security-baseline',['apt-get','install','-y','libc6=2.39-0ubuntu8.12','libc-bin=2.39-0ubuntu8.12','libc6-dev=2.39-0ubuntu8.12','libc-dev-bin=2.39-0ubuntu8.12'],240)
        # Restart only an empty fixture cron hierarchy after package updates.
        deadline=time.monotonic()+90
        while True:
            pid=run(['systemctl','show','cron.service','--property=MainPID','--value'])['stdout']
            group=Path('/sys/fs/cgroup/system.slice/cron.service/cgroup.procs')
            pids=group.read_text().split() if group.exists() else []
            if pids==[pid] or not pids: break
            if time.monotonic()>deadline: raise RuntimeError('Wait for natural fixture cron drainage')
            time.sleep(.5)
        if engine=='cronie':
            command('cronie-install',['apt-get','install','-y','cronie=1.7.1-1'],180)
        command('cron-restart',['systemctl','restart',engine+'.service'])
        stock_profile_files(private,release,env,report)
        stock_ubuntu_sources(private,report)
        # Hosted CI tools install unrelated Python packages into Certbot's
        # global search path. Archive them for the stock vendor-only fixture;
        # this does not qualify third-party Python code or change its guard.
        python_local=Path('/usr/local/lib/python'+str(sys.version_info.major)+'.'+str(sys.version_info.minor)+'/dist-packages')
        if python_local.exists() and any(python_local.iterdir()):
            info=python_local.lstat()
            if not stat.S_ISDIR(info.st_mode) or info.st_uid!=0 or info.st_mode&0o022:raise RuntimeError('Unsafe Python fixture directory')
            python_local.rename(private/'python-local.original');python_local.mkdir(mode=0o755)
            report['python_local_fixture']=str(python_local)
        for name in ['fluxo','scheduler.test','maintenance.test','executiondomain.test']:
            target=private/name;shutil.copyfile(Path(os.environ['FLUXO_ARM64_BUILD'])/name,target);target.chmod(0o700)
        image=digest(private/'fluxo');report['candidate_sha256']=image;env['FLUXO_LOCAL_BINARY_SHA256']=image
        before=None
        if predecessor=='true':
            from arm64_release_probe import EXPECTED_INSTALLER
            installer=private/'published47-install.sh';download('install.sh',installer,EXPECTED_INSTALLER)
            env.pop('FLUXO_LOCAL_BINARY_SHA256');env['FLUXO_VERSION']=TAG
            command('published47-installer',['bash',str(installer),'--db-engine=none','--no-redis','--no-node','--no-python','--no-harden-ssh'],900)
            if digest('/usr/local/bin/fluxo')!=EXPECTED_BINARY:raise RuntimeError('Published predecessor bytes differ')
            command('predecessor-nginx-start',['systemctl','start','nginx.service'])
            checks=basic_api_checks('https://127.0.0.1:9595',Path('/var/lib/fluxo'))
            if not checks.get('passed'):raise RuntimeError('Published predecessor application fixture failed')
            report['predecessor_application']=checks
            before=installation_invariants()
            env.pop('FLUXO_VERSION');env['FLUXO_LOCAL_BINARY_SHA256']=image
        command('installer',['bash','install.sh','--local-binary='+str(private/'fluxo'),'--db-engine=none','--no-redis','--no-node','--no-python','--no-harden-ssh'],900)
        if digest('/usr/local/bin/fluxo')!=image: raise RuntimeError('Installed image differs')
        if before is not None:
            after=installation_invariants()
            for key in before:
                if key=='retained':
                    if any(after[key].get(path)!=value for path,value in before[key].items()):raise RuntimeError('Upgrade changed retained archive/helper bytes')
                elif before[key]!=after[key]:raise RuntimeError('Upgrade changed installation invariant: '+key)
            report['published47_upgrade']={'passed':True,'predecessor_sha256':EXPECTED_BINARY,'users_sites_crons_keys_tls_floor_and_archives_preserved':True}
        if release=='22.04':
            command('native-domain-lifetime',[str(private/'executiondomain.test'),'-test.run=^TestVMPersistent249DomainLifetime$','-test.v'],240,required=False)
        command('native-apt-fences',[str(private/'maintenance.test'),'-test.run=^TestVMAPTNoQueueArchiveFence$','-test.v'],180,required=False)
        if release=='26.04':
            command('native-apt-solver3-fences',[str(private/'maintenance.test'),'-test.run=^TestVMAPTNoQueueStockSolver3$','-test.v'],180,required=False)
        command('native-loader',[str(private/'scheduler.test'),'-test.run=^TestVMARM64LoaderClosure$','-test.v'],180)
        command('native-loader-refusals',[str(private/'scheduler.test'),'-test.run=^TestVMARM64LoaderRefusals$','-test.v'],180)
        command('native-bootstrap',[str(private/'executiondomain.test'),'-test.run=^TestVMNonLeaderSchedulerBootstrapABI$','-test.v'],180)
        maintenance_passed=command('native-maintenance',[str(private/'maintenance.test'),'-test.run=^TestVMARM64MaintenanceProfiles$','-test.v'],180,required=False)
        command('nginx-start',['systemctl','start','nginx.service'])
        command('guarded-scheduler',['python3','internal/services/scheduler/actual_candidate_vm_acceptance.py','--expected-sha256',image],420)
        reports=sorted(Path('/var/lib').glob('fluxo-candidate-cli-*/result.json'))
        if len(reports)!=1: raise RuntimeError('Missing unique guarded Scheduler result')
        report['guarded_scheduler']=json.loads(reports[0].read_text())
        report['guarded_root_maintenance']=root_maintenance_execution(image)
        report['execution_state']={name:state_summary(Path(path)) for name,path in {'floor':'/var/lib/fluxo/execution-readers.json','scheduler':'/var/lib/fluxo-scheduler/state.json','tasks':'/var/lib/fluxo-system-tasks/state.json'}.items()}
        report['passed']=report['guarded_scheduler']['passed'] is True and maintenance_passed and report['native-apt-fences']['exit_code']==0
    except Exception as error:
        report['error']=sanitized(str(error))
        # Status counts only: no operation arguments, logs or credentials.
        database=Path('/var/lib/fluxo/fluxo.db')
        if database.exists():
            with sqlite3.connect('file:'+str(database)+'?mode=ro',uri=True) as db:
                counts={}
                for table,column in [('php_update_operations','status'),('node_update_operations','status'),('php_update_generations','state'),('node_update_generations','state')]:
                    if db.execute("SELECT 1 FROM sqlite_master WHERE type='table' AND name=?",(table,)).fetchone():
                        counts[table]=db.execute('SELECT '+column+',COUNT(*) FROM '+table+' GROUP BY '+column).fetchall()
                report['operation_status_counts']=counts
        limits=Path('/etc/security/limits.d')
        if limits.is_dir():report['limits_fragment_metadata']={p.name:digest(p) for p in limits.iterdir() if p.is_file() and not p.is_symlink()}
    finally:
        run(['systemctl','stop','fluxo.service'],60)
        if not nginx_was_active:run(['systemctl','stop','nginx.service'],60)
        for target,archive in [(Path('/etc/environment'),private/'environment.original'),(Path('/etc/pam.d/common-session-noninteractive'),private/'common-session-noninteractive.original'),(Path('/etc/pam.d/common-session'),private/'common-session.original'),(Path('/etc/security/limits.conf'),private/'limits.conf.original')]:
            if archive.exists():
                shutil.copy2(archive,target)
                if digest(archive)!=digest(target):report['passed']=False
        if (private/'apt.conf.d.original').exists():
            Path('/etc/apt/apt.conf.d').rename(private/'apt.conf.d.tested')
            (private/'apt.conf.d.original').rename('/etc/apt/apt.conf.d')
        for item in report.get('source_fixtures',[]):
            path=Path(item['path']);shutil.copy2(private/(path.name+'.source.original'),path)
            if digest(path)!=item['before_sha256']:report['passed']=False
        if report.get('python_local_fixture'):
            path=Path(report['python_local_fixture']);path.rename(private/'python-local.tested');(private/'python-local.original').rename(path)
        for original,archive in moved:
            if original.exists():report['passed']=False
            else:archive.rename(original)
        for unit,active in timers.items():run(['systemctl','start' if active else 'stop',unit])
        report['timers_restored']=all((run(['systemctl','is-active','--quiet',unit])['exit_code']==0)==active for unit,active in timers.items())
        report['passed']=report['passed'] and report['timers_restored']
        (output/'report.json').write_text(json.dumps(report,indent=2)+'\n')
        print(json.dumps({'passed':report['passed'],'release':release,'error':report.get('error')}))
    return 0 if report['passed'] else 1

if __name__=='__main__':raise SystemExit(main())
