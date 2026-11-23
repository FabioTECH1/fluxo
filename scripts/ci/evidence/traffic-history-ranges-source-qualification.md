# Retained Traffic history ranges — source qualification

Implemented on `baseline/clean-base` at **cd8efc9f8c1769e64f9e9652280ad01a3602b77b**,
after the completed ten dashboard fixes at c285fb1f. **Unreleased.**

The approved compact Range dropdown offers Today and 7/30/60/90/180/365-day
presets up to the server's saved retention limit. Manual retention adds All
retained history. A refresh updates these choices; if retention is reduced,
an invalid selection clears its old report and loads the longest permitted
range. Failed reads remain errors rather than relabeling an earlier report.

The former 30-day viewing limit and raw 721-hour fetch are replaced with SQL
aggregation over existing hourly tables. Short ranges remain hourly; ranges
through 180 days use daily totals; longer history uses weekly or wider week
multiples. Each overview/API series contains at most 720 periods. Existing
ranking limits and a five-second query budget keep reads bounded. Weighted
averages use summed durations and request counts, rather than averaging
averages. No collection/storage schema, parser, retention cleanup, API
classification, log replay or background worker was changed.

The Report adds `retention_days`, `bucket_seconds` and nullable
`available_from`. Existing `hour` names the period start; the last period is
clipped at `to`. UTC/year labels, coverage gaps, partial/capped periods and
genuine quiet zeros remain distinct. Increasing retention cannot recreate
deleted or uncollected history. The existing API/operator docs and demo match.

Validation passed:

- **136 frontend TAP tests + 8 existing maintenance-console checks**, including
  six new tests for every retention menu, variable chart widths, missing/quiet
  data, year labels, real mounted view refresh/fallback/failure and bounded
  demo history with forward-only API rules/deletion behavior.
- **Go1.26.3 full Traffic and server suites**, focused History/Traffic race
  checks and affected vet. Five new Traffic tests plus one HTTP test cover
  UTC bounds, weighted overview/API totals, 1,000 stored hours without the old
  truncation, end-exclusive/fractional boundaries, 1970-to-present bounded
  lifetime output, expiry/deletion/no backfill, cancellation and validation.
- Dashboard typecheck/Vite, site/demo/functions/SSR/prerender/docs builds and
  UI-first **Go1.26.3 CGO0 AMD64 development compilation**. Development binary
  SHA256: `c5da32eb325b45e6129c27b7f25c411186bdc229eacd5f59e17337cb0b370b84`.
  This is compilation evidence, not a signed/public/native release image.
- `git diff --check`; installer copies, workflow, bridge and module-pin paths
  have no changes from c285fb1f.

Tests use actual dedicated SQLite queries/private aggregate fixtures and Vue
component/SSR fixtures. No VM, VPS, service, package, reboot, external browser,
new subagent, release build/tag/publication or main/latest promotion was used.
The approved simulation used sample data; no new live-browser, native ARM,
public installer/update or whole-VPS performance result is claimed. Existing
native/public evidence retains its original sources and limits. Immutable
base-test.2 still contains its original source, not these fixes.

The JSON companion records exact selections, source, scope, local log paths
and SHA256s under `.base-validation/traffic-history/`. Initial fixture-only
adjustments and the updated unsupported-range negative case are identified;
no product validation was weakened to pass tests.
