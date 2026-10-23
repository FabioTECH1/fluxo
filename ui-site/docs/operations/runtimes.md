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

### Consumers and draining {#managed-consumers-and-draining}

Fluxo can drain supported managed and custom Queue Workers, Horizon and qualified Octane services automatically. Fluxo-generated Nightwatch agents also participate in automatic stop/restart, with exact command, interpreter, effective service and owned-listener checks. Nightwatch is a telemetry agent: monitoring pauses while it is stopped and buffered telemetry may be lost. Runtime checks verify its local process/listener, not remote authentication or event delivery. Queue and Horizon workers stop taking further work and finish work already in progress. New Redis jobs remain queued; the update does **not** wait for the queue to become empty. A reservation already in progress can race with shutdown. Qualified Octane services stop admitting new application work and drain existing requests and supported tasks.

Before changing packages, Fluxo prevents affected consumers from restarting and waits for their complete systemd process groups, including nested groups and Horizon descendants left by `fast_termination`, to become empty. Local process signals avoid broadcasting restart commands through shared Redis/cache. Fluxo does not force-kill work when its observation window expires; configured application job/request deadlines and native server shutdown deadlines still apply. A timeout retains recovery state instead of permitting package mutation. Originally stopped services remain stopped.

Commands, interpreter identities and shutdown support must pass inspection. Custom service overrides, uninitialized Laravel queue signal handlers and unsupported configurations require operator attention. Supported Fluxo-managed Laravel Scheduler entries stay configured and pause automatically for the actual PHP interpreter in their recorded command. New ticks are skipped while current tasks and background children finish naturally; missed ticks are not replayed. Fluxo does not send Scheduler tasks a stop signal. Admission resumes only after the recorded package goal and affected services pass verification. A failed or interrupted attempt keeps admission closed through Repair or Rollback.

### Custom queue workers

A custom background-process entry does not need to be converted to the Queue
Worker feature or removed. Maintenance can qualify a direct `phpX.Y artisan
queue:work` command, or the same command with `/usr/bin/phpX.Y`, running as
`fluxo` in its PHP/Laravel site's current application directory. An explicit
asynchronous connection is optional; omitting it preserves the application's
default. Optional arguments use the feature's existing validated forms:
`--queue=high,default`, `--sleep=N` (0–60), `--tries=N` (0–100), `--timeout=N`
(1–86400), `--backoff=N` (0–86400), `--memory=N` (32–4096), `--max-time=N`
(0–86400), and `--force`. Duplicate options, wrappers, generic `php`, unknown
arguments and custom service lifecycle hooks remain unsupported.

An empty `managed_kind` remains empty. The update review records a separate
maintenance protocol only after command and site qualification, followed by
the full effective systemd/interpreter/process checks. Active workers must
show initialized Laravel signal handlers (including SIGCONT as well as SIGTERM,
since PHP itself can install a default SIGTERM wrapper). Each instance uses the
same durable maintenance barrier and recovery path as managed queues. Fluxo preserves every
daemon record, command, queue setting, feature config and enabled/disabled state;
it restarts only instances which were running beforehand. Separate workers on
the same site are never merged or assumed to replace one another.

**Stopped** in Background Processes describes current activity, not maintenance
qualification or whether systemd will start the process after reboot. A custom
worker remains custom even when it qualifies for PHP maintenance. Enabling the
Queue Worker feature creates a separate managed process; it does not adopt or
remove earlier entries. Two rows on the same site are not proof of a duplicate:
an omitted connection uses the application's default, which may differ from an
explicit `redis` connection. Keep both when they are intentional. Do not rename
rows or edit `managed_kind` in SQLite to bypass qualification.

### Other custom Artisan commands

Other direct site-bound commands, such as `php8.4 artisan reports:generate` or
`/usr/bin/php8.4 artisan reverb:start --port=8080`, can use a separate
`artisan_natural` maintenance protocol. They retain their custom ownership and
all process settings. This uses the same explicit interpreter, site directory,
package, effective unit and process checks as other PHP consumers. Arguments
must be literal unquoted tokens; wrappers, PHP interpreter flags, shell/systemd
expansion and custom lifecycle hooks remain unsupported.

Fluxo holds restarts and waits for the complete process group and recorded
processes to exit naturally. It sends these commands no stop signal and never
infers shutdown behavior from an Artisan command name. For a command that runs
indefinitely, use the application's documented graceful shutdown; a timeout
keeps PHP packages unchanged and retains maintenance holds for Repair. There is
no need to delete the process entry. Unknown PHP processes outside the verified
consumers still block installation.

After package verification, originally running instances are resumed once;
originally stopped instances stay stopped and enabled state is preserved. A
short-lived resumed command can finish successfully rather than remaining
active. Dispatch intent, execution identity and verified successful completion
are retained with the maintenance generation, beyond temporary barrier cleanup,
so Repair does not replay a completed command. The existing background-service
policy is restored; this does not certify the command's application results.
If execution evidence is lost, fails or crosses a reboot, automatic recovery
requires operator review rather than guessing that a repeated command is safe.

### Scheduler coordination and panel upgrades

Automatic Scheduler handling needs systemd 250 or newer with cgroup v2, normally available on Ubuntu 24.04 and newer. Existing exact Fluxo-generated minute entries migrate to a trusted launcher when the panel starts. They continue normal scheduling. For the qualified Ubuntu 24.04 cron `3.0pl1-184ubuntu2` and standard PAM/unit configuration, Fluxo verifies replacement-trigger receipts and natural completion of earlier cron work without restarting cron or the VPS. The installer also establishes the supported old manual-Scheduler boundary before stopping the panel. A busy or unverified migration blocks PHP installation while ordinary scheduling continues; check for updates remains available. Newly created managed entries are tracked immediately. Custom cron commands/schedules and unsupported hosts retain their existing behavior and require a manual maintenance plan. External cron, SSH commands and other process managers are not held by this workflow: coordinate their pause before maintenance so they cannot start competing work. Fluxo defers its own conflicting deployments while PHP recovery remains pending.

If installation reports **Earlier manual Scheduler runs need verification**, use
`sudo /usr/local/bin/fluxo scheduler-verify-panel` from SSH with a release that
provides this command, then open a fresh PHP update review. This performs the
same exclusive-admission and natural panel-work drain used by the installer,
without reinstalling packages or restarting services. It records only the manual
Scheduler boundary; cron reload receipts, earlier cron jobs, backend identity,
and PHP recovery holds still have to pass their own checks. See [CLI recovery
and diagnostics](/reference/cli#scheduler-verification-and-diagnostics) for
custom data directories, unsupported panels, and older-release repair guidance.

Cronie is supported for automatic legacy migration on Ubuntu 24.04 with the qualified `1.7.1-1` amd64 package and standard service/PAM configuration. Fluxo preserves the existing provider. It does not replace Cronie with cron, restart the engine, or require a reboot. Other package revisions, custom units/PAM chains and architectures without runtime acceptance retain ordinary scheduling but can block automatic legacy maintenance. New tracked launchers use the shared systemd admission path.

An earlier unsuccessful provider check can recover automatically after qualification succeeds. A recorded backend identity change remains blocked until its earlier execution domain is accounted for; a currently idle replacement daemon is not proof that earlier jobs finished. **Processes > Scheduler** shows engine status separately from each file's configuration status. Neither badge claims the Laravel application completed its work successfully.

The root-owned Scheduler journal and dispatcher are stored independently at `/var/lib/fluxo-scheduler`, with an initialization anchor alongside that directory. Each invocation runs as `fluxo` in its own systemd cgroup, including background descendants. Root launches only this fixed managed command; application code never runs as root. Manual **Run now** uses the same gate for managed entries. Verified plain/custom crons retain their ordinary execution path.

Backend migration evidence is retained separately in `backend-proofs.json` and its initialization files within that directory, so already-running older helpers cannot discard it. Back up the whole directory and its external initialization anchor together. Do not remove a journal, proof or marker to bypass a blocker. Once this evidence is established, restoring a panel that lacks scheduler support level 3 is refused.

Installation inventories scheduling providers before APT changes. Upgrades preserve the provider and its enabled/disabled/masked state. A genuinely fresh server with no provider or pre-existing schedules receives Ubuntu cron; if installing a provider would activate existing work, installation stops for operator review instead.

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

Rollback remains available after a successful update so you can respond to an application problem discovered later. The dashboard offers only the latest update attempt per PHP branch; it does not build a list of older rollback targets.

Recovery archives live under the Fluxo data directory. After a newer forward update on the same branch passes package and service verification, Fluxo removes recorded package payloads from older completed updates. The latest package sets remain, together with anything required by unfinished recovery or retained installer database snapshots. If the newer update was allowed without rollback files, Fluxo also keeps the preceding complete rollback set; that older set does not become an eligible dashboard rollback target.

Cleanup preserves manifests, configuration backups, operation history and repository recovery catalogs. It defers while maintenance is active or recovery is unresolved, and leaves unknown files or uncertain evidence untouched. Cleanup errors are logged separately from update success. Bounded cleanup runs after successful maintenance and retries after startup and hourly; several passes may be needed. Each operation's unique archive set remains limited to 2 GiB, with free-space and inode checks. Monitor disk usage: protected evidence and metadata can still accumulate. Local retention is not an off-server snapshot or a guarantee that packages remain recoverable forever.

Updates may interrupt requests during installation and activation. Package installation can succeed while runtime verification fails. Verification checks package revisions, process adoption, FPM request handling and qualified service transport/process health, not every application route. **Retry service verification** remains only for legacy reports and never reinstalls packages; use reviewed **Repair** for the new recovery workflow.

There is no automatic or scheduled installation, server reboot, or automatic rollback. Persistent service holds and admission blocks survive interrupted maintenance and require recovery; do not remove them manually to make the panel appear ready. Retain a tested server recovery path: site backups do not restore operating-system packages. See [Troubleshooting](../reference/troubleshooting#php-patch-updates).

Existing sites keep their selected PHP version when the default changes. PHP removal is a server-wide package operation, so manually migrate and test every dependent site before removing a version.

Site commands and managed deployment scripts use version-specific PHP and Composer variables, avoiding accidental use of the wrong CLI version.

## Node.js toolchain

### Node.js updates

Use **Check for updates** and **Update** on Runtime > Node.js to maintain the currently installed, supported LTS major. Node.js 24 stays on 24.x; changing major versions is a separate operation. Checks run daily on installed production servers and never install anything automatically. Opening the page reads the stored check. A failed or stale check does not authorize an update or mean that no update exists.

Updates require the active Node executable and its npm/npx links to belong to Fluxo. An external Node installation is not eligible merely because Fluxo manages Bun or Corepack. Initial verification compares the installed tree with an authenticated official release; modified global packages, shadowed commands, unsafe permissions or changed release files can require manual repair.

The confirmation shows exact Node.js/npm versions and affected applications. Fluxo verifies a signed official checksum manifest against its bundled reviewed public keyring, downloads and validates the exact archive, and retains the previous runtime. Unknown or expired signing keys and a stale support policy require a trusted Fluxo release update. The release index cannot prove that an upstream server has not replayed an older valid release; availability describes the latest eligible release found by the successful check.

Only Node.js and its bundled npm/npx change. Corepack, pnpm, Yarn and Bun keep their versions and must still run afterward. Application files, dependencies, lockfiles and build output are not rebuilt. Static files keep serving, while subsequent builds—including PHP asset builds using Node—use the selected release. Compatible existing managed Node installations and approved runtime selections are preserved by toolchain repair and the Fluxo installer; build pins seed a new installation rather than forcing an existing runtime to another major.

Allow for an interruption while affected server applications stop and restart. Stopped applications stay stopped. Fluxo supports its generated single-instance Node application services when it can identify their runtime processes and assigned listener. A Bun package-manager selection alone does not prove that a service uses only Bun. Generated Bun applications also stop and restart: their code can launch Node later, even when no Node child exists during review. Fluxo verifies the same root-managed Bun executable and version, drains the complete service cgroup, and checks any Node children against the selected Node release. Stopped Bun applications stay stopped, including after reboot. Bun itself is not updated. Custom commands, service overrides, unqualified background processes, custom cron entries, unqualified Scheduler dispatchers and observed Node processes outside supported service groups block automatic maintenance. Qualified managed Scheduler work shares admission with the updater, including detached descendants; refresh an old dispatcher with the current installer first. External SSH, cron or process-manager actions must cooperate with maintenance; Fluxo cannot prevent arbitrary root actions.

Accepted updates run in a separate systemd worker with a persistent operation history. Closing the browser or restarting the panel does not cancel the worker. Fluxo coordinates deployments, site commands and runtime changes through the existing host lifecycle lock. Persistent service overrides prevent automatic restart or boot activation during interrupted maintenance. Fluxo does not force-kill an application to finish an update; an application that cannot stop safely remains held for recovery.

Verification requires a stable managed process group, its ownership of the assigned TCP listener, and adoption of the selected Node binary. This checks process and connection health, not application business behavior. Check important routes and jobs after maintenance. Neither same-major updates nor process health checks guarantee compatibility with every native dependency or application engine requirement.

If activation fails, Fluxo attempts to restore and verify the retained previous release. A verified rollback is reported as an update failure with restoration, not an update success. An incomplete rollback or interruption keeps conflicting changes paused and exposes **Review repair** and **Review rollback**. Repair completes the recorded target; rollback restores the recorded previous release. Both require a fresh review and explicit approval, and refuse changed application or runtime evidence. Rollback remains available after success until a later update supersedes that recovery point. This initial implementation retains referenced release trees without automatic cleanup.

Recovery can review a known app that has failed, is starting/stopping, or is running without its assigned listener. The review shows its current state and preserves whether it was running or stopped before the original update. Fluxo safely drains its identified processes, activates the chosen retained release and verifies health before releasing maintenance holds. Changed service definitions, unknown Node executables and ports owned by another process still block recovery. A process that cannot drain safely keeps the hold and reports the stopping failure.

The scheduled-work check includes every Fluxo cron, including global jobs and records without a valid site. Arbitrary commands and wrappers block maintenance with their cron ID and name; only qualified managed Scheduler jobs with verified execution admission can continue. Remove unsupported jobs before reviewing an update. The same inventory is checked again before execution and during recovery.

Every recorded background process is checked, including stopped, global and orphaned records. Alongside generated Node/Bun services, positively qualified PHP consumers can participate: managed Queue/Horizon/Nightwatch/native Octane, independent direct custom queue workers, and strict site-bound literal Artisan commands. Both custom and managed queue entries remain independent; no adoption, deletion or feature change occurs. The review lists exact commands, PHP versions, instances, protocols and original running/stopped/enabled state. PHP interpreters, packages and FPM remain unchanged; only these background consumers pause because they can launch Node later. Unqualified Python/PHP/wrapper/unknown services still block with their daemon ID, name and kind. Stopping alone cannot establish a safe lifecycle contract.

Qualified Queue/Horizon workers use their verified graceful shutdown. Nightwatch monitoring pauses and buffered telemetry may be lost. Supported Octane services use their verified native master shutdown and deadlines; unsupported native forms remain blocked. Other direct Artisan commands receive **no inferred stop signal**: Fluxo holds restarts and waits for their complete execution domain to exit naturally, including children launched during shutdown. A continuously running command needs its application's documented graceful exit. After five minutes, Fluxo retains recovery holds without force-killing work or switching Node. Keep the process entry and review recovery after the work finishes.

Originally running natural Artisan commands resume once after activation; stopped instances remain stopped. Durable boot/start identity and successful completion evidence survive repair, rollback and hold cleanup. A completed command is never replayed by recovery. Unconfirmed, failed, replaced or rebooted arbitrary invocations require application-outcome review and keep recovery pending. Restoring Node alone does not justify a successful rollback when a consumer outcome is uncertain. This does not take FPM down or certify arbitrary web requests or external commands which launch Node; those remain outside the coordinated background-consumer boundary.

Each new review uses the current consumer policy. Older reviews must be renewed. Recovery of an older generation that exempted Bun captures a fresh managed Bun receipt and full service-coordination plan, saves that evidence separately from immutable history before mutation, and preserves the original running/stopped intent. The Bun app restart is disclosed before approval; unknown or changed Bun executables fail closed.

Recovery requires access to official Node.js downloads. Both original exact releases are reauthenticated with the current bundled keys, and their archive checksums and complete trees must still match the recorded versions. Refreshed proofs are saved separately from the original history. Adding keys or updating keybox metadata does not discard a retained recovery point; removed, revoked or expired signers are rejected unless the official release is reauthenticated by a currently trusted signature for the same archive. Neither trust metadata nor a release is silently substituted. Review and worker preflight verify both activation paths before stopping applications; authentication or proof-save failure leaves a previously completed generation and its running apps unchanged. An existing unresolved generation retains its earlier holds. Normal panel upgrades may carry new trust data after maintenance has completed; pending operations and barriers still block the installer.

Runtime rollback does not restore application files, databases, Redis or jobs. Older runtimes may restore vulnerabilities. Interrupted or manually changed systems can still require operator repair. Use a current installer: older Fluxo binaries which cannot preserve runtime approvals and journals are rejected by the current installer after this feature has been used.

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
