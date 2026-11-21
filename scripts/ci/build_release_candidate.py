"""Build native acceptance subjects with the official release pin assignments."""
import os
import re
from pathlib import Path
import shlex
import subprocess
import sys


def main():
    destination, version = sys.argv[1:]
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
    # 'auto' uses a newer host compiler when one is installed. Candidate
    # evidence must use the same exact go.mod directive as the release workflow.
    match = re.search(r'^go ([0-9]+\.[0-9]+\.[0-9]+)$', Path('go.mod').read_text(), re.MULTILINE)
    if not match:
        raise RuntimeError('Missing exact project Go toolchain version')
    toolchain = 'go' + match.group(1)
    env = dict(os.environ, CGO_ENABLED='0', GOOS='linux', GOTOOLCHAIN=toolchain)
    subprocess.run(['go', 'build', '-ldflags', shlex.join(flags), '-o', destination, './cmd/fluxo'], env=env, check=True)
    metadata = subprocess.check_output(['go', 'version', '-m', destination], env=env, text=True)
    if metadata.splitlines()[0].rsplit(': ', 1)[-1] != toolchain:
        raise RuntimeError('Candidate compiler does not match the project pin')
    for key, value in values.items():
        if key + '=' + value not in metadata:
            raise RuntimeError('Missing official release assignment: ' + key)
    print('Native candidate built with all 19 official toolchain/installer pins.')


if __name__ == '__main__':
    main()
