# Ordinary base update: focused phase measurement

Measured on 19 November 2026, using the unchanged qualified installer and native AMD64 candidate. This is a small observation, not a universal performance claim. [Exact identities, boundaries and preservation evidence](base-update-phase-measurement.json).

| Phase | Core update | Clone core, same image | Clone with Node, same image |
|---|---:|---:|---:|
| **Total** | **53.25 s** | **49.68 s** | **65.17 s** |
| Local staging, admission, guards, account/firewall and activation | 3.65 s | 3.22 s | 3.35 s |
| PHP repository and APT indexes | 7.10 s | 5.56 s | 4.42 s |
| APT dependency work | 1.64 s | 1.51 s | 1.56 s |
| Lifecycle exclusion and panel rollback snapshot | 6.10 s | 5.01 s | 4.93 s |
| Composer download and verification | 3.40 s | 2.23 s | 4.44 s |
| **WP-CLI download and verification** | **14.31 s** | **15.81 s** | **15.61 s** |
| Node outer snapshot | — | — | 1.78 s |
| Node toolchain command, including its internal snapshot | — | — | 12.94 s |
| systemd/Fail2Ban | 2.87 s | 2.63 s | 2.76 s |
| Dashboard health wait | 6.27 s | 6.15 s | 6.16 s |
| Post-health cleanup | 0.23 s | 0.23 s | 0.52 s |
| Scheduled-job compatibility/final cleanup | 7.63 s | 7.26 s | 6.65 s |
| Success summary | 0.05 s | 0.06 s | 0.05 s |
| **Approximate dashboard unavailability** | **28.60 s** | **28.09 s** | **45.65 s** |

Rounded rows may differ slightly from totals; the JSON retains exact observations.

WP-CLI was the largest individual phase in every run. In the Node-enabled run, Composer plus WP-CLI took **20.05 s / 30.8%**. All Node work, including both snapshots, took **14.73 s / 22.6%**. The comparison does not establish that enabling Node universally adds the difference between these single runs.

## What Node spent time doing

| Observed warm Node work | Duration |
|---|---:|
| Entry/lease/database checks before the first Node progress message | 2.20 s |
| Shell snapshot + internal snapshot | 1.78 + 1.22 s |
| Three npm registry/integrity checks | 3.25 s |
| Corepack install | 0.60 s |
| pnpm preparation | 0.10 s |
| Yarn preparation plus following version checks | 1.16 s |
| Managed path/executable validation | 2.82 s |
| Final inspection plus internal snapshot cleanup | 1.44 s |
| Current Node / Bun checks | 0.14 / 0.003 s |

Node 24.19.0, npm 11.17.0, Corepack 0.35.0, pnpm 11.20.0, Yarn 4.18.0 and Bun 1.3.14 were unchanged before/after. No Node/Bun binary was downloaded in the warm update, and no runtime apply operation was requested. Initial Node provisioning took 120.16 s and is **setup only**, excluded from update timings.

## Opportunities supported by this evidence

1. Stage authenticated Composer/WP-CLI downloads and verification before panel stop, retaining locked atomic activation and rollback. The measured 20.05 s lies inside panel unavailability; relocating work targets downtime, not automatically total time.
2. Investigate exact verified-byte reuse for unchanged tools. Both warm runs produced identical before/after Composer and WP-CLI PHAR hashes despite repeated downloads. A version string alone must never authorize reuse.
3. Review Node's repeated registry/package-manager work (~5.11 s) and inspection (~4.26 s). Preserve integrity, ownership, approved runtime selection and recovery. The two snapshots cost ~3 s here and serve distinct rollback contracts.
4. Review APT refresh policy separately with explicit freshness/security expectations. No package was upgraded/installed/removed in these samples; the existing guards and archive-404 retry remain necessary.

**No optimization, skipped check or new skip behavior was implemented.**

## Method and limits

An external root-private observer timestamps existing stdout/stderr lines with `time.monotonic`. It does not edit the installer, instrument functions, wrap commands, inject faults or alter execution authority. TLS health sampling uses 250 ms cadence / 700 ms timeout, so downtime is approximate. Silent work is grouped between available progress messages; nested Node spans are subsets of the main Node duration.

The first run replaces an earlier development image. The two clone runs use the same qualified image/version with `--local-binary`, exercising the **full update path**, not normal same-version early exit. Public candidate download and release-attestation verification are **excluded**, since no public base release is approved. All other optional components were deselected. The clone has 2 vCPU/~2 GiB RAM and no Node applications/daemons, so real application restart workload is not represented. OS, npm/Corepack caches and internet variance are not controlled.

Application rows, decrypted credential values, keys and retained history/archive files passed preservation checks in every run. The source VM and pre-existing `fluxo-dev` are stopped; the single measurement clone is stopped with private logs retained. Main/latest, release assets, production code and the nine-case qualification were unchanged.
