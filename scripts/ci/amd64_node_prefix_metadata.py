"""Fast shared fixture prerequisite: real root and fluxo-owned cache creation."""
import json
import os
from pathlib import Path
import platform
import pwd
import stat
import subprocess
import tempfile
from amd64_fixture_metadata import PreparedRoots,public_metadata

required={'GITHUB_ACTIONS':'true','RUNNER_ENVIRONMENT':'github-hosted','RUNNER_ARCH':'X64',
          'GITHUB_REF':'refs/heads/test/amd64-v040-cronie-upgrade-20261118'}
if os.geteuid()!=0 or platform.machine()!='x86_64' or any(os.environ.get(k)!=v for k,v in required.items()):
    raise SystemExit('Requires own isolated hosted AMD64 workflow')
for name in ('/opt/fluxo','/home/fluxo','/var/lib/fluxo','/usr/local/bin/fluxo'):
    if Path(name).exists() or Path(name).is_symlink():raise SystemExit('Refuse preexisting fixture data')
try:pwd.getpwnam('fluxo')
except KeyError:pass
else:raise SystemExit('Refuse preexisting fluxo account')
output=Path(os.environ['FLUXO_NATIVE_OUTPUT']);output.mkdir(mode=0o755,parents=True,exist_ok=True)
private=Path(tempfile.mkdtemp(prefix='fluxo-systematic-metadata-',dir='/root'));private.chmod(0o700)
parents=PreparedRoots(private);old_umask=os.umask(0o022)
report={'probe':'systematic-parent-metadata-and-native-user-creation','old_installer_run':False,
        'authority_created':False,'passed':False,'fixtures_restored':False,
        'private_original_acl_directory':str(private)}
env={'PATH':'/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin','LANG':'C.UTF-8'}
try:
    parents.prepare();report['parents_before']=parents.before;report['parents_prepared']=parents.prepared
    root=Path('/opt/fluxo/node-toolchain/bin');root.mkdir(parents=True,mode=0o755)
    child=root/'new-vendor-subdirectory';child.mkdir()
    report['root_prefix']=[public_metadata(path) for path in (root.parent.parent,root.parent,root,child)]
    subprocess.run(['useradd','fluxo','-m','-s','/bin/bash','-G','www-data'],env=env,
        stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=15,check=True)
    account=pwd.getpwnam('fluxo');Path(account.pw_dir).chmod(0o755)
    code="import os;from pathlib import Path;os.umask(0o022);p=Path('/home/fluxo/.cache/node/corepack');p.mkdir(parents=True);(p/'new-vendor-subdirectory').mkdir()"
    subprocess.run(['runuser','-u','fluxo','--','/usr/bin/python3','-c',code],env=env,
        stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=15,check=True)
    report['native_fixture_user']={'uid':account.pw_uid,'gid':account.pw_gid,'home':account.pw_dir}
    report['fluxo_cache_ancestry']=[public_metadata(Path(name)) for name in (
        '/home/fluxo','/home/fluxo/.cache','/home/fluxo/.cache/node','/home/fluxo/.cache/node/corepack',
        '/home/fluxo/.cache/node/corepack/new-vendor-subdirectory')]
    def safe(m,uid,gid):
        return m['type']=='directory' and m['uid']==uid and m['gid']==gid and int(m['mode'],8)&0o022==0 and m['default_acl_sha256'] is None and m['access_acl_sha256'] is None
    report['root_creation_safe']=all(safe(m,0,0) for m in report['root_prefix'])
    report['fluxo_cache_creation_safe']=all(safe(m,account.pw_uid,account.pw_gid) for m in report['fluxo_cache_ancestry'])
    report['authority_created']=any(Path(name).exists() for name in ('/var/lib/fluxo-scheduler/state.json','/var/lib/fluxo-system-tasks/state.json'))
    report['passed']=report['root_creation_safe'] and report['fluxo_cache_creation_safe'] and not report['authority_created']
except Exception as error:
    report['error']=type(error).__name__
finally:
    parents.restore();os.umask(old_umask)
    report['parents_restored']=parents.restored;report['fixtures_restored']=parents.preserved
    report['owned_paths_retained_until_ephemeral_runner_disposal']=['/opt/fluxo','/home/fluxo',str(private)]
    (output/'report.json').write_text(json.dumps(report,indent=2,sort_keys=True)+'\n')
    print(json.dumps(report,sort_keys=True))
if not report['passed'] or not report['fixtures_restored']:raise SystemExit(1)
