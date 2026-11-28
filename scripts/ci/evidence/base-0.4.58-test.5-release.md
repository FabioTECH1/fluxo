# v0.4.58-test.5 opt-in release

Immutable prerelease [461537576](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.58-test.5), exact source `55c20b208ad8d4d67a60e6b95368624f0a6dbe85`. [Official build](https://github.com/FabioTECH1/fluxo/actions/runs/46143697872): success. Main/latest/stable remain unchanged, awaiting user VPS testing and explicit later promotion approval.

## Restart corrections

- Distinguish a refused request from uncertain scheduling and preserve the original request ID if acknowledgement/state inspection fails.
- Recover the narrow legacy empty-ID tracking pattern seen after an earlier completed reboot; permit closing obsolete tracking without claiming another reboot or automatically retrying.
- Complete service checking only after authenticated changed-boot evidence for the matching accepted/unconfirmed request.
- Minimise uses one shared persistent status toast. Opening details removes the toast while the modal is open; completed/refused results are closeable.
- Top-align progress content and give reboot-context wording when scheduled jobs block admission. Retain actual Ubuntu requirements and all existing maintenance exclusion.
- Preserve test.4 WordPress/site-switcher fixes and earlier runtime-detail/card changes.

## Evidence and limits

189 frontend TAP tests plus eight maintenance presentation checks, dashboard typecheck/build, website/functions/demo/SSR/prerender/docs builds, full server/hostsystem suites, focused race checks and vet pass at production-identical implementation `ed83df1b2c0603d51c59f0486ef5fcdc05a19ae9`. The release commit changes version metadata/changelog only. The official build repeats frontend checks/builds and the full Go suite on the exact tag.

The source record retains the initial multiline-import test-loader failure and final passes, including the customer tracking pattern, negative recovery cases, duplicate/refusal/uncertainty paths, changed-boot gating and actual shared-toast visibility. Mounted tests stub modal focus/transitions; they do not establish browser geometry or a live reboot.

All eight public file digests/checksums, four exact source/tag/hosted attestations, both ELF/Go1.26.3/CGo0 images and all 19 tool pins, actual AMD version/base contract and embedded feature markers pass. The branded exact-tag installer matches the public signed script. Full tuples and proof are in the adjacent JSON.

No VPS/VM deployment or reboot, new native ARM/installer acceptance, bridge/helper authority/installer/workflow/tool-pin change, main/latest/stable promotion or older immutable asset replacement occurred. Existing read-only customer boot evidence proves the earlier restart, not acceptance of this new release on that VPS.

## Install this exact test

```bash
curl -fsSL https://fluxo.fabiotech.dev/releases/v0.4.58-test.5/install.sh -o /tmp/fluxo-0.4.58-test.5-install.sh && sudo env FLUXO_VERSION=v0.4.58-test.5 bash /tmp/fluxo-0.4.58-test.5-install.sh
```
