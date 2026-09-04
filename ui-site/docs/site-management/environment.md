---
title: Environment and WordPress Config
description: Edit site environment files, control deployment exposure, and manage wp-config.php.
---

# Environment and WordPress config

Fluxo provides editors for application configuration that must remain on the server.

## Environment file

Open **Site > Settings > Environment** to read and update the site's root `.env` file. Saving replaces the file contents used by the application.

In managed zero-downtime deployments, Fluxo links this persistent file into each new release. In standard deployments, it remains an untracked file in the site root and is deliberately preserved when tracked Git files are reset.

Python systemd services also load the persistent `.env` directly. A Python site with a nested application directory receives a symlink from that directory back to the root file, while process-level values are available without requiring an application-specific dotenv package.

## Deployment exposure

By default, the environment exists as a file but its values are not exported into the deployment shell. Enable environment exposure only when build commands must read values directly from process environment.

Fluxo filters environment syntax and reserves names beginning with `FLUXO_`. Those variables describe the deployment lifecycle and cannot be replaced by application content.

::: warning Build-time secrets
Frontend frameworks can embed environment values into browser assets. Only expose variables intended for the build, and follow the framework's rules for public prefixes.
:::

## Apply changes

- PHP applications may read `.env` per request or cache configuration. Clear or rebuild framework caches after changing values.
- Long-running Node.js, Python, Queue Worker, Horizon, Octane, and custom daemon processes normally need a restart. Fluxo handles deploy-time restarts for its managed application and feature processes.
- Build-time variables require a new deployment.

## WordPress configuration

WordPress does not use `.env` by default. Its dedicated **WordPress** settings page edits the active `wp-config.php` in the configured web directory.

Fluxo-generated WordPress configuration includes database settings, unique salts, the table prefix, forwarded-HTTPS handling, debugging defaults, `ABSPATH`, and the required `wp-settings.php` include.

Use the WordPress editor for constants such as `WP_DEBUG`, cache configuration, multisite settings, and application-specific values. Avoid duplicating WordPress database credentials into an unused `.env` file.

## Secret handling

Do not paste environment or WordPress configuration into public issue reports. Remove credentials from deployment logs and command output before sharing them. Maintain a secure independent copy of values needed for disaster recovery.
