# ARM64 investigation and candidate acceptance

Work is isolated on `test/arm64-bridge-acceptance`. Main and the public release
channel are unchanged. The candidate is **not approved for merge or public ARM64
production support**.

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

[Run 44336023958](https://github.com/FabioTECH1/fluxo/actions/runs/44336023958),
source `76db22b5e25493962b40eb684b014c84ca6ee2c5`, tested source-candidate SHA256
`c01eb18ba6459ec7b562ccff4a6d6b08bf27605db5d54f5d9429979cc54729e6`.
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
source `76db22b5`; the new profile selector remains ARM-only. A complete new native
AMD64 installer run is not claimed. There are no UI, database-schema or HTTP API changes.
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
