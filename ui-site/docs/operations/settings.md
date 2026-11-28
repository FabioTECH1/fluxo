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

## System

Open **Settings > System** for security-update settings, the running kernel and
server restart controls.

### Automatic security updates

**Automatic security updates** manages the installed Ubuntu
`unattended-upgrades` policy and APT timers. Fluxo writes only its own
`/etc/apt/apt.conf.d/99zz-fluxo-security-updates` override, verifies the effective
policy and restores the previous override if verification fails. Existing
repositories, exclusions and schedules are preserved.

| Status | Action |
|---|---|
| Enabled / Disabled | Use the toggle when the configuration is supported. |
| Not installed | Follow the displayed SSH installation guidance; Fluxo does not install the package. |
| External / Unsupported / Unknown | Review the reported policy or timer issue over SSH before changing it. |

Coverage is limited to configured security repositories; third-party PHP, Node
and Nginx packages may need separate updates. An existing administrator automatic reboot
policy requires review and is never silently changed. There is no
automatic-reboot setting in Fluxo.

**Last update attempt** uses the actual `apt-daily-upgrade.service` timestamp
and result. A missing date shows **Unavailable**; failures retain their details.
A completed attempt does not guarantee every package was updated.
**Running kernel** shows the currently booted release from
`/proc/sys/kernel/osrelease`, not an installed or candidate package version.

### Restart the server

**Show details** on the **Server restart required** notice opens its reasons and
known packages. **Restart server** in that modal leads to confirmation in the same
window; the Power card opens this same flow. All hosted sites and Fluxo will be temporarily
unavailable. Active package work, deployments, runtime recovery or incompatible
execution can block the request; finish the reported work first.

Keep the browser open. An accepted request confirms scheduling, not completion.
Fluxo confirms a different boot ID after reconnecting, then allows expected
services up to **90 seconds** to start. Expired sessions require signing in again.
Repeated clicks cannot schedule duplicate restarts, and Fluxo never retries a
reboot automatically.

The persistent **Server restart required** notice comes from Ubuntu's
`/run/reboot-required`, with package reasons when available. It clears only when
Ubuntu confirms a restart is no longer required; failed status reads preserve
it. An available update alone does not create this notice.

### Service status after restart

The same modal follows restarting, waiting, boot confirmation and service checks.
Its checklist retains confirmed steps: **Restart requested**, **New boot confirmed**
and **Services checked**. Temporary connection loss during an accepted restart
shows automatic reconnection; scheduling, authentication and service errors remain visible.
Use **Minimise** to keep working with a persistent loading toast. Its **Show details**
action reopens the modal and removes the toast. **Check status** refreshes the automatic checks. The service table scrolls
inside the modal; a completed result can be closed or dismissed.
Checks cover enabled, managed services that were running before the request.
Intentionally stopped services stay stopped. Fluxo does not automatically repair
services or test individual hosted sites.

A completed restart result can be dismissed. Dismissal survives reload for that
restart and result; it does not hide a new restart, changed service outcome,
failed status check or the separate restart-required notice.

A refused request shows **Restart not started** and can be closed. Older browser
tracking without a matching request may show **No matching restart request**;
closing that status does not schedule or confirm a reboot. A real uncertain
request remains pending until its status can be verified.

If the server remains unreachable or the result is unconfirmed, use the provider console
or SSH access before requesting another reboot. Read-only diagnostics:

```bash
sudo systemctl status fluxo-server-reboot.service --no-pager
sudo journalctl -u fluxo-server-reboot.service --no-pager
sudo systemctl list-jobs
```

A shutdown already requested can retain maintenance exclusion until shutdown or
operator review. Do not remove lock or operation files to bypass it.
