"""Observe cache creation in a new unprivileged user on a disposable ARM runner."""
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import tempfile

if os.geteuid()!=0 or platform.machine()!='aarch64' or os.environ.get('GITHUB_REF')!='refs/heads/test/arm64-bridge-acceptance' or os.environ.get('RUNNER_ENVIRONMENT')!='github-hosted':
    raise SystemExit('Dedicated hosted ARM fixture only')
user='fluxo-cache-fixture'
if Path('/home/'+user).exists():raise SystemExit('Refuse existing fixture user')
subprocess.run(['useradd','-m','-s','/bin/bash',user],check=True)
try:
    report={'root_umask':oct(os.umask(0o022))}
    program='import os,json,pathlib; p=pathlib.Path.home()/".cache/node/corepack"; p.mkdir(parents=True); print(json.dumps({"mask":oct(os.umask(0o022)),"mode":oct(p.stat().st_mode&0o777)}))'
    report['sudo_user_creation']=json.loads(subprocess.check_output(['sudo','-u',user,'python3','-c',program],text=True))
    report['sudo_root_mask']=subprocess.check_output(['sudo','python3','-c','import os;print(oct(os.umask(0o022)))'],text=True).strip()
    report['login_mask']=[s for s in Path('/etc/login.defs').read_text().splitlines() if s.startswith(('UMASK','USERGROUPS_ENAB'))]
    report['paths']={}
    for path in [Path('/home'),Path('/home/'+user),Path('/home/'+user+'/.cache'),Path('/home/'+user+'/.cache/node'),Path('/home/'+user+'/.cache/node/corepack')]:
        info=path.lstat();item={'uid':info.st_uid,'gid':info.st_gid,'mode':oct(info.st_mode&0o7777),'acl':{}}
        for name in ('access','default'):
            try:item['acl'][name]=os.getxattr(path,'system.posix_acl_'+name).hex()
            except OSError:item['acl'][name]=None
        report['paths'][str(path)]=item
    print(json.dumps(report,indent=2))
finally:
    subprocess.run(['userdel','--remove',user],check=True)
