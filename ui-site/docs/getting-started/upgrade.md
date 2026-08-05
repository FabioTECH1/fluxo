---
title: Upgrade Fluxo
description: Upgrade Fluxo safely and verify the installed version.
---

# Upgrade Fluxo

Re-run the installer to upgrade to the latest published release:

```bash
curl -fsSL https://fluxo.fottify.com/install.sh | sudo bash
```

For an unattended upgrade that does not add optional components:

```bash
curl -fsSL https://fluxo.fottify.com/install.sh | sudo bash -s -- \
  --db-engine=none \
  --no-redis \
  --no-node
```

The installer validates the candidate and its signed release provenance before modifying the host. For an existing installation, it stores Fluxo-owned sudoers, SSH, and Fail2Ban policy, then stops Fluxo cleanly and stores the current binary, SQLite database and WAL state, systemd service, Composer and WP-CLI executables, and Fluxo-managed cron files in `/var/lib/fluxo/upgrades/`. If the candidate fails its exact health and version checks, those files are restored automatically and the previous service is restarted. If a Node rollback is incomplete, Fluxo remains stopped instead of starting against a partial toolchain. The three most recent application snapshots are retained.

When `--node` is selected, the installer creates a separate temporary snapshot before changing Fluxo-managed Node files. It covers `/opt/fluxo/node`, `/opt/fluxo/node-toolchain`, the managed state file, and Fluxo-owned links in `/usr/local/bin`. A failed Node installation or later failed Fluxo health check restores that snapshot without changing external Node installations or user caches. The temporary Node snapshot is removed after a healthy upgrade.

Each release also carries exact Composer and WP-CLI baseline versions, architecture-specific Node.js and Bun hashes, and npm integrity values for Corepack, pnpm, and Yarn. Re-running the installer installs and verifies the tools selected for the target Fluxo release rather than using moving downloads. After installation, Fluxo's weekly maintenance job may advance Composer to a newer stable Composer 2 release; WP-CLI remains at the release-selected version.

An existing active or inactive UFW policy is preserved without changing its rules or enabled state. The installer queries effective UFW status and stops on command errors or disagreement with UFW's configuration file. Existing Fluxo SSH hardening is revalidated; a server without Fluxo's SSH drop-in is not hardened unless `--harden-ssh` is explicitly supplied.

## Pin an upgrade

```bash
curl -fsSL https://fluxo.fottify.com/install.sh | \
  FLUXO_VERSION=v0.4.10 sudo -E bash
```

Pinning is useful when coordinating multiple servers or holding on a known release while reviewing a newer one.

## Before upgrading

1. Review the [GitHub release notes](https://github.com/FabioTECH1/fluxo/releases).
2. Confirm recent off-server application and Fluxo data backups exist. The local automatic rollback snapshot is useful, but it is not a substitute for a server snapshot or off-server backup.
3. Confirm you have root SSH or provider-console access.
4. Record the current version with `fluxo --version`.

## Verify the upgrade

```bash
fluxo --version
sudo systemctl status fluxo --no-pager
curl -k https://127.0.0.1:9595/api/v1/health
```

Refresh the dashboard after the service is healthy. Existing browser sessions may need to sign in again when an authentication-related migration intentionally invalidates them.

## Roll back the Fluxo binary

The installer can install a previous release by setting `FLUXO_VERSION`. Automatic rollback handles a candidate that fails during installation, while the retained files in `/var/lib/fluxo/upgrades/` support manual recovery. Database migrations remain forward-moving after a successful upgrade, so review release notes before intentionally downgrading. A provider snapshot is still the safest recovery point for migration-sensitive releases.
