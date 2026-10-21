---
title: Upgrade Fluxo
description: Upgrade Fluxo safely and verify the installed version.
---

# Upgrade Fluxo

When a newer release is available, Fluxo shows an informational banner after an administrator signs in. The banner identifies the installed and latest versions and links to the GitHub release notes. Dismissing it hides that specific release; a later release will appear again.

The dashboard never downloads or installs an update. Its asynchronous version check is cached and does not delay login or page navigation. If the release service or outbound network is unavailable, the dashboard continues normally without a banner.

Re-run the installer to upgrade to the latest published release:

```bash
curl -fsSL https://fluxo.fabiotech.dev/install.sh | sudo bash
```

If the selected release is already installed and the running dashboard and configured panel domain pass health checks, the installer reports **Fluxo is already up to date** and exits successfully before component prompts, artifact downloads, package changes, or service restarts. An installed version newer than the resolved `latest` release is also left unchanged; an explicitly pinned different release still follows the normal installation path. If the latest release cannot be resolved, the installer stops rather than assuming the server is current.

`--reinstall` reinstalls the **selected** release and applies optional component/configuration flags even if that release is already installed. To repair a specific installed release without changing versions, pin its published tag as well:

```bash
curl -fsSL https://fluxo.fabiotech.dev/install.sh | \
  FLUXO_VERSION=v0.4.35 sudo -E bash -s -- --reinstall
```

Replace `v0.4.35` with the exact published release you intend to repair; check `fluxo --version` first. `--reinstall` without a pin selects `latest`, which can upgrade or downgrade the installed version. Keep any original release selection and component flags when repairing an interrupted installation. A stopped service or failed dashboard/panel-domain check produces repair guidance instead of an up-to-date success. Explicit local builds, custom binary URLs, and custom `FLUXO_GITHUB_REPO` repositories always use the normal installation path because different artifacts can report the same version. `--reinstall` retains the existing checksum, provenance, snapshot, and health checks.

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

## Pin an upgrade

```bash
curl -fsSL https://fluxo.fabiotech.dev/install.sh | \
  FLUXO_VERSION=v0.4.35 sudo -E bash
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

The installer can install a previous release by setting `FLUXO_VERSION`. Automatic rollback handles a candidate that fails during installation, while the retained files in `/var/lib/fluxo/upgrades/` support manual recovery. Database migrations remain forward-moving after a successful upgrade, so review release notes before intentionally downgrading. A provider snapshot is still the safest recovery point for migration-sensitive releases.
