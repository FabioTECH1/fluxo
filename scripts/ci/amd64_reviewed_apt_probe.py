"""Cheap signature/hash/installability prerequisite; never invokes old installer."""
import json
import os
from pathlib import Path
import platform
import signal
import subprocess
import tempfile
from amd64_reviewed_apt import ReviewedAPT

if os.geteuid()!=0 or platform.machine()!='x86_64' or os.environ.get('GITHUB_REF')!='refs/heads/test/amd64-v040-cronie-upgrade-20261118' or os.environ.get('RUNNER_ENVIRONMENT')!='github-hosted':raise SystemExit('Requires own isolated hosted AMD64 job')
output=Path(os.environ['FLUXO_NATIVE_OUTPUT']);output.mkdir(mode=0o755,parents=True,exist_ok=True)
private=Path(tempfile.mkdtemp(prefix='fluxo-apt-proof-',dir='/root'));private.chmod(0o700)
work=Path(tempfile.mkdtemp(prefix='fluxo-reviewed-apt-',dir='/var/tmp'));work.chmod(0o755)
report={'passed':False,'old_installer_run':False,'candidate_run':False,'packages_installed':False,
    'private_logs':str(private),'owned_public_package_cache':str(work)}
def save():(output/'report.json').write_text(json.dumps(report,indent=2,sort_keys=True)+'\n')
env={'PATH':'/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin','HOME':'/root','LANG':'C.UTF-8','LC_ALL':'C.UTF-8','DEBIAN_FRONTEND':'noninteractive'}
def invoke(label,args,timeout,cwd):
    report['stage']=label;save();print(json.dumps({'stage':label}),flush=True)
    proc=subprocess.Popen(args,cwd=cwd,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,start_new_session=True)
    try:raw,_=proc.communicate(timeout=timeout)
    except subprocess.TimeoutExpired:
        os.killpg(proc.pid,signal.SIGKILL);raw,_=proc.communicate(timeout=10)
        (private/(label+'.log')).write_bytes(raw);raise RuntimeError('Bounded APT prerequisite timed out: '+label)
    (private/(label+'.log')).write_bytes(raw)
    if proc.returncode:
        selected=[line for line in raw.decode(errors='replace').splitlines() if line.startswith(('E:','W:','gpgv: BAD','gpgv: No public key'))]
        report['selected_diagnostics']=selected[-10:];save();raise RuntimeError('APT prerequisite failed at '+label)
    return raw.decode(errors='replace')
try:
    details={};report['reviewed_apt']=details
    proof=ReviewedAPT(work,invoke,details);proof.prove();report['passed']=True
except Exception as error:report['error']=str(error) if isinstance(error,RuntimeError) else type(error).__name__
finally:save();print(json.dumps({'passed':report['passed'],'stage':report.get('stage'),'error':report.get('error')}))
if not report['passed']:raise SystemExit(1)
