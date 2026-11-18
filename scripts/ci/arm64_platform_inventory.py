"""Authentic native platform inputs; no Fluxo installation or authority editing."""
import hashlib
import json
import os
from pathlib import Path
import platform
import struct
import subprocess
import tempfile

from arm64_release_probe import runner_system_paths, isolate_ci_microsoft_source


def command(args, timeout=120):
    result = subprocess.run(args, capture_output=True, text=True, timeout=timeout)
    return {"exit_code": result.returncode, "stdout": result.stdout, "stderr": result.stderr}


def main():
    if os.geteuid() != 0 or platform.machine() != "aarch64" or os.environ.get("FLUXO_ARM64_RELEASE_PROBE") != "1" or os.environ.get("GITHUB_ACTIONS") != "true" or os.environ.get("RUNNER_ARCH") != "ARM64" or os.environ.get("RUNNER_ENVIRONMENT") != "github-hosted" or os.environ.get("GITHUB_REF") != "refs/heads/test/arm64-bridge-acceptance":
        raise SystemExit("Native opted-in disposable ARM runner only")
    output = Path(os.environ["FLUXO_PROBE_OUTPUT"])
    output.mkdir(parents=True, mode=0o755, exist_ok=False)
    private = Path(tempfile.mkdtemp(prefix="fluxo-arm-inventory-", dir="/root"))
    moved = []
    report = {"machine": platform.machine(), "os_release": Path('/usr/lib/os-release').read_text(), "published_or_enabled": False}
    try:
        report['fixture_directories'] = runner_system_paths()
        isolate_ci_microsoft_source(private, moved)
        report['apt_refresh'] = command(['apt-get', '-o', 'APT::Update::Error-Mode=any', '-o', 'Acquire::http::Timeout=20', '-o', 'Acquire::https::Timeout=20', 'update'], 180)
        report['packages'] = command(['dpkg-query','-W','-f=${Package} ${Version} ${Architecture}\n','cron','libc6','libpam0g','libpam-modules','libpam-modules-bin','libpam-runtime','libpam-cap','libpam-systemd','libnss-systemd','libcap2','libselinux1','systemd'])
        pam = Path('/etc/pam.d/common-session-noninteractive')
        report['pam_before'] = {'sha256':hashlib.sha256(pam.read_bytes()).hexdigest(), 'policy':pam.read_text()}
        env = dict(os.environ, DEBIAN_FRONTEND='noninteractive')
        p = subprocess.run(['pam-auth-update','--package','--force'],env=env,capture_output=True,text=True,timeout=40)
        report['vendor_pam_regeneration'] = {'exit_code':p.returncode,'stderr':p.stderr}
        report['pam_after'] = {'sha256':hashlib.sha256(pam.read_bytes()).hexdigest(), 'policy':pam.read_text()}
        report['cron_sha256'] = hashlib.sha256(Path('/usr/sbin/cron').read_bytes()).hexdigest()
        report['cron_elf'] = command(['readelf','-h','-l','/usr/sbin/cron'])
        report['loader_declarations'] = {str(p):p.read_text() for p in Path('/etc/ld.so.conf.d').glob('*.conf')}
        raw = Path('/etc/ld.so.cache').read_bytes()
        report['cache_sha256'] = hashlib.sha256(raw).hexdigest()
        if raw[:20] != b'glibc-ld.so.cache1.1':
            raise RuntimeError('Unexpected native loader cache')
        count=struct.unpack_from('<I',raw,20)[0]
        flags={}
        for i in range(count):
            value=struct.unpack_from('<I',raw,48+i*24)[0]
            flags[hex(value)]=flags.get(hex(value),0)+1
        report['cache_flags']=flags
        version=command(['dpkg-query','-W','-f=${Version}','cron'])['stdout']
        report['cron_package_metadata']=command(['apt-cache','show','cron='+version])
        report['cron_archive_download']=command(['apt-get','download','cron='+version],120)
        # apt-get download uses cwd, which is the disposable checkout, not root state.
        archives=list(Path('.').glob('cron_*_arm64.deb'))
        report['archive_sha256']={p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in archives}
    except Exception as error:
        report['error']=str(error)
    finally:
        for original,archive in moved:
            if not original.exists():archive.rename(original)
        (output/'platform.json').write_text(json.dumps(report,indent=2)+'\n')
        print(json.dumps({'machine':report['machine'],'cron_sha256':report.get('cron_sha256'),'cache_flags':report.get('cache_flags'),'error':report.get('error')}))
    return 1 if 'error' in report else 0


if __name__=='__main__':
    raise SystemExit(main())
