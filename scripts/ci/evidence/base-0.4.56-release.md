# Fluxo v0.4.56 clean base release

The immutable signed base release is qualified for production promotion. Promotion state is recorded in the companion JSON; no main/latest change is claimed before that state is updated.

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

## Limits

These are existing-server upgrade/reinstall cases. There is no new cold-fresh, actual reboot, live certificate issuance/DNS, full runtime-apply or other-OS native claim. Source-scoped System/Network/native records remain attributed to their original images. No local VM or user VPS was touched.

See the [companion JSON](base-0.4.56-release.json) for full sanitized public reports, asset identities, source scopes and final promotion/website disposition.
