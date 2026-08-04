---
title: Security and Firewall
description: Secure Fluxo access, SSH, UFW, credentials, applications, and backups.
---

# Security and firewall

Fluxo automates useful server hardening, but the server owner remains responsible for operating-system updates, provider access, application security, and recovery.

## Default controls

The installer:

- Enables UFW
- Allows SSH, HTTP, HTTPS, and the Fluxo dashboard port
- Installs Fail2Ban
- Creates a dedicated `fluxo` system user
- Configures key-only SSH authentication
- Stores bootstrap credentials in a root-only file
- Runs deployments and site commands without root privileges

## Firewall rules

Use **Settings > Network** to list, add, and remove UFW rules. Specify the minimum protocol, port, and source required.

Keep SSH allowed before applying network changes. Restrict port `9595` to trusted addresses at the provider firewall when possible. Do not expose MariaDB/MySQL, PostgreSQL, or Redis directly to the internet for ordinary same-server applications.

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

