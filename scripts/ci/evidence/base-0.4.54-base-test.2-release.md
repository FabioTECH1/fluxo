# Clean base v0.4.54-base-test.2: stable bridge handoff

This immutable opt-in preview is ready for **Ubuntu 24.04 AMD64 update trials**. It supports completed stable v0.4.54 bridge → base2 and admitted base-test.1 → base2 directly. It remains off main/latest. The [JSON record](base-0.4.54-base-test.2-release.json) retains exact assets, both independent verifiers, all three native case reports, the failed fixture assertion and its disposition, timings and limits.

| Identity | Value |
|---|---|
| Release source | `58fe706895a80f64374b76d6e28d35f1211fad1c` |
| Release | [v0.4.54-base-test.2](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.54-base-test.2), immutable prerelease `454368236` |
| Official build | [45012710975](https://github.com/FabioTECH1/fluxo/actions/runs/45012710975), first attempt passed |
| AMD64 SHA256 | `631fb49e8a275357741cf37790f431a913613e9c580ed029b5f23fbb33bc24ac` |
| ARM64 SHA256 | `44e19f034dcd093d8142c5c021842df8834e97443b59bb6decfb42a420f95f3f` |
| Installer SHA256 | `5573c252562d67bd3305f775050e7abb01a395cf009ffd14dd9cd75114539de4` |

All eight public assets match their immutable metadata and SHA256SUMS. Four attestations bind both binaries, installer and bridge contract to the exact source, tag and official hosted workflow. Hosted and two independent local checks passed both ELF architectures, Go 1.26.3, CGO=0, the 19 official assignments plus version, full tool manifest, source-identical installer copies/LICENSE, approved embedded UI and actual AMD64 metadata-only CLI. The new release has no preview-admission linker flag or preview tuple. Official generated build metadata is recorded as observed, not relabeled pristine.

The primary predecessor is the exact immutable stable [v0.4.54 bridge](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.54), source `b1677c94960680022e7185544bb4aaab03712fef`: AMD64 `345b52da04ad779ffc5f1d14bc6279592face802ada55b1198b5e08a29f846e6`, ARM64 `e39f4d2fd5c2f6a1d34318af7ca50f61a13229832d1a1c99918ec616363b222b`. Superseded v0.4.51/52/53 and the old trial7 images are recognized for refusal, including beside a surviving base receipt. They are never admitted from a version claim or that receipt. The test.1 preview exception is retired. Existing admitted bases stay on the direct path, and the new checker excludes stable v0.4.54 from its base update banner while retaining later base discovery.

Identity/key, current schema, archive/floor/reader contracts, actual service image, owned lease/exclusion, pending recovery, real acknowledgements and rollback controls remain. No global reader floor/schema or authority format changed, no historical conversion writer was imported, and no manual host certification or optional GitHub report became mandatory. Runtime service, server/database and UI source trees are byte-identical to the previously qualified branch; their hashes are recorded in JSON. This is a policy/channel/version/docs correction, not a new runtime feature.

| Native stock-cron AMD64 case | Result |
|---|---|
| Completed public stable054 → base2 → ordinary full update, Node selected | Passed in the AMD64 bridge job of [45013098094](https://github.com/FabioTECH1/fluxo/actions/runs/45013098094), fixture `fa2e5fb` |
| Public base-test.1 → direct base2 → ordinary full update | Passed in [45014113804](https://github.com/FabioTECH1/fluxo/actions/runs/45014113804), fixture `c1bdaf8` |

The stable-bridge fixture starts with authenticated public6 and a stock managed PHP8.4 installation without optional Redis. Its normal signed full predecessor installer creates real execution state before the normal stable054 update. It is not a cold predecessor-install claim. The old-base fixture starts through the normal public base-test.1 fresh installer without optional Node/Python/database/Redis. Its fresh setup is fixture preparation, not new native fresh-base2 acceptance. Every actual update uses public downloads and exact signed assets, without a local-binary/hash override or attestation bypass. `--reinstall` forces the full ordinary same-image path rather than the version shortcut.

Both successful routes preserved original database columns, credentials, UID/GID, keys/TLS, site/cron IDs, deployment history, protected helpers/archives/history, and analytics policy/aggregates. They exercised healthy matching public binaries and proc images, served the embedded UI, real admin/SSH/application access, actual deployment timestamps, genuine root/Scheduler acknowledgements and execution, consumed authority/replay refusal, dropped privileges and natural drainage. No retained managed jobs, authority, nonces or acknowledgements were edited/deleted to obtain a pass.

The selected Node route authenticated and enrolled the existing payload on handoff, then reused its protected proof during the ordinary update. No package-manager registry installation/preparation ran, and executable bytes, ownership/links and all six versions remained: Node `24.19.0`, npm `11.17.0`, Corepack `0.35.0`, pnpm `11.20.0`, Yarn `4.18.0`, Bun `1.3.14`. The unchanged leased-preservation coordinator's real 18-second-child test remains attributed to [base-test.1](base-0.4.54-base-test.1-release.md); it was not repeated or relabeled for base2.

The first direct-base case successfully updated and reinstalled but then failed a fixture assertion. The shared analytics probe explicitly disables/drains collection after checking an inherited policy. The former oracle omitted that actual choice and required enabled collection on its next call. The corrected fixture snapshots master/site booleans, compares their exact values and retained aggregates before any origin request, and does not reenable disabled collection to obtain a sample. New regressions retain refusal for lost policy, changed selection, missing history and nonboolean evidence. Only this affected case was rerun; the public source/assets were unchanged. The final direct case proves manual retention0, site enabled and master **disabled** survive the ordinary update with three retained requests and no new traffic sample.

| Single hosted public-installer observation | Duration |
|---|---:|
| Stable054 → base2, Node authentication/preservation selected | 50.23 s |
| Ordinary base2 update with Node proof reuse | 35.67 s |
| Base-test.1 → base2 directly, no optional components | 22.64 s |
| Its ordinary base2 update | 21.62 s |

These are external monotonic stdout-arrival samples with different cache/network conditions. Public transport/provenance are included. They are not universal speedups, separate dashboard-downtime measurements, or new performance optimization evidence.

Local affected admission/CLI/checker Go tests, targeted race, vet, embedded UI/typecheck and website/docs builds passed with Go 1.26.3. The initial harness passed 111 tests; the corrected actual-selection oracle passed 113. The official release workflow passed its required frontend and full backend checks. No additional broad audit or matrix was started.

Use the exact base2 installer for a new server or an already-admitted base, including base-test.1:

```sh
curl -fsSL https://github.com/FabioTECH1/fluxo/releases/download/v0.4.54-base-test.2/install.sh | sudo env FLUXO_VERSION=v0.4.54-base-test.2 bash
```

Existing older or experimental **bridge** installations complete stable054 first, then use the base command. Already-admitted bases do not install a bridge because of version ordering. Finish any interrupted operation with its original exact installer before changing releases.

```sh
curl -fsSL https://github.com/FabioTECH1/fluxo/releases/download/v0.4.54/install.sh | sudo env FLUXO_VERSION=v0.4.54 bash
```

Limits remain explicit: no new native ARM64/Cronie/Ubuntu22/26 or direct fresh-base2 case; no PHP/Node Apply or migration, real Reverb/ACME/DNS/GitHub credential or customer-VPS claim. Prior fresh and busy-preservation results retain their original public source. The prior ARM predecessor APT100 was not retried or bypassed. The user-skipped interruption remains excluded. Necessary historical readers/helpers/archives and application/deployment/security logs remain.

All saved cases report successful cleanup. This task started no local VM; all three existing guests were observed stopped. At this qualification checkpoint main remained `c4d8821b27b9a65f84f7225a746fcc2a023861d6` and latest remained v0.4.53. The separately authorized stable054 bridge promotion is owned by the originating chat and was still pending its own final gate; this base task did not promote it. Base2 stays off main/latest, and base-test.1's tag and eight assets remain immutable and unchanged. No customer VPS was changed.
