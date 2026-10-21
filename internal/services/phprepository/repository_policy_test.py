import pathlib,tempfile,subprocess,sys,os,re,apt_pkg
from unittest.mock import patch
# Real APT policy fixture using only temporary file repositories and private state.
namespace={"os":os,"re":re,"apt_pkg":apt_pkg}
exec(pathlib.Path(__file__).with_name('repository.py').read_text(),namespace)
with tempfile.TemporaryDirectory(prefix='fluxo-apt-policy-') as d:
    root=pathlib.Path(d); root.chmod(0o755)
    for p in ['etc/apt/sources.list.d','etc/apt/preferences.d','var/lib/apt/lists/partial','var/cache/apt/archives/partial','var/lib/dpkg']:(root/p).mkdir(parents=True,exist_ok=True)
    sources=[]
    versions={'Ubuntu':'8.3.6-0ubuntu0.24.04.13','UbuntuESM':'8.3.6-0ubuntu0.24.04.14+esm1','UbuntuESMApps':'8.3.6-0ubuntu0.24.04.15+esm1','deb.sury.org':'8.3.36-1+0~20261021.57+ubuntu24.04~1.gbp7636b9'}
    for i,(origin,v) in enumerate(versions.items()):
        repo=root/f'repo{i}';repo.mkdir()
        (repo/'Release').write_text(f'Origin: {origin}\nLabel: {origin}\nSuite: noble\nCodename: noble\n')
        (repo/'Packages').write_text(f'Package: php8.3-fpm\nVersion: {v}\nArchitecture: amd64\nMaintainer: Fixture <test@example.invalid>\nDescription: fixture\nFilename: fixture.deb\nSize: 1\n\n')
        sources.append(f'deb [trusted=yes] file:{repo} ./\n')
    (root/'etc/apt/sources.list').write_text(''.join(sources))
    (root/'var/lib/dpkg/status').write_text('Package: php8.3-fpm\nStatus: install ok installed\nArchitecture: amd64\nVersion: 8.3.6-0ubuntu0.24.04.12\nMaintainer: Fixture <test@example.invalid>\nDescription: fixture\n\n')
    (root/'etc/apt/preferences.d/fluxo-php-vendors').write_text(namespace['repo_native_pin']('8.3'))
    args=['-o',f'Dir={root}','-o',f'Dir::State::status={root}/var/lib/dpkg/status','-o','APT::Architecture=amd64','-o','APT::Sandbox::User=root','-o','Acquire::Languages=none']
    subprocess.run(['apt-get',*args,'update'],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    def candidate():
        result=subprocess.check_output(['apt-cache',*args,'policy','php8.3-fpm'],text=True)
        return next(line.split(': ',1)[1] for line in result.splitlines() if 'Candidate:' in line),result
    got,result=candidate();print(result);assert got==versions['UbuntuESMApps'],got
    (root/'etc/apt/preferences').write_text('Package: php8.3-fpm\nPin: version 8.3.6-0ubuntu0.24.04.15+esm1\nPin-Priority: -1\n\n')
    got,result=candidate();print(result);assert got==versions['UbuntuESM'],got
    # A valid fragment sorting after Fluxo's managed rules would lose its
    # exclusion. The production migration must detect it before publishing.
    (root/'etc/apt/preferences').unlink()
    (root/'etc/apt/preferences.d/zz.local.pref').write_text('Package: php8.3-fpm\nPin: version 8.3.6-0ubuntu0.24.04.15+esm1\nPin-Priority: -1\n\n')
    got,result=candidate()
    assert got==versions['UbuntuESMApps'], 'fixture no longer models specific-pin precedence: '+result
    old_listdir=os.listdir
    def fixture_file(path,**kwargs):
        file=root/path.lstrip('/')
        return file.read_bytes() if file.exists() else None
    with patch.dict(namespace,repo_file=fixture_file),patch.object(os,'listdir',lambda path:old_listdir(root/path.lstrip('/'))):
        try: namespace['repo_check_custom_pins'](['8.3'])
        except RuntimeError as error: assert 'zz.local.pref' in str(error)
        else: raise AssertionError('dotted custom exclusion not detected before publication')
    (root/'etc/apt/preferences.d/zz.local.pref').unlink()
    (root/'var/lib/dpkg/status').write_text((root/'var/lib/dpkg/status').read_text().replace('8.3.6-0ubuntu0.24.04.12','8.3.6-0ubuntu0.24.04.16+esm1'))
    got,result=candidate();assert got=='8.3.6-0ubuntu0.24.04.16+esm1',result
    print('PASS: real APT selects newest native ESM candidate, respects custom exclusion and never forces downgrade.')
