"""Metadata-only hosted fixture probe: no packages, installer, tasks or authority."""
import errno
import json
import os
from pathlib import Path
import platform
import stat
import struct

required={'GITHUB_ACTIONS':'true','RUNNER_ENVIRONMENT':'github-hosted','RUNNER_ARCH':'X64',
          'GITHUB_REF':'refs/heads/test/amd64-v040-cronie-upgrade-20261118'}
if os.geteuid()!=0 or platform.machine()!='x86_64' or any(os.environ.get(k)!=v for k,v in required.items()):
    raise SystemExit('Requires own isolated hosted AMD64 workflow')
output=Path(os.environ['FLUXO_NATIVE_OUTPUT']);output.mkdir(mode=0o755,parents=True,exist_ok=True)
opt=Path('/opt');prefix=opt/'fluxo'
if prefix.exists() or prefix.is_symlink():raise SystemExit('Refuse preexisting prefix')

def metadata(path):
    info=path.lstat()
    kind='directory' if stat.S_ISDIR(info.st_mode) else 'symlink' if stat.S_ISLNK(info.st_mode) else 'regular' if stat.S_ISREG(info.st_mode) else 'other'
    result={'path':str(path),'type':kind,'uid':info.st_uid,'gid':info.st_gid,
            'mode':oct(stat.S_IMODE(info.st_mode)),'nlink':info.st_nlink}
    for name in ('access','default'):
        try:raw=os.getxattr(path,'system.posix_acl_'+name)
        except OSError as error:
            if error.errno not in (errno.ENODATA,errno.ENOTSUP):raise
            result[name+'_acl']=None;continue
        if len(raw)<4 or struct.unpack('<I',raw[:4])[0]!=2 or (len(raw)-4)%8:
            raise RuntimeError('Unknown ACL metadata encoding')
        result[name+'_acl']=[{'tag':tag,'permissions':oct(perms),'id':identifier}
            for tag,perms,identifier in struct.iter_unpack('<HHI',raw[4:])]
    return result

before=metadata(opt)
if before['uid']!=0 or before['type']!='directory':raise SystemExit('Unsafe hosted opt ancestry')
old_mask=os.umask(0o022);old_mode=stat.S_IMODE(opt.lstat().st_mode)
report={'probe':'node-prefix-metadata-only','old_installer_run':False,'authority_created':False,
        'opt_before':before,'fixtures_restored':False,'source_requirements':{
        'source':'v0.4.40 internal/services/nodetoolchain/toolchain.go:348',
        'uid':0,'gid':0,'group_world_write_forbidden':True,'directories_public_read_traverse':True,
        'directory_nlink_equals_one_required':False}}
try:
    opt.chmod(old_mode & ~0o022)
    report['opt_after_existing_fixture_preparation']=metadata(opt)
    leaf=prefix/'node-toolchain'/'bin';leaf.mkdir(parents=True,mode=0o755)
    paths=[prefix,prefix/'node-toolchain',leaf]
    report['created_prefix_before_correction']=[metadata(path) for path in paths]
    report['first_offending_cf649d3_assertion']=next((m for m in report['created_prefix_before_correction']
        if m['uid']!=0 or m['mode']!='0o755'),None)
    for path in paths:
        for name in ('access','default'):
            try:os.removexattr(path,'system.posix_acl_'+name)
            except OSError as error:
                if error.errno not in (errno.ENODATA,errno.ENOTSUP):raise
        os.chown(path,0,0);path.chmod(0o755)
    report['created_prefix_after_fixture_correction']=[metadata(path) for path in paths]
    report['corrected_prefix_meets_published_metadata_requirements']=all(
        m['type']=='directory' and m['uid']==m['gid']==0 and int(m['mode'],8)&0o022==0
        and int(m['mode'],8)&0o005==0o005 for m in report['created_prefix_after_fixture_correction'])
    child=leaf/'new-vendor-subdirectory';child.mkdir()
    report['new_child_after_correction']=metadata(child)
    if stat.S_IMODE(child.stat().st_mode)!=0o755:raise RuntimeError('Inherited directory permissions remain unsafe')
    report['owned_empty_paths_retained_until_ephemeral_runner_disposal']=[str(path) for path in paths]
finally:
    opt.chmod(old_mode);os.umask(old_mask)
    report['opt_after_restoration']=metadata(opt)
    report['fixtures_restored']=report['opt_after_restoration']['mode']==before['mode']
    (output/'report.json').write_text(json.dumps(report,indent=2,sort_keys=True)+'\n')
    print(json.dumps(report,sort_keys=True))
if not report.get('corrected_prefix_meets_published_metadata_requirements') or not report['fixtures_restored']:
    raise SystemExit(1)
