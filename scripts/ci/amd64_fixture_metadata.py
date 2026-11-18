"""Reversible metadata preparation for fresh hosted fixtures; never production."""
import errno
import hashlib
import os
from pathlib import Path
import stat

ROOTS=('/home','/opt','/usr/local','/usr/local/bin','/usr/local/lib','/usr/local/lib/node_modules')

def acl(path,name):
    try:return os.getxattr(path,'system.posix_acl_'+name)
    except OSError as error:
        if error.errno not in (errno.ENODATA,errno.ENOTSUP):raise
        return None

def public_metadata(path):
    value=path.lstat()
    result={'path':str(path),'type':'directory' if stat.S_ISDIR(value.st_mode) else 'other',
        'uid':value.st_uid,'gid':value.st_gid,'mode':oct(stat.S_IMODE(value.st_mode)),'nlink':value.st_nlink}
    for name in ('access','default'):
        raw=acl(path,name)
        result[name+'_acl_sha256']=hashlib.sha256(raw).hexdigest() if raw is not None else None
    return result

class PreparedRoots:
    def __init__(self,private):
        self.private=private
        self.originals={}
        self.before={}
        self.prepared={}
        self.restored={}
        self.preserved=False

    def prepare(self):
        for name in ROOTS:
            path=Path(name)
            if not path.exists():continue
            value=path.lstat()
            if not stat.S_ISDIR(value.st_mode) or value.st_uid!=0:
                raise RuntimeError('Unsafe known hosted fixture parent: '+name)
            original={'mode':stat.S_IMODE(value.st_mode),'access':acl(path,'access'),'default':acl(path,'default')}
            self.originals[name]=original
            self.before[name]=public_metadata(path)
            for key in ('access','default'):
                if original[key] is not None:
                    saved=self.private/('parent-'+str(len(self.originals))+'-'+key+'.acl')
                    saved.write_bytes(original[key]);saved.chmod(0o600)
            # No recursive permission changes. Preserve existing access ACLs;
            # remove only default inheritance on these exact known CI parents.
            if original['default'] is not None:os.removexattr(path,'system.posix_acl_default')
            path.chmod(original['mode'] & ~0o022)
            self.prepared[name]=public_metadata(path)

    def restore(self):
        for name,original in reversed(list(self.originals.items())):
            path=Path(name);path.chmod(original['mode'])
            for key in ('access','default'):
                if original[key] is not None:os.setxattr(path,'system.posix_acl_'+key,original[key])
                else:
                    try:os.removexattr(path,'system.posix_acl_'+key)
                    except OSError as error:
                        if error.errno not in (errno.ENODATA,errno.ENOTSUP):raise
            self.restored[name]=public_metadata(path)
        # Owned new fixture entries legitimately change parent link counts.
        keys=('type','uid','gid','mode','access_acl_sha256','default_acl_sha256')
        self.preserved=all(all(self.before[name][key]==self.restored[name][key] for key in keys) for name in self.originals)
        if not self.preserved:raise RuntimeError('Original parent ownership/permissions/ACL differs after restoration')
