---
title: WordPress Sites
description: Create and manage WordPress sites with WP-CLI, MySQL, and an editable wp-config.php.
---

# WordPress sites

Fluxo installs WordPress in place with WP-CLI. Git is optional in many professional WordPress workflows and is not required by Fluxo's WordPress site creation flow.

## Prerequisites

- MariaDB/MySQL is installed and running.
- An installed PHP version is available.
- The domain points to the server before requesting SSL.

If MySQL is missing, install it from **Runtime > Databases**. WordPress site creation remains blocked until a MySQL-compatible database engine is available.

## Create the site

1. Select **WordPress**.
2. Choose the PHP version.
3. Select an unassigned MySQL/MariaDB database or create one.
4. Confirm the web directory, normally `/public`.
5. Create the site.

Fluxo downloads WordPress, generates `wp-config.php` with the selected database credentials and unique salts, adds forwarded-HTTPS handling, configures Nginx and PHP-FPM, and secures the site directory ownership.

## Complete WordPress setup

Visit the domain after provisioning. WordPress opens its browser installer, where you select the language and create the WordPress site title and administrator account.

The WordPress administrator is separate from the Fluxo administrator. Fluxo does not create, store, or recover the WordPress admin password.

## WordPress configuration

Open **Settings > WordPress** to inspect or edit `wp-config.php`. This is the source of database credentials, salts, table prefix, debugging constants, reverse-proxy HTTPS handling, and custom WordPress constants.

The general **Environment** and **Deployments** pages remain available for consistency, but both are empty by default for WordPress. WordPress does not use a Laravel-style `.env`, and Fluxo does not assume a Git deployment pipeline.

::: warning Editing wp-config.php
A syntax error can take the site offline. Keep the required `ABSPATH` and `wp-settings.php` lines, and do not expose database credentials in logs or support messages.
:::

## WP-CLI commands

Run commands from the site's **Commands** page, for example:

```bash
wp core version
wp plugin list
wp theme list
wp cache flush
```

When a WP-CLI command does not contain `--path`, Fluxo automatically appends the site's WordPress web directory. Do not prefix WordPress commands with `php artisan`.

## Updates and deployments

WordPress is managed in place. Update core, plugins, and themes through WordPress or WP-CLI, and take a backup first. If you operate a Git-managed WordPress project, add a deliberate custom deployment script after creation rather than assuming Laravel or Node defaults.

## Database deletion

Deleting a WordPress site does not automatically drop its attached database. The confirmation dialog offers an explicit database-deletion checkbox. Only selected attached databases are dropped; database users are preserved.

