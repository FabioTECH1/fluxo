---
title: Troubleshooting
description: Diagnose common Fluxo access, site, deployment, SSL, database, runtime, and process problems.
---

# Troubleshooting

Start with the narrowest failing layer: DNS, Nginx, application runtime, deployment, database, or Fluxo itself. Preserve the first meaningful error before restarting services.

## Dashboard is unavailable

```bash
sudo systemctl status fluxo --no-pager
sudo journalctl -u fluxo -n 150 --no-pager
sudo ss -lntp | grep 9595
curl -k https://127.0.0.1:9595/api/v1/health
sudo ufw status verbose
```

If the local health check works but a browser cannot connect, inspect the provider firewall, routing, and source-IP restrictions. If using a reverse proxy, confirm WebSocket forwarding and upstream TLS settings.

For a Fluxo-managed panel domain, also check DNS, the certificate, and the dedicated proxy:

```bash
sudo nginx -t
sudo grep -n "server_name" /etc/nginx/sites-available/fluxo-panel
curl --resolve admin.example.com:443:127.0.0.1 \
  https://admin.example.com/api/v1/health
```

Replace `admin.example.com` with the configured hostname. If the panel-domain proxy is broken but the daemon's local health check succeeds, use the direct server IP and dashboard port to repair or remove the panel domain from **Settings > General**.

An upgrade stops if the configured dashboard port or `127.0.0.1:6060` remains occupied after `systemctl stop fluxo`. Use the PID and command printed by the installer to identify the owning service. Do not kill an unknown process or start `/usr/local/bin/fluxo` manually alongside its systemd service.

## Installer reports a missing SSH privilege-separation directory

Some minimal Ubuntu VPS images expose the OpenSSH server binary before creating its volatile `/run/sshd` runtime directory. Older Fluxo installers then stop safely during preflight with:

```text
ERROR: Unable to evaluate the current SSH server configuration:
Missing privilege separation directory: /run/sshd
```

No Fluxo services or firewall rules have been installed at this point. Confirm that the path is not a symlink, restore it with the ownership and permissions OpenSSH expects, and validate SSH before retrying:

```bash
if sudo test -L /run/sshd; then
  echo "Refusing to replace symlinked /run/sshd" >&2
  exit 1
fi
sudo install -d -o root -g root -m 0755 /run/sshd
sudo sshd -t
```

`sshd -t` produces no output when validation succeeds. Re-run the installer only after that command passes. Current Fluxo installers check this prerequisite first, perform the same safe repair when the directory is absent, and then evaluate the SSH configuration. Other SSH configuration errors still stop installation and must be corrected manually.

## A domain opens the wrong site

1. Confirm the domain's `A` and `AAAA` records point only to this server.
2. Check both HTTP and HTTPS; they may be reaching different proxies or addresses.
3. Confirm Fluxo lists the hostname under the intended site's Domains page.
4. Validate and inspect Nginx:

```bash
sudo nginx -t
sudo nginx -T | less
```

Search for duplicate `server_name` entries. Fluxo installs an unknown-host guard, but a stale manually created Nginx virtual host can still conflict. Remove conflicts through their owning system and reload Nginx only after validation.

Sites created before a hostname-routing fix may still contain old generated configuration. Saving the site's settings or recreating the managed Nginx configuration can apply current templates without deleting application data.

## Deployment failed

Open the persistent failure alert, then read the complete deployment output. Check repository access, branch, runtime versions, lockfiles, environment values, database connectivity, filesystem permissions, and the first command that exited non-zero.

The deployment limit is ten minutes. Build processes killed without a normal application error may indicate memory exhaustion; inspect `journalctl` and kernel logs.

## Node.js site creation is disabled

Open **Runtime > Node.js** in the new tab offered by the form. Install or repair Node.js, npm, pnpm, Yarn, Corepack, and Bun. Return to the existing form and select **Check again**.

If runtime status stays incomplete, inspect the missing-component list and the Fluxo service journal. Node.js must be at least `22.13.0`.

## Python site creation is disabled

Open **Runtime > Python** in the new tab offered by the form. Install or repair Python application support, then return to the existing form and select **Check again**. Fluxo requires Python 3.10 or newer, working `venv` and `ensurepip` modules, and uv.

If the runtime remains incomplete, inspect the missing-component list and `sudo journalctl -u fluxo -n 100 --no-pager`. Fluxo uses Ubuntu's system Python, so a server with an unsupported distribution-provided version must be upgraded to a supported Ubuntu release rather than replacing `/usr/bin/python3` manually.

## Python application returns 502

Open **Site > Processes** and inspect the protected Python process and its logs. Confirm the configured entrypoint exists in the application directory, dependencies were installed into `.venv`, and the process binds to `127.0.0.1:$FLUXO_APP_PORT`. Review the most recent deployment output before restarting the process.

## Node.js toolchain installation appears stalled

The installer reports the active phase and shows percentage checkpoints for large Node.js and Bun downloads. A healthy installation commonly takes one to seven minutes depending on the package mirror, network, disk, and server size; 15 minutes is the overall safety limit, not the expected duration.

Fluxo `v0.4.23` and earlier can appear to stop after `Checking Node.js system prerequisites...` while a silent `apt-get install` waits on package configuration or a package-manager lock. Current releases first inspect `ca-certificates`, `xz-utils`, and `unzip`, skip APT when all three are ready, and print a heartbeat every 15 seconds during required package operations. Required APT commands run noninteractively and stop after a finite lock and command timeout.

On an older release, open a second SSH session and check whether Ubuntu is still running package maintenance. Do not kill an active `apt`, `dpkg`, `unattended-upgrade`, or provider initialization process:

```bash
ps -ef | grep -E '[a]pt|[d]pkg|[u]nattended-upgrade|[c]loud-init'
sudo systemctl status apt-daily.service apt-daily-upgrade.service --no-pager
```

If no package operation is active, finish any interrupted package configuration and install the prerequisites noninteractively before retrying Fluxo:

```bash
sudo dpkg --audit
sudo env DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a dpkg --configure -a
sudo env DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a \
  APT_LISTCHANGES_FRONTEND=none \
  apt-get install -y ca-certificates xz-utils unzip
```

Use `--no-node` to finish provisioning without the optional Node.js toolchain when the server package manager cannot be repaired immediately. Install the toolchain later from **Runtime > Node.js** after resolving the package-manager problem.

Fluxo retries temporary download and npm registry failures up to three times. Direct downloads fail after 90 seconds without receiving data, and the final error distinguishes DNS, connection timeout, TLS, HTTP, interrupted transfer, and local file-write failures. Do not start a second installer while the first is active. If a phase fails, preserve its error message; Fluxo restores the previous managed Node.js toolchain before exiting.

## Node application returns 502

Check the site's managed Node process, its logs, and internal port. The application must bind to `127.0.0.1` on the configured port and remain running.

Confirm no second site uses that port and that the start command matches the built output. A successful static build should use static mode instead of a server process.

Fluxo-managed Node sites run as `fluxo-daemon-<id>.service`; they do not use PM2. If `pm2`, `forever`, or `nodemon` appears in the process tree or daemon command, treat it as an external setup that Fluxo cannot restart or roll back. Keep PM2 state under `/home/fluxo` and owned by `fluxo`; do not recursively change `/opt/fluxo` away from root ownership.

## Let's Encrypt failed

Check DNS from an external resolver, inbound port 80, IPv6, CDN proxy behavior, and Certbot output. Every requested hostname must resolve to and reach this server.

For custom or cloned certificates, confirm the SAN list covers the exact hostname and the private key matches. Fluxo intentionally omits incompatible certificates from the clone list.

## WordPress installer cannot connect to the database

Confirm MariaDB/MySQL is running, the attached database still exists, and `wp-config.php` contains the expected database, user, password, and `127.0.0.1` host. Test with a non-destructive WP-CLI command from the site's Commands page.

Do not replace WordPress commands with Laravel `php artisan` commands.

## CPU load remains 0.00

Linux load averages can legitimately remain zero on an idle server. Generate real work and refresh. If memory, disk, uptime, and all other metrics also fail, inspect the Fluxo API response and daemon journal.

## Site deletion is interrupted

The site remains visible with a deletion stage and stored intent. Correct the reported external failure, such as an unavailable database engine, then select **Retry deletion**. Do not manually delete the site row or managed files between retries.

## Database or backup action failed

Verify the engine service, local socket, credentials, available disk, and active backup state. For S3/R2, run the destination connection test and verify read, write, delete, bucket, region/account, and prefix permissions.

## Ask for help

Open a [GitHub issue](https://github.com/FabioTECH1/fluxo/issues) with:

- Fluxo version and server OS
- Application type and deployment mode
- Exact steps and expected behavior
- Sanitized error output
- Relevant service status

Remove domains when necessary and always remove tokens, passwords, private keys, `.env`, `wp-config.php`, and database credentials.
