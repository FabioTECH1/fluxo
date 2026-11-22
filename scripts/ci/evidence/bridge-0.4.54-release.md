# Stable bridge v0.4.54 qualification

**Released on main/latest as stable v0.4.54. Exact public Ubuntu 24.04 AMD64 upgrades, main CI, Cloudflare Pages and live installer/docs/feed verification passed.**

Immutable stable release [v0.4.54](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.54), release `454361228`, source `b1677c94960680022e7185544bb4aaab03712fef`, official [build 45011704268](https://github.com/FabioTECH1/fluxo/actions/runs/45011704268). The daemon, installer and dashboard production sources are byte-identical to qualified bridge-test.7; the stable image/version/source identities are distinct.

## Public artifact integrity

All eight public files were checked against immutable GitHub metadata and SHA256SUMS, and all four signed subjects were verified against the exact source/tag/official hosted workflow. Hosted verification and an independent local cached-byte verifier passed. Both Linux ELF architectures, Go 1.26.3, CGO=0, all 19 official pins plus the exact version assignment, embedded bridge UI, source-identical installers/LICENSE, tool manifest, signed bridge contract and four optional profile features passed. Generated official files explain the recorded `vcs.modified=true`; this is not a development clean-tree claim.

- AMD64: `345b52da04ad779ffc5f1d14bc6279592face802ada55b1198b5e08a29f846e6`.
- ARM64: `e39f4d2fd5c2f6a1d34318af7ca50f61a13229832d1a1c99918ec616363b222b`.
- Installer: `3064cd5c92805dcbc0b96b67c4db490bfe4af62cff3eb26921146c91463eb617`.

## Actual public native paths

The [trial7/Cronie case](https://github.com/FabioTECH1/fluxo/actions/runs/45012089645) passed at fixture `d92162f90a565443f2cac94532bd5c1d73b6cb48`. It used authenticated immutable public trial7, then the normal public stable54 installer with Node and Python selected, ordinary public reinstall and the actual owned-leased Node CLI during a real 18-second cron child. Installed and running process bytes matched the stable AMD64 image. Records, authenticated credentials, key/TLS, old helpers and archives were preserved; six tool versions/payloads and reader-floor bytes survived busy-Node maintenance. Wrong lease refused before mutation, work finished naturally, holds/upgrade state cleared and cleanup passed. This sample observed 61.456s for update and 46.926s for reinstall; it is not a universal time or downtime claim.

The [v053/cron case](https://github.com/FabioTECH1/fluxo/actions/runs/45014574880) passed at fixture `d9d810a74270e415375f544ab551429bca25346c`. Its original signed v053 installer completed before stable54 ran. The fixture waited for actual registered startup acknowledgements without making global host qualification mandatory. The normal stable54 selected-options install, ordinary reinstall, image/data/credentials/key/TLS/helper/history preservation and real busy-cron Node case all passed. The old fixture acknowledged Composer and cleanup but had no enabled certificate registration: the optional hook-refresh probe is explicitly unexercised, not silently greened. No registration, nonce, seal or acknowledgement was synthesized.

## Retained attempts and limits

Three earlier v053 preparation attempts remain in the JSON record: an original short-wait refusal before target execution (including its old security-rollback warning), an overly strict three-entry fixture assumption and an absent optional certificate-task assertion. All disposable fixture cleanup passed. Corrections affected test preparation/observation only; stable tags/assets and production code were unchanged.

These existing-server fixtures have no active hosted Node/Python application workload; there is no new stable54 cold-install or live ACME/hook execution claim. ARM64 bytes/build/provenance passed, but no new native stable54 ARM install is claimed. Earlier source, audit, native-profile and trial evidence retains its original source/image scope. No local VM or customer VPS was changed for this qualification.

## Base channel

The separate immutable [base-test.2 preview](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.54-base-test.2) binds these exact stable54 images. Ubuntu24 AMD64 stable54→base2 and base-test.1→base2 direct update paths passed, with ordinary updates, genuine execution, data/key/history/analytics-state and Node preservation. It stays off main/latest. Older base-test.1 assets retain their original bridge policy and were not replaced; admitted bases stay on their explicit base installer.

## Install this bridge

```bash
sudo curl -fsSL https://github.com/FabioTECH1/fluxo/releases/download/v0.4.54/install.sh -o /root/fluxo-install-0.4.54.sh &&
sudo env FLUXO_VERSION=v0.4.54 bash /root/fluxo-install-0.4.54.sh
```

Complete any interrupted durable operation using its original exact release first. Saved component choices are reused. The same signed release is now latest; the final production results follow.


## Production promotion completed

The bridge was fast-forwarded to main at `8b83e6eb8fe939a355407ee9d6bbc445d645413d`. [Main Test 45015631222](https://github.com/FabioTECH1/fluxo/actions/runs/45015631222) and Cloudflare Pages deployment `211b3453-0142-47d0-8f34-a5a697c26150` both passed. Production daemon/installer/dashboard files still match immutable release source `b1677c94960680022e7185544bb4aaab03712fef`; subsequent candidate-workflow, fixture and documentation changes do not change the signed payload. This final record changes only the two evidence files.

Before promotion, the public `/install.sh` SHA256 matched `3064cd5c92805dcbc0b96b67c4db490bfe4af62cff3eb26921146c91463eb617`. Landing, documentation index, installation/upgrade instructions, GitHub, API, files and bridge operations pages all returned HTTP 200; the relevant instructions contained v0.4.54 and the qualified base-test.2 link.

At 2026-11-22T11:08:28Z, GitHub latest and the public `/api/v1/releases/latest` feed both reported v0.4.54. Release ID `454361228`, tag/source, all eight asset IDs/sizes/digests and immutability were unchanged by promotion. The separately immutable base-test.2 remains a prerelease off main/latest, with unchanged assets/tag. Existing immutable releases were not overwritten. No local VM or customer VPS deployment was performed.
