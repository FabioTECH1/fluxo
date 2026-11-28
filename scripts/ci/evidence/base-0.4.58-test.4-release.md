# v0.4.58-test.4 opt-in release

Immutable prerelease [461505321](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.58-test.4), source `e4d2d441c00c7e6aba58401d87fb8d5de677337e`. [Official build](https://github.com/FabioTECH1/fluxo/actions/runs/46139301465): success. Main/latest/stable remain unchanged, awaiting user testing and explicit promotion approval.

## WordPress switching corrections

- Keep WordPress configuration open for another WordPress site; otherwise open **Settings > General**.
- Leave cached editors idle off-page and reject late responses/errors after navigation. Check the target application before requesting WordPress configuration.
- Preserve unsaved-change guards, masking, ordinary editor caching and mutation-based refresh. Earlier restart/navigation/runtime-detail changes are retained.

## Evidence and limits

182 frontend TAP tests and eight console checks, dashboard typecheck/build, and full website/functions/demo/SSR/prerender/docs builds pass at production-identical implementation `6ac150c8f41eea77292d696505da3edc0a8fed52`. The tagged commit changes version metadata/changelog only. The official build repeats frontend checks/builds and the full Go suite on the exact tag.

Before the fix, component regressions reproduced hidden foreign-site configuration reads and late inactive-error toasts. The normal nested-layout navigation control passed before and after; a separate nested-layout late-rejection case passes after the fix. WP-to-WP switching, unsupported direct routes, delayed metadata, unsaved-change cancellation and repeated non-WP navigation are covered. This does not claim independent reproduction of the exact customer's live browser cache timing.

All eight public byte identities/checksums, four exact source/tag/hosted attestations, both ELF/Go1.26.3/CGo0 builds, all 19 tool pins, actual AMD version/base contract and inherited feature markers pass. The branded installer matches the published script. Exact public subjects and source-scoped log hashes are in the adjacent JSON.

No VPS or VM deployment/reboot, new native ARM acceptance, bridge/installer/workflow/tool-pin change, or replacement of older immutable assets was performed. This is an opt-in trial, not main/latest/stable promotion.

## Install this exact test

```bash
curl -fsSL https://fluxo.fabiotech.dev/releases/v0.4.58-test.4/install.sh -o /tmp/fluxo-0.4.58-test.4-install.sh && sudo env FLUXO_VERSION=v0.4.58-test.4 bash /tmp/fluxo-0.4.58-test.4-install.sh
```
