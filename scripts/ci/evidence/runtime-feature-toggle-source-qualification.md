# Runtime feature toggle source checks

Source `78865266e7f3f7fc764f49d220b06c843549ee9d` on `fix/bridge-runtime-compat`. This is a source correction, not a new release or acceptance of replaced public assets.

The Scheduler enable path previously held its new cron row in an uncommitted transaction while schema-5 loader publication opened an independent read-only database connection to verify it. The row is now committed first. Atomic conditional insertion prevents duplicate enable requests. A publication failure preserves the exact saved binding for review/recovery instead of removing a row a partial publication may reference. No execution contract, floor, receipt or acknowledgement check is relaxed.

The shared HTTP guard now distinguishes durable pending work, temporary lease contention and lock validation/I/O failures. Its log records the actual refusal reason. Scheduler and Nightwatch requests prevent duplicate submissions; related feature errors display the API message rather than JSON. Existing maintenance admission remains mandatory.

Passed: server/cron/Scheduler Go suites, focused server race tests, affected vet, 56 frontend TAP cases plus 8 maintenance-console checks, dashboard typecheck/build, website/docs build and a CGo-free embedded development build. Independent SQLite reads reproduce the old uncommitted-row refusal and verify the committed binding, failed-publication preservation and duplicate prevention.

On the named Ubuntu AMD64 VM, `TestVMRuntimeFeatureGateReopensAfterLeaseRelease` passed using the real kernel lifecycle flock and production middleware: Scheduler, Nightwatch, Queue Worker, Horizon, Octane and Maintenance paths all refuse during the exclusive lease and are admitted after release. The terminal handler is a test observation, not a real application feature. This does **not** claim native Scheduler publication/ACK, Nightwatch service execution, a new PHP/Node update or ARM acceptance. Only comments and frontend test-fixture additions followed that native build; its gate code is unchanged.

The original VM snapshot `before-feature-toggle-20261121` was restored successfully; all three local guests are stopped. No customer VPS was accessed or changed.

The customer reports successful PHP update and Node migration on immutable trial 3. Their later read-only check has no pending maintenance or holders and shows protected lock metadata. It does not establish what held/refused the earlier request; that cause remains unconfirmed.

Exact local log and test-binary hashes are in the JSON companion. Main/latest, existing immutable assets and release tags are untouched. These changes are absent from installed trial 3.
