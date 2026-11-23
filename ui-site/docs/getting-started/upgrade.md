---
title: Upgrade Fluxo
description: Update the panel, preserve existing settings and recover interrupted installations.
---

# Upgrade Fluxo

The dashboard's release notice links to release notes; it does not install the
panel. PHP and Node.js updates have separate dashboard workflows.

## Before upgrading

1. Review the [release notes](https://github.com/FabioTECH1/fluxo/releases).
2. Confirm recent off-server application and Fluxo-data backups exist.
3. Keep root SSH or provider-console access available.
4. Record the current version with `fluxo --version`.
5. Finish active runtime maintenance and let running jobs complete.

Older installations that have not entered the base must first follow
[Upgrade an older installation](../operations/baseline-bridge). Existing bases,
including base-test installations, update directly.

## Run the installer

```bash
sudo curl -fsSL https://github.com/FabioTECH1/fluxo/releases/download/v0.4.56/install.sh -o /root/fluxo-install-0.4.56.sh &&
sudo env FLUXO_VERSION=v0.4.56 bash /root/fluxo-install-0.4.56.sh
```

This pins the selected release and its installer. The installer verifies
checksums and signed provenance, snapshots the current installation, applies
the upgrade and checks the running version and dashboard health.

If that version is already installed and healthy, the normal run checks
Scheduler compatibility without repeating component setup. Use `--reinstall`
for explicit component/configuration changes or repair of that same release:

```bash
sudo env FLUXO_VERSION=v0.4.56 bash /root/fluxo-install-0.4.56.sh --reinstall
```

For a different release, select its corresponding tag and installer. An
unpinned `--reinstall` resolves latest, so it may change versions.

## What is preserved

- Site IDs, application types, database credentials, keys, history and
  custom-vhost settings.
- Saved optional-component choices in
  `/var/lib/fluxo-installer/preferences.json`. Explicit flags override them;
  skipped runtimes are not removed. See [installer flags](./installation#unattended-installation).
- Installed PHP packages and CLI selection. Apply patches separately in
  **Runtime > PHP**.
- Existing UFW rules/enabled state and dashboard transport/port. SSH hardening
  changes only when requested or maintaining an existing Fluxo policy.
- The configured panel hostname. A previously healthy proxy must pass the
  post-upgrade check; an already-unhealthy proxy is reported while direct
  dashboard health remains the recovery requirement.

### Existing Node.js toolchains

Panel updates verify and preserve the managed Node toolchain, including an
approved runtime version. `--node` and `--reinstall` do not request a different
major or automatic toolchain repair. Older installations may need a one-time
network authentication of existing files; verified repeats reuse local proof.
Missing/changed files require **Runtime > Node.js > Repair toolchain** rather
than silent replacement.

When Node is selected, the installer retains an independent toolchain snapshot
and checks previously active Fluxo-managed Node applications. Failed activation
restores compatible prior files and verifies the restored applications.
PM2, Forever and other external managers are outside this rollback; review
their applications independently.

## If recovery is pending

### Paused Node.js recovery

For supported paused Node recovery, the installer can stage a compatible
recovery worker and Scheduler dispatcher while keeping the dashboard running.
Open **Runtime > Node.js > Review repair**, review and approve it there. The
installer waits up to **15 minutes** for successful recovery and cleared holds;
it never approves a runtime change automatically.

If repair fails or needs longer, finish it and rerun the same tagged installer.
Stop an earlier waiting installer before starting another. Active workers,
unsupported history and pending PHP recovery can still refuse an upgrade.

### Busy or interrupted installation

Let reported Scheduler jobs or package work finish, then retry the same release
with its original options. Do not force-stop work or delete database files,
locks, journals, helpers or snapshots to clear the refusal.

An interrupted operation may require its **original exact release**, even if a
newer release is available. Follow its printed recovery instructions first.
A stopped or unhealthy panel is not an up-to-date success. For Scheduler or
certificate-renewal errors, inspect the reported task and use the
[runtime recovery guidance](../operations/runtimes#scheduler-coordination-and-panel-upgrades)
or [CLI reference](../reference/cli).

## Verify the upgrade

```bash
fluxo --version
sudo systemctl status fluxo --no-pager
curl -k https://127.0.0.1:9595/api/v1/health
```

Use your configured port and HTTP scheme if different. If a panel domain is
configured, verify its origin route too, replacing the example hostname:

```bash
curl --resolve panel.example.com:443:127.0.0.1 \
  https://panel.example.com/api/v1/health
```

A healthy API returns `{"status":"ok"}`. Refresh the dashboard and sign in again
if the release invalidated sessions. Check important hosted application routes
and jobs separately.

## Rollback and backups

The installer retains the three most recent installation snapshots under
`/var/lib/fluxo/upgrades/`. A failed upgrade attempts automatic restoration only
when the previous binary remains compatible, then checks the restored service.
An incomplete rollback leaves explicit recovery guidance rather than starting
against partial state.

Snapshots do not replace off-server backups. Database migrations move forward; installing an older binary may be refused or incompatible with saved
runtime/Scheduler state. Review release notes and retain a provider snapshot
before a deliberate downgrade.
