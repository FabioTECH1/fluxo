# Bounded native traffic analytics qualification

Candidate runtime: `d73c04d5f90399db3806cac1b38617a44e17ee90`, amd64 SHA-256 `4264c6c8965e2b9f448970129d1bb471da00da9554bd4226faa194caf0746dd7`. Installer SHA-256 `51f772b333d150f326ca8bc424015127680251d9fff610236c5da7384661b913`. This is a local SHA-authorized candidate, **not signed public-release acceptance**. Source/build binding is the parent builder manifest, not embedded Go VCS stamps.

The named `fluxo-php-patch-test` VM ran Ubuntu 24.04.5, two vCPUs and approximately 2 GiB RAM. Real JWT-authenticated APIs created two disposable HTML sites; existing applications were preserved. The feature checks passed: OFF created no analytics files; master/per-site transitions, validation-failure preservation, graceful in-flight disable, disabled gaps/no backfill, exact controlled metrics, query/referrer privacy, restart deduplication, rotation, bounded cardinality and active history deletion with no resurrection. The 15-request exact test included six estimated page views, three 404s, one other 4xx and one 5xx, with exact body-byte sums.

A separate guarded native regression proved that USR1 alone cannot reopen through private source directories for an unprivileged worker. The validated same-generation reload moved actual traffic to the new file, and the old inode retired. The production burst also confirmed current-file growth and an unchanged retired `.1` file.

## Measured conditions

Each value is one observation. CPU is cumulative cgroup-v2 CPU divided by observation duration; **100% means one core**, not both VM CPUs. RSS is total panel process resident memory, not isolated analytics allocation. Nginx shared pages may be counted more than once. Existing background activity and GC make a precise incremental RSS claim unsupported.

| Condition | Requests / window | Panel CPU | Nginx CPU | Panel RSS min–max MiB | HTTP mean / p95 ms | DB / WAL / logs KiB at end |
|---|---|---|---|---|---|---|
| off_idle | 0 / 35.17s | 5.03% | 0.00% | 39.4–153.5 | — | 0.0 / 0.0 / 0.0 |
| off_ordinary | 1000 / 35.17s | 4.85% | 1.52% | 41.7–162.1 | 1.298 / 2.061 | 0.0 / 0.0 / 0.0 |
| enabled_idle | 0 / 35.18s | 4.75% | 0.00% | 37.3–158.0 | — | 44.0 / 0.0 / 0.0 |
| enabled_ordinary | 1000 / 35.19s | 6.21% | 1.82% | 43.0–154.5 | 1.479 / 2.135 | 44.0 / 0.0 / 177.9 |
| burst_bot_high_cardinality | 25000 / 40.06s | 4.53% | 6.83% | 43.9–174.3 | 2.747 / 6.288 | 44.0 / 0.0 / 5157.6 |

Ordinary OFF/ON traffic used the same paced 1,000 requests over 35 seconds (about 28.6 requests/second). Their observed local mean latency differed by 0.181 ms and p95 by 0.075 ms. These are local origin measurements, not internet/CDN/browser timings or a general performance promise. Enabled idle stored about 76 KiB including SQLite shared memory/control files; disabled operation stored zero analytics bytes.

The separate burst sent 25,000 unpaced requests, with unique page paths/referring domains and 50% known bots. Its 40-second window includes processing after the burst. All website requests completed without errors; dimensions remained at 98 rows (64 paths + overflow, 32 domains + overflow). The API reported incomplete/capped collection. Numeric processed/backlog/drop counters were not persisted before the explicit history purge, so **no claim that all 25,000 were aggregated, or that unprocessed requests were lost, is made**. The retained report leaves these numbers null. Soft producer limits and bounded processing may deliberately leave gaps under load.

## Separate update gate and retained failures

The local normal-installer preservation step and its one isolated retry both reached a stalled, partially downloaded pinned WP-CLI asset on GitHub and hit the QA controller's 240-second deadline. The active socket was IPv4 TLS, with 1,081,344 bytes downloaded; later HEAD probes returned 200. Authentication, official pins and tool sources were not weakened. Local ordinary-update acceptance therefore remains **network-failed**, separately from the passing feature/resource checks. The parent owns the independent hosted AMD64 update gate.

Earlier attempts are retained: immediate pre-reload vhost readiness and randomized AEAD ciphertext fooled the first harness; a transfer/start race loaded the previous harness in the second attempt. The corrected observer compares authenticated credential plaintext plus the unchanged key; all later input transfers completed and their three remote digests were verified before execution.

Both timeout recoveries preserved the original state. The supported installer lease acquire/release recovered a dead owner's durable hold; no journal edits or artificial acknowledgements were used. Final cleanup preserved all seven original application rows and eleven original key/helper/history/archive files, restored the installed and running original image `52a09d04c8a74f8ce43d2b14eaf136aaa2ba6e7642e66bb494f1fde633698529`, original Nginx/service/preferences/timers, and deleted only the two test sites. Raw logs/snapshots remain root-private in the VM. All three existing Multipass VMs are stopped.

The optional system-Python Certbot Nginx plugin parser was unavailable; no package was installed for it. Fluxo's actual certificate path uses `certonly --webroot`. Per-service kernel IO counters were unavailable; actual DB/WAL/log footprints were measured instead. Retention and storage-limit assertions have deterministic Go tests; this was not a 30-day wall-clock retention observation.

Exact resource samples, image identities, independent gate outcomes and cleanup records: [traffic-native-resource-measurements.json](traffic-native-resource-measurements.json). Guarded rotation: [traffic-private-rotation.json](traffic-private-rotation.json). No tag, merge, publication or public promotion was performed.
