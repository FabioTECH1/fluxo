"""Build native acceptance subjects with the official release pin assignments."""
import hashlib
import json
import os
import platform
from pathlib import Path
import shlex
import subprocess
import sys


SOURCE_PATHS = ['cmd', 'internal', 'ui/src', 'ui/embed.go', 'ui/package.json',
                'ui/package-lock.json', 'go.mod', 'go.sum', 'install.sh',
                'ui-site/public/install.sh', '.github/release-tool-versions.conf',
                'scripts/ci/build_release_candidate.py', 'scripts/ci/base_candidate.py',
                '.github/workflows/base-candidate.yml']


def digest(path):
    value = hashlib.sha256()
    with open(path, 'rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            value.update(block)
    return value.hexdigest()


def source_identity():
    prefix = ['git', '-c', 'safe.directory=' + os.getcwd()]
    source = subprocess.check_output(prefix + ['rev-parse', 'HEAD'], text=True).strip()
    tree = subprocess.check_output(prefix + ['rev-parse', 'HEAD^{tree}'], text=True).strip()
    changed = subprocess.check_output(prefix + ['status', '--porcelain', '--untracked-files=all', '--', *SOURCE_PATHS], text=True)
    if changed:
        raise RuntimeError('Candidate source or installer differs from the recorded commit')
    if os.environ.get('GITHUB_SHA', source) != source:
        raise RuntimeError('Candidate checkout differs from the requested workflow source')
    if Path('install.sh').read_bytes() != Path('ui-site/public/install.sh').read_bytes():
        raise RuntimeError('Website and release installers differ')
    return {'source': source, 'source_tree': tree, 'tracked_source_clean': True,
            'installer_sha256': digest('install.sh'),
            'tool_pins_sha256': digest('.github/release-tool-versions.conf')}


def main():
    destination, version = sys.argv[1:]
    identity = source_identity()
    expected_go = next(line.split()[1] for line in Path('go.mod').read_text().splitlines() if line.startswith('go '))
    go_version = subprocess.check_output(['go', 'version'], text=True).strip()
    if not go_version.startswith('go version go' + expected_go + ' '):
        raise RuntimeError('Candidate must use the exact repository Go toolchain')
    pins = {}
    for line in Path('.github/release-tool-versions.conf').read_text().splitlines():
        if line and not line.startswith('#'):
            key, value = line.split('=', 1)
            pins[key] = shlex.split(value)[0]
    assignments = {
        'nodetoolchain.PinnedNodeVersion': 'NODE_TOOLCHAIN_VERSION',
        'nodetoolchain.PinnedNodeAMD64SHA256': 'NODE_AMD64_SHA256',
        'nodetoolchain.PinnedNodeARM64SHA256': 'NODE_ARM64_SHA256',
        'nodetoolchain.PinnedCorepackVersion': 'COREPACK_VERSION',
        'nodetoolchain.PinnedCorepackIntegrity': 'COREPACK_INTEGRITY',
        'nodetoolchain.PinnedPNPMVersion': 'PNPM_VERSION',
        'nodetoolchain.PinnedPNPMIntegrity': 'PNPM_INTEGRITY',
        'nodetoolchain.PinnedYarnVersion': 'YARN_VERSION',
        'nodetoolchain.PinnedYarnIntegrity': 'YARN_INTEGRITY',
        'nodetoolchain.PinnedBunVersion': 'BUN_VERSION',
        'nodetoolchain.PinnedBunAMD64SHA256': 'BUN_AMD64_SHA256',
        'nodetoolchain.PinnedBunAMD64BaselineSHA256': 'BUN_AMD64_BASELINE_SHA256',
        'nodetoolchain.PinnedBunARM64SHA256': 'BUN_ARM64_SHA256',
        'pythontoolchain.PinnedUVVersion': 'UV_VERSION',
        'pythontoolchain.PinnedUVAMD64SHA256': 'UV_AMD64_SHA256',
        'pythontoolchain.PinnedUVARM64SHA256': 'UV_ARM64_SHA256',
    }
    values = {'main.version': version}
    values.update({'fluxo/internal/services.' + key: pins[value] for key, value in assignments.items()})
    values.update({'fluxo/internal/releaseinfo.' + key: pins[value] for key, value in {
        'ComposerVersion': 'COMPOSER_VERSION', 'ComposerSHA256': 'COMPOSER_SHA256',
        'WPCLIVersion': 'WP_CLI_VERSION',
    }.items()})
    # Package paths use '/', while the exported variable remains after '.'.
    values = {key.replace('/services.', '/services/'): value for key, value in values.items()}
    flags = ['-s', '-w']
    for key, value in values.items():
        flags += ['-X', key + '=' + value]
    goarch = subprocess.check_output(['go', 'env', 'GOARCH'], text=True).strip()
    if {'x86_64': 'amd64', 'aarch64': 'arm64'}.get(platform.machine()) != goarch:
        raise RuntimeError('Native candidate builder cannot cross-compile its acceptance subject')
    env = dict(os.environ, CGO_ENABLED='0', GOOS='linux')
    subprocess.run(['go', 'build', '-ldflags', shlex.join(flags), '-o', destination, './cmd/fluxo'], env=env, check=True)
    metadata = subprocess.check_output(['go', 'version', '-m', destination], text=True)
    for key, value in values.items():
        if key + '=' + value not in metadata:
            raise RuntimeError('Missing official release assignment: ' + key)
    identity.update({'version': version, 'binary_sha256': digest(destination),
                     'go_version': go_version, 'goarch': goarch, 'goos': 'linux', 'cgo_enabled': False,
                     'official_pin_assignments': {key: value for key, value in values.items() if key != 'main.version'},
                     'build_metadata': metadata})
    if len(identity['official_pin_assignments']) != 19:
        raise RuntimeError('Expected exactly 19 official release assignments')
    Path(destination + '.manifest.json').write_text(json.dumps(identity, indent=2) + '\n')
    print('Native candidate built with all 19 official toolchain/installer pins and exact-source manifest.')


if __name__ == '__main__':
    main()
