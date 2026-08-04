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

The installer is designed to repeat its provisioning steps. It refreshes required packages, verifies Composer and WP-CLI, reapplies Fluxo-owned service and security configuration, atomically replaces the binary, and restarts the daemon. Existing Fluxo data and managed sites are preserved.

## Pin an upgrade

```bash
curl -fsSL https://fluxo.fottify.com/install.sh | \
  FLUXO_VERSION=v0.4.10 sudo -E bash
```

Pinning is useful when coordinating multiple servers or holding on a known release while reviewing a newer one.

## Before upgrading

1. Review the [GitHub release notes](https://github.com/FabioTECH1/fluxo/releases).
2. Confirm recent application and Fluxo data backups exist.
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

The installer can install a previous release by setting `FLUXO_VERSION`. Database migrations are forward-moving, so review release notes before downgrading across versions that changed the schema. A server snapshot is the safest rollback for a migration-sensitive release.

