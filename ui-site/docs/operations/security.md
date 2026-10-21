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
- Verifies signed GitHub build provenance before executing new release binaries and binds Node.js, Bun, and uv hashes, npm package integrity values, Composer checksums, and the WP-CLI release identity to that release. A managed weekly job subsequently keeps Composer on the stable Composer 2 channel; WP-CLI and uv remain release-selected.
- Stores bootstrap credentials in a root-only file
- Runs deployments and site commands without root privileges

## Firewall rules

Use **Settings > Network** to inspect persisted UFW rules and add or remove rules owned by the dashboard. Fluxo checks every stored managed rule against UFW and marks rules changed outside Fluxo as missing. Baseline SSH, HTTP, HTTPS, and dashboard rules created by the installer are labelled and protected from dashboard deletion to reduce accidental lockouts; change those deliberately over SSH or at the provider firewall. Other UFW rules are derived from `ufw show added`, labelled **External**, and displayed read-only without importing or claiming ownership of them. Provider firewalls and security groups remain outside Fluxo and are not visible on this page.

When upgrading an older Fluxo installation, startup restores the protected records for the four historical baseline rules only when the exact corresponding UFW rules still exist. This reconciliation never adds, removes, or broadens a firewall rule.

Keep the effective SSH port allowed before applying network changes. A fresh installation allows port `9595` from any source by default so the authenticated dashboard is reachable across devices. Restrict it at the provider firewall when possible, or use `--management-cidr` while Fluxo creates a new UFW policy. That option is rejected for an already active policy rather than risking an accidental lockout or broader rule. Do not expose MariaDB/MySQL, PostgreSQL, or Redis directly to the internet for ordinary same-server applications.

Fail2Ban limits repeated dashboard login failures on HTTP, HTTPS, and direct dashboard ports (`80`, `443`, and `9595`). This includes requests reaching Fluxo through its managed panel-domain proxy. Administrators behind shared NAT should retain provider-console access because repeated failures from the shared address can temporarily block dashboard access for everyone using that address.

## Patch management

Apply operating-system security updates regularly and reboot when kernel or critical library updates require it. Check Fluxo release notes and upgrade the control panel independently of application deployments.

Fluxo's daily PHP check only discovers package updates. Installation requires an administrator's explicit confirmation in **Runtime > PHP**. Ubuntu's unattended security-update configuration is separate, and adding the PHP PPA does not itself authorize unattended installation from that PPA.

PHP patch eligibility uses complete Debian package revisions and configured repository policy. Upstream `PHP_VERSION` alone cannot establish whether Ubuntu backported a fix, and a repository with no newer candidate does not prove that a recent upstream vulnerability is fixed. Unknown/unsupported package provenance and extension layouts require manual maintenance. Fluxo does not label every package update a security fix.

Managed PHP maintenance drains qualified Queue, Horizon and Octane consumers locally before package mutation. Persistent per-unit holds and a durable admission block prevent conflicting Fluxo work during interrupted recovery. External cron, SSH sessions, other process managers and independent OS update automation are outside those holds; coordinate them before maintenance. Fluxo does not force-kill jobs or requests to satisfy its own wait window, but configured application and native deadlines still apply.

Octane support is deliberately limited to [verified framework/native combinations](./runtimes#octane-qualification). Composer references/source hashes and native binary/library identities are checked. This is not permission to update Swoole's native package: version/rebuild changes are blocked, and retained native payloads must match the inspected library. Qualified RoadRunner support uses the official 2025.1.15 Linux amd64 binary. Active FrankenPHP and unqualified configurations require manual maintenance.

**Repair** and **Roll back PHP packages** require a fresh review and confirmation. Repair may replay only the recorded package set; it does not authorize unrelated package repair. Rollback can restore a vulnerable revision and must be weighed against the failure being recovered. There is no automatic rollback, broad broken-package repair or automatic configuration restoration. Preserve hold files, journals and package locks while investigating an interruption; do not bypass them or blindly repeat a native signal whose delivery is uncertain.

## Credentials

- Store the one-time credentials export in a password manager.
- Protect GitHub tokens and backup credentials with least privilege.
- Rotate application and database passwords after suspected exposure.
- Never include `.env`, `wp-config.php`, private keys, or access tokens in public logs.
- Remove access promptly when an administrator or automation identity is retired.

## Application isolation

Sites share the `fluxo` Linux identity. This keeps operations simple but is not a hard tenant boundary. Host only applications and operators that you trust on the same Fluxo server.

Patch WordPress plugins and themes, Composer packages, npm and Python dependencies, and application frameworks. Fluxo manages the server layer; it does not make vulnerable application code safe.

## Backups and recovery

Keep encrypted off-server backups in a separate account, test recovery, and retain provider-console access. A control panel cannot repair a lost server if its only backup lived on that server.

PHP recovery normally retains authenticated complete previous/target package sets before stopping consumers. Proceeding without available previous packages requires explicit acknowledgement, and subsequent archive downloads can still fail. The retained unique archive set is limited to 2 GiB per operation, with additional space/inode checks. Cleanup removes recorded package payloads from superseded, completed updates only after a newer verified forward update. Latest sets, unfinished recovery, retained installer snapshot dependencies and an exceptional prior complete rollback set stay protected. Monitor disk use: protected evidence and metadata can still accumulate. Local retention is not a permanent availability guarantee.

Package rollback preserves current configuration and does not restore application files, database/Redis contents, completed jobs or a whole server snapshot. Configuration archives are diagnostic copies for deliberate operator recovery. Test application routes after transport/process health checks pass, and retain independent application/database backups and console access for failures that need SSH repair.
