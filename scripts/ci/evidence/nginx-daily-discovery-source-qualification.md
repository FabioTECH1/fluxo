# Daily Nginx discovery: base source qualification

Daily background discovery is implemented at `1bea2f33d8f0cecc513b8c695b63d6b704192ea0` on `baseline/clean-base`. This is an **unreleased base source candidate**. [Exact record](nginx-daily-discovery-source-qualification.json) retains commands, checks, build and log hashes, earlier fixture failures and boundaries.

Installed production servers use the existing daemon lifecycle to check when discovery is due after a staggered two-to-five-minute startup delay. Successful manual or scheduled checks persist the next due time 24 hours later. A fresh saved result survives daemon restarts without another refresh. The browser need not be open, and GET/polling continues to read saved facts only. No additional daemon, cron entry or systemd service is created.

Automatic discovery defers five minutes when existing Fluxo maintenance or native package locks are busy. The prior result, age, attempt/success timestamps and candidate dismissal stay intact. Genuine refresh, inspection or 90-second deadline failures retain stale facts and retry after an hour. Integrity/state errors remain failures. Unsaved results retain an in-process retry backoff. Shutdown cancels the worker without manufacturing a failed discovery; repeated lifecycle registration cannot duplicate it. Manual checking remains available before the scheduled due time.

The native lock preflight releases dpkg locks before the existing `apt-get update`, and inspection reacquires them. This remains package-awareness only: no package installation/removal, repository migration, service activation or reboot action is added. Metadata refresh has some repository network/CPU cost and may briefly compete for package coordination. Already-busy maintenance defers the check; work arriving after a check starts can wait for its bounded completion. Existing administrator APT refresh hooks remain in effect.

## Validation

- Nine new Go test methods cover persisted schedules/reopen, manual/daily interaction, candidate dismissal, busy deferral, failed-check retry, legacy records/clock handling, startup jitter, browser-independent daily execution, duplicate registration/checking, cancellation, unsaved results and timeout truth.
- Virtual elapsed time uses Go 1.26.3 `testing/synctest`; this is not a claimed overnight/native APT run. Test-only counters and failure switches were changed to atomics/mutexes after the first race run exposed fixture races. Final affected race passes.
- Full nginxupdate/server Go suites, focused Nginx race and affected vet pass. The module has 18 top-level methods, plus the existing HTTP/SQLite/wiring checks in the server suite.
- Nine unchanged Python/libapt tests pass using isolated Python. The first non-isolated invocation shadowed standard-library `inspect`; its failure log remains separate. No production policy checks were relaxed.
- Eight console and 117 UI/demo TAP checks, dashboard typecheck/build and complete site/functions/SSR/prerender/docs build pass.
- Clean-source Go 1.26.3 CGO0 AMD64 development build with all 19 official pin assignments passes: SHA256 `e86204ed1347b02c88364ff424fe11df41b3c1cafce033176212e80bcc68bc6a`. This is compilation evidence, not a signed public release.

## Preserved scope

No VM was started or changed. `fluxo-dev` remains Stopped with zero snapshots. Existing System/Network native evidence and its source/image scopes remain unchanged; none is borrowed for this new scheduler. Real configured-host APT discovery/native-lock acceptance for the Nginx feature is still unclaimed. Candidate availability does not prove an installable dependency transaction or running Nginx binary adoption.

The user's production request was clarified as **the bridge only**. GitHub's public latest-release endpoint confirmed existing immutable stable `v0.4.55` (release `454442567`), matching main/tag identities already recorded. No new bridge release, retag, asset replacement or base promotion was performed. Base source remains off main/latest; existing immutable base055-test1 does not contain this follow-up.
