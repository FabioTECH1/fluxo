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

## Node application returns 502

Check the site's managed Node process, its logs, and internal port. The application must bind to `127.0.0.1` on the configured port and remain running.

Confirm no second site uses that port and that the start command matches the built output. A successful static build should use static mode instead of a server process.

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

