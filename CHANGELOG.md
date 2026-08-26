# Release notes

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
