---
title: Security and Firewall
description: Secure Fluxo access, SSH, UFW, credentials, applications, and backups.
---

# Security and firewall

Fluxo automates useful server hardening, but the server owner remains responsible for operating-system updates, provider access, application security, and recovery.

## Default controls

The installer:

- Enables UFW with the effective SSH port, HTTP, HTTPS, and the Fluxo dashboard port when no active policy already exists
- Preserves an existing active UFW policy without broadening source-restricted rules
- Stops without firewall changes when effective UFW status cannot be read or disagrees with UFW's configuration
- Installs Fail2Ban
- Creates a dedicated `fluxo` system user
- Validates exact PHP-FPM reload commands through sudoers without granting a wildcard command
- Can configure key-only root SSH authentication with the explicit `--harden-ssh` installer flag
- Verifies signed GitHub build provenance before executing new release binaries and binds Node.js and Bun hashes, npm package integrity values, Composer checksums, and the WP-CLI release identity to that release. A managed weekly job subsequently keeps Composer on the stable Composer 2 channel; WP-CLI remains release-selected.
- Stores bootstrap credentials in a root-only file
- Runs deployments and site commands without root privileges

## Firewall rules

Use **Settings > Network** to list, add, and remove UFW rules created from the dashboard. The page checks every stored rule against UFW's persisted rule commands and marks rules changed outside Fluxo as missing. Baseline SSH, HTTP, HTTPS, and dashboard rules created by the installer are labelled and protected from dashboard deletion to reduce accidental lockouts; change those deliberately over SSH or at the provider firewall. Rules created manually, by another panel, or by a provider firewall are intentionally not imported or presented as Fluxo-owned.

Keep the effective SSH port allowed before applying network changes. A fresh installation allows port `9595` from any source by default so the authenticated dashboard is reachable across devices. Restrict it at the provider firewall when possible, or use `--management-cidr` while Fluxo creates a new UFW policy. That option is rejected for an already active policy rather than risking an accidental lockout or broader rule. Do not expose MariaDB/MySQL, PostgreSQL, or Redis directly to the internet for ordinary same-server applications.

Fail2Ban limits repeated dashboard login failures on port `9595`. Administrators behind shared NAT should retain provider-console access because repeated failures from the shared address can temporarily block dashboard access for everyone using that address.

## Patch management

Apply operating-system security updates regularly and reboot when kernel or critical library updates require it. Check Fluxo release notes and upgrade the control panel independently of application deployments.

## Credentials

- Store the one-time credentials export in a password manager.
- Protect GitHub tokens and backup credentials with least privilege.
- Rotate application and database passwords after suspected exposure.
- Never include `.env`, `wp-config.php`, private keys, or access tokens in public logs.
- Remove access promptly when an administrator or automation identity is retired.

## Application isolation

Sites share the `fluxo` Linux identity. This keeps operations simple but is not a hard tenant boundary. Host only applications and operators that you trust on the same Fluxo server.

Patch WordPress plugins and themes, Composer packages, npm dependencies, and application frameworks. Fluxo manages the server layer; it does not make vulnerable application code safe.

## Backups and recovery

Keep encrypted off-server backups in a separate account, test recovery, and retain provider-console access. A control panel cannot repair a lost server if its only backup lived on that server.
