"""Independent hosted verification of immutable opt-in bridge assets; no installation.

Same checks as the local verifier; full-file transport avoids local CDN throttle.
The scoped workflow token stays in the environment, never in output or artifacts.
"""
import concurrent.futures
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import time
import shutil
import tarfile
import urllib.request

ROOT=Path.cwd()
OUT=Path(os.environ['FLUXO_VERIFY_OUTPUT'])
ASSETS=OUT/'assets'
EXPECTED=json.loads((ROOT/'scripts/ci/installer_options_expected_public.json').read_text())
TAG=EXPECTED['tag']
assert TAG=='v0.4.55' and EXPECTED['source']==os.environ['FLUXO_RELEASE_SOURCE']
SOURCE=os.environ['FLUXO_RELEASE_SOURCE']
REPO='FabioTECH1/fluxo'
GH=OUT/'tools/gh_2.94.0_linux_amd64/bin/gh'
GO=Path(shutil.which('go'))
NAMES={'fluxo-linux-amd64','fluxo-linux-arm64','install.sh','BRIDGE-CONTRACT.json','TOOLCHAIN-VERSIONS','SHA256SUMS','fluxo-release-attestation.json','LICENSE'}

def run(args,timeout=90,env=None):
    p=subprocess.run([str(x) for x in args],capture_output=True,text=True,timeout=timeout,env=env)
    if p.returncode:
        raise RuntimeError('Command failed: '+str(args[0])+' (exit '+str(p.returncode)+')')
    return p.stdout

def api(path):
    return json.loads(run([GH,'api',path]))

def sha(path):
    with path.open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()

def download(asset):
    path=ASSETS/asset['name']
    if not path.is_file() or 'sha256:'+sha(path)!=asset['digest']:
        temporary=path.with_suffix(path.suffix+'.part')
        run(['curl','-fsSL','--retry','2','--connect-timeout','10','--max-time','180','-o',temporary,asset['browser_download_url']],timeout=570)
        assert temporary.stat().st_size==asset['size'],asset['name']+' size mismatch'
        assert 'sha256:'+sha(temporary)==asset['digest'],asset['name']+' digest mismatch'
        temporary.replace(path)
    assert path.stat().st_size==asset['size']
    return asset['name'],{'sha256':sha(path),'size':path.stat().st_size}

ASSETS.mkdir(parents=True,exist_ok=True)
assert os.environ.get('GITHUB_ACTIONS')=='true' and os.environ.get('RUNNER_ARCH')=='X64'
tools=OUT/'tools';tools.mkdir(exist_ok=True)
archive=tools/'gh.tar.gz'
run(['curl','-fsSL','--retry','2','--connect-timeout','10','--max-time','180','-o',archive,'https://github.com/cli/cli/releases/download/v2.94.0/gh_2.94.0_linux_amd64.tar.gz'],timeout=570)
assert sha(archive)=='a757f1ba6db18f4de8cbadb244843a5f89bc75b5e7c6fc127d2bd77fbd12ed62'
with tarfile.open(archive) as tar:
    for member in tar.getmembers():
        assert member.name.startswith('gh_2.94.0_linux_amd64/') and '..' not in Path(member.name).parts
        assert member.isdir() or member.isfile()
    tar.extractall(tools,filter='data')
assert run([GH,'--version']).startswith('gh version 2.94.0 ')
release=api('repos/'+REPO+'/releases/tags/'+TAG)
assert release['tag_name']==TAG and release['immutable'] is True and release['draft'] is False and release['prerelease'] is False
assert api('repos/'+REPO+'/commits/'+TAG)['sha']==SOURCE
assert api('repos/'+REPO+'/releases/latest')['tag_name']=='v0.4.54'
assets={x['name']:x for x in release['assets']}
assert len(assets)==len(release['assets']) and set(assets)==NAMES
assert all(a['state']=='uploaded' and re.fullmatch(r'sha256:[0-9a-f]{64}',a['digest']) for a in assets.values())
print(json.dumps({'stage':'verified-release-metadata','release_id':release['id']}),flush=True)
with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
    inventory=dict(pool.map(download,assets.values()))
print(json.dumps({'stage':'downloaded-and-checked-assets','count':len(inventory)}),flush=True)
checks={}
for line in (ASSETS/'SHA256SUMS').read_text().splitlines():
    digest,name=line.split();name=name.lstrip('*')
    assert name not in checks and re.fullmatch(r'[0-9a-f]{64}',digest)
    checks[name]=digest
assert set(checks)==NAMES-{'SHA256SUMS','fluxo-release-attestation.json'}
assert all(inventory[name]['sha256']==digest for name,digest in checks.items())
for name in ('install.sh','LICENSE'):
    source=subprocess.check_output(['git','show',SOURCE+':'+name])
    assert hashlib.sha256(source).hexdigest()==inventory[name]['sha256']

env=dict(os.environ,GH_PROMPT_DISABLED='1',GH_PAGER='cat')
attestations={}
for name in ('fluxo-linux-amd64','fluxo-linux-arm64','install.sh','BRIDGE-CONTRACT.json'):
    result=run([GH,'attestation','verify',ASSETS/name,'--repo',REPO,'--bundle',ASSETS/'fluxo-release-attestation.json','--signer-workflow',REPO+'/.github/workflows/release.yml','--source-ref','refs/tags/'+TAG,'--source-digest',SOURCE,'--deny-self-hosted-runners','--format','json'],timeout=180,env=env)
    json.loads(result)
    (OUT/(name+'.attestation-verification.json')).write_text(result)
    attestations[name]=True
    print(json.dumps({'stage':'verified-source-bound-attestation','asset':name}),flush=True)

pins={}
for line in run(['git','show',SOURCE+':.github/release-tool-versions.conf']).splitlines():
    if line and not line.startswith('#'):
        k,v=line.split('=',1);pins[k]=shlex.split(v)[0]
node='fluxo/internal/services/nodetoolchain.'
python='fluxo/internal/services/pythontoolchain.'
info='fluxo/internal/releaseinfo.'
assignments={node+'PinnedNodeVersion':'NODE_TOOLCHAIN_VERSION',node+'PinnedNodeAMD64SHA256':'NODE_AMD64_SHA256',node+'PinnedNodeARM64SHA256':'NODE_ARM64_SHA256',node+'PinnedCorepackVersion':'COREPACK_VERSION',node+'PinnedCorepackIntegrity':'COREPACK_INTEGRITY',node+'PinnedPNPMVersion':'PNPM_VERSION',node+'PinnedPNPMIntegrity':'PNPM_INTEGRITY',node+'PinnedYarnVersion':'YARN_VERSION',node+'PinnedYarnIntegrity':'YARN_INTEGRITY',node+'PinnedBunVersion':'BUN_VERSION',node+'PinnedBunAMD64SHA256':'BUN_AMD64_SHA256',node+'PinnedBunAMD64BaselineSHA256':'BUN_AMD64_BASELINE_SHA256',node+'PinnedBunARM64SHA256':'BUN_ARM64_SHA256',python+'PinnedUVVersion':'UV_VERSION',python+'PinnedUVAMD64SHA256':'UV_AMD64_SHA256',python+'PinnedUVARM64SHA256':'UV_ARM64_SHA256',info+'ComposerVersion':'COMPOSER_VERSION',info+'ComposerSHA256':'COMPOSER_SHA256',info+'WPCLIVersion':'WP_CLI_VERSION'}
labels=('Node.js','Node.js amd64 SHA-256','Node.js arm64 SHA-256','Corepack','Corepack integrity','pnpm','pnpm integrity','Yarn','Yarn integrity','Bun','Bun amd64 SHA-256','Bun amd64 baseline SHA-256','Bun arm64 SHA-256','uv','uv amd64 SHA-256','uv arm64 SHA-256','Composer','Composer SHA-256','WP-CLI','Attestation verifier (temporary GitHub CLI)')
assert (ASSETS/'TOOLCHAIN-VERSIONS').read_text()==''.join(label+' '+pins[key]+'\n' for label,key in zip(labels,tuple(assignments.values())+('ATTESTATION_GH_VERSION',)))
assert run(['git','show',SOURCE+':ui-site/public/install.sh']).encode()==(ASSETS/'install.sh').read_bytes()
metadata={}
for arch,machine in (('amd64',62),('arm64',183)):
    binary=ASSETS/('fluxo-linux-'+arch)
    raw=binary.read_bytes()
    for marker in (b'Manage GitHub connections and deployment access.',b'NoticeBanner',b'UpdateNotice',b'Stored connection',b'Approve & reconnect',b'GitHub ID not recorded',b'--editor-operator',b'Legacy recovery requires your panel password',b'grid grid-cols-1 gap-5 lg:grid-cols-2',b'Review jobs',b'Maintenance details',b'certbot-manual-nohooks-v1',b'managed-service-v1',b'Recovery state changed. Reopen the error details to review the current attempt.',b'repairReviewRequest',b'Waiting for current cron work to finish before Scheduler setup; no jobs will be stopped...',b'Waiting for Scheduler work to settle before the upgrade...',b'scheduler_policy_pending',b'Scheduler compatibility refreshed for the current engine; saved jobs and history preserved',b'loader provider policy changed; supported changes are being revalidated automatically'):
        assert marker in raw,'requested UI missing from '+arch
    assert b'GitHub account link is missing' in raw
    assert b'Update token & repair' not in raw, 'unapproved base redesign included'
    header=raw[:20]
    assert header[:6]==b'\x7fELF\x02\x01' and int.from_bytes(header[18:20],'little')==machine
    build=run([GO,'version','-m',binary]);assert build.splitlines()[0].endswith(': go1.26.3');(OUT/(arch+'.buildinfo.txt')).write_text(build)
    settings={}
    for line in build.splitlines():
        m=re.match(r'\s*build\s+(\S+?)=(.*)$',line)
        if m:settings[m[1]]=json.loads(m[2]) if m[2].startswith('"') else m[2]
    assert settings['GOOS']=='linux' and settings['GOARCH']==arch and settings['CGO_ENABLED']=='0'
    assert settings['vcs.revision']==SOURCE
    flags=shlex.split(settings['-ldflags'])
    assert 'main.version='+TAG[1:] in flags
    actual={}
    for index,flag in enumerate(flags):
        if flag=='-X':
            key,value=flags[index+1].split('=',1)
            assert key not in actual
            actual[key]=value
    assert len(assignments)==19 and actual==dict({key:pins[pin] for key,pin in assignments.items()},**{'main.version':TAG[1:]})
    metadata[arch]={'source':settings['vcs.revision'],'CGO_ENABLED':settings['CGO_ENABLED'],'vcs.modified':settings.get('vcs.modified'),'official_pin_assignments_checked':len(assignments),'compiler':'go1.26.3'}

binary=ASSETS/'fluxo-linux-amd64';binary.chmod(0o700)
assert run([binary,'--version']).strip()=='fluxo version '+TAG[1:]
contract=run([binary,'--bridge-contract'])
assert contract.encode()==(ASSETS/'BRIDGE-CONTRACT.json').read_bytes()
assert json.loads(contract)['role']=='bridge'
assert run([binary,'--installer-github-links-support']).strip()=='1'
cap=json.loads(run([binary,'--execution-capabilities']))
assert set(cap['role_contracts']['system-task-optional-profile'])=={'imagick-3.8.1-im6-q16-composer-startup-v1','nginx-conditional-v1','nginx-conditional-v2','certbot-manual-nohooks-v1'}
expected='composer='+pins['COMPOSER_VERSION']+'\ncomposer-sha256='+pins['COMPOSER_SHA256']+'\nwp-cli='+pins['WP_CLI_VERSION']
assert run([binary,'--installer-tool-versions']).strip()==expected
latest=api('repos/'+REPO+'/releases/latest')
after=api('repos/'+REPO+'/releases/'+str(release['id']))
assert latest['tag_name']=='v0.4.54' and after['immutable'] is True and after['prerelease'] is False
assert [(a['name'],a['digest'],a['size']) for a in after['assets']]==[(a['name'],a['digest'],a['size']) for a in release['assets']]
report={'tag':TAG,'source':SOURCE,'release_id':release['id'],'release_url':release['html_url'],'immutable':True,'prerelease':False,'latest_unchanged':latest['tag_name'],'asset_count':len(inventory),'assets':inventory,'source_bound_attestations':attestations,'build_info':metadata,'amd64_metadata_flags_passed':True,'public_artifact_integrity_passed':True,'requested_ui_embedded_in_both_architectures':True,'native_final_artifact_execution':False,'qualification_scope':'Stable bridge v0.4.55 exact public asset integrity; native execution and promotion recorded separately','production_promotion':False,'verified_at_utc':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime())}
(OUT/'verified-public-release.json').write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps({'passed':True,'tag':TAG,'assets':len(inventory),'amd64_sha256':inventory['fluxo-linux-amd64']['sha256'],'arm64_sha256':inventory['fluxo-linux-arm64']['sha256'],'latest':latest['tag_name']}),flush=True)
