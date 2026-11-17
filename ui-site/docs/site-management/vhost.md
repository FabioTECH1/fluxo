---
title: Nginx vhost editor
description: Safely customize a site's complete Nginx virtual host and restore Fluxo's generated default.
---

# Nginx vhost editor

Open **Site > Settings > Vhost** to inspect the complete Nginx configuration Fluxo currently expects for a site. A new site starts in **Fluxo managed** mode, where the displayed configuration is generated from its application type, domains, certificates, runtime, and directory settings.

## Save a custom vhost

Edit the configuration and select **Save Vhost**. Fluxo asks for confirmation before making a server change. After confirmation it:

1. Stages the complete replacement at the site's existing infrastructure path.
2. Runs `nginx -t` against the full server configuration.
3. Reloads Nginx only when validation succeeds.
4. Persists the custom vhost only after successful activation.
5. Restores the last working file and reloads it if validation or activation fails.

The editor accepts at most 256 KiB. It detects stale browser tabs through a configuration revision and returns `409 Conflict` instead of overwriting a newer change.

After the first successful edit, the site is marked **Customized**. Fluxo stores that exact configuration in SQLite and reapplies it whenever an existing domain, SSL, PHP, Octane, or other site action requests Nginx regeneration. Upgrading the Fluxo binary does not delete the override.

::: warning Managed settings while customized
Fluxo preserves a custom vhost exactly. Later dashboard changes to domains, certificates, paths, PHP, or the application runtime are recorded, but their generated directives are not inserted into the custom text. Review and apply the equivalent Nginx changes yourself, or restore the Fluxo default to generate a current configuration from those settings.
:::

Keep the `/.well-known/acme-challenge/` location reachable when using Let's Encrypt. Certbot can continue renewing an existing certificate at the same paths, but HTTP-01 issuance and renewal can fail when the custom vhost blocks its challenge directory.

Fluxo supplies `server_names_hash_bucket_size 512` through `/etc/nginx/conf.d/fluxo-http-defaults.conf` when Nginx has no explicit setting. This accommodates complete DNS names. An operator's existing valid explicit value is preserved. To move the setting elsewhere, remove that managed default before validating so the directive is not duplicated. If a long domain fails with a server-name hash error under a custom smaller value, increase that HTTP-context setting yourself and validate with `sudo nginx -t`; do not add the directive to an individual server block.

An interrupted HTTP-default update retains its recovery data in the private `.fluxo-http-defaults-transaction` directory under `conf.d`. Fluxo processes that recovery before publishing another default. If an operator replacement conflicts with restoration, it preserves both files and reports their paths for review. Do not delete retained recovery data to make an upgrade or certification pass.

## Discard unsaved changes

**Discard Changes** restores the editor to the last configuration loaded from Fluxo without touching Nginx. Fluxo also warns before leaving the page with unsaved changes. `Ctrl+S` or `Cmd+S` opens the same save confirmation as the button; undo and redo shortcuts are supported inside the editor.

## Restore Fluxo's default

When a custom vhost is active, select **Restore Fluxo Default**. After confirmation, Fluxo generates a fresh default from the site's current settings—not a stale copy from when customization began. The generated configuration is validated and activated before the stored override is removed.

If generation, validation, reload, or persistence fails, the custom vhost remains active or is automatically reactivated. Copy any custom directives you may need later before completing a successful restore because the override is removed afterward.

## Recovery

A syntactically valid configuration can still route traffic incorrectly. Retain root SSH or provider-console access before editing. If application traffic is unavailable, inspect the site vhost and Nginx logs, run `sudo nginx -t`, then use **Restore Fluxo Default** or correct the file through SSH.
