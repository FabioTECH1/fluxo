# Fluxo v0.4.57 — WordPress Cron

The immutable release adds an optional per-site WordPress Cron toggle, off by default. The skill, MCP and temporary agent authentication are excluded. Existing bases update directly; the qualified legacy bridge remains v0.4.55.

- Signed product source: `7c2804f242d8d8d2e32d47840ceda76f53ebb39f`.
- [Official release build](https://github.com/FabioTECH1/fluxo/actions/runs/45359909354): PASS; release `456506657` is immutable and promoted to GitHub latest.
- [Source-native WordPress check](https://github.com/FabioTECH1/fluxo/actions/runs/45358259238): PASS on a disposable Ubuntu 24.04 AMD64 runner with real WordPress 7.1.5.
- [Exact-public update check](https://github.com/FabioTECH1/fluxo/actions/runs/45361926908): PASS, authentic public v0.4.56 → v0.4.57 → ordinary reinstall.
- Eight public assets, four exact-source/tag/hosted-workflow attestations, both ELF architectures, Go 1.26.3/CGo=0, all 19 tool assignments, exact version and unchanged base contract passed hosted and independent cached-byte verification.

## WordPress behavior verified

The real API created a WordPress site and database, and the toggle initially reported off. A genuine cron-launched event ran as the unprivileged application user. The real installer maintenance lease waited for its natural completion, prevented new payloads across a 65-second hold, and allowed genuine cron to resume after release.

The enabled job and its identity survived an ordinary panel update. Disabling restored the original `wp-config.php` bytes, and a real HTTP visit then ran a due event through PHP-FPM. No manual cron acknowledgement, journal mutation, forced application stop or replay was used.

The exact-public case also preserved installation data, key, TLS, application/history/retention evidence and genuine root-task/Scheduler execution. Installer timings were 25.345s for v0.4.56 → v0.4.57 and 25.378s for the ordinary reinstall: individual hosted observations, not performance guarantees. All saved native runs report successful fixture cleanup.

## Validation and retained failures

Full local and official Go suites pass, along with focused race checks and affected vet. All 152 dashboard TAP tests and 8 existing maintenance-console checks pass. Dashboard typecheck/build, site/demo/functions/SSR/prerender/docs builds, and 46 focused public-fixture tests pass.

Two earlier source-native attempts are retained: the first fixture did not wait for the native dpkg lock during database setup; the second observed only 95 seconds, insufficient for a first loader acknowledgement followed by the next genuine payload tick. The revised fixture waits without removing locks or changing production guards and observes two full initial intervals.

The initial public attempt successfully installed the signed v0.4.57 image, then the fixture rejected the new exact tag because its capability table ended at v0.4.56. Only that table was corrected, requiring the same four existing optional capabilities. The final public case passed. No immutable asset or tag was replaced. The independent cache relay comes from the first public attempt, after its complete byte/provenance verification; the successful native result remains separately attributed.

## Scope

This is one native Ubuntu 24.04 AMD64 route with real WordPress. ARM64 bytes, provenance and build metadata are verified; no new native ARM WordPress, full PHP/Node apply, reboot, ACME, external DNS or exhaustive plugin compatibility claim is made. Source-level PHP/Node launch-boundary tests and the real shared maintenance lease are distinct from full runtime installation acceptance.

No local VM or user VPS was used. The original stopped fluxo-dev was untouched. Older release assets remain immutable. Production website verification is recorded after the main deployment. See the [companion JSON](base-0.4.57-release.json) for actual reports, scopes, identities and retained failures.
