# Release notes

## v0.4.41 — 2026-10-29

### Fixed
- Allow guarded Certbot renewal for Fluxo's own panel ACME webroot, `/var/lib/fluxo-acme`. Previously, a valid panel certificate could prevent scheduled-job registration and block reviewed Node.js or PHP maintenance.
- Share the panel webroot constant between certificate issuance and maintenance qualification to prevent those paths from drifting apart.

### Compatibility and validation
- Existing supported site webroots remain accepted. No certificate, renewal configuration, cron schedule, database schema or maintenance journal is rewritten by this correction. Custom hooks, private data paths, lookalike prefixes and noncanonical paths remain rejected.
- The reported renewal configuration (including `renew_hook` and both persisted webroot forms) fails against the previous validator and passes with the correction. Full Go tests and UI/site/docs builds passed; no live certificate renewal, production upgrade or new runtime-switch acceptance is claimed.
- After upgrading, Fluxo retries scheduled-job registration automatically. Retry runtime review once registration completes; other qualification failures remain visible rather than being bypassed.

## v0.4.40 — 2026-10-29

### Runtime maintenance
- Bring the v0.4.39 hotfix series into the stable release: reviewed migration of qualified older NodeSource installations, signed same-major Node.js updates, exact retained-runtime rollback, and coordinated Node.js/Bun/PHP consumer drainage and recovery.
- Coordinate supported scheduled jobs with runtime maintenance. Running work and detached children finish naturally; new launches pause; completed or uncertain jobs are not automatically replayed. Supported root jobs use qualified fixed dispatchers; unsupported root hooks and nonempty APT removal plans still require operator review.
- Correct PHP installed-version detection, Scheduler active-directory qualification and targeted panel-proof recovery. PHP version installation requires confirmation, and Node.js outcomes appear in Activity instead of a duplicate history panel.

### Installer and confirmation improvements
- Remember optional Node.js, Python, database-engine and Redis choices, including declined components. Explicit flags override saved choices. Older installations without a saved record preserve installed components and skip absent optional components on upgrades without prompting.
- Save preferences atomically in root-owned `/var/lib/fluxo-installer/preferences.json`; refuse malformed or unsafe records. Saved choices record intent, not successful provisioning, and never remove components installed later through Runtime.
- Keep Node.js migration, update, repair and rollback confirmations concise while retaining affected domains/services, scheduled-job counts, actual blockers and relevant warnings.

### Validation and upgrade notes
- Full Go tests, installer preference and existing installer regressions, and UI/site/docs builds passed. Earlier hotfix reports retain their explicit live-acceptance limits; this release does not claim a new full live VPS upgrade or ARM runtime acceptance.
- This is a stable release and becomes the default selected by the installer. Existing published hotfix tags remain available for pinned repairs.

## v0.4.39-hotfix.5 — 2026-10-29

### Fixed
- Remember optional Node.js, Python, database-engine and Redis installer choices, including declined components. Explicit component flags override saved choices. Older installations without a saved record preserve installed components and skip absent optional components on upgrades without prompting.
- Save preferences atomically in root-owned `/var/lib/fluxo-installer/preferences.json`. Malformed or unsafe records refuse installation rather than silently changing component choices. Preferences record intent, not successful provisioning; components installed later through Runtime are not removed or disabled.
- Shorten Node.js migration, update, repair and rollback confirmations to the version change, affected domains/services, scheduled-job count and relevant warnings. Actual blockers and confirmation requirements remain unchanged.

### Scope
- Installer preference fixtures, existing installer/update-check regressions, full Go tests and UI/site/docs builds passed. No full live VPS upgrade or new runtime-switch acceptance was performed for this installer/UI-only change.
- This is an explicitly selected prerelease below reserved `v0.4.40`. Stable `latest` is unchanged; pin `FLUXO_VERSION=v0.4.39-hotfix.5` when installing.

## v0.4.39-hotfix.4 — 2026-10-29

### Fixed
- Coordinate supported scheduled jobs with Node.js/PHP maintenance instead of blocking them by command name. The exact generated System Cleanup, Renew SSL Certificates and Update Composer jobs use qualified fixed root dispatchers; ordinary local jobs running as `fluxo` or `www-data` use guarded non-root execution.
- Scheduled ticks and Run now share durable admission. Running work and detached children finish naturally before a runtime switch; pending maintenance skips new launches. Interrupted execution retains an unknown outcome until verified reconciliation and is never replayed automatically.
- Authenticate scheduled Node children by their exact runtime, current helper role, shared lease, credentials and boot/start identity. Preserve unrelated-process and wrong-runtime blockers.
- Recover interrupted empty initialization and orphaned partial registrations without resetting history, recreating deleted database rows or launching their commands. Changed job rows can still be safely revoked after verified cron acknowledgement/drain.
- Preserve independent jobs, schedules, stopped/deleted intent and immutable runtime plans. Document the shared scheduled-job review receipts and root reconciliation command.

### Scope
- Root support requires the documented qualified implementation/configuration. Initial APT cleanup allows an empty removal plan and cache cleanup; nonempty package/script plans, custom root commands/hooks/plugins and external delegation still require operator review.
- Local Ubuntu 24.04 amd64 VM acceptance covered real cron/systemd, API admission, descendant drainage, interruption/no replay, default-root profiles and guarded Certbot/Composer execution. Full Go/race/vet, amd64/arm64 builds and UI/site/docs builds passed. This turn did not repeat signed runtime activation/reboot, full installed panel-main, ARM runtime or issued-lineage certificate renewal acceptance.
- This is an explicitly selected prerelease below reserved `v0.4.40`. Stable `latest` is unchanged; pin `FLUXO_VERSION=v0.4.39-hotfix.4` when installing.

## v0.4.39-hotfix.3 — 2026-10-29

### Fixed
- PHP version installation now asks for confirmation through the shared confirmation modal before starting. Canceling makes no installation request.
- Laravel Scheduler maintenance qualification now follows the site's active deployment directory, including zero-downtime `current/artisan` layouts. Review and execution bind the current site layout while preserving the PHP interpreter specified by the task.
- Node.js maintenance outcomes appear in Activity. The duplicate Recent maintenance list is removed from the Node.js runtime page, and periodic checks do not create Activity noise. Active progress and recovery controls remain available.

### Scope
- System Cleanup, Renew SSL Certificates, and Update Composer remain unsupported root scheduled-task blockers for Node.js maintenance. This release does not bypass them or add the unfinished root-task adapter.
- This is an explicitly selected prerelease below reserved `v0.4.40`. Stable `latest` is unchanged; pin `FLUXO_VERSION=v0.4.39-hotfix.3` when installing.

## v0.4.39-hotfix.2 — 2026-10-29

### Fixed

- Offer an explicit reviewed migration for qualified legacy NodeSource installations created by older Fluxo versions. Select an authenticated official Node.js release within the installed supported LTS major, update its bundled npm, and retain the exact APT source for verified rollback while it remains unchanged. Migration does not overwrite or remove dpkg-owned runtime files, change other package managers, or install updates automatically.
- Coordinate managed Node.js, Bun and qualified PHP consumers through durable holds, complete process-domain drains and health verification. Preserve stopped/disabled intent and completed finite Artisan receipts through repair, rollback and interruption recovery.
- Determine PHP installation state from package metadata and actual executables. Residual configuration directories no longer appear as installed FPM versions; incomplete installations and metadata errors receive distinct guidance without admitting an unsafe runtime operation. Recorded maintenance recovery remains available.
- Remove owned Node start permits before releasing maintenance admission, retaining recovery state if cleanup fails.

### Upgrade notes

- This is an explicitly selected prerelease below the reserved `v0.4.40`; stable `latest` is unchanged. Install using `FLUXO_VERSION=v0.4.39-hotfix.2` and retain your existing component flags.
- After upgrading, review legacy migration in **Runtime > Node.js**. Unsupported custom layouts remain blocked; do not fabricate ownership receipts or manually redirect runtime links. PHP residual configuration is preserved, not automatically deleted.
- Includes the installer corrections from `v0.4.39-hotfix.1`. Supported-scope acceptance covered Ubuntu 24.04 amd64, real migration/rollback, journal faults, native package locks and reboot recovery. ARM builds passed; live ARM and full installed-panel-main acceptance were not run.

## v0.4.39-hotfix.1 — 2026-10-23

### Fixed

- Target the installed production database during Node.js toolchain installation instead of inspecting an unrelated `fluxo.db` in the SSH working directory. The installer supplies explicit production configuration; direct CLI use retains admission checks for the production database and any explicitly configured absolute custom data directory. Relative custom directories are rejected before installation.
- Report successful Node.js snapshot restoration correctly during failed-install exit cleanup. Successful restored-path and application-health checks no longer inherit the original installation error. Genuine copy, removal and rename failures still retain their snapshots and report incomplete rollback; the original installation failure remains an error.

### Upgrade notes

- This hotfix is a prerelease and does not replace the stable `latest` release. Select `FLUXO_VERSION=v0.4.39-hotfix.1` explicitly when installing it.
- No schema migration, database deletion, ownership change or maintenance-journal reset is required. If v0.4.39 stopped with `unsafe ownership or permissions: .`, first confirm the restored panel and Node.js state, preserve its snapshots, then rerun the v0.4.39-hotfix.1 installer with your original component choices. See [installer troubleshooting](https://fluxo.fabiotech.dev/docs/reference/troubleshooting#node-js-installer-rejects-the-ssh-working-directory).
- Includes the reviewed Node.js/PHP maintenance workflows from v0.4.39. Their supported-consumer and recovery requirements remain unchanged.

## v0.4.39 — 2026-10-23

### Added

- Update a Fluxo-managed Node.js runtime and its bundled npm from Runtime > Node.js. Daily cached checks and manual checks discover releases within the currently installed, supported LTS major; installation requires review and approval of exact versions and affected applications.
- Verify official signed Node.js checksum metadata against a bundled trusted keyring, retain the previous runtime, and run maintenance in an independent durable worker. If activation fails, Fluxo attempts automatic rollback and verifies the restored runtime; interrupted maintenance provides separately reviewed Repair and Rollback actions.
- Add the root SSH command `fluxo scheduler-verify-panel` to establish missing manual Scheduler verification without reinstalling or restarting the panel. It waits for earlier work to finish naturally and preserves Scheduler history and unfinished installer/maintenance holds.

### Fixed

- Qualify supported independent custom queue workers and other direct site-bound Artisan commands without adopting, deleting or combining process entries. Preserve commands, settings, enabled state and original running/stopped intent. Commands with unknown shutdown behavior receive no guessed stop signal; durable execution receipts prevent replay of completed one-off work during recovery.
- Coordinate inspected Node.js and Bun applications, qualified PHP background processes, deployments, commands and managed Scheduler work around Node.js activation. Drain complete execution groups, including late Node children, then verify replacements. PHP packages and PHP-FPM remain unchanged during a Node.js update.
- Preserve approved Node.js releases through toolchain repair and installer runs carrying older or different-major build pins. Keep pnpm, Yarn, Corepack and Bun versions unchanged during a runtime update, while checking their usability.
- Retain recovery protection through worker/panel interruptions, reboot, failed application health checks and journal failures. Support disabled consumers and short-lived Artisan completion without enabling stopped services or replaying uncertain work.
- Identify affected sites by domain and background processes or scheduled tasks by name in update messages and review details. Missing-domain records remain explicit blockers. Remove numeric site fallbacks from dashboard messages and webhook activity.

### Upgrade notes

- SQLite migrations are additive. Resolve unfinished runtime maintenance before upgrading Fluxo; existing recovery journals and barriers must be preserved. Installing this panel release does not automatically approve or install a Node.js or PHP runtime update.
- Automatic maintenance requires the supported managed runtime, effective service definitions and verified consumer ownership described in the [runtime guide](https://fluxo.fabiotech.dev/docs/operations/runtimes). Unknown/custom process managers and unqualified scheduled work remain blocked rather than being stopped speculatively. A long-running custom Artisan command may require its application's documented graceful exit; an unconfirmed execution across interruption or reboot requires operator review.
- Runtime rollback restores runtime files and service intent, not application files, databases or jobs. Older Fluxo releases which cannot preserve established runtime approvals and coordination may be rejected by the current installer.
- Live acceptance covered Ubuntu 24.04 on amd64, including combined Node.js/PHP consumers, rollback and recovery. Linux arm64 binaries are cross-built; an arm64 runtime acceptance run was not performed.

## v0.4.38 — 2026-10-21

- Shorten the PHP rollback confirmation to a downtime notice, deduplicated affected sites/services, and one security warning.
- Automatically reclaim recorded package payloads from superseded, completed PHP updates after a newer verified forward update on the same branch. Preserve the latest package sets, unfinished recovery, retained installer snapshot dependencies and the previous complete rollback set when a newer update lacks rollback files.
- Keep manifests, configuration backups and operation history. Defer unsafe or uncertain cleanup, retry in the background, and report cleanup separately from PHP update success.

## v0.4.37 — 2026-10-21

### Fixed

- Recognize cron and Cronie separately from managed-file configuration; report their operational and PHP maintenance status truthfully.
- Recover eligible initial Scheduler qualification failures while preserving normal ticks, registration identities and deleted jobs. Qualify standard Ubuntu 24.04 Cronie `1.7.1-1` on amd64 without a daemon restart or VPS reboot.
- Preserve existing scheduling providers and service state during installation. Guard APT transactions against unintended provider changes and stop fresh provisioning if installing cron would activate pre-existing schedules.
- Add sanitized read-only `scheduler-diagnostics` output, background migration reconciliation and support negotiation compatible with in-flight schema-2 helpers.
- Retain Scheduler records when coordinated removal fails, and distinguish missing/modified files from configured jobs in the Scheduler view.

### Upgrade notes

- Existing qualified cron and Cronie schedules keep running while Fluxo verifies the tracked launcher and waits for earlier work to finish naturally. No scheduler restart or server reboot is required. PHP installation remains blocked until that verification completes; checking for package updates stays available.
- Cronie legacy migration is qualified for the standard Ubuntu 24.04 `1.7.1-1` amd64 package and service/PAM configuration. Custom configurations and other architectures continue scheduling but may require manual PHP maintenance.
- Scheduler backend recovery evidence is preserved independently of panel snapshots. Once established, it requires a compatible panel release; the installer refuses incompatible downgrades before stopping the panel.

## v0.4.36 — 2026-10-21

### Added

- Pause supported managed Laravel Scheduler entries during PHP patch maintenance. Running tasks and their background children finish naturally; new ticks are skipped until package and service verification succeeds. Failed or interrupted maintenance keeps Scheduler paused through Repair or Rollback.
- Track Scheduler invocations in dedicated systemd cgroups with a persistent root-owned journal. Manual Run now follows the same maintenance gate; stopped or absent schedules remain unchanged.

### Changed

- Show the installed PHP patch (for example, PHP 8.4.26) only in the Runtime > PHP Versions table; selectors and actions continue to use branch identifiers.
- Simplify the PHP update confirmation to a brief downtime notice and deduplicated affected sites and services. Keep detailed diagnostics in the operation report. Ask for explicit acknowledgement only when package rollback is unavailable.

### Fixed

- Recheck a previous package-retention failure so a temporary download outage does not keep requiring the exceptional rollback acknowledgement after verified archives become available.
- Preserve Scheduler admission across recovery, server restarts, panel upgrades and partial service activation. Refuse stale registrations bound to another panel database, and keep absent Scheduler triggers absent during startup log repair.
- Retain an independent trusted installer helper through candidate publication and cleanup, with bounded waits and safe retry after an interrupted installer.

### Upgrade notes

- Automatic Scheduler maintenance requires systemd 250 or newer and cgroup v2, normally available on Ubuntu 24.04 and newer. Custom cron entries and unsupported hosts still need a manual maintenance plan.
- Existing exact Fluxo-generated minute Scheduler entries migrate at panel startup and continue scheduling normally. On qualified Ubuntu 24.04 cron configurations, Fluxo verifies the new launcher was loaded and earlier work finished naturally, without restarting cron or the VPS. Other cron/PAM configurations require a manual maintenance plan. Newly created managed entries are tracked immediately.
- Scheduler ticks skipped during maintenance are not replayed. Application tasks are not force-killed by Fluxo when draining times out.
- The independent Scheduler journal and dispatcher are preserved across panel snapshots. Automatic restoration of an older panel binary that cannot understand established coordination is refused; the installer retains its recovery snapshot. PHP package rollback remains a separate supported recovery action.

## v0.4.35 — 2026-10-21

### Added

- Automatically migrate recognized PHP PPA configurations during installer upgrades to Ondřej Surý’s official Ubuntu repository using a scoped, release-pinned key, private metadata validation, configuration backups and resumable journaled publication.
- Preserve authenticated unchanged independent extensions across repository revision differences, and retain exact PHP package recovery rights across explicitly recorded repository-policy transitions.
- Use the new repository for fresh installs and preserve existing PHP versions and CLI defaults during panel upgrades. Keep concise repository repair/resume controls for exceptions.

### Fixed

- Separate PHP package discovery from installation review. Service restart blockers appear in the installation confirmation, while checks report available packages without inspecting or stopping application services.
- Stop and restart Fluxo-generated Nightwatch agents during PHP maintenance, preserving stopped instances and verifying the assigned listener. Monitoring pauses and buffered telemetry may be lost.
- Identify affected Laravel Scheduler cron commands by their actual PHP interpreter, including after a site changes branches. Installation requires pausing the scheduler until maintenance completes.
- Keep successful package discovery available when runtime readiness changes or an installation review expires; require another installation review instead of reporting a failed package check.

### Upgrade notes

- Normal installer upgrades migrate recognized legacy PHP PPA entries automatically. Ubuntu PHP branches retain Ubuntu and ESM security sources; conflicting custom source or PHP pinning configurations require reconciliation.
- Panel upgrades preserve installed PHP packages and CLI selection. Review and install PHP patches separately from Runtime > PHP.
- Interrupted repository setup resumes its saved configuration journal. Package rollback keeps the new repository configuration, and remains subject to complete verified recovery archives.

## v0.4.34 — 2026-10-21

### Added

- Patch installed PHP branches from Runtime > PHP, with daily cached package checks, manual refresh, affected-site review, asynchronous progress, persistent notices, and operation reports.
- Drain and restart affected managed queue workers, Horizon, and qualified Swoole/RoadRunner Octane configurations while preserving services that were stopped. New queued jobs remain available for consumers after maintenance.
- Retain verified previous and target PHP package sets for separately reviewed Repair and manual package rollback. Updating without the complete previous package set requires explicit acknowledgement.
- Preserve maintenance holds and recovery state across interruptions, verify FPM configuration and running runtimes, and block conflicting managed operations while maintenance remains unresolved.
- Document the supported runtime configurations, recovery limits, API contracts, and isolated Ubuntu acceptance tests, with a release article and simulated demo workflow.

### Fixed

- Exit the installer without package changes or service restarts when the selected official Fluxo release is already installed and healthy. Use `--reinstall` for an intentional reinstall.
- Use the existing close icon for PHP notices, distinguish table row hover from headers in both themes, improve dark-mode Manage link contrast, and format demo errors as readable messages.

### Upgrade notes

- Additive SQLite migrations store PHP maintenance plans, operations, archives, barriers, and recovery state. Existing site and CLI PHP defaults are preserved.
- PHP patching is manual and stays within the installed branch. Package rollback is a separate action and does not restore application files, databases, Redis data, or jobs. Interrupted package journals or unrelated damage can still require SSH repair.
- Octane automation is limited to the versions and configurations listed in the runtime documentation. Existing instances need the maintenance integration and a controlled full restart before using it.

### Changed

- License Fluxo under GNU AGPLv3 (`AGPL-3.0-only`) and update the website, documentation, application footer, and package metadata to describe it as open source. Previously published releases retain their original licensing terms.
- Accept contributions under AGPLv3. Include the license text with future release assets.

## v0.4.33 — 2026-09-30

### Changed

- Move the public website, documentation, installer links, and release-check endpoint to `fluxo.fabiotech.dev`. The former `fluxo.fottify.com` hostname remains available through a permanent redirect that preserves paths and query strings.
- Show complete process and scheduled-job commands on hover in both server-wide and per-site views and overview summaries.

### Fixed

- Show GitHub webhook commit message and author as soon as an automatic deployment is queued, avoiding a temporary manual-deployment label.
- Use a trigger-specific label when a pending deployment has no commit message yet.

### Upgrade notes

- No database migration is required. Existing installation and upgrade commands using the former hostname continue to work through the redirect.

## v0.4.32 — 2026-09-18

### Fixed

- Reconcile Push to Deploy webhooks and retain one working callback per repository. Fluxo prefers a verified working public-IP callback, repairs its push-only configuration when needed, and removes duplicate panel-domain or legacy callbacks only after establishing that they belong to this server.
- Register a webhook only when Push to Deploy is enabled or an enabled site changes repository. Saving unrelated deployment settings no longer creates another callback based on the hostname used to access the dashboard.
- Remove the tracked repository webhook when Push to Deploy is disabled or an enabled site moves repositories, while preserving a shared callback when another local site still deploys from that repository.
- Resolve the existing webhook ID when GitHub reports an identical callback and remember both its ID and URL for reliable future cleanup.
- Verify signed deliveries against GitHub's delivery API before recording their hook IDs as ownership evidence, then reconcile that repository in the background. This lets upgrades safely discover legacy callbacks without trusting unsigned request headers or claiming another Fluxo server's webhook.
- Suppress repeated webhook deliveries for the same site and incoming commit within a short safety window so duplicate provider callbacks cannot enqueue two deployments while reconciliation is pending.

### Upgrade notes

- An additive SQLite migration stores the retained webhook URL. Existing sites, deploy keys, repositories, branches, and deployment settings are preserved.
- Webhook reconciliation runs in the background after startup and does not delay the health check. The connected GitHub token must still have permission to list, update, and delete repository webhooks; failures are logged and retried on the next restart or relevant Push to Deploy change.
- Reconciliation does not claim callbacks merely because they use a public, private, or legacy-looking address. A callback must be tracked locally, exactly match the configured panel endpoint or the server's listening address and port, or have a signed delivery independently matched to its hook through GitHub's API before Fluxo can update or remove it.

## v0.4.31 — 2026-09-14

### Fixed

- Preserve non-empty Laravel application names from environment templates; use `Fluxo` only when `APP_NAME` is missing or blank.
- Persist the Laravel configuration-cache preference per site and restore it on page refresh. Unsaved preference changes are protected alongside environment edits.
- Run automatic configuration-cache rebuilding within the environment save using the site's PHP version and active release, without creating terminal-command history. Failed rebuilds return a warning rather than a false success.
- Clarify SSH public-key setup with separate copyable generation and display commands, local-computer guidance, and private-key warnings.

### Upgrade notes

- An additive SQLite migration stores the cache preference and defaults existing sites to off. Older API clients may omit the optional field without resetting it.
- Existing environment files, application names, and command-history entries are not rewritten or deleted by the upgrade. Automatic cache failures do not roll back a saved environment file.

## v0.4.30 — 2026-09-14

### Changed

- Replace the shared script editor with a lazy-loaded CodeMirror editor for reliable selection, scrolling, clipboard operations, undo, and syntax highlighting across environment, deployment, vhost, and file editing.
- Refresh environment files and GitHub repository choices when returning to their pages while preserving unsaved local changes. Show attached databases as unavailable during site creation.
- Include the administrator username in downloaded credential filenames.

### Fixed

- Prevent environment saves before the initial file load succeeds and discard stale refresh responses without overwriting edits.
- Group generated database environment settings while preserving unrelated multiline secrets, newline style, and application configuration.
- Clean up unused dedicated database accounts during explicitly requested site/database deletion while retaining shared, administrative, and externally managed MySQL accounts.
- Keep installer success reporting independent of optional snapshot cleanup and recover first-login instructions securely after interrupted installations.
- Add backend regression coverage for environment merging and database-account cleanup ownership checks.

### Upgrade notes

- No database migration is required. Existing site settings and credentials are preserved. Dedicated-account cleanup runs only when attached databases are explicitly selected for deletion with their site.
- Includes the installer retry and diagnostic improvements from v0.4.29, whose release workflow did not publish binaries.

## v0.4.29 — 2026-09-14

### Fixed

- Reuse an active Ondřej Surý PHP repository for the current Ubuntu release instead of contacting Launchpad to register it again when retrying installation.
- Bound new PHP repository registration to three attempts with two-minute timeouts.
- Identify failed dependency download URLs, curl exit codes, and HTTP statuses while omitting URL credentials and query parameters.
- Show runtime selection flags after an interrupted installation so Node.js and Python choices can be reused. The existing Node.js prompt behavior is preserved.

### Upgrade notes

- Installer-only changes; no database migration is required. These changes improve retry handling and diagnostics but do not eliminate upstream or VPS network outages.

## v0.4.28 — 2026-09-14

### Added

- Add authenticated, one-off MySQL, MariaDB, and PostgreSQL database downloads from the database management page, with preparation status, bounded temporary storage, automatic expiry, and a confirmation prompt for sensitive exports.

### Changed

- Limit direct download artifacts to 256 MB while preserving the existing scheduled-backup behavior for larger databases.
- Coordinate direct downloads with scheduled and manual backups, database deletion, site deletion, and site mutations so conflicting operations return a retryable conflict instead of running concurrently.

### Upgrade notes

- No database migration or existing configuration change is required. Existing backup plans and database operations retain their stored behavior; direct downloads are opt-in and require an authenticated administrator.

## v0.4.27 — 2026-09-04

### Added

- Add first-class Python site support for Django, Flask, FastAPI, and generic WSGI/ASGI applications, including verified Python and uv runtime provisioning, pip or uv dependency installation, managed systemd processes, zero-downtime deployments, per-site commands, cron jobs, database configuration, and runtime health controls.
- Add an advanced per-site Nginx vhost editor with revision checks, validation before activation, managed-configuration restore, and persistence across automatic Nginx regeneration.
- Add runtime package management and expanded application-aware controls throughout site creation, settings, processes, deployment output, and the public documentation.

### Changed

- Pin and verify the release-selected Python uv artifacts alongside the existing Node.js, Bun, Composer, and WP-CLI toolchain inputs, and include Python as an optional installer component.
- Keep application capabilities explicit so PHP-FPM lifecycle work remains limited to PHP-based sites while Node.js and Python services use their own managed runtimes.

### Fixed

- Use the active Django application directory as the Nginx and Let's Encrypt challenge root, including application subdirectories and zero-downtime release layouts.
- Preserve customized Python build and start commands when the settings page hydrates framework fields from an existing site.
- Preserve managed backup-artifact exclusions and keep public SEO and generated sitemap metadata synchronized with the expanded platform support.

### Upgrade notes

- Database changes are additive and applied automatically on startup. Existing PHP, Laravel, WordPress, HTML, and Node.js sites retain their stored application type, deployment settings, Nginx behavior, and process lifecycle.
- Python support is opt-in. Re-run the installer with `--python` or install the runtime from the dashboard before creating a Python site. Existing sites do not receive a custom vhost override unless an operator explicitly saves one.

## v0.4.26 — 2026-09-01

### Changed

- Default new ICANN registrable root domains to **Redirect from www**, while new subdomains, explicit `www.` hostnames, private suffixes, and unknown suffixes default to **No redirect**. Site creation keeps the root-domain choice subtle, hides it for subdomains, and leaves every domain editable later from **Site > Domains**.
- Show the exact additional DNS hostname required by a WWW redirect in the SSL workflow. Let's Encrypt requests include only the hostnames implied by the selected behavior, while certificate cloning offers a direct way to remove an incompatible WWW redirect and retry with the exact hostname.
- Show the detected server address in the SSH password-login hardening confirmation command, including valid bracket formatting for IPv6, instead of relying on a generic server-IP placeholder when an address is available.

### Fixed

- Disable domain-configuration actions while hostname classification is pending, and prevent an empty reusable domain modal from saving a behavior for a missing hostname.
- Keep frontend domain classification aligned with the backend's public-suffix-aware default so multi-label roots such as `example.co.uk` are not mistaken for subdomains.

### Upgrade notes

- No database migration or existing domain configuration is changed. The new default applies only when a newly created site's primary domain or alias omits an explicit WWW behavior.
- A WWW redirect requires DNS and certificate coverage for both the configured hostname and its generated `www.` counterpart. Subdomains now avoid that additional requirement unless an operator explicitly enables the redirect later.

## v0.4.25 — 2026-08-29

### Added

- Add Forge-style WWW behavior controls for primary and alias domains: **Redirect from www** by default for new domains, **Redirect to www**, or **No redirect**. The same configuration modal is available during site creation, when adding an alias, and from each existing domain's action menu.
- Add optional password-based AES-256 OpenPGP encryption for backup artifacts. Operators can enter or generate a password, while Fluxo encrypts the stored plan secret and removes plaintext temporary artifacts before upload.
- Add guided SSH access hardening in **Settings > SSH Keys**. Fluxo reports the effective policy, requires confirmed key and recovery access, validates remote IPv4, remote IPv6, and local contexts for `fluxo` and `root`, and can install or restore its managed key-only OpenSSH policy.

### Changed

- Request and validate certificates against every hostname implied by a domain's WWW behavior. Fluxo blocks a behavior change when the active certificate would not cover the new hostname set, preventing Cloudflare Full (strict) origin failures.
- Generate Laravel deployment scripts without `artisan migrate --force` when no database was connected during site creation. Fluxo also corrects the recognized untouched managed default for existing database-free Laravel sites while preserving customized scripts byte for byte.
- Clear site-creation and certificate-modal form state when the operator explicitly closes or cancels the flow, while preventing accidental backdrop or Escape dismissal during site creation.

### Fixed

- Prevent privileged human-key and deploy-key operations from being redirected through a symlink or concurrent replacement of `/home/fluxo/.ssh`. Generation, rotation, removal, and bootstrap initialization now use a descriptor-pinned, no-follow filesystem boundary, and deploy-key mutations are serialized.
- Reconcile the live SSH daemon after a failed hardening reload, serialize hardening changes with final-key deletion, and stop restricted or malformed legacy key entries from incorrectly satisfying the last-key safety check.
- Preserve an alias's WWW behavior when Nginx regeneration fails during deletion and Fluxo restores the database record.

### Upgrade notes

- Database columns for WWW behavior and backup encryption are added automatically on startup. Existing domains retain **No redirect** and existing backup plans remain unencrypted. Customized deployment scripts are unchanged; only the recognized untouched Laravel managed default loses `artisan migrate --force` when the site has no attached database.
- SSH password login is not disabled automatically. Add and verify a `fluxo` login key, retain provider-console recovery access, and opt in from **Settings > SSH Keys**.
- When changing WWW behavior on a domain with an incompatible active certificate, deactivate that domain's SSL assignment, save the behavior, then issue or assign a certificate covering every required hostname.

## v0.4.24 — 2026-08-28

### Fixed

- Prevent Node.js toolchain installation from appearing to stall while checking system prerequisites. Fluxo now skips package-manager work when `ca-certificates`, `xz-utils`, and `unzip` are already installed, reports missing packages explicitly, and emits progress heartbeats during longer package operations.
- Run required APT operations noninteractively with automatic `needrestart` handling, no pseudo-terminal, and a finite package-lock wait. Timed-out environment commands now terminate their complete process group and retain captured diagnostics instead of potentially leaving a child `dpkg` process behind.

### Upgrade notes

- No database migration or service configuration change is required. The fix applies to Node.js toolchain installation during fresh provisioning, upgrades using `--node`, and repairs started from **Runtime > Node.js**.

## v0.4.23 — 2026-08-28

### Fixed

- Check for the volatile OpenSSH `/run/sshd` runtime directory before evaluating the effective SSH configuration during installation. When it is absent, Fluxo recreates it as `root:root` with mode `0755`; symlinks, non-directory paths, and genuine SSH configuration errors still stop installation safely.

### Changed

- Use the Laravel brand mark for Laravel application icons in the dashboard and list Laravel among the supported technologies on the public site.

### Upgrade notes

- No database migration or service configuration change is required. Existing installations are unaffected; the installer repair applies only when `/run/sshd` is missing on a new or minimal Ubuntu VPS.

## v0.4.22 — 2026-08-26

### Fixed

- Keep long background-process commands inside the site Overview content column so managed Queue Worker arguments no longer push the Laravel feature sidebar off-screen or create a page-level horizontal scrollbar.

### Changed

- Simplify the Laravel Features Queue Worker control to a single enable/disable toggle. Existing managed workers are now reconfigured from **Site > Processes > Background Processes** through the worker row's action menu.
- Reuse the same Queue Worker configuration form for initial activation and later edits so connection, concurrency, retry, timeout, memory, and lifecycle settings remain consistent in both flows.

### Upgrade notes

- No database migration, service configuration change, or application environment rewrite is required. This release changes only the dashboard presentation and configuration entry point.

## v0.4.21 — 2026-08-26

### Added

- Add a managed Laravel Queue Worker control with connection, queue priority, process count, retry, timeout, backoff, memory, lifetime, and maintenance-mode settings. Fluxo validates the selected queue backend before replacing an existing worker, updates `QUEUE_CONNECTION`, supervises every configured process with systemd, and runs Laravel's graceful `queue:restart` hook after deployments and release rollbacks.

### Changed

- Make Queue Worker and Horizon mutually exclusive. Horizon validates its package, Redis configuration, and Redis connectivity before replacing an active managed worker; if Horizon cannot start, Fluxo restores the worker with its preserved configuration.
- Launch the configured number of systemd processes for multi-instance daemons, reconcile legacy single-unit records during upgrade, report partially running groups as degraded, and cap custom process groups at 64 instances.
- Classify existing Fluxo-managed Node.js daemons during database upgrades so they retain managed-process protections.
- Preserve user-authored `queue:restart` commands in legacy deployment scripts by removing only hooks carrying Fluxo's ownership marker.

### Upgrade notes

- Fluxo applies the new queue-worker configuration table and managed-process metadata automatically when the service starts; no manual SQL or configuration migration is required.
- Existing custom queue daemons are not adopted, stopped, or removed. Enabling the managed Queue Worker updates `QUEUE_CONNECTION`; enabling Horizon requires the application to already use a working Redis queue connection.
- Existing background processes configured with more than one process now launch the full configured count. Review unusually high legacy process counts before upgrading; Fluxo limits new custom process groups to 64 and managed Queue Workers to 16.

## v0.4.20 — 2026-08-24

### Fixed

- Quote database passwords whenever Laravel, PHP, or Node.js environment files are generated or merged from `.env.example`, preserving characters such as `#`, `$`, spaces, and backslashes. Generated database settings now consistently use the managed `127.0.0.1` TCP account.
- Keep environment and deployment-script editor text, selections, line numbers, carets, and syntax highlighting aligned while scrolling. Native scrollbar space is reserved so the final line no longer renders beneath a scrollbar.
- Prevent selected syntax-highlighted text from being painted twice by the browser.
- Keep deployment-output modals full width with responsive commit and branch labels. Long labels are visually truncated with their complete values available on hover, while long log lines stay inside the modal.

### Upgrade notes

- No database migration or configuration change is required.
- Existing site `.env` files are not rewritten during upgrade. Passwords already affected by dotenv comment parsing should remain single-quoted.

Earlier releases are documented on the [GitHub releases page](https://github.com/FabioTECH1/fluxo/releases).
