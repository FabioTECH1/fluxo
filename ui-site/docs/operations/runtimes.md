---
title: Runtimes
description: Install, inspect, configure, restart, and remove Fluxo server runtimes.
---

# Runtimes

The Runtime section controls server-wide software used by hosted sites. Runtime changes can affect every application using that service, so schedule restarts and removals carefully.

## PHP

### PHP package repository

New installations and normal installer upgrades prepare Ondřej Surý’s official Ubuntu repository at `https://packages.sury.org/php/` automatically. The internal review validates a private proposed APT configuration and preserves authenticated current packages before backing up and disabling recognized legacy PHP PPA entries. No separate switch confirmation is needed. Custom conflicts stop setup with an actionable error; **Runtime > PHP > Review repair** is available for exceptional or unfinished setups.

Repository setup does not install PHP packages, change PHP defaults, or restart applications. Run **Check for updates** afterwards, then review and confirm each branch update separately. Keeping both repositories enabled does not bypass a failing PPA: strict APT refresh still fails if either active source is unavailable.

The managed switch supports Ubuntu 22.04, 24.04 and 26.04 on amd64/arm64. It preserves unrelated repositories and priorities. Overlapping custom PHP pins require manual reconciliation so their restrictions cannot be silently overridden. A separate managed priority rule keeps existing Ubuntu PHP branches on the Ubuntu vendor family (Ubuntu, UbuntuESM and UbuntuESMApps) at priority 990; it does not force downgrades. Ambiguous mixed stanzas, unexpected signing policies, unsafe source files and unsupported layouts require manual reconciliation. Shared source files retain their ownership and permissions. Interrupted publication keeps a durable blocker; **Resume repository setup** reconciles the saved journal and refuses to overwrite operator edits.

Some PPA extensions have higher revisions than the new repository. Fluxo preserves an independent extension unchanged only when its original authenticated package archive has been retained and verified. The review identifies these extensions; they are included unchanged in both recovery sets. Core PHP still requires fresh active-repository metadata, and a later newer extension candidate follows the normal update path. Missing provenance, incompatible dependencies and actual downgrade requirements remain blockers. Retention does not establish an extension’s current security status.

A completed repository switch remains in place during PHP package repair or rollback. Existing valid retained recovery generations receive an exact, recorded policy-transition authorization; unrelated repository or pin changes still require reconciliation. Package rollback availability remains subject to complete verified archives and the existing acknowledgement rules.


The PHP page lists installed PHP-FPM versions and service state. You can:

- Install an available PHP version
- Set the default PHP version used for new sites
- Set the server CLI default
- Start, stop, or restart a version's PHP-FPM service
- Edit supported `php.ini` limits
- Remove an unused version

Each installed version has the existing three-dot action menu: **Update**, **Check for updates**, **Start/Stop FPM**, and **Remove**. **Restart FPM** remains directly accessible while FPM is running. Update is enabled only for a fresh, eligible package plan. It patches packages within that branch; it does not switch a site's PHP branch or change the site/CLI defaults.

On installed production servers, Fluxo checks all installed PHP branches once per day using one repository refresh. Opening the PHP page reads the stored result and does not refresh repositories. **Check for updates** runs an on-demand background check and reports the result through a toast. A repository failure is not treated as “no updates.” Checking only discovers packages; it does not inspect or stop application services. Selecting **Update** performs a separate installation review. New packages remain available if that review finds a service blocker; the confirmation lists what to resolve and disables installation until a new review passes.

**Update** opens the normal confirmation modal with affected sites and deduplicated service categories, plus a short downtime notice. Detailed runtime notes remain in the operation report. Applying runs in a separate systemd-supervised job and continues if the browser or panel restarts. Toasts show progress; available updates and unresolved failures also have persistent notices. Dismissing an availability notice applies to that candidate package family; a newer revision can notify again.

Only existing packages belonging to the selected branch may be upgraded. Fluxo respects package holds and pinning, refuses removals/downgrades/new dependencies and unfamiliar repository transitions, and blocks unsupported/custom runtime configurations. Independently packaged extensions have their own revision numbers. Repository candidates may lag an upstream release; “no updates” is not a security certification. Ubuntu backported fixes may leave the upstream PHP version unchanged.

### Managed consumers and draining

Fluxo can drain supported managed Queue Workers, Horizon and qualified Octane services automatically. Fluxo-generated Nightwatch agents also participate in automatic stop/restart, with exact command, interpreter, effective service and owned-listener checks. Nightwatch is a telemetry agent: monitoring pauses while it is stopped and buffered telemetry may be lost. Runtime checks verify its local process/listener, not remote authentication or event delivery. Queue and Horizon workers stop taking further work and finish work already in progress. New Redis jobs remain queued; the update does **not** wait for the queue to become empty. A reservation already in progress can race with shutdown. Qualified Octane services stop admitting new application work and drain existing requests and supported tasks.

Before changing packages, Fluxo prevents affected consumers from restarting and waits for their complete systemd process groups, including nested groups and Horizon descendants left by `fast_termination`, to become empty. Local process signals avoid broadcasting restart commands through shared Redis/cache. Fluxo does not force-kill work when its observation window expires; configured application job/request deadlines and native server shutdown deadlines still apply. A timeout retains recovery state instead of permitting package mutation. Originally stopped services remain stopped.

Managed commands, interpreter identities and shutdown support must pass inspection. Custom service overrides, unregistered queue-worker SIGTERM handlers and unsupported configurations require operator attention. Supported Fluxo-managed Laravel Scheduler entries pause automatically for the actual PHP interpreter in their recorded command. New ticks are skipped while current tasks and background children finish naturally; missed ticks are not replayed. Fluxo does not send Scheduler tasks a stop signal. Admission resumes only after the recorded package goal and affected services pass verification. A failed or interrupted attempt keeps admission closed through Repair or Rollback.

Automatic Scheduler handling needs systemd 250 or newer with cgroup v2, normally available on Ubuntu 24.04 and newer. Existing exact Fluxo-generated minute entries migrate to a trusted launcher when the panel starts. They continue normal scheduling. For the qualified Ubuntu 24.04 cron `3.0pl1-184ubuntu2` and standard PAM/unit configuration, Fluxo verifies replacement-trigger receipts and natural completion of earlier cron work without restarting cron or the VPS. The installer also establishes the supported old manual-Scheduler boundary before stopping the panel. A busy or unverified migration blocks PHP installation while ordinary scheduling continues; check for updates remains available. Newly created managed entries are tracked immediately. Custom cron commands/schedules and unsupported hosts retain their existing behavior and require a manual maintenance plan. Merely stopping a custom daemon does not remove its unsupported configuration; disable/remove the process entry through Fluxo before reviewing again. External cron, SSH commands and other process managers are not held by this workflow: coordinate their pause before maintenance so they cannot start competing work. Fluxo defers its own conflicting deployments while PHP recovery remains pending.


### Scheduler coordination and panel upgrades

The root-owned Scheduler journal and dispatcher are stored independently at `/var/lib/fluxo-scheduler`, with an initialization anchor alongside that directory. Each invocation runs as `fluxo` in its own systemd cgroup, including background descendants. Root launches only this fixed managed command; application code never runs as root. Manual **Run now** uses the same gate for managed entries. Verified plain/custom crons retain their ordinary execution path.

Panel upgrades acquire the PHP lifecycle lease before stopping the old panel, then hold it while taking/restoring their database and cron snapshots and checking the new panel. An interrupted installer can leave a durable upgrade marker; retry the installer to reconcile it. Scheduler history is preserved rather than rewound with the panel database. Automatic restoration of a panel binary that cannot understand established Scheduler coordination is refused, and the installer retains its recovery snapshot. This restriction concerns panel binaries, not the separately reviewed PHP package rollback.


### Octane qualification

Automatic maintenance requires the Fluxo-managed launcher and the qualified application/native combination. A version string alone is insufficient; Fluxo checks the Composer reference, selected source hashes and native runtime identity against its [bundled compatibility manifest](https://github.com/FabioTECH1/fluxo/blob/main/internal/services/octaneruntime/compatibility.json).

| Backend | Qualified scope |
| --- | --- |
| Swoole | Laravel Octane 2.22.0 at Composer reference `c49ff458e94bf14b382f6414c6b9ddb39ac82b28`, matching source hashes, and packaged Swoole 6.2.3. OpenSwoole, alternate launchers and unsupported custom launch options are excluded. Existing supported request/task deadlines remain effective. |
| RoadRunner | Laravel Octane 2.22.0 with the qualified source hashes and official RoadRunner 2025.1.15 Linux amd64 binary, verified by size and SHA-256, with an empty `.rr.yaml` and the normal qualified worker invocation. Other architectures/builds and custom configurations require manual maintenance. |
| FrankenPHP | Its embedded PHP interpreter is not replaced by the selected APT PHP branch. Active FrankenPHP consumers are not supported by this automatic workflow and block it. |

The Swoole extension itself must remain unchanged during this PHP patch: extension package-version changes and rebuild changes are blocked. Fluxo also binds the actual native library inside retained package archives to the inspected library, so unchanged metadata cannot authorize a different native payload.

Legacy Octane instances need a controlled full restart after traffic drains, using the managed integration installed through **Enable**. A worker reload is insufficient. Fluxo does not silently migrate a running legacy instance. Failed enablement can retain a protected cleanup state; resolve it before editing or deploying that site. See [Troubleshooting](../reference/troubleshooting#octane-maintenance-and-failed-enablement).

### Recovery and retained packages

Before consumers stop, Fluxo normally downloads and verifies the complete authenticated previous and target package sets. If verified previous packages are unavailable, proceeding requires the explicit **without package rollback** acknowledgement. That acknowledgement does not bypass target-archive verification, and downloads can still fail before draining starts.

Use **View report** for the current operation. New operations offer reviewed **Repair** and, when available, **Roll back PHP packages**. Repair can finish service activation or replay only the exact recorded package set when package state requires it. Rollback is a separate manual confirmation that restores the retained previous package revisions and may reintroduce vulnerabilities fixed by the update. Neither action is an automatic fallback. Current configuration is preserved; these actions do not restore configuration archives, application files, databases, Redis or completed jobs.

Recovery archives live under the Fluxo data directory. Each operation's combined unique archive set is limited to 2 GiB, with additional free-space and inode checks. There is no automatic archive garbage collection; monitor accumulated storage. Retention is local evidence for recovery, not an off-server snapshot or a guarantee that packages will remain recoverable forever.

Updates may interrupt requests during installation and activation. Package installation can succeed while runtime verification fails. Verification checks package revisions, process adoption, FPM request handling and qualified service transport/process health, not every application route. **Retry service verification** remains only for legacy reports and never reinstalls packages; use reviewed **Repair** for the new recovery workflow.

There is no automatic or scheduled installation, server reboot, or automatic rollback. Persistent service holds and admission blocks survive interrupted maintenance and require recovery; do not remove them manually to make the panel appear ready. Retain a tested server recovery path: site backups do not restore operating-system packages. See [Troubleshooting](../reference/troubleshooting#php-patch-updates).

Existing sites keep their selected PHP version when the default changes. PHP removal is a server-wide package operation, so manually migrate and test every dependent site before removing a version.

Site commands and managed deployment scripts use version-specific PHP and Composer variables, avoiding accidental use of the wrong CLI version.

## Node.js toolchain

The Node.js page presents one grouped toolchain, not six unrelated services. It reports:

- Node.js and its binary path
- npm
- pnpm
- Yarn
- Corepack
- Bun
- Missing components and the minimum supported Node.js version

Fluxo requires Node.js `22.13.0` or newer and all listed package managers before Node site creation is enabled.

**Install toolchain** installs a compatible Node.js LTS release and the package-manager tools. **Repair toolchain** fills missing or incompatible components for servers that already had Node.js. Legacy Fluxo installations can use Repair to add pnpm, Yarn, Corepack, and Bun without recreating sites. Fluxo checks the required Ubuntu packages first and avoids an APT refresh when they are already installed; when packages are missing, installation is noninteractive and uses bounded lock and command waits.

Release builds bind architecture-specific Node.js and Bun hashes plus Corepack, pnpm, and Yarn package integrity values into the Fluxo binary. Runtime installation verifies those values before activation and durably snapshots the previous Fluxo-managed binaries, links, state, and Corepack selection. Ordinary failures restore immediately; after a process or machine interruption, Fluxo restores the snapshot on startup before serving the dashboard.

Fluxo reserves `/opt/fluxo` for root-owned runtime files. Its directories and executables remain readable and executable by the `fluxo` account but must not be recursively assigned to that account. User-owned package-manager state belongs under `/home/fluxo`. Installation verifies both ownership boundaries and runs every advertised tool as `fluxo` before activation.

**Restart applications** restarts Node.js and Bun application processes managed by Fluxo; it does not restart a single global Node daemon because Node itself is an executable, not a server service.

**Remove toolchain** is available only when Fluxo owns the installation. It refuses removal while Node.js sites still depend on it and does not delete externally managed installations, site files, or package caches.

Fluxo does not install or operate PM2, Forever, or Nodemon. Applications using an external process manager do not participate in Fluxo's restart, health-check, or upgrade rollback workflow.

Installed package managers consume disk space but no idle RAM. RAM is used when their commands or hosted applications are running.

## Python application support

The Python page groups the components required to host Python sites:

- Ubuntu's system `python3` and its version
- `venv` and `ensurepip` support for isolated site environments
- pip availability
- Fluxo's release-pinned and checksum-verified uv binary

Fluxo requires Python 3.10 or newer. **Install support** installs supported Ubuntu packages and Fluxo's verified uv release. **Repair support** fills missing components. Existing system Python packages are not replaced with an independently managed interpreter.

Every Python site has its own `.venv`; packages are not installed globally. **Restart applications** restarts all Fluxo-managed Python site processes. **Remove managed tools** is refused while Python sites exist and removes only Fluxo-managed uv files. Ubuntu's system Python, application files, and site virtual environments remain untouched.

The Python interpreter and command-line tools use storage but no idle RAM. Each running Gunicorn, Uvicorn, or custom application process uses RAM according to the application and worker configuration. See [Python sites](../sites/python.md) for presets, dependency handling, and deployments.

## Nginx

The Nginx runtime page shows version, service status, configuration paths, and restart controls. Fluxo validates generated configuration before reloading it during site, domain, or SSL changes.

If Nginx fails to restart, inspect its error log and run `sudo nginx -t` over SSH before changing more configuration.

## Database engines

The database runtime page reports MariaDB/MySQL, PostgreSQL, and Redis status and versions. Missing supported engines can be installed from the dashboard. Installed services can be started, stopped, or restarted.

Stopping an engine immediately affects every application using it. Database engine removal is intentionally different from deleting an individual application database.
