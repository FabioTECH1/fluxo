# Restart tracking and minimised status — source qualification

Production source: `ed83df1b2c0603d51c59f0486ef5fcdc05a19ae9` on `feat/dashboard-navigation-notifications`.

Status: source-qualified, not released. The published `v0.4.58-test.4` remains unchanged.

Read-only customer metadata shows an accepted request from an older boot and a later empty-ID browser attempt on the current boot. The first restart completed; the later tracking entry must not be presented as another successful or pending reboot. Private VPS identifiers are omitted.

The change distinguishes pre-scheduling refusal from recorded failure and uncertain acknowledgement, preserves request IDs, completes checks only after matching changed-boot evidence, and retires the narrowly identified legacy local tracking state without sending another request. Real pending records remain protected.

Minimise now uses the existing shared persistent toast. Show details removes that toast while the same modal is open. The progress summary is top-aligned; completed/refused tracking results are closeable. Actual Ubuntu restart requirements remain independent. Existing API/settings docs and safe demo behavior are synced.

Checks:
- PASS: 189 TAP + 8 maintenance presentation checks; log `.base-validation/restart-outcome-followup/ui-final.log`, SHA256 `4fad3e7aadd11309671bb2e67bb4886b0afd49878fc1a5c645a503795abc7ecd`.
- PASS: vue-tsc and Vite build; log `.base-validation/restart-outcome-followup/ui-build.log`, SHA256 `dd61dab3c5882749af5c494cdc5396e95b05e4e210dfcf15d6d7c6f47b2b35d3`.
- PASS: Functions, demo, SSR, prerender and docs build; log `.base-validation/restart-outcome-followup/site-final-build.log`, SHA256 `d219e7a36c8e7aa4009aae551c54a1c2220e57e50150d8ddef4d71b4959ab4dd`.
- PASS: Full server and hostsystem suites; log `.base-validation/restart-outcome-followup/go-full.log`, SHA256 `30db3c9a7dff4c59ec85e24ebc93b6b91ad0417efbe93315a4e9c054f9c013a1`.
- PASS: Focused ServerSystem and RestartScheduling race tests; log `.base-validation/restart-outcome-followup/go-race.log`, SHA256 `cd63676d717bad8b80702360dbbed69ea92a0eff3e99db54ed23fb69c1399abc`.
- PASS: Affected server and hostsystem packages; log `.base-validation/restart-outcome-followup/go-vet.log`, SHA256 `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`.

The 30 System tests include the customer metadata pattern, legacy negative cases, no retry/duplicates, lost-acknowledgement ID retention, changed-boot gating, real shared-toast visibility, dismissal and persistent Ubuntu requirement. Initial mounted-test import-loader failure is retained separately and was corrected before the final passes.

No native/live reboot, VPS/VM, package/service action or official build occurred. Mounted component tests stub modal focus/transitions and do not establish browser geometry. Bridge/helper authority, locks, installer/workflows, main/latest and immutable releases are unchanged. Full details and log digests are in the adjacent JSON record.
