# Corrective bridge v0.4.55 qualification

**The official signed release and exact-public Ubuntu 24.04 AMD64 acceptance passed. Production main/website/latest promotion is pending in this record.**

Immutable release [v0.4.55](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.55), ID `454442567`, source `e5e27a7925e315150ff43987d474b647613aabfa`, official [build 45023760211](https://github.com/FabioTECH1/fluxo/actions/runs/45023760211). This correction creates private helper streams before asynchronous startup and adds targeted exact-release retry guidance. There are no Go runtime, dashboard, dependency, tool-pin, reader/schema or execution-authority changes from v0.4.54.

## Artifact and regression evidence

Hosted and independent local checks verified all eight public files against immutable API identities, SHA256SUMS and exact source bytes. All four subjects passed source/tag/official-hosted-workflow attestations using the pinned verifier. Both ELF architectures have Go 1.26.3, CGO=0, the exact source revision and all 19 official tool assignments plus the correct version; signed bridge contract, metadata-only AMD64 commands, tool manifest and embedded bridge UI passed. The official workflow’s generated files explain `vcs.modified=true`; this is not a clean-development-tree claim.

- AMD64: `4be2cf6e84a9c0412f313721dedf432d227a2a1f1b17197639c0c1c6f7d4a418`.
- ARM64: `0e9f87f7d240afbdcbe7f5f3b2506d5e097ccf1dc092e34ed67968e1466eb02c`.
- Installer: `c0e9936b8b4ecbcf2a61aa28feaa081c17d962e2bc8fa40a4179055796d830f1`.

The actual published installer also passed the eight focused shell regressions, with the intentionally guarded local-VM case skipped. The deterministic delayed-child regression reproduces the original missing-output failure before the fix; corrected startup preserves private 0600 files, readiness/compatibility checks and release. Retry-summary checks preserve exact tags, original quoted flags, source overrides, stdout/stderr and failure status. Nine task-pass driver tests passed locally. See the [source regression record](./installer-lease-output-race.md).

## Actual public native path

[Run 45024138781](https://github.com/FabioTECH1/fluxo/actions/runs/45024138781), fixture `f787c28c7a96641f748e13fcf4cee4c4cae22c56`, passed one Ubuntu24 AMD64/cron case. Authentic public v0.4.54 binary and installer attestations were verified and its normal installer completed, then the normal signed public v0.4.55 installer ran with Node and Python selected, followed by ordinary public reinstall. Installed and running process image hashes matched the exact v0.4.55 subject. User/site/cron records, authenticated credentials, key/TLS, helper/history/archive state and selected reinstall invariants passed.

The actual owned-leased Node CLI also ran during a real 18-second calendar-cron child with 21 provider processes at entry. Waiting preceded mutation; work finished naturally. The valid lease survived, a wrong lease refused before installation, all six tool versions/payloads and reader-floor bytes were preserved, and no upgrade/holds remained after release. Fixture, hosted shims and ACL cleanup passed. No authority, nonce, journal or acknowledgement was manufactured.

The normal update took 58.392s and reinstall 57.195s in this one hosted observation; the busy CLI case took 29.676s. These are not universal performance or downtime claims.

## Limits and release channels

There is no new native ARM64, v0.4.45 full-upgrade or cold-install claim here. The source-level missing-file interleaving was reproduced against both original authenticated bridge and base installer bytes; earlier complete platform and runtime evidence retains its own source/image scope. This existing-server fixture starts with stock vendor PHP84 without the optional Redis extension, and has no active hosted Node/Python application workload or live ACME/hook execution claim. No customer VPS or local VM was changed.

The base receives the same fix and exact verified v0.4.55 identities in a separate opt-in test release off main/latest. Its additional Node check-result wording is base-only. Already-admitted bases update directly; a new bridge handoff needs a base release explicitly admitting these exact images. Published prior tags/assets remain untouched.

## Exact bridge command

```bash
sudo curl -fsSL https://github.com/FabioTECH1/fluxo/releases/download/v0.4.55/install.sh -o /root/fluxo-install-0.4.55.sh &&
sudo env FLUXO_VERSION=v0.4.55 bash /root/fluxo-install-0.4.55.sh
```

Follow any original exact-image recovery/review instructions first. Final main CI, Pages, public installer/feed and immutable post-promotion identity checks will be appended after those gates pass.
