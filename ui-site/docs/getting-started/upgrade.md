---
title: Upgrade Fluxo
description: Upgrade Fluxo safely and verify the installed version.
---

# Upgrade Fluxo

When a newer release is available, Fluxo shows an informational banner after an administrator signs in. The banner identifies the installed and latest versions and links to the GitHub release notes. Dismissing it hides that specific release; a later release will appear again.

The Fluxo release check is informational; panel upgrades run through the SSH installer. PHP and Node.js runtime updates have separate reviewed dashboard workflows. The asynchronous panel version check is cached and does not delay login or page navigation. If the release service or outbound network is unavailable, the dashboard continues normally without a banner.

Re-run the installer to upgrade to the latest published release:

```bash
curl -fsSL https://fluxo.fabiotech.dev/install.sh | sudo bash
```

If the selected release is already installed and the running dashboard and configured panel domain pass health checks, the installer reports **Fluxo is already up to date** and exits successfully before component prompts, artifact downloads, package changes, or service restarts. An installed version newer than the resolved `latest` release is also left unchanged; an explicitly pinned different release still follows the normal installation path. If the latest release cannot be resolved, the installer stops rather than assuming the server is current.

## Upgrading while Node.js recovery is paused

Use the same installer command. When a supported Node.js recovery is paused,
the installer verifies a compatible release and stages its recovery worker
before package work or optional-component questions. The dashboard stays running;
maintenance holds, approvals and history remain intact.

The installer asks you to open **Runtime > Node.js > Review repair** in the
dashboard. Review and approve that repair there. The installer waits for up to
15 minutes and continues the normal upgrade once recovery has succeeded and all
runtime holds and pending operations have cleared. It never approves or retries
a runtime transition automatically.

If the repair fails or you need more time, the installer stops without stopping
your worker or deleting recovery evidence. Finish the reviewed repair, then run
the same installer command again; it recognizes a staged executable and completes
the dashboard upgrade without requiring a separate support script or special
reinstall flag. Active workers, unsupported history and pending PHP recovery
remain protected and receive an explicit refusal.

`--reinstall` reinstalls the **selected** release and applies optional component/configuration flags even if that release is already installed. To repair a specific installed release without changing versions, pin its published tag as well:

```bash
curl -fsSL https://fluxo.fabiotech.dev/install.sh | \
  FLUXO_VERSION=v0.4.43 sudo -E bash -s -- --reinstall
```

Replace `v0.4.43` with the exact published release you intend to repair; check `fluxo --version` first. `--reinstall` without a pin selects `latest`, which can upgrade or downgrade the installed version. Keep any original release selection and component flags when repairing an interrupted installation. A stopped service or failed dashboard/panel-domain check produces repair guidance instead of an up-to-date success. A verified staged executable with an older running dashboard continues the installer so that an interrupted recovery-aware upgrade can finish. Explicit local builds, custom binary URLs, and custom `FLUXO_GITHUB_REPO` repositories always use the normal installation path because different artifacts can report the same version. `--reinstall` retains the existing checksum, provenance, snapshot, and health checks.

For a PHP update blocked only by earlier manual Scheduler verification, releases
with `scheduler-verify-panel` provide a [targeted SSH verification
command](/reference/cli#scheduler-verification-and-diagnostics). It avoids a full
installer repair. An ordinary same-version installer run exits before Scheduler
verification. A genuinely interrupted installer still needs its original repair
path; the targeted command preserves its durable marker.

Optional component choices (Node.js, Python, database engines and Redis) are saved in root-owned `/var/lib/fluxo-installer/preferences.json`. Upgrades reuse both Yes and No answers without prompting. Explicit flags override saved choices. On older installations without a saved record, upgrades preserve installed components and skip absent optional components; they remain available in Runtime. Saving a choice does not mean provisioning completed, and skipped components installed later through Runtime are never removed by the installer.

For an unattended upgrade that does not add optional components:

```bash
curl -fsSL https://fluxo.fabiotech.dev/install.sh | sudo bash -s -- \
  --db-engine=none \
  --no-redis \
  --no-node \
  --no-python
```

The installer validates the candidate and its signed release provenance before modifying the host. For an existing installation, it stores Fluxo-owned sudoers, SSH, and Fail2Ban policy, then stops Fluxo cleanly and stores the current binary, SQLite database and WAL state, systemd service, Composer and WP-CLI executables, and Fluxo-managed cron files in `/var/lib/fluxo/upgrades/`. It verifies that the dashboard port and loopback-only pprof port `6060` were released by the stopped service; an unexpected listener aborts the upgrade and is reported with its PID and command instead of being killed automatically. If the candidate fails its exact health and version checks, those files are restored automatically and the previous service must pass the same health and version checks before rollback is considered complete. If any rollback is incomplete, Fluxo remains stopped instead of starting against a partial release or toolchain. The three most recent application snapshots are retained.

When `--node` is selected, the installer creates a separate temporary snapshot before changing Fluxo-managed Node files. It covers `/opt/fluxo/node`, `/opt/fluxo/node-toolchain`, the managed state file, Corepack's `fluxo`-owned selection cache, and Fluxo-owned links in `/usr/local/bin`. It also records every active, stable, and reachable Node.js site process created by Fluxo; an already unhealthy or restarting managed application blocks the toolchain upgrade before runtime files change. After the dashboard is healthy, the recorded processes are restarted one at a time and must remain stable and accept TCP connections on their configured application ports. A failed installation, dashboard check, or managed application check restores the previous release and toolchain, then restarts the previously active applications against that restored runtime.

PM2, Forever, Nodemon, and other external process managers are outside this transaction. The installer reports detected external managers but does not stop, restart, repair, or change ownership for them. Migrate production applications to Fluxo-managed processes before relying on automatic application rollback.

Each release also carries exact Composer, WP-CLI, and uv versions, architecture-specific Node.js, Bun, and uv hashes, and npm integrity values for Corepack, pnpm, and Yarn. An upgrade or explicit `--reinstall` installs and verifies the tools selected for the target Fluxo release rather than using moving downloads. After installation, Fluxo's weekly maintenance job may advance Composer to a newer stable Composer 2 release; WP-CLI and uv remain release-selected.

An existing active or inactive UFW policy is preserved without changing its rules or enabled state. The installer queries effective UFW status and stops on command errors or disagreement with UFW's configuration file. Existing Fluxo SSH hardening is revalidated; a server without Fluxo's SSH drop-in is not hardened unless `--harden-ssh` is explicitly supplied.

The effective dashboard transport is preserved across upgrades. A server using the default self-signed HTTPS remains HTTPS; an existing `FLUXO_USE_HTTP=1` service remains HTTP for its trusted local reverse proxy. When a panel domain is configured, the installer records the hostname and whether that proxy is healthy before stopping the current release. After starting the candidate, it first uses the preserved scheme and port for direct loopback health and exact-version checks. A panel domain that was healthy before the upgrade must also pass through Nginx after the upgrade and after any rollback. An already-unhealthy proxy produces a warning but does not prevent a security or recovery upgrade; its stored hostname must still remain unchanged, and direct dashboard health becomes the recovery requirement.

## Runtime maintenance introduced in v0.4.39

After upgrading the panel, use **Runtime > Node.js > Check for updates** or the
PHP branch's **Check for updates** action. Discovery does not install anything;
review the exact versions, affected domains and processes before confirming.
Node.js updates stay on the installed supported LTS major and include bundled
npm. Approved runtime updates survive later toolchain repair without being
replaced by an older or different-major release pin.

Node.js maintenance also pauses qualified PHP background processes and managed
Bun applications because they can launch Node.js. It preserves their original
running/stopped and enabled/disabled states; PHP packages and PHP-FPM remain
unchanged. Unknown consumers still require a manual maintenance plan.

Independent custom queue workers can qualify without adoption or deletion.
Other qualified direct Artisan commands exit naturally and resume once after
verification. Completed executions are not replayed by recovery. A continuous
command needs its own documented graceful exit, and uncertain execution evidence
requires operator review. See [supported consumers and recovery requirements](/operations/runtimes).

Complete pending PHP or Node.js maintenance before a panel upgrade. Preserve
runtime approval receipts and both maintenance journals; compatibility checks
may refuse older panel binaries once this coordination is established.

## Scheduler coordination and upgrades

Version 0.4.37 additionally recognizes qualified Ubuntu 24.04 Cronie `1.7.1-1` on amd64 and retries eligible initial qualification failures. Existing cron/Cronie providers and disabled/masked states are preserved during installation. No provider conversion or reboot is required. Custom configurations remain operational but may need manual PHP maintenance. See [Scheduler runtime requirements](/operations/runtimes#scheduler-coordination-and-panel-upgrades).

Fluxo migrates existing exact managed Laravel Scheduler minute entries to a tracked launcher when the panel starts. Scheduling continues normally. On qualified standard Ubuntu 24.04 cron configurations, it verifies cron has loaded the replacement and waits for earlier tasks and background children to finish naturally. **No VPS or cron restart is required.** While this transition is pending, package checks still work and PHP installation stays blocked for the affected branch. Review installation again after the next cron ticks and earlier work complete.

The installer acquires the existing lifecycle lease before stopping the panel, so protected manual Scheduler runs finish while their output readers and temporary resources remain available. A busy panel can ask you to let commands finish and retry; it does not terminate those tasks to make progress. Avoid starting unrelated terminal commands or backups during a panel upgrade; their existing upgrade requirements still apply.

Upgrading from a version before 0.4.34 remains possible, but it cannot certify that release's historical manual-launch admission. Finish existing manual work before upgrading, then rerun the installer with `--reinstall` while the new panel is running to establish its supported boundary. A deliberately stopped or unaccounted old panel also receives no invented proof: start the compatible panel and rerun `--reinstall` before reviewing PHP maintenance.

Newly created managed entries are tracked immediately. Automatic handling requires systemd 250 or newer with cgroup v2. Legacy adoption qualifies the Ubuntu noble cron and Cronie packages and standard PAM/unit configurations described in the [runtime documentation](../operations/runtimes#managed-consumers-and-draining). Custom configurations continue scheduling but may need PHP maintenance over SSH after operator review; do not remove the migration journal to bypass a blocker.

Resolve pending PHP maintenance before upgrading the panel. Installer upgrades preserve an independent Scheduler journal and dispatcher, and refuse automatic restoration of an older panel binary that cannot understand established coordination. An interrupted installer may leave a durable upgrade marker; retry the installer to reconcile it rather than removing the marker. The retained snapshot remains available if compatible automatic restoration cannot complete.

## Pin an upgrade

```bash
curl -fsSL https://fluxo.fabiotech.dev/install.sh | \
  FLUXO_VERSION=v0.4.43 sudo -E bash
```

Pinning is useful when coordinating multiple servers or holding on a known release while reviewing a newer one.

## Before upgrading

1. Review the [GitHub release notes](https://github.com/FabioTECH1/fluxo/releases).
2. Confirm recent off-server application and Fluxo data backups exist. The local automatic rollback snapshot is useful, but it is not a substitute for a server snapshot or off-server backup.
3. Confirm you have root SSH or provider-console access.
4. Record the current version with `fluxo --version`.
5. If Node applications use PM2 or another external manager, migrate or verify them independently; Fluxo cannot roll them back.

## Verify the upgrade

```bash
fluxo --version
sudo systemctl status fluxo --no-pager
curl -k https://127.0.0.1:9595/api/v1/health
```

If a panel domain is configured, verify it too:

```bash
curl --resolve panel.example.com:443:127.0.0.1 \
  https://panel.example.com/api/v1/health
```

Refresh the dashboard after the service is healthy. Existing browser sessions may need to sign in again when an authentication-related migration intentionally invalidates them.

Upgrades preserve every existing site's application type and stored database credentials. Fluxo does not force legacy sites to rotate credentials or replace an existing control-plane database login. The dedicated database-user requirement applies when a new site is created with an attached database. Existing sites remain operable, while application type is displayed as read-only in site settings.

## Roll back the Fluxo binary

The installer can install a previous release by setting `FLUXO_VERSION`. Automatic rollback handles a candidate that fails during installation when the previous binary remains compatible with established Scheduler coordination, while the retained files in `/var/lib/fluxo/upgrades/` support manual recovery. Database migrations remain forward-moving after a successful upgrade, so review release notes before intentionally downgrading. A provider snapshot is still the safest recovery point for migration-sensitive releases.

### Scheduled-job coordination hotfix

`v0.4.39-hotfix.4` adds guarded admission for the exact supported System Cleanup, Renew SSL Certificates and Update Composer jobs and ordinary local non-root scheduled jobs. These jobs share runtime maintenance admission and natural descendant drainage; names alone no longer determine eligibility. Root inputs still require the documented qualification, and nonempty APT removal plans/custom root hooks remain unsupported. See [scheduled-job maintenance](../operations/runtimes) for supported profiles and `system-task-reconcile` recovery guidance. Deleted jobs and unknown execution outcomes are not replayed or reset.

This includes the PHP install confirmation, Activity-only maintenance outcomes and active-site Laravel Scheduler layout fixes from `v0.4.39-hotfix.3`.

### Panel certificate renewal registration

Version `v0.4.41` corrects guarded renewal qualification for Fluxo panel certificates using `/var/lib/fluxo-acme`. Existing site webroots remain supported. The upgrade does not move or reissue certificates or change their renewal settings. Fluxo retries registration after startup; if it remains blocked, inspect `system-task-diagnostics` and the `Scheduled-job coordination pending` service logs for any additional unsupported input.
