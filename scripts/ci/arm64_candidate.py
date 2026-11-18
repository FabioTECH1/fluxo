"""Source-candidate execution on named disposable native ARM runners.

This is explicit local-SHA transport, not public artifact/delivery acceptance.
No execution authority, receipt or recovery journal is edited by this controller.
Only sanitized controller results are exported; private raw logs stay in /root.
"""
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import tempfile
import time

from arm64_release_probe import runner_system_paths, isolate_ci_microsoft_source, run, sanitized, state_summary


def digest(path):
    # Jammy's native Python is 3.10; hashlib.file_digest arrived in 3.11.
    value = hashlib.sha256()
    with open(path, 'rb') as stream:
        for block in iter(lambda: stream.read(1024*1024), b''):
            value.update(block)
    return value.hexdigest()


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
    output=Path(os.environ['FLUXO_PROBE_OUTPUT']);output.mkdir(mode=0o755,parents=True,exist_ok=False)
    private=Path(tempfile.mkdtemp(prefix='fluxo-arm64-candidate-',dir='/root'));private.chmod(0o700)
    moved=[]
    timers={u:run(['systemctl','is-active','--quiet',u])['exit_code']==0 for u in ['apt-daily.timer','apt-daily-upgrade.timer']}
    report={'source_commit':os.environ['GITHUB_SHA'],'release':release,'native_arm64':True,'public_delivery':False,'production_approved':False,'passed':False}
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
        command('cron-restart',['systemctl','restart','cron.service'])
        for name in ['fluxo','scheduler.test','maintenance.test','executiondomain.test']:
            target=private/name;shutil.copyfile(Path(os.environ['FLUXO_ARM64_BUILD'])/name,target);target.chmod(0o700)
        image=digest(private/'fluxo');report['candidate_sha256']=image;env['FLUXO_LOCAL_BINARY_SHA256']=image
        command('installer',['bash','install.sh','--local-binary='+str(private/'fluxo'),'--db-engine=none','--no-redis','--no-node','--no-python','--no-harden-ssh'],900)
        if digest('/usr/local/bin/fluxo')!=image: raise RuntimeError('Installed image differs')
        if release=='22.04':
            command('native-domain-lifetime',[str(private/'executiondomain.test'),'-test.run=^TestVMPersistent249DomainLifetime$','-test.v'],240,required=False)
        command('native-apt-fences',[str(private/'maintenance.test'),'-test.run=^TestVMAPTNoQueueArchiveFence$','-test.v'],180,required=False)
        command('native-loader',[str(private/'scheduler.test'),'-test.run=^TestVMARM64LoaderClosure$','-test.v'],180)
        command('native-loader-refusals',[str(private/'scheduler.test'),'-test.run=^TestVMARM64LoaderRefusals$','-test.v'],180)
        command('native-bootstrap',[str(private/'executiondomain.test'),'-test.run=^TestVMNonLeaderSchedulerBootstrapABI$','-test.v'],180)
        maintenance_passed=command('native-maintenance',[str(private/'maintenance.test'),'-test.run=^TestVMARM64MaintenanceProfiles$','-test.v'],180,required=False)
        command('nginx-start',['systemctl','start','nginx.service'])
        command('guarded-scheduler',['python3','internal/services/scheduler/actual_candidate_vm_acceptance.py','--expected-sha256',image],420)
        reports=sorted(Path('/var/lib').glob('fluxo-candidate-cli-*/result.json'))
        if len(reports)!=1: raise RuntimeError('Missing unique guarded Scheduler result')
        report['guarded_scheduler']=json.loads(reports[0].read_text())
        report['execution_state']={name:state_summary(Path(path)) for name,path in {'floor':'/var/lib/fluxo/execution-readers.json','scheduler':'/var/lib/fluxo-scheduler/state.json','tasks':'/var/lib/fluxo-system-tasks/state.json'}.items()}
        report['passed']=report['guarded_scheduler']['passed'] is True and maintenance_passed and report['native-apt-fences']['exit_code']==0
    except Exception as error:
        report['error']=sanitized(str(error))
    finally:
        run(['systemctl','stop','fluxo.service'],60)
        for target,archive in [(Path('/etc/environment'),private/'environment.original'),(Path('/etc/pam.d/common-session-noninteractive'),private/'common-session-noninteractive.original')]:
            if archive.exists():
                shutil.copy2(archive,target)
                if digest(archive)!=digest(target):report['passed']=False
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
