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

[Run 44332342602](https://github.com/FabioTECH1/fluxo/actions/runs/44332342602),
source `465f33bdb40d9cd404a4512abf06620bcde08c27`, tested source-candidate SHA256
`f5d7b6728a9d71ddbb138b144fa61e7e17c696deca95f4a94f69c73eb08aceb4`.
The overall workflow remains **failed** because the 26.04 case is refused.
These use the normal installer's explicit local-SHA transport; they are **not**
immutable public-artifact delivery acceptance.

| Native ARM profile | Scoped result |
| --- | --- |
| Ubuntu 22.04 cron, fresh | PASS |
| Ubuntu 24.04 cron, fresh | PASS |
| Ubuntu 24.04 Cronie 1.7.1-1, fresh | PASS |
| Ubuntu 24.04 cron, public v0.4.47 upgrade | PASS: install/preservation and subsequent guarded/root execution |
| Ubuntu 26.04 current runner | Loader REFUSED; vendor-profile update required |

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
Scheduler and execution domains under Go 1.26.3. No production Go, installer or UI source changed after the tested runtime
revision `88def300`; later commits adjust only acceptance controllers. A complete
new native AMD64 installer run is not claimed. There are no UI, database-schema or HTTP API changes.
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

Ubuntu 26.04 has systemd/NSS/PAM 259.9-0ubuntu0.1, while the existing profile pins
259.5-0ubuntu3.4. Its new `20-coredump-debian.conf` SHA256 is
`264c87465f59c92067860c2f1dba07c05ebdc07477b39b7b1301316626044643`.
The official systemd-coredump package contains these exact soft-core-infinity
rules. This vendor change is retained and refused; no version or fragment is
silently admitted. Its complete current loader/runtime acceptance needs a
separate reviewed vendor-profile update.

Historical ARM V1 recovery, optional Node/Python/database/Redis provisioning,
live ACME, independent final change review and immutable release-artifact/delivery
acceptance remain outside approval. The public v0.4.47 probe and candidate source
results must not be combined into a blanket production PASS.
