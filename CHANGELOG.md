# Release notes

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
