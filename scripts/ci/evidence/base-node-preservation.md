# Verified Node preservation in the clean base

Ready for the separately approved release build. Public signed base-asset acceptance is still pending; main/latest remain unchanged.

Ordinary updates preserve a verified managed toolchain. Node/Bun already skipped ready binaries; the removed repeated work is registry metadata lookup, Corepack installation, pnpm/Yarn preparation and the inner install snapshot. The outer rollback snapshot, provenance, ownership, actual execution, runtime leases/holds and recovery controls remain. Explicit installation/repair still uses the original guarded setup path.

Older healthy installations without optimization evidence receive authenticated, non-mutating assessment during the normal installer. This can download comparison artifacts once; missing proof is not a request to reinstall. A real defect is reported for existing explicit repair/recovery. The protected receipt cannot approve a runtime update or another Node major.

Both native jobs passed at source `9fb28ca1ce11870d2f39119b6b0a76aa9e4ce339`, tree `f22d7a4747d8cf361711f481edc5cf88030b9d45`. Runtime/installer inputs are identical to fix commit `95dd84ab79ca777fded62c09d40784a2ede1436b`; subsequent changes are fixture/report documentation. Embedded UI built first, native Go 1.26.3, CGO disabled and all 19 official assignments verified.

| Ubuntu 24.04 runner | Node command before → after | Whole update before → after | Approximate dashboard interruption |
|---|---:|---:|---:|
| amd64 | 10.71 → 3.36 s | 34.76 → 26.76 s | 22.26 → 14.76 s |
| arm64 | 10.58 → 2.47 s | 32.85 → 24.14 s | 21.51 → 13.26 s |

The Node command column excludes the separate outer rollback snapshot. These are single paired warm samples on the same machine with the same current candidate, pins and flags. The before sample uses the exact unmodified 43a1f21 installer, which takes the retained full installation/repair path. The retained explicit install/repair behavior also clears optimization evidence. This is not an old public-release binary comparison. Both runs use same-image local-SHA transport and deliberately follow the full update path; public release download/attestation and the normal same-version skip are excluded. Network/cache differences prevent a universal speedup claim.

After reuse, the largest measured phases were dashboard health waiting (~5.6 s), post-health compatibility/cleanup (~3.3–4.4 s), Node verification (~2.5–3.4 s), repository/APT refresh (~2.7–2.8 s), and panel snapshots/exclusion (~2.1–3.0 s). The retained outer Node snapshot took ~0.7–0.8 s. Composer/WP-CLI were fast on these hosted machines; the earlier local fixture measured slower downloads. Their behavior was not changed.

Native coverage included fresh base setup, initial Node installation, populated ordinary updates, absent-proof enrollment without payload mutation, verified reuse with kernel-denied IP sockets, healthy startup and genuine execution receipts. Missing `pnpx` was refused without repair or proof mutation, then restored by the existing explicit installation command and authenticated again. Versions, selected runtime, original application/credential/key/SSH/history data, payload bytes and metadata/links were checked. No Node application fleet or load benchmark was added.

| Architecture | Exact binary SHA256 | Native CI |
|---|---|---|
| amd64 | `d59947201acbdb56c928ec8c606b31b51779d41c390a38c6e050d559cc770823` | [Passed](https://github.com/FabioTECH1/fluxo/actions/runs/44586249310) |
| arm64 | `08bacbfd800b3e56130a79656305dcf446ea9e3af52b37fc88e7d7ac815cecb9` | [Passed](https://github.com/FabioTECH1/fluxo/actions/runs/44586274912) |

Installer SHA256: `51f772b333d150f326ca8bc424015127680251d9fff610236c5da7384661b913`. Before-installer SHA256: `292c1572ec3fe684c93eb68d073303ef5d3a447e47a1818bc225cc94c0f35aa5`. Exact source, manifests, artifact IDs, report digests, all phase times, probes and audit dispositions are in [the machine-readable report](base-node-preservation.json).

One immutable Astra 6.2 Low audit of 43a1f21..c1ab3f6 found three defects. Commit 95dd84a fixes the writable-cache race using private immutable probes, adds a framed no-follow digest rather than relying on historical ambiguous framing, and excludes unrelated project cache growth from active-manager proof. Regression tests, focused Go suites, race/vet, candidate tests and documentation build passed. No repeated broad audit is claimed. Historical digest formats, authority records, readers/helpers and rollback archives remain intact.

Failed attempts are retained. The first hosted attempt refused an unsafe fixture shim assumption; the next correctly refused a network namespace hiding a required Unix listener after successful enrollment. The final test retains Unix observability and denies only IP socket creation. On the local AMD64 fixture, official Bun ZIP comparison downloads timed out twice; one normal installer rollback restored a healthy panel and semantic credential/application data, and a non-mutating retry kept the panel active. No receipt was fabricated and no local optimized timing pass is claimed. A separate incomplete official download was never used as proof. The local clone is stopped; original fixtures/evidence are preserved.

The [earlier nine-case base matrix](base-native-source-qualification.json) keeps its own source/artifact identities and Ubuntu 22.04/24.04/26.04/provider scope. This amendment qualifies the changed Node path on native Ubuntu 24.04 AMD64/ARM64, without re-running that matrix. The user-skipped historical early-interruption case remains excluded. No history or archives were deleted.

Main remains `f416b0ac7c4bfdd150f1828db810772160f164b9`; immutable latest remains [v0.4.51](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.51). No public base version, tag, release, merge or promotion was performed. Final signed PUBLIC artifact verification follows the separately approved release build.
