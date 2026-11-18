"""Authenticated historical Noble tuple; isolated indexes, no policy bypass."""
import hashlib
import re
from pathlib import Path
import subprocess
import urllib.parse

SNAPSHOT='20261117T000000Z'
REVIEWED={'libpam0g':'1.5.3-5ubuntu5.7','libpam-modules':'1.5.3-5ubuntu5.7',
    'libpam-modules-bin':'1.5.3-5ubuntu5.7','libpam-cap':'1:2.66-5ubuntu2.4',
    'libcap2':'1:2.66-5ubuntu2.4','libnss-systemd':'255.4-1ubuntu8.18',
    'libpam-systemd':'255.4-1ubuntu8.18','libselinux1':'3.5-2ubuntu2.1','libc6':'2.39-0ubuntu8.12',
    'libc-bin':'2.39-0ubuntu8.12','libc6-dev':'2.39-0ubuntu8.12','libc-dev-bin':'2.39-0ubuntu8.12'}
GLIBC=('libc6','libc-bin','libc6-dev','libc-dev-bin')

def digest(path):return hashlib.sha256(path.read_bytes()).hexdigest()

def source_metadata():
    result=[]
    paths=[Path('/etc/apt/sources.list'),Path('/etc/apt/apt-mirrors.txt'),*sorted(Path('/etc/apt/sources.list.d').glob('*.list')),
        *sorted(Path('/etc/apt/sources.list.d').glob('*.sources'))]
    for path in paths:
        if not path.is_file():continue
        selected=[]
        for line in path.read_text().splitlines():
            if (path.name=='apt-mirrors.txt' and line.startswith(('http://','https://'))) or line.startswith(('Types:','URIs:','Suites:','Components:','Signed-By:','Snapshot:','deb ')):
                def clean(found):
                    url=urllib.parse.urlsplit(found[0]);host=url.hostname or ''
                    if url.port:host+=':'+str(url.port)
                    return urllib.parse.urlunsplit((url.scheme,host,url.path,'',''))
                selected.append(re.sub(r'https?://[^\s]+',clean,line)[:500])
        result.append({'path':str(path),'sha256':digest(path),'declarations':selected})
    return result

class ReviewedAPT:
    def __init__(self,work,invoke,report):
        self.work=work;self.invoke=invoke;self.report=report
        work.mkdir(mode=0o755,parents=True,exist_ok=True)
        self.lists=work/'lists';self.archives=work/'archives';self.downloads=work/'exact-debs'
        for path in (self.lists,self.archives,self.downloads):path.mkdir(mode=0o755,exist_ok=True)
        source=work/'historical.sources'
        source.write_text('Types: deb\nURIs: https://snapshot.ubuntu.com/ubuntu/'+SNAPSHOT+'/\nSuites: noble noble-updates noble-security\nComponents: main universe restricted multiverse\nSigned-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\nSnapshot: no\n')
        source.chmod(0o644)
        self.opts=['-o','Dir::Etc::sourcelist='+str(source),'-o','Dir::Etc::sourceparts=-',
            '-o','Dir::State::lists='+str(self.lists),'-o','Dir::Cache::archives='+str(self.archives),
            '-o','Acquire::AllowInsecureRepositories=false','-o','Acquire::AllowDowngradeToInsecureRepositories=false',
            '-o','APT::Get::AllowUnauthenticated=false','-o','Acquire::Check-Valid-Until=true']
        report.update({'snapshot':SNAPSHOT,'snapshot_source_sha256':digest(source),
            'original_sources':source_metadata(),'installed_before':{},'original_glibc_available_versions':{},
            'signed_metadata':[],'exact_glibc_archives':{},'installability_proven':False,'installed':False})

    def run(self,label,args,timeout=120,cwd=None):return self.invoke(label,args,timeout,cwd)

    def prove(self):
        for package,wanted in REVIEWED.items():
            current=self.run('installed-'+package,['dpkg-query','-W','-f=${Version}',package],15).strip()
            self.report['installed_before'][package]=current
            comparison=subprocess.run(['dpkg','--compare-versions',current,'gt',wanted],
                stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=5)
            if comparison.returncode==0:raise RuntimeError('Refuse downgrade of newer security package: '+package)
        try:
            self.run('refresh-original-indexes',['apt-get','-o','Acquire::AllowInsecureRepositories=false',
                '-o','APT::Get::AllowUnauthenticated=false','update'],240)
            self.report['original_index_refresh_succeeded']=True
        except RuntimeError:
            self.report['original_index_refresh_succeeded']=False
        config=self.run('original-snapshot-policy',['apt-config','dump'],15)
        found=re.search(r'^APT::Snapshot "([0-9]{8}T[0-9]{6}Z)";',config,re.M)
        self.report['original_global_snapshot']=found[1] if found else None
        for package in GLIBC:
            values=self.run('original-policy-'+package,['apt-cache','madison',package],15)
            self.report['original_glibc_available_versions'][package]=[parts[1].strip() for line in values.splitlines()
                if len(parts:=line.split('|'))>=3 and re.fullmatch(r'[0-9A-Za-z.+:~_-]+',parts[1].strip())]
        self.run('historical-signed-indexes',['apt-get',*self.opts,'update'],240)
        for path in sorted(self.lists.glob('*_InRelease')):
            status=self.run('verify-signed-release',['gpgv','--status-fd=1','--keyring',
                '/usr/share/keyrings/ubuntu-archive-keyring.gpg',str(path)],20)
            fingerprints=re.findall(r'\[GNUPG:\] VALIDSIG ([A-Fa-f0-9]{40,64}) ',status)
            if not fingerprints:raise RuntimeError('Missing verified official archive signing identity')
            self.report['signed_metadata'].append({'name':path.name,'sha256':digest(path),'signing_fingerprints':fingerprints})
        if len(self.report['signed_metadata'])!=3:raise RuntimeError('Historical archive suite verification incomplete')
        targets=[package+'='+version for package,version in REVIEWED.items()]
        self.run('historical-tuple-resolution',['apt-get',*self.opts,'--simulate','--no-remove','install',*targets],60)
        for package in GLIBC:
            wanted=REVIEWED[package]
            shown=self.run('historical-package-'+package,['apt-cache',*self.opts,'show',package+'='+wanted],30)
            hashes=re.findall(r'^SHA256: ([0-9a-f]{64})$',shown,re.M)
            if not hashes:raise RuntimeError('Exact archive hash absent from authenticated indexes: '+package)
            self.run('download-exact-'+package,['apt-get',*self.opts,'download',package+'='+wanted],120,self.downloads)
            candidates=[]
            for path in self.downloads.glob('*.deb'):
                fields=self.run('inspect-exact-deb',['dpkg-deb','-f',str(path),'Package','Version','Architecture'],15)
                metadata=dict(line.split(': ',1) for line in fields.splitlines() if ': ' in line)
                if metadata.get('Package')==package and metadata.get('Version')==wanted and metadata.get('Architecture')=='amd64':candidates.append(path)
            if len(candidates)!=1 or digest(candidates[0]) not in hashes:raise RuntimeError('Exact authenticated package hash mismatch: '+package)
            self.report['exact_glibc_archives'][package]={'version':wanted,'sha256':digest(candidates[0])}
        self.report['installability_proven']=True

    def install(self):
        if not self.report['installability_proven']:raise RuntimeError('Historical tuple must be proven before installation')
        self.run('install-reviewed-historical-tuple',['apt-get',*self.opts,'install','-y','--no-remove',
            *[package+'='+version for package,version in REVIEWED.items()]],300)
        for package,wanted in REVIEWED.items():
            actual=self.run('verify-installed-'+package,['dpkg-query','-W','-f=${Version}',package],15).strip()
            if actual!=wanted:raise RuntimeError('Historical installed version differs: '+package)
        self.report['installed']=True
