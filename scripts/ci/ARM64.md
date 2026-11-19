# ARM64 qualification and retained investigation

Current release qualification is recorded in
[the v0.4.48 artifact record](evidence/bridge-0.4.48-release.json). It distinguishes
source tests from execution of the exact immutable public binaries and records
promotion separately. The original investigation and candidate records below
retain their original subjects, failures and limits.

The final source matrix passed eight native ARM64 cases in
[run 44481600570](https://github.com/FabioTECH1/fluxo/actions/runs/44481600570):
Ubuntu 22.04/24.04/26.04 cron fresh installs, Noble Cronie, public-v0.4.47 upgrades
on 24.04 and 26.04, and fresh managed Node.js/Python provisioning on 24.04 and
26.04. This run used SHA-authorized local source bytes. The separate exact-public
matrix is [run 44485655111](https://github.com/FabioTECH1/fluxo/actions/runs/44485655111);
its outcome belongs to the release record rather than being inferred from the
source pass. Optional toolchain checks include non-root Node execution and a
Python virtual environment with pip and uv; full Laravel applications, live ACME
renewal, arbitrary provider configurations and old ARM historical recovery are
outside this matrix.

## Re-running acceptance

The full matrix contains eight reviewed ARM profiles. With no
`scripts/ci/release_subject.json`, it exercises an explicitly SHA-authorized
source candidate. To qualify public delivery, pin the published tag, source
commit and exact installer/architecture digests in that file. The controller
rejects production-source differences from the pinned subject. Remove or update
the public subject before testing a new runtime revision; source passes and
public-artifact passes are separate evidence.

The v0.4.48 public subject is retained at
[`evidence/bridge-0.4.48-public-subject.json`](evidence/bridge-0.4.48-public-subject.json).
Its public profiles passed, but promotion remains held for a separate
source-established early historical-conversion recovery gap. The active matrix
is restored to all eight profiles in source-candidate mode for the unreleased
v0.4.49 correction. All eight passed on `614c39a` in
[run 44499190086](https://github.com/FabioTECH1/fluxo/actions/runs/44499190086).
Report digests are retained in the
[v0.4.49 candidate record](evidence/bridge-0.4.49-candidate.json).
No v0.4.48 public hash is used to qualify new source. The release owner explicitly
skipped the additional early-crash native case; it remains unverified and is not
counted as a pass. Exact public-artifact acceptance proceeds separately. See the
release record for failed attempts, scoped retries and the skipped native case.

## Unchanged published release

[Native run 44316884590](https://github.com/FabioTECH1/fluxo/actions/runs/44316884590)
tested unmodified **v0.4.47** on Ubuntu 24.04.5 ARM. The public installer verified
provenance and returned 0. SQLite/login/metrics, automatic reader-format setup,
PHP site creation, Nginx PHP serving and an ordinary non-root PHP cron tick passed.
Guarded Scheduler/task execution remained unqualified: no Scheduler entry/epoch
or guarded task registration existed. Its overall gate correctly stayed false.

| Published subject | Identity |
| --- | --- |
| Source | `71c35f4e7810005f3e06cc25339e86631ab9bf2b` |
| ARM64 binary SHA256 | `621f162b43e481f777929b8f280800cd4e9837facf6414545be317563bedc621` |
| Installer SHA256 | `d5830d050b14b6209f28a74a5da06a0deb040c8025e0fe27b654d3423656c16d` |

This phase used no rebuilt application, local binary override or attestation
bypass. [Published-release evidence](evidence/arm64-v0.4.47.json) records earlier
failed runs, their report hashes and fixture corrections. These include unsafe
CI directory modes, an unrelated Microsoft-repository HTTP 429, inactive Nginx,
and observer route/readiness errors. They remain failed evidence.

## Candidate results

[Run 44369636310](https://github.com/FabioTECH1/fluxo/actions/runs/44369636310),
source `9255d3b1dd444535e2055eb15b4ecf9ff2e4c33b`, tested source-candidate SHA256
`b3431dfc468d47680924bff833934dbfcaedb38a169c43a70813b861b0ec8384`.
The overall workflow is **successful**, with all six scoped cases passing.
These use the normal installer's explicit local-SHA transport; they are **not**
immutable public-artifact delivery acceptance.

| Native ARM profile | Scoped result |
| --- | --- |
| Ubuntu 22.04 cron, fresh | PASS |
| Ubuntu 24.04 cron, fresh | PASS |
| Ubuntu 24.04 Cronie 1.7.1-1, fresh | PASS |
| Ubuntu 24.04 cron, public v0.4.47 upgrade | PASS: install/preservation and subsequent guarded/root execution |
| Ubuntu 26.04 cron, fresh | PASS |
| Ubuntu 26.04 cron, public v0.4.47 upgrade | PASS: install/preservation and subsequent guarded/root execution |

Fresh passes include real guarded PHP scheduling, genuine loader acknowledgements,
one-use bootstrap/all-thread privilege checks, exclusive maintenance exclusion,
natural drainage, retirement and duplicate refusal. All three fixed task profiles
qualify. A real fixed root renewal task completes and refuses consumed-authority
replay; no live ACME renewal is claimed. Jammy's persistent-domain tests cover
successful main exit and helper loss with surviving work until natural drainage.
Native APT archive/codec fences pass on all three releases, including Resolute's
additional solver-3 case. Full Laravel framework behavior is untested.

The public-v0.4.47 upgrade preserved checked users/sites/crons, authenticated
credential values, keys/TLS, reader floor and retained helper/archive bytes. The earlier run 44330791474 failed its follow-up because the observer
required an immediately free global maintenance lock after its own domain drained.
The corrected observer waits at most 30 seconds for a real exclusive window
without stopping other work; persistent retention still fails. The new run passed
this check and subsequent guarded/root execution. The earlier whole-case failure
remains recorded. See [candidate evidence](evidence/arm64-candidate.json).

## Bounded source changes

- Native architecture data selects ELF64 little endian, AArch64 machine/cache ABI,
  interpreter, NSS/PAM/library directories and authenticated package manifests.
  AMD64 definitions/revision, historical backend meanings and reader roles remain
  unchanged. Unsupported architectures and foreign/ambiguous payloads refuse.
- ARM cron hashes are verified against official packages. Jammy's glibc 2.35
  legacy `tls`/`atomics`/`aarch64` overrides are refused, as are populated modern
  `glibc-hwcaps` directories. [Architecture/source notes](../../internal/services/scheduler/pam_architecture_sources.md)
  retain the primary references and inventory provenance.
- Native lifetime and APT fence results admit Jammy ARM persistent domains and
  Jammy/Resolute ARM APT profiles, subject to the independent host/provider gates.
- A standard installed-LZ4 configuration tuple is accepted only with exact
  extension, binary, cost and arguments. The three exact APT source archives
  pinned in `apt_platform.go` select linked native LZ4 before any pipe branch;
  failed native opens do not fall back externally. Traced native tests preserve
  NoQueue/archive fences and forbid external codec execution.
- Two shared installer ordering fixes wait for genuine first-boot receipts before
  full readiness admission, and retry only queued/running read-only PHP/Node
  checks. Lifecycle admission is released between retries; the existing CLI
  deadline bounds the wait. Apply/repair/rollback, generations, recovery and
  barriers remain refused. No authority, receipt or journal is synthesized.

Affected AMD64 race suites and vet pass for bridge, PHP updates, maintenance tasks,
Scheduler and execution domains under Go 1.26.3. The Resolute update additionally passes affected AMD64 race suites and vet at
source `76db22b5`; the new profile selector remains ARM-only. A combined-source native
AMD64 installer run is not claimed; the separately tested historical-fix subject
is recorded below. The architecture/installer changes add no database-schema or
HTTP API changes; the combined PHP/UI delta is described below.
Every applicable native result contributes to the final acceptance gate; a local
negative check confirms each individual required failure prevents a pass.

## Fixture boundaries and retained failures

These are native GitHub ARM runners, not emulated CPUs or pristine VPS images.
The disposable images have custom PAM, environment, limits, APT hooks/sources and
Python tools. Controllers archive those inputs and establish the already-reviewed
stock profile explicitly. Vendor limits come from authenticated APT payloads;
Noble libc is updated to the existing reviewed security baseline. A separate
Noble case installs actual Cronie. Root-owned writable vendor paths are restricted
without altering their bytes; vendor authentication remains mandatory.

PAM/environment/limits/APT/source/Python inputs and timer states are restored.
Services started only for the fixture are stopped. Restricted vendor permissions
last until runner destruction. No production hostname switch, environment bypass,
customer repair step, raw authority edit or synthetic acknowledgement is added.
Sanitized logs/reports are exported; credentials, databases, keys, process
environments and raw logs remain private to the disposable runner.

Earlier candidate failures remain recorded in their workflow artifacts:
44319966586/44320832685 (CI profile/controller mismatches), 44322233679 (APT fences
passed; PAM refused), 44323611261 (Jammy guarded execution passed; remaining
profile refusals), 44326685830 (startup ordering), and 44328841643 (both Noble
providers executed guarded/root tasks; renewal profile was checked before Nginx
start). The superseded 44330008840 run was cancelled, not passed. The upgrade
observer now compares authenticated plaintext credential digests in memory,
rather than randomized AES-GCM ciphertext; no secret or individual credential
digest is exported and all other user fields remain exact.

## Remaining merge/release gates

The earlier Ubuntu 26.04 refusal is resolved for the exact tested ARM profile.
A separate versioned loader input profile admits systemd/NSS/PAM
259.9-0ubuntu0.1 and authenticates its complete native dependency closure. It
supports either the new exact `20-coredump-debian.conf` alone or that file plus
the exact retained old `10-` file. Both directory inventories remain strict.
[Source review notes](../../internal/services/scheduler/resolute_update_sources.md)
record the archive checks and focused NSS/userdb/PAM review within the existing
finite vendor/administrator trust boundary.

The native run passes altered-limit/PAM/NSS/loader/module refusals, plus a positive
current-only directory-layout check in a private mount namespace. Actual guarded
PHP and root execution use the real host provider, genuine receipts and natural
drainage. Both clean Fluxo installation and public-v0.4.47 upgrade pass on the
hosted stock-profile fixture. The old vendor profile, AMD64 definitions, saved
identities and backend reader roles remain unchanged; this does not silently
migrate a saved experimental loader profile or normalize historical hashes.
Earlier refusals remain recorded in prior results rather than relabeled.

Historical ARM V1 recovery, optional Node/Python/database/Redis provisioning,
live ACME, independent final change review and immutable release-artifact/delivery
acceptance remain outside approval. The public v0.4.47 probe and candidate source
results must not be combined into a blanket production PASS.


## Combined PHP/UI and historical-retirement integration

The initial combined native run included the user's PHP inventory/dispatcher/UI fix and the
archived historical Cronie retirement implementation. Combined affected race
suites (bridge, Scheduler, maintenance, PHP update, execution domains), vet,
11 UI tests and UI production build pass. The original user checkout is preserved.

All six ARM cases pass on this combined image. This does not replace the separate
AMD64 v0.4.40 producer test: its full upgrade/recovery and uncertain-work refusal
reports passed on image bb12f988 with optional toolchains disabled. That test
image omitted the official Node/Python linker pins and does not qualify their
release provisioning. The corrected Node-enabled run now passes in full; see
[AMD64 evidence](evidence/amd64-v040-cronie-node.json). Fixture-only permission/ACL
probes are not upgrade passes. No main merge, public release or blanket
historical-support approval is implied by this integration report.

[Run 44402175737](https://github.com/FabioTECH1/fluxo/actions/runs/44402175737)
passed on historical-retirement source `4a613f6a`, with harness `02bf15ab` and
source-candidate SHA256
`def4b7942413079383dbff9d3c9a8d4da80937471b7117c69fbb4e74163da98c`.
Its build injects and checks all 19 official toolchain/installer pin assignments
before provisioning. The published v0.4.40 Node-enabled predecessor installed
with normal provenance verification and produced four genuine cron receipts.
The case reproduced v0.4.47's same-boot domain refusal, refused surviving work,
then completed the corrected normal installer across a durable interruption and
exact-operation recovery. Original task cores/triggers were archived, old
helpers/history retained, fresh genuine acknowledgements required, and real
scheduled execution continued. Fixture metadata restoration completed.

This AMD64 candidate and the six-case ARM integration image are distinct builds
and source revisions. Their results do not qualify one combined release artifact.
The Node-enabled installer passed; separate Node application deployment and
post-recovery Node API readiness are not additional claims of this fixture.
Exact public candidate delivery and final release acceptance remain separate.


## Pre-publication recovery and retry corrections

Final source review at `54dd82d` held publication for two installer defects.
A durable initial activation can have a converted task core but no reader floor
or replacement trigger; initial receipt waiting must therefore read the protected
intent and defer to exact-image archive recovery for an existing operation.
Image, service, installation, archive and subsequent fresh ACK checks remain in
the ordinary recovery path. A second correction inspects every maintenance table
before returning the read-only-check retry result, so a PHP/Node check cannot mask
a mutation, unresolved generation, repository migration or barrier elsewhere.

The focused regression drives the actual transition writer to interruption after
task-core publication and before the floor, then verifies recovery routing and
retained subsequent receipt refusal. Cross-table SQLite cases exercise checks
alongside all blocker classes. Restoring either old behavior through a Go source
overlay makes its regression fail. These are unit/transition fault regressions,
not new native execution or fabricated receipt evidence. The earlier native
campaigns remain bound to their own images and predate these final corrections.
