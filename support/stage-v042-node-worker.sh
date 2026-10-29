#!/bin/bash
# Explicit recovery bootstrap; does not run the normal installer or restart Fluxo.
set -Eeuo pipefail
if [[ "${1:-}" != --stage-v042-node-worker || $# != 1 || $EUID != 0 ]]; then
    echo 'Usage: sudo bash stage-v042-node-worker.sh --stage-v042-node-worker' >&2
    exit 2
fi
umask 077
work="$(mktemp -d /root/fluxo-v042-worker.XXXXXXXX)"
trap 'rm -rf -- "$work"' EXIT
fetch() { /usr/bin/curl -fsSL --proto '=https' --tlsv1.2 --connect-timeout 15 --max-time 600 --retry 2 "$2" -o "$1"; }
fetch "$work/install.sh" https://raw.githubusercontent.com/FabioTECH1/fluxo/v0.4.42/install.sh
fetch "$work/patcher.py" https://raw.githubusercontent.com/FabioTECH1/fluxo/main/support/stage-v042-node-worker.py
printf '%s  %s\n' c5b93156a66f768ad6e0998c49f17adfcad273755ed36cb88f87f9ee453aa308 "$work/install.sh" c4ddc8eb38aa99e5f2c9a2d4a4626456e5f183e997244b9438bde5296bbd2348 "$work/patcher.py" | /usr/bin/sha256sum -c -
# Reuse only the pinned published installer's download/provenance functions.
# The entry point and all provisioning/service actions are excluded.
/usr/bin/python3 -I - "$work/install.sh" "$work/verify-only.sh" <<'PY_PREFIX'
import pathlib, sys
source=pathlib.Path(sys.argv[1]).read_text()
marker='\nskip_current_fluxo_release\npreflight_host\n'
if source.count(marker)!=1:raise SystemExit('Pinned installer entry point did not match')
prefix=source.split(marker)[0]
pathlib.Path(sys.argv[2]).write_text(prefix+'\nprepare_fluxo_binary\nverify_release_attestation\n/usr/bin/python3 -I "$FLUXO_RECOVERY_PATCHER" "$PREPARED_BINARY"\n')
PY_PREFIX
/usr/bin/env -i PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin HOME=/root TMPDIR="$work" \
    FLUXO_VERSION=v0.4.42 FLUXO_GITHUB_REPO=FabioTECH1/fluxo FLUXO_RECOVERY_PATCHER="$work/patcher.py" \
    /bin/bash "$work/verify-only.sh"
