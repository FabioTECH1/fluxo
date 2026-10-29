#!/usr/bin/python3
"""Explicit v0.4.41 -> v0.4.42 Node recovery worker staging.

Called only after the companion shell script verifies the official release's
checksum and signed provenance. It replaces the on-disk panel executable, not
its running process; the existing API's normal launch path then uses the fixed
worker. No database, runtime, hold, service, cron or package mutation is made.
"""
import contextlib
import fcntl
import hashlib
import json
import os
import pathlib
import sqlite3
import stat
import subprocess
import sys
import tempfile
import time
import uuid

INSTALLED = '/usr/local/bin/fluxo'
LIFECYCLE = '/run/lock/fluxo-php-lifecycle.lock'
OLD_SHA = {
 'x86_64': 'a0b52573d8dcdcd30469fcafb411403dafa985a879b7f010863475c76a7c6876',
 'aarch64': '794ace2c940c891ebd9f7928afa4ce8c5ae0916041a705f22542b45d047f98e3',
}

NEW_SHA = {
 'x86_64': 'cdf2ec57a5aa7273c542ac2d1a9658fe3638932ad3a963ce684e5b7e1823520a',
 'aarch64': '47a763b587d0a0880959c8f00866f95ebd78e83356de84a4563b8ba00408666f',
}


def identity(s):
 return (s.st_dev,s.st_ino,s.st_mode,s.st_uid,s.st_gid,s.st_nlink,s.st_size,s.st_mtime_ns,s.st_ctime_ns)


def regular(path, owner=0):
 s=os.lstat(path)
 if not stat.S_ISREG(s.st_mode) or s.st_uid!=owner or s.st_gid!=owner or s.st_nlink!=1 or s.st_mode&0o022:
  raise RuntimeError('Unsafe regular file: '+str(path))
 return s


def trusted_directory(path):
 path=pathlib.Path(path)
 if not path.is_absolute() or os.path.normpath(path)!=str(path):raise RuntimeError('Noncanonical trusted path')
 for item in (path,*path.parents):
  s=os.lstat(item)
  if not stat.S_ISDIR(s.st_mode) or s.st_uid!=0 or s.st_gid!=0 or s.st_mode&0o022:raise RuntimeError('Unsafe directory: '+str(item))


def digest_file(path):
 before=regular(path)
 fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
 with os.fdopen(fd,'rb') as source:
  if identity(os.fstat(source.fileno()))!=identity(before):raise RuntimeError('File replaced before read')
  digest=hashlib.file_digest(source,'sha256').hexdigest()
  if identity(os.fstat(source.fileno()))!=identity(before) or identity(os.lstat(path))!=identity(before):raise RuntimeError('File changed while read')
 return digest


def run(*args):
 return subprocess.check_output(args,text=True,timeout=10).strip()


def process_start(pid):
 return pathlib.Path('/proc',str(pid),'stat').read_text().rsplit(')',1)[1].split()[19]


def panel_snapshot():
 props=dict(line.split('=',1) for line in run('/usr/bin/systemctl','show','fluxo.service','--property=MainPID,ControlPID,ActiveState,ControlGroup,Delegate,User,ExecStart').splitlines() if '=' in line)
 pid=int(props.get('MainPID','0'))
 if pid<=1 or props.get('ControlPID')!='0' or props.get('ActiveState')!='active' or props.get('ControlGroup')!='/system.slice/fluxo.service' or props.get('Delegate')!='no' or props.get('User')!='root' or not props.get('ExecStart','').startswith('{ path=/usr/local/bin/fluxo ; argv[]=/usr/local/bin/fluxo ;'):
  raise RuntimeError('Requires the standard active root Fluxo service')
 start=process_start(pid)
 exe='/proc/'+str(pid)+'/exe'
 expected=OLD_SHA.get(os.uname().machine)
 with open(exe,'rb') as f:old=hashlib.file_digest(f,'sha256').hexdigest()
 if not expected or old!=expected or run(exe,'--version')!='fluxo version 0.4.41':raise RuntimeError('Only the official running v0.4.41 panel is supported')
 environment={}
 for raw in pathlib.Path('/proc',str(pid),'environ').read_bytes().split(b'\0'):
  key,_,value=raw.partition(b'=')
  if key in (b'FLUXO_ENV',b'FLUXO_DATA_DIR'):environment[key.decode()]=value.decode()
 if environment.get('FLUXO_ENV')!='prod':raise RuntimeError('Panel is not using production configuration')
 data=environment.get('FLUXO_DATA_DIR','/var/lib/fluxo')
 trusted_directory(data)
 if start!=process_start(pid):raise RuntimeError('Panel restarted while inspecting it')
 return {'pid':pid,'start':start,'boot':pathlib.Path('/proc/sys/kernel/random/boot_id').read_text().strip(),'binary_sha256':old,'data_dir':data}


def pending_node(path):
 regular(path);trusted_directory(pathlib.Path(path).parent)
 uri=pathlib.Path(path).as_uri()+'?mode=ro'
 with contextlib.closing(sqlite3.connect(uri,uri=True,timeout=3)) as db:
  tables={row[0] for row in db.execute("SELECT name FROM sqlite_master WHERE type='table'")}
  for table,where in {
   'node_update_operations':"status IN ('queued','running')",'php_update_operations':"status IN ('queued','running')",
   'php_update_generations':"state NOT IN ('succeeded','rolled_back')",'php_update_barriers':'1','php_repository_migrations':"state='pending'",
  }.items():
   if table in tables and db.execute('SELECT count(*) FROM '+table+' WHERE '+where).fetchone()[0]:raise RuntimeError('Active work or PHP maintenance must finish first: '+table)
  if 'node_update_generations' not in tables:raise RuntimeError('No Node recovery exists; use the normal installer')
  rows=db.execute("SELECT id,head,state,payload FROM node_update_generations WHERE state NOT IN ('succeeded','rolled_back')").fetchall()
  if len(rows)!=1 or rows[0][2]!='recovery_required':raise RuntimeError('Requires one stopped Node recovery_required generation')
  gid,head,state,raw=rows[0];g=json.loads(raw)
  if (g.get('id'),g.get('head'),g.get('state'))!=(gid,head,state) or g.get('plan',{}).get('consumer_policy')!=4:raise RuntimeError('Unrecognised recovery history; refusing to rewrite it')
  for table in ('node_update_barriers','node_update_php_barriers'):
   if table in tables and db.execute('SELECT count(*) FROM '+table+' WHERE generation_id!=?',(gid,)).fetchone()[0]:raise RuntimeError('Foreign maintenance owner: '+table)
  op=db.execute('SELECT status FROM node_update_operations WHERE id=?',(head,)).fetchone()
  if not op or op[0] not in ('failed','interrupted','recovery_required'):raise RuntimeError('Recovery head is not a finished attempt')
  return {'generation':gid,'head':head}


@contextlib.contextmanager
def maintenance_lock(path=LIFECYCLE):
 fd=os.open(path,os.O_RDWR|os.O_NOFOLLOW|os.O_CLOEXEC)
 try:
  s=regular(path)
  if identity(os.fstat(fd))!=identity(s):raise RuntimeError('Lock file identity changed')
  end=time.monotonic()+10
  while True:
   try:fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB);break
   except BlockingIOError:
    if time.monotonic()>=end:raise RuntimeError('Maintenance is busy; nothing was replaced')
    time.sleep(.1)
  if identity(os.fstat(fd))!=identity(os.lstat(path)):raise RuntimeError('Lock file was replaced')
  yield
 finally:os.close(fd)


def sync_directory(path):
 fd=os.open(path,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
 try:os.fsync(fd)
 finally:os.close(fd)


def write_record(path,value):
 fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
 with os.fdopen(fd,'w') as f:json.dump(value,f,indent=2);f.write('\n');f.flush();os.fsync(f.fileno())
 sync_directory(pathlib.Path(path).parent)


def copy_regular(source,target,mode):
 before=regular(source)
 with open(source,'rb') as src:
  if identity(os.fstat(src.fileno()))!=identity(before):raise RuntimeError('Source changed before copy')
  fd=os.open(target,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,mode)
  with os.fdopen(fd,'wb') as dst:
   while data:=src.read(1024*1024):dst.write(data)
   dst.flush();os.fchmod(dst.fileno(),mode);os.fsync(dst.fileno())
  if identity(os.fstat(src.fileno()))!=identity(before) or identity(os.lstat(source))!=identity(before):raise RuntimeError('Source changed during copy')


def stage_binary(installed,candidate,archive,expected_old,expected_new,revalidate):
 trusted_directory(pathlib.Path(installed).parent);trusted_directory(archive)
 if digest_file(installed)!=expected_old or digest_file(candidate)!=expected_new:raise RuntimeError('Binary identity changed before staging')
 backup=pathlib.Path(archive)/'fluxo-v0.4.41'
 copy_regular(installed,backup,0o700)
 if digest_file(backup)!=expected_old:raise RuntimeError('Backup verification failed')
 staged=pathlib.Path(installed).parent/('.fluxo-worker-v042-'+uuid.uuid4().hex)
 replaced=False
 try:
  copy_regular(candidate,staged,0o755)
  if digest_file(staged)!=expected_new:raise RuntimeError('Staged binary verification failed')
  write_record(pathlib.Path(archive)/'intent.json',{'old':expected_old,'new':expected_new,'installed':installed,'backup':str(backup)})
  revalidate()
  if digest_file(installed)!=expected_old:raise RuntimeError('Installed binary changed before replacement')
  os.replace(staged,installed);replaced=True;sync_directory(pathlib.Path(installed).parent)
  if digest_file(installed)!=expected_new:raise RuntimeError('Installed binary readback failed')
  write_record(pathlib.Path(archive)/'result.json',{'worker_staged':True,'old':expected_old,'new':expected_new,'no_services_restarted':True})
 except BaseException:
  if replaced:print('The on-disk binary may already be updated. Preserve the receipt and backup at '+str(archive)+'; do not roll back or retry blindly.',file=sys.stderr)
  raise
 finally:
  if staged.exists():staged.unlink()


def main():
 if os.geteuid()!=0 or len(sys.argv)!=2:raise RuntimeError('Root and one verified candidate are required')
 candidate=sys.argv[1];trusted_directory(pathlib.Path(candidate).parent);new_sha=digest_file(candidate)
 if new_sha!=NEW_SHA.get(os.uname().machine):raise RuntimeError('Candidate is not the pinned official v0.4.42 binary')
 if run(candidate,'--version')!='fluxo version 0.4.42':raise RuntimeError('Wrong candidate version')
 for flag,want in (('--node-update-support','1'),('--system-task-support','1'),('--scheduler-support','3')):
  if run(candidate,flag)!=want:raise RuntimeError('Candidate recovery compatibility mismatch')
 trusted_directory(pathlib.Path(INSTALLED).parent)
 with maintenance_lock():
  panel=panel_snapshot();database=pathlib.Path(panel['data_dir'])/'fluxo.db';pending=pending_node(database)
  scheduler=pathlib.Path('/var/lib/fluxo-scheduler/state.json')
  if scheduler.exists():
   trusted_directory(scheduler.parent);regular(scheduler)
   if json.loads(scheduler.read_text()).get('upgrade') is not None:raise RuntimeError('An unfinished installer owns admission; preserve it and request recovery assistance')
  old=digest_file(INSTALLED)
  if old==new_sha:
   print('The fixed v0.4.42 worker is already staged. Use Review repair in the existing panel; no file was changed.')
   return
  if old!=panel['binary_sha256']:raise RuntimeError('Installed and running binaries differ')
  parent=pathlib.Path(panel['data_dir'])/'worker-recovery-backups'
  if not parent.exists():
   parent.mkdir(mode=0o700);sync_directory(parent.parent)
  trusted_directory(parent)
  archive=pathlib.Path(tempfile.mkdtemp(prefix='v041-to-v042-',dir=parent));sync_directory(parent)
  write_record(archive/'context.json',{'panel':panel,'recovery':pending,'target_sha256':new_sha})
  def revalidate():
   if panel_snapshot()!=panel or pending_node(database)!=pending:raise RuntimeError('Panel/recovery changed during staging')
  stage_binary(INSTALLED,candidate,archive,old,new_sha,revalidate)
  print('Fixed v0.4.42 worker staged. Recovery records and holds were preserved. Backup: '+str(archive))
 print('Next: in the existing panel choose Runtime > Node.js > Review repair, then approve the reviewed repair.')
 print('The running dashboard remains v0.4.41 for now. After recovery succeeds, run the normal v0.4.42 installer with --reinstall to finish the panel upgrade.')

if __name__=='__main__':
 try:main()
 except (OSError,ValueError,RuntimeError,sqlite3.Error,subprocess.SubprocessError) as error:sys.exit('Recovery worker staging refused: '+str(error))
