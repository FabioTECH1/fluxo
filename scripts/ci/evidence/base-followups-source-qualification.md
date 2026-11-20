# Base interface and final bridge qualification

Implemented and tested, ready for a separately approved base release build.
**Publication remains on hold.** No base main merge, tag, release, latest
promotion, VPS deployment or changes to immutable base-test assets were made.

The [source-bound record](base-followups-source-qualification.json) contains
exact source, installer, image, artifact and log identities. The final local
development build is from `e34bd1221ebf688247ce19d8398e6419f5c7f2e7`.

## Implemented behavior

- Compact Source Control reports use shared components. Retry remains
  observational; token replacement repairs existing public-key registration
  while preserving private keys and all identity/password/locking guards.
  Generic SSH failures do not claim a particular remote cause.
- Server-wide traffic retention supports 30/60/90/180/365 days or manual
  deletion, retaining the 30-day default. Actual bounded cleanup honors the
  policy. Storage pressure pauses collection without evicting retained history;
  temporary busy checkpoints remain retryable.
- Approved deployment Duration sits tightly between author and relative date,
  with the same value beside status in output. Running values increase without
  extra wording. Atomic start and observed finish timestamps exclude queue wait
  and include the admitted lifecycle. Historical/interrupted timing remains
  unavailable. Off-page modal refresh and serialized polling stop completed
  timers correctly; metadata and commit text remain bounded and truncated.
- Legacy entry requires exact public v0.4.52 bridge images and prints its tagged
  installer command before provisioning. Fresh and admitted base paths remain
  direct. GitHub health and optional recovery reports are not admission gates.
  Base update awareness excludes the final bridge lineage, preserving later
  base updates. A non-base selected target asks for the intended base target.

## Native candidate acceptance

[Run 44738609867](https://github.com/FabioTECH1/fluxo/actions/runs/44738609867)
passed at exact source `2792f7763a502b4b6c9e0aae5e564e0518903545`:

| Profile | Executed acceptance |
| --- | --- |
| AMD64 Ubuntu 24.04, cron | Public v0.4.52 → base candidate → ordinary base update |
| ARM64 Ubuntu 24.04, cron | Public v0.4.52 → base candidate → ordinary base update |

Both installed the bridge through its normal public download and verified signed
GitHub provenance. Both candidate installed/running images matched the archived
artifact hashes and all 19 official pin assignments. Data, credentials, keys,
site IDs, application files, history, archives and genuine Scheduler/root-task
execution survived. Real deployments captured timing; a deployment created by
the bridge retained unknown historical timing. Real Nginx analytics aggregates
and manual retention survived the ordinary base update. Both fixtures cleaned
up successfully. Nonselected matrix rows did not execute acceptance.

Later changes are UI polling serialization/metadata bounds, the non-base-target
diagnostic, docs and tests. Production Go source is unchanged from the native
candidate. Final UI tests/build and exact-source local build passed. Native
image identities remain bound to `2792f77`; no exact-final-image native or public
base delivery acceptance is claimed. Earlier nine-case, Ubuntu 22.04/26.04 and
Cronie results retain their original source/asset identities. The explicitly
skipped historical interruption scenario remains excluded.

## Bounded validation and remaining gates

Full affected Go suites, targeted race checks, vet, 42 UI/demo tests, 33 candidate
harness tests, UI build and complete website/docs build passed. Tests cover
schema admission/additive initialization, malformed columns without repair,
history/identity preservation, cancellation, interruption, persistence retries,
off-page and overlapping refresh, retention defaults/validation/deletion and real
SQLite storage pressure. Desktop/mobile actual-component renders were inspected.
Two bounded Astra 6.2 Low reviews produced small findings, all fixed with
regressions; no broad repeat audit was run.

Native fixtures omit optional Node/Python/database-engine/Redis components.
Recorded installer durations have different cache/network/transport conditions;
they are not universal speedup or Node benchmark claims. No real GitHub
credential positive case or additional interruption scenario was added.

The original local VM was restored and stopped by the parent, then left
untouched. Historical recovery readers, helpers, archives and logs remain.
The parent separately promoted bridge v0.4.52; base publication is still held.
An explicit release/version approval, official signed build, exact public-asset
acceptance and separate promotion approval remain required.
