---
title: Settings and Access
description: Configure the Fluxo panel domain, administrator settings, source control accounts, and SSH keys.
---

# Settings and access

The Settings area controls the panel domain, administrator identity, source-control integrations, server SSH keys, and network policy.

## General settings

Use **Settings > General** to maintain server-level defaults and change the Fluxo administrator password. Use a unique password stored in a password manager.

Changing the administrator password updates the signing secret used for sessions, invalidating previously issued authentication tokens. Sign in again on trusted devices.

Account recovery is intentionally available only from a root shell. See [First login and recovery](../getting-started/first-login.md).

## Source control

**Settings > Source Control** connects one or more GitHub accounts using classic personal access tokens. Fluxo uses them for repository discovery and lifecycle operations such as deploy keys and webhooks.

Label accounts by owner or purpose so operators can select the correct identity during site creation. Account cards show token expiry, linked sites and deployment-access results. Use **Check access** for a read-only token/SSH check, **Update token** or **Reconnect** to replace credentials while keeping site links and private keys, and the three-dot menu for **Disconnect**. Reconnect requires the same verified numeric GitHub identity; a username change is supported.

Use a different token for the same account when credentials expire or may have been exposed. Connecting a different owner does not automatically claim another account's sites. See [GitHub connection recovery](/deployments/github#token-expiry-and-connection-recovery) for partial failures, legacy ownership limits and disconnect behavior.

## SSH keys

**Settings > SSH Keys** manages public keys authorized for the `fluxo` system user. Add a descriptive name and a valid public key.

Adding a key does not silently change the server's authentication policy. The **SSH access security** card reads the effective OpenSSH settings and reports whether password login is enabled, whether public-key authentication is available, and whether the policy is managed by Fluxo or externally.

To switch an existing server to key-only access:

1. Add at least one key for the `fluxo` user.
2. Keep the current SSH session open and successfully run `ssh fluxo@YOUR_SERVER_IP` from a separate terminal.
3. Confirm that provider-console or equivalent recovery access is available.
4. Select **Disable password login** and complete both acknowledgements.

Fluxo stages a global OpenSSH drop-in, validates its effective policy for both `fluxo` and `root` from remote IPv4, remote IPv6, and local IPv4/IPv6 connection contexts, validates the complete configuration, and reloads OpenSSH. Validation or reload failure restores the previous policy and reloads the restored configuration; if live state cannot be reconciled, Fluxo reports that explicitly instead of claiming a clean rollback.

Human login keys and site deploy keys are written through a descriptor-pinned `.ssh` directory. Fluxo refuses an operation if that directory is a symlink or is replaced while the operation is running, preventing application or deployment processes running as `fluxo` from redirecting a privileged filesystem write. Fluxo also refuses to overwrite an unmanaged policy file and prevents removal of the final usable bare RSA, Ed25519, or ECDSA key while password login is disabled. Restricted or malformed legacy entries remain untouched but do not satisfy the final-key safety check.

**Restore server SSH policy** removes only Fluxo's managed drop-in. It does not force password authentication on: an underlying VPS-provider or administrator policy may continue to require keys. Installations hardened with `--harden-ssh` appear as Fluxo-managed because the installer uses the same validated policy location.

Site deploy keys used for repository access are separate from human login keys and are managed through the source-control workflow. Their generation, rotation, and removal use the same pinned-directory boundary.

## Dashboard access

Fluxo listens on port `9595` and uses HTTPS with a self-signed certificate by default. After initial login, use the **Panel Domain** card in **Settings > General** to connect a dedicated hostname. Point the hostname's DNS record to the server, enter it in the card, and select **Connect Domain**. The setup modal then offers one of Fluxo's existing certificate workflows:

- **Let's Encrypt** issues or renews a certificate after the DNS and HTTP challenge checks succeed.
- **Existing Certificate** installs a certificate and private key supplied by the administrator.
- **Clone Certificate** copies a compatible custom certificate already managed for a site, leaving the site's certificate independent.

A panel hostname is not hardcoded. Use any valid fully qualified hostname that is not already assigned to a Fluxo site or alias, such as `admin.example.com` or `fluxo.example.net`. Panel-domain activation always uses HTTPS; Fluxo does not save an HTTP-only panel hostname.

Fluxo validates the generated Nginx configuration and verifies the panel through the new HTTPS hostname before saving the change. A failed activation rolls the proxy configuration back. Removing the hostname removes only the Fluxo-managed panel proxy; it does not stop the dashboard service.

After activation, the setup methods are hidden. A healthy Let's Encrypt panel needs no manual renewal action: Fluxo's existing root job runs `certbot renew --quiet` every 12 hours, checks every Certbot-managed lineage on the server, and reloads Nginx after a successful renewal. **Repair SSL** appears only when the panel's Let's Encrypt certificate or proxy needs attention. Custom and cloned certificates are not managed by Certbot, so Fluxo offers their provider-specific replacement action only when they are expiring or unhealthy.

The original direct address, including port `9595`, remains available after a panel domain is connected. Retain root SSH or provider-console access and record that direct address as the recovery path. Restrict port `9595` using the provider firewall, a VPN, or a trusted network if public direct access is unnecessary.

Set `FLUXO_USE_HTTP=1` only when the upstream connection is restricted to a trusted local reverse proxy. The installer preserves that effective systemd setting and uses the correct upstream scheme for both panel-domain proxying and upgrade health checks.

Do not expose the dashboard through a CDN configuration that weakens origin authentication.

## System (base candidate)

**Settings > System** reads Ubuntu's installed `unattended-upgrades` policy and APT timers. Automatic security updates can be enabled or disabled when the effective security-only configuration and active timers are supported. Fluxo changes only `/etc/apt/apt.conf.d/99zz-fluxo-security-updates`, verifies the effective configuration and restores its prior override if verification fails. Existing schedules, repositories, exclusions and other APT settings remain intact. A changed administrator-owned override refuses modification.

An absent package shows **Not installed**, with SSH installation guidance; Fluxo never installs it automatically. Inactive/custom timers, non-security origins, unknown intervals or an enabled administrator automatic-reboot policy require SSH review. Fluxo does not overwrite that reboot policy. Last-attempt information comes from the actual `apt-daily-upgrade.service` execution timestamps/result; unavailable or ambiguous information remains unknown. Successful unit execution does not mean every package was updated or every vulnerability fixed. Third-party PHP/Node/Nginx repositories are not implicitly covered. Nginx package upgrades and Ubuntu release upgrades remain SSH operations.

A persistent **Server restart required** notice appears across authenticated routes only when Ubuntu's `/run/reboot-required` reports installed updates requiring a reboot. Details use the reported package list when available, without guessing a kernel cause. There is no dismiss control; failed/unknown status checks preserve the last confirmed warning, including reload. Only confirmed absence clears it.

**Restart server** is always present in the Power card. It and the global notice use the same confirmation. All hosted sites and Fluxo will be temporarily unavailable. A restart is blocked while package work, runtime recovery, installation, deployments or incompatible execution is outstanding. A root transient helper retains exclusive maintenance and native dpkg locks through delayed shutdown. Repeated requests cannot schedule duplicate restarts. HTTP acceptance confirms scheduling, not that reboot completed; requests are never automatically repeated.

Keep the browser open. Fluxo waits for an authenticated connection with a **different boot ID**, then gives expected services up to 90 seconds of startup grace before reporting attention. Only installed, enabled managed services running before the request are checked; intentionally stopped/disabled services are preserved. Service checks never start or repair them, and do not test individual sites, database connectivity or applications. Expired sessions require signing in again. A delayed or ambiguous restart stays unconfirmed; use the provider console or SSH, review any recorded failure and do not repeatedly request reboots. Read-only diagnostics are available with `sudo systemctl status fluxo-server-reboot.service`, `sudo journalctl -u fluxo-server-reboot.service` and `sudo systemctl list-jobs`. Once a shutdown request has been sent, the helper keeps maintenance exclusion until shutdown or explicit operator review, rather than releasing it on an arbitrary timeout.

The public demo simulates settings only and refuses actual restarts. Native reboot acceptance for this candidate is pending a separately authorized named fixture; simulated tests are not live reboot proof.
