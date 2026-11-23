---
title: Upgrade an older installation
description: Move an older Fluxo installation through v0.4.55 to the current release.
---

# Upgrade an older installation

Use this guide only for installations that have not entered the base release.
**New servers and existing base installations, including base-test releases,
install or update directly to v0.4.56.** See [Installation](../getting-started/installation)
or [Upgrade Fluxo](../getting-started/upgrade).

## 1. Complete v0.4.55

Back up your applications and Fluxo data, keep root SSH or provider-console
access, and let active runtime maintenance finish. Download and run the
supported intermediate release:

```bash
sudo curl -fsSL https://fluxo.fabiotech.dev/releases/v0.4.55/install.sh -o /root/fluxo-install-0.4.55.sh &&
sudo env FLUXO_VERSION=v0.4.55 bash /root/fluxo-install-0.4.55.sh
```

This bridge prepares the existing database and scheduling state for the base.
Use the official release, not a development build or an experimental bridge.
Wait for the installer to report success. No separate certification or internal
Scheduler command is required.

## 2. Install v0.4.56

```bash
sudo curl -fsSL https://fluxo.fabiotech.dev/releases/v0.4.56/install.sh -o /root/fluxo-install-0.4.56.sh &&
sudo env FLUXO_VERSION=v0.4.56 bash /root/fluxo-install-0.4.56.sh
```

The installer verifies the completed bridge, installation identity, credentials
and retained execution state before making changes. After this handoff, future
updates use the normal base installer without returning to the bridge.

## If installation stops

- Let active jobs finish when the installer reports that scheduling or package
  work is busy, then retry the **same release** and keep the original options.
- For an interrupted operation, use its original tagged installer and follow
  its recovery message before changing releases. Check `fluxo --version` when
  selecting the installer.
- Keep database files, keys, journals, helpers and upgrade snapshots. Deleting
  them does not repair an interrupted installation.
- Never run two installers at once.

See [Troubleshooting](../reference/troubleshooting) for common errors and
[CLI commands](../reference/cli) for exceptional recovery operations.

Historical build identities and test results are kept in the repository's
[release records](https://github.com/FabioTECH1/fluxo/tree/main/scripts/ci/evidence).
The [previous guide](https://github.com/FabioTECH1/fluxo/blob/17f472e687b986984e4f30d1bc2bc3adeeab844a/ui-site/docs/operations/baseline-bridge.md)
is retained as a historical snapshot, not current installation instructions.
