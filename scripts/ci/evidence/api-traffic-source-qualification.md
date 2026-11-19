# API traffic: isolated base VPS trial qualification

Runtime source: `65929152bed5b2235a8cc058ed410b5646d6a290`. Installer SHA-256: `51f772b333d150f326ca8bc424015127680251d9fff610236c5da7384661b913`. The native images were built UI-first with Go 1.26.3, CGO disabled and all 19 official tool pins. This qualifies the API delta for an opt-in trial build, not production promotion.

## Native cases

| Case | Result | Evidence |
|---|---|---|
| amd64 Ubuntu 24.04: Published base-test.1 overview/history → candidate → ordinary update | PASS | [Native run](https://github.com/FabioTECH1/fluxo/actions/runs/44622247177) |
| arm64 Ubuntu 24.04: Fresh base → API/lifecycle checks → ordinary update | PASS | [Native run](https://github.com/FabioTECH1/fluxo/actions/runs/44622247238) |

AMD64 used the exact immutable published previous binary and installer. Overview aggregates, generations and cursors survived, old API history remained unavailable, new API collection began forward, and a subsequent ordinary update preserved it. ARM64 exercised settings/auth/site isolation, API prefix boundaries and explicit method/route groups, real 2xx/3xx/4xx/5xx, Nginx bytes and weighted timing, pending/disabling/gaps, restart, actual rotation, bounded detail, deletion, genuine scheduled jobs and SSH/application checks. Terminal informational-only responses were not generated; the real final 1xx count was zero and deterministic tests cover 1xx aggregation.

The named local `fluxo-php-patch-test` VM independently passed the full feature/resource suite and ordinary installer update. Its image SHA-256 was `7eceef055372b14f00103658299a5e526a11d3d4b33398447bdd4b47eb072057`. Hardware: two logical CPUs, 2,008,672 KiB RAM, Ubuntu 24.04, Nginx 1.24.

## Resource observations

One observation per condition, localhost traffic with warm caches. CPU percentages use one core as 100%; RSS is the entire panel process resident sum, including background jobs and GC, not isolated analytics allocation or PSS. Idle conditions include one probe. Kernel I/O counters were unavailable; actual DB/WAL/log footprint growth was measured.

| Condition | Requests / seconds | Panel CPU | Panel peak RSS MiB | Origin p95 ms | Analytics growth KiB |
|---|---:|---:|---:|---:|---:|
| off_idle_one_probe | 1 / 35.16 | 5.600% | 100.08 | 1.2173 | 0.00 |
| off_ordinary_api | 1000 / 35.18 | 5.562% | 105.36 | 1.5275 | 0.00 |
| enabled_idle_one_probe | 1 / 35.17 | 5.570% | 150.99 | 1.5143 | 0.18 |
| enabled_ordinary_api | 1000 / 35.18 | 6.117% | 160.77 | 1.5682 | 188.80 |
| burst_bot_high_cardinality_api | 25000 / 40.31 | 5.281% | 160.94 | 5.4706 | 5430.77 |

All origin requests completed without errors. The burst varied API paths, methods and referring domains and mixed known bots. API detail remained at 65 rows (64 method/path groups plus overflow), with visible incomplete/capped detail and preserved request totals. Counts describe processed records, not a guarantee that all burst requests were aggregated; exact processed/backlog/drop counters are retained in the JSON report. These samples do not establish a universal latency/RAM/CPU cost or internet/CDN/browser performance.

## Validation, review and retained failures

Full Go tests, affected race tests, vet, UI tests/build, website/docs build and 32 offline candidate tests passed. Deterministic tests additionally cover weighted status/bytes metrics, rule ambiguity, long paths and methods, delayed-rule classification across rotation/restart, old-schema unavailability, checkpoint rollback, history deletion and cache rollback, retention, cardinality, and capacity.

The independent bounded review found two P2 issues: orphan preferences could permanently exhaust capacity, and historical route segment caches could grow excessively. Both were fixed with regression tests. The final source uses compact allocation-free route matching and a 64 KiB/site retained-rule payload limit, retaining old rules until expiry rather than evicting them to admit changes. Parent source review and race tests verified the corrections; no second independent re-audit is claimed.

The first local attempt stopped on a fixture assertion that omitted its own idle probe from the expected page/byte baseline. Its report is retained. The corrected observer compares deltas from that baseline; both attempts used the same candidate binary. No runtime check was weakened. The fixture correction changes only QA code after the executed runtime source.

Cleanup restored the original executable/service/Nginx state and APT timers, removed only temporary sites, preserved seven original application rows and eleven key/helper/history/archive files, left no installer lease or analytics directory, and stopped the VM. Hosted fixture cleanup passed.

## Publication boundary

Only the isolated baseline/trial branches may advance. Main and latest remain unchanged. A new immutable prerelease must separately pass exact signed public-asset checksum/provenance/source/tool-pin/UI verification before its installation command is handed to the user. Final public-artifact runtime acceptance and stable promotion are not claimed here. The original clean-base/Node/analytics qualification reports retain their original identities; this report is the separate API amendment.

Exact digests, checks, measurements and limits: [api-traffic-source-qualification.json](api-traffic-source-qualification.json).
