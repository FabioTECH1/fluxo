# Fluxo v0.4.56 clean base release

The immutable signed base release has been promoted to main and GitHub latest. The production website deployment uses `b1573c9dba53ef26ad162fe47e643f01266d408b`; later evidence-only commits do not change the signed product source or deployed feature documentation. The public update feed returned v0.4.56 after its normal cache expired; the companion JSON records that final observation.

- Release source: `c9047da3a3b0f2d86aee565efde1efe25455f45c`.
- [Official release build](https://github.com/FabioTECH1/fluxo/actions/runs/45174794158): PASS.
- [Exact-public native acceptance](https://github.com/FabioTECH1/fluxo/actions/runs/45175745697): all three jobs PASS.
- Eight public assets, four exact-source/tag/hosted-workflow attestations, both ELF architectures, Go 1.26.3/CGo=0, all 19 tool assignments, current base contract and new embedded UI markers passed hosted and independent cached-byte verification.
- All 146 frontend tests plus 8 console checks, dashboard build/typecheck, site/demo/SSR/prerender/docs builds and the full local Go suite passed.

## Exact-public installer routes

| Native Ubuntu 24.04 route | Result | Base install |
|---|---|---|
| amd64-base | PASS, preserved data/history and clean fixture cleanup | 28.425s |
| amd64-bridge | PASS, preserved data/history and clean fixture cleanup | 48.944s |
| arm64-bridge | PASS, preserved data/history and clean fixture cleanup | 33.043s |

Each route also passed an ordinary reinstall and genuine root-task/Scheduler execution, retained data/key/history/credential/archive checks and feature-state preservation. The admitted base-test.3 route entered directly; both bridge routes completed automatic handoff. AMD64 bridge additionally retained its selected Node toolchain. Timings are single hosted observations, not universal latency or speedup claims.

## Features and documentation

The release includes Traffic reports and retention-aware ranges, Site Settings > Network password protection and exact redirects, wildcard routing with separately configured DNS/SSL, daily/manual Nginx package awareness with SSH instructions, supported Ubuntu security-update controls and manual restart/service reporting. Completed restart results use the shared modal and operation/boot/result-scoped dismissal; the Ubuntu restart-required warning remains persistent. Routine success/unknown attempt badges are omitted, healthy GitHub accounts have ordinary token actions, and Domains navigation retains its list.

README, installation/upgrade, feature guides, monitoring, API documentation and the docs index were reviewed for stable-base wording. Earlier bridge/base-test tags and assets remain immutable; historical qualification records were not rewritten.

## Production publication and supplemental CI

GitHub latest and the live public release feed both identify immutable `v0.4.56`.
The production website installer matches the
signed release installer exactly. The live docs index identifies v0.4.56, and the
Traffic, Network, Nginx and System feature guides returned HTTP 200 with their
current content. Main's ordinary test and Cloudflare Pages checks passed.
Previous v0.4.55 bridge and base-test.3 asset identities remain unchanged. The
contained temporary public-acceptance branch was deleted after all runs ended.
No blog was published as part of this release.

The automatically triggered [baseline candidate Node job](https://github.com/FabioTECH1/fluxo/actions/runs/45178420940)
is retained separately as **failed**. Its earlier fresh setup, ordinary update,
authenticated enrollment, offline proof reuse and warm-update preservation
phases passed. Its final injected missing-link check received the real
Scheduler-busy refusal before the expected `pnpx` diagnostic, so it did not
qualify that negative-link/explicit-repair scenario. No unchanged native retry
or runtime patch was made. This development candidate is not the signed public
artifact; the three exact-public release jobs above all passed, including
selected Node preservation on the AMD64 bridge route. The full failure and
cleanup result are recorded in the companion JSON.

## Limits

These are existing-server upgrade/reinstall cases. There is no new cold-fresh, actual reboot, live certificate issuance/DNS, full runtime-apply or other-OS native claim. Source-scoped System/Network/native records remain attributed to their original images. No local VM or user VPS was touched.

See the [companion JSON](base-0.4.56-release.json) for full sanitized public reports, asset identities, source scopes and final promotion/website disposition.
