# v0.4.58-test.3 opt-in release

Immutable prerelease [461493638](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.58-test.3), source `7c6a06d32c88dc31f8303e7cb21d77a3cb7b7fe5`. [Official release build](https://github.com/FabioTECH1/fluxo/actions/runs/46137593471): success. Main/latest/stable remain unchanged pending user testing and explicit promotion approval.

## Restart corrections

- Keep the restart-required banner compact; its explanation stays in Show details.
- Restore the approved progress summary and retained **Restart requested → New boot confirmed → Services checked** checklist. Completed steps remain visible; actual confirmations drive them.
- Show automatic reconnection during expected accepted-restart disconnects. Keep actual backend, authentication, scheduling and service errors visible. Never automatically retry a reboot.
- Preserve the shared modal, minimise/check/close flow, service startup grace, bounded sticky-header table and all earlier test.2 navigation/runtime changes.

## Evidence and limits

175 frontend TAP tests and eight console checks pass at production-identical implementation `e13a22a6fd533646a3740e3b7e59f113b8471610`; dashboard typecheck/build and website/functions/demo/SSR/prerender/docs builds pass. The release commit changes version metadata/changelog only. The official build reruns frontend builds/tests and the full Go suite on the exact tag.

All eight public byte identities/checksums, four exact source/tag/official-hosted attestations, both ELF/Go1.26.3/CGo0 images, all 19 tool pins, actual AMD version/base contract and embedded new progress markers pass. The branded tagged installer matches the published script. See the adjacent JSON for exact public subjects and scoped log hashes.

The initial new empty-500 regression failure was retained and fixed with contextual status/service error text. Expected disconnects do not erase unknown state or suppress structured server errors. No actual VPS action/reboot, new native installer/ARM/runtime-apply acceptance or browser pixel proof is claimed. Backend reboot/maintenance authority, bridge, installer, tool pins and old immutable assets are unchanged.

## Install this exact test

```bash
curl -fsSL https://fluxo.fabiotech.dev/releases/v0.4.58-test.3/install.sh -o /tmp/fluxo-0.4.58-test.3-install.sh && sudo env FLUXO_VERSION=v0.4.58-test.3 bash /tmp/fluxo-0.4.58-test.3-install.sh
```
