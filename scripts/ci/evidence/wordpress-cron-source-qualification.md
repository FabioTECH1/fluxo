# WordPress Cron source qualification

Source: `196f1cc48e2111b6ca4c3c5de86adc144cc6a37f`. Base-only implementation; not released.

WordPress sites have an optional Overview toggle, off by default. It publishes one every-minute guarded application job with the site's selected PHP branch and `fluxo` credentials. The generated command uses `flock` and WordPress's own `wp-cron.php`, preserving WordPress's event preferences and existing task execution semantics.

The feature uses the existing application-job journal and PHP/Node maintenance admission, lifecycle lease, acknowledgement and drain protocol. No new task kind, helper capability, execution format, floor or bypass was added. Package updates within the site's current PHP branch continue through existing maintenance. Running plugin work can delay drainage; it is not force-stopped or replayed. Switching PHP branch/web root or deleting the site first requires disabling this owned job.

A marked `DISABLE_WP_CRON` block is added only after guarded publication. Disable restores visitor scheduling before retirement. Config writes preserve ownership, permissions and unrelated bytes, validate syntax as the application user, and reject symlinks, hardlinks and concurrent edits. Only a SHA-256 of pre-change config is retained; no config credentials are stored in feature state. Custom/alternate cron and multisite settings are preserved for manual review.

Interrupted work retains its cron identity and explicit retry/off controls. A proven prepublication refusal discards its unused DB rows under the publication mutation reservation, so a refused optional feature does not create an unknown maintenance consumer. Uncertain authority is never discarded.

Validation: eight full affected Go suites, focused race checks and affected vet pass; 152 frontend TAP tests plus eight existing maintenance-console checks pass; dashboard typecheck/build and full site/demo/functions/SSR/prerender/docs build pass. Fifteen new top-level Go tests and six UI tests cover ownership, duplicate prevention, config preservation, interrupted publication/retirement, maintenance gates, strict API/auth, existing Node task inventory and shared component rendering. The synthetic host PHP/flock test verifies system overlap skipping only.

Initial incorrect Node fixture columns and missing Laravel UI fixture bindings are recorded in JSON and corrected. The private development build compiles with Go 1.26.3 and CGO disabled, SHA-256 `360db4acf7525bedf7f4957a0675f20ddf3ae48de0a9a8fbf7fe04cc432a023f`. Its embedded VCS identity is unavailable in this local compiler environment; this is compile evidence, not exact-image or release evidence.

No actual WordPress core/plugin/database execution, genuine new-job cron acknowledgement, native systemd publication, live PHP/Node apply or installer acceptance is claimed. No VM/VPS or production release was changed. Existing bridge, installer, workflow and dependency/tool-pin paths remain untouched.

Raw local logs: `.base-validation/wordpress-cron/`. Their hashes and the full validation scope are bound in the companion JSON.
