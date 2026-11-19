---
title: Baseline bridge
description: Inspect and certify an existing Fluxo installation before a future baseline release.
---

# Baseline bridge

The bridge keeps historical upgrade and recovery support available while an installation is prepared for a future clean baseline. Installing the bridge binary does **not** certify the installation. The current promoted bridge is [v0.4.49](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.49). Use the normal installer; supported reader conversion and Scheduler compatibility are automatic. Earlier candidate records below are historical snapshots; their channel status describes the time of each trial.

## v0.4.50 patch candidate

The next candidate adds an explicit, password-confirmed administrator recovery
for an unverified legacy GitHub connection whose saved token is rejected. It
preserves the local connection and site IDs, repositories and private keys while
establishing the chosen new token's numeric identity. A known numeric identity
cannot be overridden. Recovery also requires a current session and the exact
reviewed site/repository list; it records the authorization atomically with the
credential update. See the [GitHub recovery guide](/deployments/github#recover-an-unverified-legacy-connection).

The candidate also includes the shared filename-based editor colours and the
website installer's bounded package-download retry. Database schema, execution
reader formats, Scheduler and PHP/Node runtime authority are unchanged from the
promoted bridge. Qualification is focused on these changes; the eight-profile
native matrix and historical recovery passes remain v0.4.49 evidence, not new
v0.4.50 native runs. The additional early-interruption native case remains
explicitly skipped. The candidate stays off latest pending exact public-artifact
verification and the affected user's GitHub reconnect trial.

## Current v0.4.49 qualification

The immutable release is bound to source
`dead68d06a2201d410b9000d319dfdc2973970cf`. All eight assets and four signed,
source-bound hosted-workflow attestations passed verification. Both architecture
builds contain the requested GitHub and shared-notice UI, the desktop Git-field
layout and compact-switch alignment. Build metadata reports `vcs.modified=true`;
this record does not claim a pristine build.

The exact public ARM64 artifact passed all eight reviewed profiles: Ubuntu
22.04/24.04/26.04 cron fresh installs, Noble Cronie, v0.4.47 upgrades on 24.04 and
26.04, and managed Node.js/Python provisioning on 24.04 and 26.04. Each obtained
genuine guarded Scheduler and root-task execution. See
[run 44515336839](https://github.com/FabioTECH1/fluxo/actions/runs/44515336839).
The exact public AMD64 binary also passed an ordinary Ubuntu 24.04 upgrade from
authentic v0.4.40 state under unchanged Cronie. Original database rows, credentials,
keys, helpers and history were preserved. All five enabled tasks acknowledged
the new registrations, and a scheduled root task completed with its fresh nonce.
The installed and running images matched the published digest; a healthy normal
installer rerun exited 0. The local fixture used parallel public HTTPS range
downloads for the binary and pinned verifier because its single-connection rate
was too slow. Installer checksums and signatures remained enforced. That transport
adjustment and the original controller interruption are disclosed in the record;
this is not a cold single-connection transfer or native crash-test claim.

The [v0.4.49 release record](https://github.com/FabioTECH1/fluxo/blob/main/scripts/ci/evidence/bridge-0.4.49-release.json)
records the exact public AMD64 upgrade result and transport, asset hashes,
source regression evidence, promotion and the skipped case separately.

The release owner explicitly skipped one additional early-interruption native
scenario. It remains unverified natively and is not counted as a pass. Isolated
regressions cover the correction; ordinary upgrade passes do not qualify that
crash boundary. No application guard or receipt check was disabled. Qualification
covers these reviewed profiles and is separate from optional whole-host
certification, arbitrary custom providers, imported historical ARM authority,
live ACME renewal and full Laravel application behavior.

## Recovery correction and retained v0.4.48 evidence

A final source review found an early interruption window
in v0.4.48: after its installer records the operation and plan digest, retained
historical helpers can still compact execution history before the first
conversion fence. The resulting legitimate source change prevents exact replay.
The v0.4.48 tag and assets remain immutable and off latest. The v0.4.49 source
correction places a durable task-writer guard before publication of the operation
pointer or any helper/core replacement. Same-image recovery can identify the
staged plan from that guard if publication of the ordinary pointer was interrupted.
This applies to supported first conversions of existing schema1/2 task cores,
including unchanged and prior-boot providers. The guard remains until the exact
converted task core and prepared reader floor are durable. Ordinary dispatch
continues to refuse until commit. A newer
authenticated installer can withdraw an older unfenced operation only when it
proves that no conversion target was written and validates the narrowly supported
historical journal advancement. It preserves the archive and advanced live
history before replanning. Unknown changes or partial conversion remain refused.

**Additional native case skipped:** the release owner explicitly asked to skip
the early-interruption VM scenario and proceed with the separate public-artifact
checks. The boundary remains unverified natively; isolated regression passes
are retained as separate evidence. Earlier passing cases below do not qualify
that boundary. The v0.4.49 release record contains the separate artifact results.

The corrected source at `a708889` passed the eight-profile ARM source matrix and
a normal Ubuntu 24.04 amd64 upgrade from authentic v0.4.40 state under an
unchanged Cronie provider. All 64 selected regression test/subtest results passed
with no skips. The normal installer preserved original rows, credentials, keys,
helpers and execution history; all five enabled tasks obtained genuine
acknowledgements. This was a locally SHA-authorized source build, not a
public-artifact or interrupted-upgrade qualification. See the
[v0.4.49 candidate record](https://github.com/FabioTECH1/fluxo/blob/main/scripts/ci/evidence/bridge-0.4.49-candidate.json).

The normal installer preserves supported historical upgrades, automatically
converts reader formats and renews supported Scheduler compatibility. Separate
activation or policy-renewal commands are not part of that upgrade procedure.
The future clean baseline is a separate release; this bridge retains historical
upgrade and live recovery support.

Final review identified and corrected combined-archive reader admission,
dispatcher image binding, interrupted historical conversion after a new boot,
and GitHub identity/deploy-key concurrency. New GitHub connections and recorded
reconnect links bind the provider's numeric identity, including account renames.
An expired legacy credential that cannot prove the owner preserves linked sites
and requires explicit owner recovery rather than guessing an association.

An exceptional second Cronie replacement during a pending historical conversion
keeps the original operation and asks for an operator-approved reboot followed
by the exact same release's normal installer. Recovery verifies the new kernel
boot and unchanged provider contract, then still requires real cron receipts.
See the [recovery procedure](/reference/cli). This is not a prerequisite for
ordinary upgrades.

Before the additional writer-guard correction, source `b564dcb` passed both real Ubuntu 24.04 amd64 reboot cases:
a task-only conversion and a combined Scheduler/task conversion. Each was
interrupted after task-core publication but before the reader floor, refused a
second same-boot Cronie generation, and recovered through the same normal
installer after a genuine guest reboot. The panel had naturally exhausted its
restart attempts; no manual failed-state reset was needed. Original operation
and archive bytes remained intact, task receipts were false at commit, and real
current-boot task and loader acknowledgements followed. These runs used the
explicitly SHA-authorized local image built from `b564dcb`, not public artifacts.
[Source validation record](https://github.com/FabioTECH1/fluxo/blob/test/arm64-bridge-acceptance/scripts/ci/evidence/bridge-0.4.48-source-validation.json)

The immutable `v0.4.48` release is bound to source
`c1b4a518de3fceaf72b3db0a2a6ec58392f3bf73`. All eight assets and four
source-bound hosted-workflow attestations passed verification. The amd64 image is
`870e81648a4320575fadcd72d5e4232ebc91ffe2cfc26f5034adbcf918096673`;
the arm64 image is
`465aee7b141e2e7c3a1d1df7a78485768e7b0a981d657b93ef1fb10d7aea59ae`.
Both contain the requested GitHub and shared-notice UI, desktop Git-field layout
and compact-switch alignment. Build metadata reports `vcs.modified=true`; the
record does not claim a pristine build.

All eight required native ARM profiles passed using those exact public files:
Ubuntu 22.04/24.04/26.04 cron fresh installs, Noble Cronie, v0.4.47 upgrades on
24.04 and 26.04, and managed Node.js/Python provisioning on 24.04 and 26.04.
The first upgrade attempts correctly refused while predecessor PHP discovery was
active. The fixture observer was corrected to wait for the old daemon's delayed
startup checks; original failed runs remain failed records. No installation
authority or receipt was injected.

Both exact-public amd64 reboot recovery cases also passed. The first normal
installer replay restored a panel that had naturally failed at boot, preserved
the planned registrations with receipts false at commit, and then obtained real
new-boot task ACKs. The combined case additionally completed independent
Scheduler retirement and loader acknowledgement. The public binary's held-lease
probe waited for a genuine cron child to drain naturally and refused an unknown
surviving child while recorded work was settled.

The [release qualification record](https://github.com/FabioTECH1/fluxo/blob/main/scripts/ci/evidence/bridge-0.4.48-release.json)
records final gates and promotion separately. Whole-host certification, arbitrary
custom providers, imported old ARM authority, live ACME renewal and full Laravel
application behavior are outside this qualification. Historical upgrade and
recovery support remains available. Earlier preview sections below retain their
original versions, failures and limits.

## Preview bridge v0.4.48-bridge.2

The immutable, off-latest [v0.4.48-bridge.2 preview](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.48-bridge.2)
adds bounded automatic waiting for brief cron activity observed after the installer
lease is held. It repeats admission and ownership checks for up to 90 seconds and
continues only after natural drainage. Long-running or uncertain work and changed
provider contracts still stop the upgrade safely. At that preview publication, stable/latest was v0.4.47.

It also includes GitHub token expiry, replacement/reconnect and independent SSH
repository-access checks, plus the shared error/update notice frame across PHP,
the managed Node.js/npm toolchain and Fluxo. Existing private deploy keys are
retained. Reconnection restores only links recorded by a supported disconnect;
older orphaned sites are not assigned a guessed owner. Fresh and migrated GitHub
metadata schemas now agree, with tests preserving credentials, account IDs and
site links.

```bash
curl -fsSL https://github.com/FabioTECH1/fluxo/releases/download/v0.4.48-bridge.2/install.sh | sudo env FLUXO_VERSION=v0.4.48-bridge.2 bash
```

Combined source `06992ba` passed all six native ARM cases in
[run 44417026204](https://github.com/FabioTECH1/fluxo/actions/runs/44417026204).
The focused Ubuntu 24.04 amd64 v0.4.40/Cronie test in
[run 44417026994](https://github.com/FabioTECH1/fluxo/actions/runs/44417026994)
observed the installer waiting while a genuine Cronie child was live, then
continuing after natural completion without an installer retry. Provider
generation and original task bytes were unchanged. This focused amd64 check
tests pre-snapshot lease verification, not a full new-candidate installer run.
Go/UI/docs checks, affected race/vet suites and the rebuilt embedded UI passed.
[Source-candidate evidence and scope](https://github.com/FabioTECH1/fluxo/blob/test/arm64-bridge-acceptance/scripts/ci/evidence/bridge-0.4.48-preview2-native.json)
remain separate from public asset/provenance verification. Real GitHub credentials,
optional ARM Node/Python provisioning, live ACME and full Laravel Scheduler
behavior were not exercised. Installation is not whole-host certification.

### Published bridge.2 artifact record

[Release CI 44419836528](https://github.com/FabioTECH1/fluxo/actions/runs/44419836528)
published source `3bfdf4e06cd94a6897a5e92974368e648ea6d05d` on attempt 2.
Attempt 1 stopped when the attestation service returned HTTP 404 while fetching
a transparency-log entry; the unchanged retry passed. All eight downloaded
assets matched their API hashes/sizes and checksum manifest. All four
source-bound attestations passed; both build records matched the exact source,
architecture, CGO-disabled build and 19 official pin assignments. Both downloaded
binaries contain the requested GitHub connection and shared-notice UI. Build
records report `vcs.modified=true`; no pristine-build claim is made.

| Public preview binary | SHA256 |
| --- | --- |
| amd64 | `fba7a0ea2d6abf9199b8fdc0b04f69e02478e31d26a027c73ac8604301ebb5a4` |
| arm64 | `052a1d7af60e67685d362e6e05f4dc859b67fa85fffeaf8c88e718a8045902ba` |

[Public verification record](https://github.com/FabioTECH1/fluxo/blob/test/arm64-bridge-acceptance/scripts/ci/evidence/bridge-0.4.48-preview2.json)
retains the exact asset identities. Native execution above used source candidates,
not these public bytes. At that preview checkpoint its VPS trial was pending;
main/latest were not promoted and the default release was v0.4.47.

## Preview bridge v0.4.48-bridge.1

The off-latest [v0.4.48-bridge.1 preview](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.48-bridge.1)
combines the historical Cronie upgrade correction, PHP inventory/dispatcher/UI
fixes and native ARM64 compatibility work. At that preview publication, stable/latest was v0.4.47.
This preview retains historical recovery support while testing the next bridge;
it does not introduce the future clean baseline.

Use the normal installer with an explicit preview version for the trial:

```bash
curl -fsSL https://github.com/FabioTECH1/fluxo/releases/download/v0.4.48-bridge.1/install.sh | sudo env FLUXO_VERSION=v0.4.48-bridge.1 bash
```

No separate format activation or Scheduler policy-renewal command is part of
this supported upgrade path. Active or uncertain work, changed provider contracts
and unsupported profiles continue to stop the installer safely.

The historical correction handles acknowledged schema1/2 Noble/Cronie task
registrations whose cron generation changed during the same boot. It proves the
saved daemon is gone and its cgroup inode is absent from the complete visible
hierarchy, requalifies the unchanged saved provider contract, and requires
current and recorded execution domains to be drained. Conversion archives the
original evidence, preserves old helpers/history and requires fresh genuine
acknowledgements. Ordinary execution identity checks remain strict.

The combined-source ARM campaign passed six native cases on the tested Ubuntu
22.04/24.04/26.04 profiles, including Noble Cronie and public-v0.4.47 upgrades on
24.04/26.04. A separate AMD64 historical-fix build passed the published-v0.4.40
Node-enabled normal upgrade, surviving-work refusals, durable interruption,
exact-operation recovery and continued genuine cron execution. These are
source-candidate results on distinct images, not a single combined release
artifact qualification. [Exact source identities, evidence and limits](https://github.com/FabioTECH1/fluxo/blob/test/arm64-bridge-acceptance/scripts/ci/ARM64.md)
are retained alongside the implementation. Public release assets and source-bound
provenance are checked separately before the preview install command is handed
to the trial operator.

The final pre-publication source review also identified a first-conversion
crash window before reader-floor publication and a cross-table maintenance retry
classification error. The installer now routes a protected durable operation to
its exact archive before first-receipt waiting, and examines all maintenance
tables before admitting a read-only retry. Focused transition/SQLite regressions
cover both failures; restoring the old behaviors makes those tests fail. The
native campaigns above predate these two final corrections, so the preview VPS
trial remains separate from their recorded source-candidate results.

The PHP correction removes the unrelated Node dispatcher gate from PHP inventory;
dispatcher synchronization remains under the Scheduler gate and defers during
holds or non-quiescent work. PHP package differences appear in the review modal.
Historical ARM V1 recovery, optional ARM Node/Python/database/Redis provisioning,
live ACME and full Laravel Scheduler behavior remain outside qualification.

### Published preview artifact record

Release CI [44409878407](https://github.com/FabioTECH1/fluxo/actions/runs/44409878407)
published immutable prerelease `v0.4.48-bridge.1` from source
`2ad3aff47fe3e710b37fe3ef9dd2e9d33fbc63a6`, off latest. All eight public assets
passed independent downloaded size/digest checks, the checksum manifest matched,
and all four source-bound attestations passed with the exact tag/commit and a
non-self-hosted-runner policy. Both Go build records matched source/architecture,
CGO-disabled compilation and all 19 official toolchain/installer pin assignments.
The downloaded amd64 image passed its metadata-only version, bridge-contract and
installer-version queries. Build metadata records `vcs.modified=true`; no pristine
build claim is made.

| Public preview binary | SHA256 |
| --- | --- |
| amd64 | `b70dc160cc6b00204a719fc8ab939b68bdc8fc5724591f05e9132ec320311d96` |
| arm64 | `3106597d7ab17cdcdf232c32219713310d0587930849f342b5a482bccd1ec4e4` |

The final-source native run passed all six ARM cases on source `2ad3aff` and
candidate image `daa9beb9810513cc1f4b43b338cc45c4bbdc1efb5ef074bcb231e5b389284346`.
Five cases passed in attempt 1; the Noble public-v0.4.47 upgrade first stopped
because the predecessor's automatic PHP check was still running, before candidate
installation, and then passed attempt 2 on unchanged source. The original failed
report is retained. These source candidates remain distinct from public release
bytes.

The existing Ubuntu 24.04 amd64 v0.4.40 VPS trial initially stopped at post-stop
cron drainage verification. A read-only report later showed one idle Cronie
thread and the unchanged old panel active. The same normal preview installer
then succeeded, reporting a ready Node toolchain, healthy daemon/panel domain and
committed automatic execution formats. No manual state repair was performed.
This is user-reported installed-upgrade success, not independently collected VPS
binary/profile certification. At that preview checkpoint, UI/application acceptance
and stable promotion were pending and stable/latest was v0.4.47. [Verification record](https://github.com/FabioTECH1/fluxo/blob/test/arm64-bridge-acceptance/scripts/ci/evidence/bridge-0.4.48-preview1.json)
retains these boundaries.

## Corrective bridge v0.4.47

The normal installer handles supported format conversion, Scheduler renewal and
same-release recovery. It can also withdraw an older installer's entirely
unapplied task renewal after verifying the original installed/archived image,
intent and plan, exact predecessor floor, installation/service binding and every
unchanged target. It archives the original operation and intent without claiming
completion, restores the original bound service state and continues the upgrade.
Prepared or partially applied operations remain refused by this withdrawal path.

The interrupted v0.4.46 trial exposed a qualification failure before the reader
fence, followed by a changed Scheduler snapshot. v0.4.46 stays off latest. New
installer renewals now publish their prepared floor before volatile qualification;
the existing bounded directory retry also retains errors from absence checks.
Current protected-file, code, configuration, receipt and runtime comparisons remain
strict. The diagnostics alone do not prove which filesystem input caused the
earlier transient refusal.

Independent Astra 6.2 source review and affected race/vet tests passed. A local
SHA-authorized v0.4.47 source candidate recovered the actual stranded operation
through the normal newer installer and a normal rerun after cron jobs drained.
It preserved original plan/intent bytes, installation records, keys, helpers,
history and proofs, then obtained genuine task and loader acknowledgements.
This source trial is separate from immutable final-artifact acceptance.

The immutable CI release binds source commit
`71c35f4e7810005f3e06cc25339e86631ab9bf2b` to amd64 SHA256
`80c05bd8e57d57b9d12630dba952a37174bbf9bf52c3a45d0f9e5808ae13719e`.
All eight asset digests and four source-bound attestations passed verification.
Those exact CI binary bytes passed installed upgrades on the tested Ubuntu
22.04, 24.04/Cronie and 26.04 amd64 fixtures, preserving records, keys, retained
helpers and history, with committed formats, healthy running images and genuine
task acknowledgements. The Noble fixture also retained its historical proofs
and genuine Scheduler acknowledgement after the upgrade.

These final native upgrades used explicit local SHA transport of verified public
CI bytes; they do not claim fresh end-to-end public binary downloads. The first
Jammy installer attempt exited before helper readiness because its output file
was not yet present; the unchanged installer passed on a normal retry, preserving
the original failed log. Earlier cold public-install and actual PHP Scheduler
results remain bound to their original artifacts. Native ARM64, live ACME,
fresh optional-tool retrieval, full Laravel Scheduler behavior and arbitrary
custom-provider configurations remain outside this bounded qualification.

## Earlier qualification records

The statuses below describe earlier candidates at the time they were tested.
They do not override the current release and exact-artifact record above.

### Automatic installer reader setup

The installer now handles first execution-reader conversion after a healthy
binary upgrade or initial database creation. It releases its package/runtime
installer lease before acquiring activation admission, stops only the panel,
archives the exact source and image, commits the reader floor, then restores
and health-checks the panel. Current compatible reader6/reader7 floors retain
their exact generation and roles. Historical retirement remains pending until
its real obligations finish; conversion does not provide a job acknowledgement
or a bridge-completion receipt.

An interrupted installer records its original image, operation and restart
intent. Rerunning the same release's installer resumes before normal reader
preflight or package provisioning. Different images and unexpected evidence
remain refused. Once this phase starts, automatic rollback of the earlier
database/binary is disarmed; the compatible image and recovery archives remain
installed. Manual activation commands are reserved for separately managed
binary installations.

The immutable, off-latest [v0.4.46-bridge.8 preview](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.46-bridge.8),
amd64 SHA256 `a31a4f484c3b9af273ae01e81d5a3dfdf7b712d9156086585867d543d1ba4971`,
passed the normal public installer on the retained Ubuntu 24.04/Cronie fixture,
including automatic conversion, fresh genuine loader acknowledgement and PHP
execution. Public reinstall on a current installation preserved its exact
floor and roles. Records, credentials, keys, TLS, old helpers and historical
proofs were retained. The running public binary also serves the reviewed shared
persistent-failure UI bundle. All eight release asset digests and four
source-bound attestations were independently verified.

Separate local-SHA trials of the same reviewed implementation cover first
database creation, partial-core interruption, different-image refusal and
same-image normal-installer recovery with the original operation/restart intent.
These are distinct from the public-delivery runs. Scoped Flash reviews and the
affected race suites passed. This is a bounded Ubuntu 24.04 amd64 trial, not
whole-host certification, native ARM acceptance or promotion to main/latest;
the stable channel remains `v0.4.45`.

At the bridge.8 qualification boundary, the local candidate remained **unapproved for public production release** pending immutable delivery and final independent review. The frozen image `eb4045fdfd9f630940587146779116fe0dc7ed7bf64487a8117dff1c738e1585` passed installed Ubuntu 22.04/26.04 amd64 acceptance, including genuinely fresh reader7 installations and upgraded fixtures. ARM cross-builds do not establish native runtime acceptance. Preserve exact source/image/installer hashes when recording results; earlier passes do not transfer automatically to newly built bytes.

Historical same-boot retirement and package-drift repair scenarios use separate fixtures and record boot/PID/start/cgroup continuity. A naturally changed boot is historical evidence under its existing contract, not a substitute for the same-boot acceptance case. See [CLI commands](../reference/cli#historical-retirement-and-reader-generations) for explicit reader and registration repair operations.

### Published preview qualification

The CI-built [v0.4.46-bridge.2 preview](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.46-bridge.2), amd64 SHA256 `b6a41a0948c12f566b6b459100204978c64712607928315dd8b58debd926495b`, passed public-tag installer and provenance verification, installed Laravel Tinker TLS/WSS, Scheduler and cleanup callers, stopped-panel certification, restart and verification on the tested Ubuntu 22.04/24.04/26.04 profiles. Installation identity, credentials, TLS and retained helpers were preserved. The Scheduler caller uses an actual PHP fixture; full Laravel framework Scheduler behavior was not tested. Current delivery fixtures have Node/Python absent; Resolute uses the signed-forward APT 3.2.1 profile.

The final independent Astra gate passed the scoped source delta and qualification of these exact bytes on 22.04/26.04. The subsequent 24.04 runs are recorded separately below; they are not attributed retrospectively to that review. Native execution remains parent-executed evidence. ARM, imported Cronie authority, live ACME, fresh optional Node/Python retrieval and untested profiles remain outside this approval. The recorded `vcs.modified=true` does not establish a pristine build.

On 24.04, the exact preview upgraded an isolated clone of the preserved installed panel through the normal public-tag installer, with provenance enforcement and no local binary override. Real Laravel Tinker, dedicated MySQL/PostgreSQL PDO connections, the genuine Scheduler/cleanup callers and complete certification/verification passed. Changed loader policy initially refused Scheduler registration; explicit stopped-panel requalification preserved its predecessor history, and the subsequent real caller passed. The tested current profile uses APT `2.8.4ubuntu0.1`, libc `2.39-0ubuntu8.12` and systemd `255.4-1ubuntu8.18`.

A separate genuine published `v0.4.45` Noble installation produced an acknowledged Composer V1 invocation. An upstream-outage fault and refused final journal rename retained its original `started` authority. After natural domain drainage, explicit current reconciliation preserved the original profile, recorded an interrupted unknown outcome with independently qualified current evidence and refused replay. Its older libc `2.39-0ubuntu8.9` correctly blocked installer admission. A signed forward Ubuntu update to `8.12` and Ubuntu-required security reboot preserved the already recovered evidence; the normal public-tag installer then passed, and installed startup retained that history and received a genuine cron acknowledgement. This case used APT `2.8.3` and proves bounded Composer recovery/startup, not current cleanup qualification or recovery after a successful Composer executable replacement. The earlier completed self-update attempt remains separate evidence. Test-only network rules and immutable flags were removed; original timer states were restored.

**Promotion remains on hold:** this preview reports `immutable=false`. Provenance and digest binding do not make its GitHub tag or assets immutable. It stays off latest; the stable installer/update channel remains `v0.4.45`. GitHub immutability applies to future releases, so a new candidate must be published with it enabled and qualified against its own digest. The prepared workflow stages all eight assets in a draft, verifies uploaded digests and the tag's source commit, publishes off latest, then requires GitHub to report an immutable release. Repository settings and latest promotion require explicit operator approval.

### Cronie correction under qualification

The published `bridge.2` preview refuses Cronie before obtaining its upgrade
lease. Its stock-cron platform results do not qualify an existing Cronie server.
The current source adds a separate Ubuntu 24.04 amd64 Cronie `1.7.1-1` loader
policy, while preserving the historical `cronie-noble-1` identity and digest.
It retains the exact PAM/vendor/ELF/NSS/SELinux checks and does not switch the
installed scheduling engine. See the [source boundary](https://github.com/FabioTECH1/fluxo/blob/bridge/production-test/internal/services/scheduler/loader_cronie_sources.md).

The local prototype `e661608937fef4ba0625fb6b4b0cb12562404a63ff12e2a51e543e2fa1ffd7fd`
has passed functional upgrade from the actual published `0.4.40` binary on an
isolated installed Cronie server. Existing records, credentials, TLS, helper
bytes and history were preserved. Genuine root acknowledgement and non-root
scheduled execution continued; active work blocked installation until natural
drainage. Current-format activation and real Laravel Tinker TLS/WSS also passed.
Scoped runtime/PAM and SELinux source reviews passed. These are local functional
results with an explicitly SHA-authorized candidate, not public-delivery proof.
The replacement preview's exact CI-built artifact and remaining native checks
must pass before a new pinned installer version is recommended.

### Legacy Scheduler admission during Node setup

The immutable `bridge.3` preview contains the Cronie correction, but a
Node-enabled upgrade from `0.4.40` can still refuse with
`reconcile outstanding Scheduler launch authority before continuing` and roll
back. The retained `0.4.40` dispatcher does not check the installer marker. It
can record an unstarted invocation while the installer's lifecycle lease blocks
execution; Node setup then correctly refuses that outstanding authority.

The `bridge.4` correction publishes installer-owned branch holds with the marker
under the Scheduler journal lock. Historical dispatchers already honor these
holds, and historical registration rejects a nonempty hold map. Admission also
rechecks invocation quiescence under that lock before publication. Successful
completion and rollback release only the installer's own holds. A retry can
replace a dead installer's holds after validating its process generation and
quiescence; active, uncertain or foreign owners remain blocking.

This does not retire unconsumed invocations or relax the existing maintenance
checks. An already unresolved journal remains blocked. In the reproduced
`bridge.3` failure, an immediate retry correctly refused retained unstarted
authority; the next genuine legacy cron tick retired it through the existing
dispatcher before the fixed installer was retried. No journal was edited.

The immutable, off-latest [v0.4.46-bridge.4 preview](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.46-bridge.4),
amd64 SHA256 `faab0b2382dc90290c1335d23f675e0ea1321ebf734df9da192273a074d6d29b`,
passed the Node-enabled upgrade from a genuine installed `0.4.40` producer on
the isolated Ubuntu 24.04/Cronie `1.7.1-1` fixture. Crossing a real cron minute
with the retained old dispatcher created no new invocation authority. The
installer completed Node setup and started the exact preview image while
preserving installation records, credentials, TLS, retained helpers and history.
Killing only the test installer helper also preserved its durable holds; an
owner-checked retry and release allowed the original schedule to resume naturally.

Scoped source review, the relevant race suites and CI passed. All eight public
artifact hashes and the four source-bound attestations were verified separately.
Native installer execution used those exact public binary bytes through the
explicit SHA-authorized local transport, cached authenticated WP-CLI bytes and
an existing compatible external Bun installation. It does not qualify fresh
Bun retrieval, completion of an uncached public-download installation or the
entire historical fresh installer. Failed historical provisioning attempts and
the first refused retry remain failed evidence. This is a bounded VPS-trial
candidate; main and the stable `v0.4.45` channel remain unchanged.

### Inactive APT source backups

APT reads source-part files with the final extension `.list` or `.sources`.
Installer backups such as `ubuntu.sources.curtin.orig`, editor saves and disabled
copies with other final extensions are retained data. The bridge records their
directory membership and content hashes without parsing them as active sources.
Protected ownership, permissions, regular-file and size checks still apply;
symlinks and unsafe entries remain refused. A backup-looking name ending in
`.list` or `.sources` still receives full source syntax/protocol validation.
This does not change repositories, source files, jobs, receipts or package
execution policy. See [APT's source-file rules](https://manpages.debian.org/bookworm/apt/sources.list.5.en.html#SOURCES.LIST.D).

### Combined readiness report preview

The immutable, off-latest [v0.4.46-bridge.10 preview](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.46-bridge.10),
amd64 SHA256 `a3e4f5c6b02433c9e3a7af617844d155ed6a23cebf987f362b53af9828c6039e`,
includes the inactive-source correction and the combined inspection report
described below. Its normal public-tag installer passed existing-installation
upgrades on isolated Ubuntu 22.04, 24.04 and 26.04 amd64 fixtures, with provenance
enforcement and matching installed/running binary digests. The 22.04/26.04
sources were the exact `bridge.2` B6 image above; 24.04 used `bridge.8` A31.
Existing committed reader7 floors, installation records, credentials, keys,
TLS, historical helpers, history and old proofs were preserved. These runs
skipped optional Node/Python installation and do not establish fresh-install,
reader6, optional-runtime retrieval or full application coverage for these bytes.

The exact installed binary passed the combined independent-failure and blocked
prerequisite tests on 24.04. A subsequent 22.04 inspection passed after genuine
cron acknowledgement and natural drainage; 26.04 inspection also passed.
Inspection does not record certification. The affected race/vet suites and CI
passed, and all eight asset digests and four source-bound attestations were
verified independently. These are bounded installer and diagnostic results,
not whole-host certification or approval for main/latest promotion. The stable
channel remains `v0.4.45`.

### Historical backup destination defaults

An installation created by `0.4.4` or earlier can retain
`backup_destinations.prefix DEFAULT 'fluxo'` after upgrading. Later releases
changed the fresh-table default to `'fluxo-backups'`; `CREATE TABLE IF NOT EXISTS`
preserves the existing table. The bridge recognizes this exact historical
default while continuing to check the column type, nullability, keys, generated
columns, triggers and all other defaults and constraints.

This compatibility check does not alter the table or rewrite destination
prefixes, credentials, manifest keys or artifact paths. Current destination
creation supplies its selected prefix explicitly. The historical default must
not be replaced by editing backup records merely to pass bridge inspection.

### Retired site hook storage

Release `0.3.6` added `sites.post_deploy_script TEXT DEFAULT ''`. Release
`0.3.7` removed its consumers without dropping the column from existing
databases. The bridge permits this exact plain, nullable historical column as
retained storage. It preserves both nonempty values and `NULL`; it does not
copy them into the active deployment script or run the retired commands.
Fresh databases do not gain the retired column.

Altered types, defaults, nullability, generated columns, added constraints or
execution triggers still fail qualification. Unknown extra columns remain
unsupported. Do not drop this historical data merely to match the fresh schema.

Private SQLite fixtures covering 24 distinct fresh schemas from 167 retained
pre-bridge release tags passed current preparation and schema validation. This
is schema compatibility evidence; populated-host recovery and runtime admission
remain separate qualification requirements.

## Automatic compatibility during installation

The bridge installer handles supported execution-format, guarded maintenance
registration and Scheduler policy transitions. Users run the normal selected
release installer; separate activation or policy-renewal commands are advanced
recovery/administration tools. An already installed current release still checks
compatibility when its installer is rerun, without provisioning packages again.

Automatic enrollment retains the saved provider/profile contract and requires
fresh qualification of current supported code, configuration, account resolution
and execution domains. Distribution backend configuration digests include their
whole PAM/package closure. Supported changes, including a qualified replacement
cron process after package maintenance or a reboot, use a distinct installer-only
prepared-floor maintenance-registration renewal. Its target has fresh nonces and
requires genuine current-provider acknowledgements; original evidence is archived.
The existing explicit configuration-requalification command keeps its narrower
same-generation meaning. Historical Noble/Cronie
configuration identities retain their exact meaning. Ordinary dispatch, receipt
and runtime equality guards are unchanged. Running work must finish naturally;
unsupported inputs are preserved and refused rather than silently rewritten.

Task transitions archive original floor/core/trigger evidence using the existing
activation protocol. Loader transitions retain a protected exact plan under
`scheduler-installations/`, with `scheduler-installer.json` binding its image,
installation, service and reader floor before any publication. Interrupted setup
resumes through that release's normal installer before package provisioning.
Different images and conflicting plans are refused. Completed retries preserve
nonces, genuine acknowledgements and invocation history. Installation waits for
real cron acknowledgements; it does not create them or certify the whole host.

The immutable off-latest `v0.4.46-bridge.14` preview (source
`081093e8a4055797b4e68df62d19ea2079eb5e23`, amd64 SHA256
`c1d69f9f6b7f0b806a38fc3562cde03d273900071f6953cdb8d6b716d6aa6f5d`)
passed public-tag installer upgrades on the retained Ubuntu 22.04, 24.04/Cronie
and 26.04 amd64 fixtures. The 24.04 run used the plain installer with its
recorded component choices. A blocked task-core rename retained the prepared
floor and exact image; the same public release's normal installer recovered the
original operation without replaying provisioning. A different authentic prior
image, supplied through explicit local SHA authorization for the negative test,
was refused before mutation.

On 26.04 an authenticated unrelated `hello` package installation changed the
protected package metadata. Rerunning the same public release's plain installer
automatically renewed task and loader evidence, preserved the reader contract,
records, keys, helpers and history, and received genuine new acknowledgements
without provisioning packages again. Normal unchanged reruns on 22.04 and
24.04 preserved panel PID, epochs, nonces and acknowledgements. The retained
24.04 PHP CLI fixture executed again; this is not full Laravel Scheduler
qualification. Private-namespace refusals included a successful unmodified
control and rejected changed provider defaults, PAM and distribution inputs.

The public-run observers initially required a new Scheduler installer pointer
even when policy was already current. Those observer failures remain recorded;
corrected read-only observers verified the successful 22.04/26.04 installations
and their retained completed predecessor publications. This is bounded preview
trial evidence, not approval for main/latest, all host profiles, native ARM,
live ACME, or fresh optional Node/Python downloads. Source self-review and the
affected race/vet checks are recorded separately from earlier independent reviews.

Final independent review of the automatic-renewal delta identified two
interruption gaps and an older-journal preservation edge. Follow-up source now
persists a bound `preparing` restart record before any panel stop, reuses a
healthy panel when recovering an already-published operation, and drains panel
work before any necessary unpublished recovery stop. Preparing records cannot
claim a publication ID/digest; an `unchanged` terminal record is allowed only
after a stop/restart-only attempt has restored the panel and verified genuine
current readiness. Existing plan-bound `pending`, `published` and `complete`
meanings stay intact. Early invocation reconciliation is restricted to the
strict current schema, leaving older formats and extension fields untouched
before database/panel admission.

Regression coverage kills a subprocess at the stop boundary, checks refusal
while panel work is active and verifies reuse after publication. The guarded
root fixture also exercises the actual protected, fsynced record writer in a
private directory, with an unchanged live reader floor. Its stop callback is a
private marker, not a real service stop. The normal entry point also gives a pending Scheduler operation precedence
over fresh task renewal. It first validates the unchanged committed floor and
saved image/service/installation, restores the recorded operation, then allows
fresh work. The installer performs two health-checked completion passes, so a
cron generation changed during interruption can be renewed after the original
operation closes, without a separate user command. A local-SHA installed fixture
exercises an actual installer SIGKILL after the real panel stop, a drained cron
restart, and a normal same-image resume with retained records and genuine,
nonempty task/loader targets. This is source-candidate evidence, not new public
artifact acceptance. The immutable `bridge.14` assets remain unchanged; these
corrections require a new build before release promotion.

The final independent Astra 6.2 review passed the bounded code-merge scope at
`33b8d4c4e183d52c79f058ee090a85f69ab5e8af`, including the recovery-ordering
correction and the separate overview navigation change. Affected Go race tests,
vet and the documentation build passed. On the local candidate, a real
authenticated PHP command remained alive while ordinary installer recovery
waited; after natural completion, recovery finished with the same panel PID.
This command shares lifecycle admission, so the source review and callback
tests supply the separate evidence that published recovery does not stop the
panel. Two earlier observers incorrectly waited beyond their own command
deadlines; those failed observations are retained and are not counted as passes.

This completes the independent review for merging the reviewed source. It is
not a canonical repository-wide security scan or approval to promote a newly
built artifact to latest. The corrected release still needs its own immutable
CI artifact and exact-artifact acceptance; earlier preview results remain bound
to their recorded bytes.

A later stable core may simplify the current installation path. It must continue
to understand retained recovery readers, operations and archives until a separate
retirement transition proves they are no longer needed. Installing the bridge
is not permission to discard historical recovery evidence.

## Inspect readiness

Historical generated PHP daemon units can retain `ExecStart=php8.4 ...` while
their database command remains unchanged. Readiness accepts that legacy form
only after checking systemd's compiled executable search, protected paths and
the exact recorded argument vector. A different/shadowed interpreter, changed
arguments, custom search/environment or filesystem selectors remain refused.
Inspection does not regenerate the unit or restart the application. Scheduler
loader-policy changes remain a separate explicit review; accepting this daemon
syntax does not renew a loader epoch or acknowledge a cron trigger.

The exact `v0.4.46-bridge.12` amd64 image
`a0568ce0da7241c26ca25f52bb8bd567100a1d9eeccf68267c9a52489522a248`
passed the legacy daemon check against an unchanged, running API-created
historical-form fixture. Its unit, database command and live PID were preserved;
this is not an authentic historical-producer claim. Final source tests on native
Ubuntu 22.04, 24.04 and 26.04 also accepted real loaded legacy unit metadata and
refused a shadow PHP interpreter. A normal public installer upgrade from the
installed bridge.8 image on a separate Ubuntu 24.04 fixture preserved identity,
credentials, keys, records, retained helpers/history and its existing reader
floor. A separate Ubuntu 26.04 current-floor fixture passed its normal public
bridge.10-to-bridge.12 upgrade with the same preservation checks and returned
`ready: true`; no certification receipt was written. These results do not
certify the entire host or approve main/latest.
Two other retained upgrade fixtures refused safely on outstanding Scheduler
launch authority and replaced mapped cron code; those runs remain failures,
and no journals or launch receipts were edited to obtain the positive result.

The bridge.12 loader policy hashed the whole protected `/var/lib/dpkg/status`
file. An unrelated package transaction, including a `needrestart` update, can
therefore require the explicit Scheduler policy review documented in the CLI
reference even when Cronie's executable and runtime identity remain unchanged.
The saved aggregate digest cannot identify every changed input. The daemon
checker correction does not narrow that policy or renew it automatically;
bridge.12 therefore required a separate explicit review. The automatic installer
transition above handles supported renewals. Live evidence can still expire
after a package transaction; rerun the normal installer for supported renewal.
Existing helpers and historical policy evidence retain their original meaning.

Run from SSH with root privileges, using the installed service's data directory:

```sh
sudo /usr/local/bin/fluxo baseline-bridge inspect
```

The command reports JSON with `contract`, `ready`, `installation_id`, component `checks`, `recovery_policy` and `recorded`. Exit status is zero only when all current checks pass. It does not change the installation, create an identity/key, execute startup migrations, repair packages or record completion. SQLite inspection copies the main database and WAL into a private temporary directory and rejects concurrent source changes. Temporary SQLite sidecars belong to that copy and are removed afterward. A busy installation may require a retry.

Inspection collects independent failures in one report, including differences in several schema objects and separate pending-work or conversion checks. The `host` check includes `details` for service/security policy, packages, PHP repositories, Scheduler, guarded jobs, sites, daemons, crons, certificates, database access and Node.js. A check whose prerequisite failed has `passed: false`, `blocked: true` and an explanation; it has not been evaluated. For example, an unsupported schema blocks application-row checks, while an unavailable encryption key blocks credential checks without hiding independent host checks. Within a feature, validation still stops when continuing would rely on an unverified prerequisite. Resolve these blockers and rerun to complete the remaining checks. Any failure or blocked check keeps `ready: false`. Certification and baseline admission retain their strict validation sequence and do not use this advisory report to grant authority.

The checks cover schema semantics and integrity, credential encryption, administrator password format, historical conversion remnants, pending deployments/commands/backups/runtime generations and holds, host configuration, guarded cron/Scheduler registrations, managed site ownership, service groups, active certificates, engine-native database/account/grant catalogs (plus TCP checks where a legacy password is retained) and managed Node selection. Historical recovery readers and archives remain supported; this contract does not authorize removing them.

Resolve reported actions using the existing supported feature/recovery flow. A stopped worker or successful operation row does not clear retained holds. Do not delete journals, conversion tables, certificates or recovery archives to make a check pass. Legacy password storage requires a successful sign-in/password change or explicit account recovery. Unknown custom cron profiles and service configurations are refused rather than automatically rewritten. Security qualification checks effective key-only SSH policy, the restricted passwordless PHP reload sudo profile and the loaded authentication jail/filter/firewall action. Stock Ubuntu 22.04 iptables and Ubuntu 24.04 nftables action profiles are resolved for both IPv4 and IPv6 before comparison. SSH Match profiles and unrecognized security customizations require explicit review. Site qualification follows the Nginx include graph and compares included vhosts against current domain, document-root, certificate and runtime intent; durable custom vhost overrides are preserved and require a separately reviewed profile.

## Preserve the service context

Inspect the actual `fluxo.service` configuration before invoking the command. For an installed HTTP service or a custom directory, pass those same values explicitly:

```sh
sudo env FLUXO_USE_HTTP=1 FLUXO_DATA_DIR=/absolute/installed/directory \
  /usr/local/bin/fluxo baseline-bridge inspect
```

The bridge compares these values with the effective installed service environment. It refuses environment files, inherited/unset assignments, custom execution wrappers, filesystem remapping, stale unit definitions and drop-ins until their service profile has been reviewed. The initial supported profile uses the installer's direct production service definition. The installer supports only the default production data directory and refuses a custom one before runtime recovery or provisioning. Do not select an unrelated directory or change the service's HTTP/TLS mode merely to pass qualification. Missing keys or a partial existing TLS pair require recovery. Normal optional components remain optional; no command here installs an engine, runtime or process manager.

## Record completion

After resolving readiness blockers and allowing admitted work to finish, stop only the panel from SSH. Keep application daemons and scheduler services in their intended states:

```sh
sudo systemctl stop fluxo.service
sudo /usr/local/bin/fluxo baseline-bridge certify
sudo systemctl start fluxo.service
```

Run the final start command even if certification reports a blocker. Newly installed or restored guarded registrations may need real cron acknowledgements before readiness returns; allow supported reconciliation to finish rather than creating receipts or removing journals. For a nondefault service context, use the same explicit environment on the certification command. Certification does not stop or start services automatically.

Certification requires a versioned build, root, a stopped/empty panel execution domain, exclusive lifecycle/package admission, native dpkg locks, existing registration gates and SQLite write exclusion. It repeats qualification and records `bridge_completion_v1` in `system_metadata` only after success. Nginx configuration validation during certification can open its configured log files; unlike inspection, certification is not advertised as having no host effects. It does not provision, drain/replay jobs or approve runtime repairs.

The receipt binds the contract, existing installation ID, encryption-key identity, certifying binary digest, build version, completion time and retained-recovery policy. It contains no credentials. A receipt is historical evidence, not an unrestricted permission token.

## Verify before a future baseline

```sh
sudo /usr/local/bin/fluxo baseline-bridge verify
```

Verification requires a compatible receipt and repeats current readiness. It also validates Nginx after receipt admission; this may open configured logs. A future baseline consumer must hold exclusive lifecycle admission and invoke the contract before its initialization/effects. The current bridge still accepts historical startup states; it does not remove compatibility support or silently turn itself into the clean release.

The contract deliberately retains live historical Node/PHP rollback rights and compatible dispatchers. A future release that removes their readers needs a separate explicit retirement/conversion, including all references and helpers. A newer panel build need not have the same executable digest as a compatible retained Scheduler dispatcher.

## Immutable distribution and release acceptance

Bridge release assets include the exact `install.sh`, `BRIDGE-CONTRACT.json`, architecture-specific binaries, `TOOLCHAIN-VERSIONS`, checksums and provenance. Verify the selected tag's assets and provenance before execution. A mutable website installer with a pinned binary version is not a substitute for the tag's installer asset.

Before designation for production, qualify fresh and historical installed-panel paths, interruption/rollback, concurrent work, retained recovery, installer delivery and actual supported Ubuntu/backend/architecture combinations. Unit tests and cross-compilation do not establish VM or arm64 runtime acceptance. Preserve the retained acceptance VM and fixtures; do not run destructive scenarios against a customer server.

## Earlier acceptance evidence

The retained Ubuntu 24.04 amd64 test VM has passed bridge inspection/WAL preservation, preparation of a private copy of its actual historical database, data/credential conversion regressions, CLI refusal without receipts or retained-state changes, and native Scheduler descendant drainage/admission. This host contains older independent application fixtures and lacks a complete installed panel service/security profile. These passes do not establish positive installed-panel certification. Existing application fixtures, database, Scheduler journal and demo remain preserved.

A separate Ubuntu 24.04 amd64 installed-panel fixture has passed explicit stopped-panel certification, restart and receipt verification; real static-site creation and vhost mismatch refusal; key/identity/schema/receipt negative checks; finite command admission; failed-candidate installer rollback; same-version reinstall; and real dedicated MySQL/PostgreSQL catalog qualification. The database checks preserve one-time credentials without inventing stored passwords. Reviewed PostgreSQL inspection uses a clean environment and fixed read-only catalog search path. MySQL grant repair uses an explicit operator selection and preserves passwords; it does not silently narrow historical wildcard intent.

An isolated native MySQL 8.0.46 fixture also passed with `partial_revokes` both enabled and disabled. It exercised fresh account creation, historical escaped grants after a mode change, readiness refusal, explicit repair, grant-before-revoke ordering, unchanged authentication material, intended-schema access and denial on a similarly named schema. The test used private mount/network namespaces and extracted verified Ubuntu archives; host MariaDB service, socket identity and catalog remained unchanged.

These installer trials used a local candidate and a test-only cache of the exact official WP-CLI artifact. The unchanged installer still verified its signature, pinned signing key and version. This establishes the tested installer behavior, not public artifact delivery or reliable downloading through every network.

Tinker is included in this candidate's acceptance scope. Real Laravel sessions on the retained Ubuntu 24.04 fixture and fresh isolated Ubuntu 22.04/26.04 fixtures passed persistence, configured PHP selection, non-root identity, clean environment, one-use ticket rejection, mutation exclusion, detached-child cleanup, idle/authentication expiry, oversized input, slow-consumer output flooding, reconnect, shutdown and bounded PTY writes. Separate native lifecycle checks passed cross-process launch/revocation ordering and retained maintenance exclusion after a successful systemd stop left a populated execution domain. Dedicated acceptance-owner SIGKILL checks also proved real PHP/detached-child termination on all three releases; new-owner sessions passed afterward on 22.04 and 26.04. These checks do not establish every optional-feature or architecture combination.

The installed Ubuntu 24.04 panel also passed an actual authenticated TLS/WebSocket Laravel Tinker session through the production router, including ticket replay refusal, concurrent site-mutation exclusion and detached-child drainage. This exposed a long-domain Nginx hash-bucket limit. A managed HTTP default now supports valid DNS names up to 253 bytes while preserving explicit operator settings. Native Nginx validation and filesystem interruption, sync-failure and replacement-race tests passed. Pending HTTP-default recovery blocks bridge qualification and installer rollback; quarantined operator files must remain recoverable.

Certbot issuance, deletion and renewal retain the installed distribution tooling. Bridge acceptance must qualify each release's existing renewal profile with preserved lineage settings, plugin configuration and hooks.

Native APT acceptance on all three releases passed real and simulated isolated package transactions, missing/corrupt archive cases, native compression codecs and a positive acquisition-method control. The fixed `apt-a0-noqueue-v3` contract prevents queue submission before the first dpkg fence; `--no-download` alone is insufficient. Historical profile readers remain available, but an old profile does not authorize execution under the new contract. Integrated maintenance activation and recovery remain required.

The Ubuntu 22.04 and 26.04 cron profiles passed native PAM/NSS qualification, including exact vendor modules, constrained configuration parsers and protected input revalidation. The current profile requires userdbd and its worker to be absent and accepts only authenticated PID1 DynamicUser identity listeners. Installations with optional userdbd configuration are preserved and require a separately reviewed profile. These checks assume administrative configuration remains stable during qualification and do not replace integrated cron replacement/deletion protocol acceptance.

## Acceptance resumed on 17 November 2026

The resumed Ubuntu 22.04/26.04 campaign targets **amd64**. ARM has only been cross-compiled and has no native production approval. The initial local candidate for this resumed campaign was `bb3f7e0a57fd36bd132c0d4264f7c95373307fcfc062be8be6b6431b63f3c94a`, version `0.4.46-bridge.1`, built with Go 1.26.3 and the release-pinned tool versions. It was unshipped; subsequent builds remain bound to their own frozen manifests and the installed-system and delivery gates below.

The earlier `8bd8c771cbefbd7f3cd5b5c935def02779d8730f763ebdd8aa42b2ba9624023a` review image passed fresh Ubuntu 24.04 inspection, stopped-panel certification, restart and receipt verification. Verification initially refused a concurrent cron admission window and passed after that work drained; concurrent activity remains a legitimate reason to retry.

Two separate fresh Ubuntu 22.04/26.04 amd64 installations passed the normal frozen installer with MariaDB, PostgreSQL, Redis, Node and Python tooling. Normal TLS sign-in, real Laravel application provisioning under the application user, and dedicated MariaDB/PostgreSQL connections passed. Same-version reinstall preserved installation identity, authentication records, TLS files, site/database metadata and retained helpers. Separate fixtures installed the actual published v0.4.45 image, created canonical API-managed sites and database accounts, observed genuine cron execution, and passed a normal installer upgrade with those invariants preserved. These historical fixtures retain checkpoints; the older independent fixtures remain unchanged.

Installed-router Tinker acceptance exposed an inheritable `CAP_SETUID` bit despite zero permitted, effective and ambient capabilities. The supervisor now clears inheritable and ambient sets on the same locked OS thread that starts PHP, preserving only the supervisor authority needed for Go's credential transition. Real HTTPS/WSS acceptance passed on both releases with zero application capability sets and supplementary groups, variable persistence, configured PHP, exact Origin, ticket replay refusal, mutation exclusion and direct observation of detached-child drainage. No console transcript is retained.

Fresh optional-tool profiles required three bounded compatibility corrections: the exact authenticated `-2+0~deb.sury.org.1+ubuntu22.04`/`26.04` PHP revision form; the stock named amd64 fakeroot loader declaration, without trusting fakeroot executable payloads; and exact PostgreSQL 14/18 `NeverAutoRemove` input bytes. Unknown variants continue to require review. Targeted Astra source audits found no concrete bypasses or regressions in these corrections. The affected race suites passed. Current-image cleanup, complete certification/recovery and immutable delivery remain gates; these bounded passes do not constitute a blanket production approval.

The actual installed Scheduler caller passed on Ubuntu 22.04, 24.04 and 26.04 amd64 with the preceding review image. These tests created a Laravel-typed application and cron through the production API, observed genuine loader acknowledgements, checked the executing image and worker credentials, exercised natural drainage and retired the test registration. The artisan fixture proves Scheduler plumbing; it is not a real Laravel framework test. Separate real Laravel Tinker adversarial tests passed after disabling PsySH's optional network manual check in the private session configuration.

Package updates are not bypassed or downgraded to satisfy compatibility checks. Explicit stopped-panel Scheduler loader-policy requalification passed on all three releases, preserving immutable predecessor epochs and history. Retained task helpers can now be refreshed through the documented stopped-panel operation. Ubuntu 24.04 fault tests passed before and after publication of the prepared reader floor: old and current helper entry points refused incomplete activation before initialization, exact-image/operation recovery completed, and genuine cron acknowledgements arrived. Repeated helper refreshes also passed on all three releases with the current image, preserving the old helpers and Scheduler state.

The exact updated APT profiles are Ubuntu 22.04 / `2.4.15ubuntu0.1`, Ubuntu 24.04 / `2.8.4ubuntu0.1` and Ubuntu 26.04 / `3.2.1ubuntu0.1`. Source review and native NoQueue tests retain the fixed `apt-a0-noqueue-v3` download, solver, hook and dpkg fences. Actual production API registration, genuine receipt, default systemd dispatch and empty cleanup completed on all three amd64 hosts. A queued configuration change was rejected before consuming execution authority, and a consumed authority could not be replayed. Package state and configuration were unchanged. Autoclean deleted obsolete cached archives; the test restored those exact fixture files under package admission and verified the cache and timer restoration before reporting success. New Jammy and Resolute execution admission is amd64-only; historical readers remain available.

Existing Certbot renewal-profile qualification and fixed startup passed on all three releases with no installed lineages. This establishes the tested distribution tooling and startup closure, not real ACME issuance, renewal of a live certificate or every possible lineage/hook configuration. Stock Jammy Nginx module declarations are accepted only through the exact verified vendor profile; arbitrary executable directives remain refused.

The combined frozen-patch security review accounted for 241 paths (239 reviewed and two generated Python bytecode files excluded). It found four conditional problems in privileged VM test writers; those application-owned paths now use the unprivileged application identity, with normal root and already-unprivileged caller checks. Subsequent targeted Astra source review accepted those fixes. Installer reader-6 admission was corrected and regression-tested. Historical V1 maintenance recovery now retains the baseline observation producer while requiring full current inspection and exact observation presence. Native APT 2.8.3, Composer and Certbot observation checks and complete reconciliation in a private journal passed on the preserved legacy VM. This private journal models an absent historical domain; it does not establish every historical crash/upgrade scenario.

Targeted Astra reviews passed the Scheduler routing, Nginx module profile, loader-policy migration, retained-helper refresh and current amd64 APT enablement. These bounded reviews do not approve the entire combined working-tree patch for production. The current source also passes the relevant Go race suites and architecture builds; cross-compilation does not establish native arm64 behavior.

## Approved platform targets and remaining gates

### Current amd64 candidate evidence

The `eb4045fdfd9f` image and installer `479ab238a63dd250ed6386c654a752e0b62afec1fcb52a8b0e3c9340c5854de1` passed normal installation, stopped-panel certification, restart and receipt verification on both fresh and upgraded Ubuntu 22.04/26.04 amd64 fixtures. Fresh installations used MariaDB, PostgreSQL, Redis, PHP 8.4 and SSH hardening, with optional Node/Python tooling absent. Real Laravel applications were installed as `fluxo`; dedicated database accounts connected through PDO. Actual HTTPS/WSS Tinker passed on both reader6 and reader7 installations, including zero inheritable/permitted/effective/ambient capabilities and supplementary groups, persistent state, replay/mutation refusal and observed detached-child cleanup.

Current-image Scheduler and cleanup callers passed with genuine acknowledgements, normal adapter selection, natural drainage, registration retirement, profile-change refusal and consumed-authority replay refusal. Controlled unhealthy activation restored the healthy image on both releases while preserving identity, authentication, TLS, site/database records and retained helpers. A collected-unit retirement refusal found on Jammy was corrected: failed stop requires independently confirmed unit absence and a fresh hierarchical-empty proof. The preserved failing cleanup then completed and retired through supported APIs.

Stock Resolute initially supplied APT `3.2.0`. Cleanup acceptance required the signed forward security update to `3.2.1ubuntu0.1`, followed by explicit task-backend and loader-policy requalification. The same boot and cron process/domain generation were preserved; reader7 roles, original history and real acknowledgements were checked. Jammy used APT `2.4.15ubuntu0.1`. No package was downgraded or profile comparison waived. Some certification attempts refused genuine cron admission windows; full certification and verification passed after those processes drained without stopping cron.

Separate same-boot historical-retirement and reader6 registration-repair cases passed with the preceding `0ee37c0f5666` image. The current image then passed reader6-to-reader7 upgrade faults before and after the prepared floor, exact recovery, retained-reader refusal in prepared and committed states, fresh acknowledgements and database/Scheduler/history preservation. Evidence from preceding images is retained with its source comparison and scoped reviews; it is not relabeled as a current-image run.

Historical V1 producer scope follows the complete published-v0.4.45 registration path. Its Composer profile alone does not establish reachability: registration first requires exact Noble cron or Cronie payload/configuration signatures. Actual stock Jammy and Resolute cron measurements differ. The real published Resolute installation had no Composer registration and refused ordinary Run now without changing its task journal; the retained actual Jammy predecessor likewise had no V1 registrations or invocations. These are negative stock-provider observations, not positive V1 recovery tests. The separate Noble Composer case above supplies genuine installed V1 recovery/startup evidence. Imported authority, distribution upgrades carrying that history, Cronie/custom providers, other V1 task kinds and changed-executable recovery remain outside that bounded case; retain their recovery evidence and do not force certification.

Existing optional Node/Python installations were preserved during current-image reinstalls; normal Node update qualification completed on Resolute. Earlier Node review refusals remain recorded, with no confirmed root cause; equality checks were retained and worker failures now identify fixed field names. A redundant optional Python download timed out on Jammy and automatic rollback restored the previous installation. Successful fresh optional-tool retrieval is not claimed for this image. Live ACME renewal, custom hooks, other runtime profiles and native ARM require their own acceptance evidence.

The complete backend suite, affected race suites, vet, console-output tests, documentation build and both architecture builds passed. The immutable CI-built artifact, provenance verification and tag-pinned installation remain required before public production designation. A newly built release digest must receive its own acceptance; local SHA-authorized installation does not prove public delivery.

The production targets are **Ubuntu 22.04, 24.04 and 26.04 LTS**. They are qualification targets, not a declaration that this candidate has passed every release gate. Interim Ubuntu releases are outside this approved matrix.

| Target | Evidence obtained | Still required before designation |
| --- | --- | --- |
| 22.04 amd64 | Local fresh/upgraded certification, Laravel Tinker/PDO, Scheduler/cleanup, rollback and reader7 recovery; exact published bridge.2 artifact delivery and bounded final review passed | GitHub-enforced immutable publication and acceptance of that new artifact; additional optional/imported-history profiles remain separate |
| 24.04 amd64 | Exact published bridge.2 public-installer upgrade, Laravel Tinker/PDO, Scheduler/cleanup, certification/restart/verification; genuine published V1 Composer interruption, explicit recovery and installed startup; earlier rollback/helper-refresh evidence retained separately | GitHub-enforced immutable publication and acceptance of that new artifact; additional optional/imported-history profiles remain separate |
| 26.04 amd64 | Local fresh/upgraded certification, Laravel Tinker/PDO, Scheduler/cleanup, rollback, signed APT update and reader7 requalification; exact published bridge.2 artifact delivery and bounded final review passed | GitHub-enforced immutable publication and acceptance of that new artifact; additional optional/imported-history profiles remain separate |
| arm64 | Cross-compilation | Native runtime acceptance for every advertised platform tuple; Jammy/Resolute APT admission remains closed |

The older Jammy and Resolute VMs retain independent historical test sites with noncanonical paths and incomplete vhost metadata. Those fixtures legitimately prevent whole-installation certification; they are preserved, not deleted or rewritten to manufacture readiness. Their isolated successful caller tests remain useful evidence. Repository setup/recovery must also be current before whole-installation qualification.

The shared execution package retains both versioned lifetime contracts. Stock Jammy amd64 selects the reviewed `systemd-persistent-v1` implementation; newer supported hosts select `systemd-cgroup-exit-v1`. The separate experimental `systemd-persistent-249-candidate-v1` identifier remains unsupported. Activation requires compatible readers before the first write, durable one-use authority and natural-drain evidence. Historical helpers and recovery archives remain available until their obligations are explicitly retired.

Immutable installer delivery, native architecture acceptance, the complete supported historical matrix and the combined security review remain release gates. No release tag is approved by this evidence. Do not install placeholder security files or remove journals to manufacture readiness.
