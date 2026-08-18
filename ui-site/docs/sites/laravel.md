---
title: Laravel Sites
description: Provision, deploy, and operate Laravel applications with Fluxo.
---

# Laravel sites

Laravel sites use PHP-FPM, a `/public` web directory, Composer-aware deployments, and Laravel-specific operational features.

## Create the site

1. Select **Laravel** as the application type.
2. Choose an installed PHP version.
3. Optionally attach a MySQL/MariaDB or PostgreSQL database with a dedicated application username and password.
4. Select a GitHub repository and branch.
5. Keep zero-downtime enabled for a conventional PHP-FPM application, or disable it if the application must run in place.
6. Create the site and run its first deployment.

During provisioning, Fluxo prepares the environment and generates an application key when one is missing. The default deployment installs Composer dependencies when `composer.json` exists, builds frontend assets with npm when `package.json` exists, clears optimized caches, creates the storage link, runs forced migrations, and activates the release.

Fluxo verifies an attached database account before creating the site and writes that least-privilege account to `.env`. The database control-plane account is not available for application use.

## Environment

Open **Settings > Environment** to edit the shared `.env`. In zero-downtime mode, each release links to the shared environment file so secrets and environment-specific values survive deployments.

Confirm at least:

- `APP_ENV`, `APP_URL`, and `APP_DEBUG`
- Database connection values
- Cache, queue, session, and mail drivers
- Third-party API credentials

Do not commit the production `.env` to Git.

## Persistent storage

Zero-downtime deployments share `storage/app` between releases and run `artisan storage:link --force`. Application uploads should live in Laravel-managed persistent storage rather than inside a release directory.

If your application writes to another directory, adapt the deployment script to link that directory from the site root before activation.

## Laravel features

Fluxo inspects the active `composer.lock`. Laravel controls appear only when Laravel and the corresponding packages are detected:

- Scheduler
- Maintenance mode
- Laravel Horizon
- Laravel Nightwatch
- Laravel Octane

See [Laravel features](../site-management/laravel-features.md) for package and deployment restrictions.

## Octane

Octane is a package-driven feature, not a separate site type. It is available only with a standard in-place deployment because long-lived workers do not fit Fluxo's current release-symlink model. Disable zero-downtime deployment before enabling Octane.

When enabled, Fluxo creates the Octane process and adds an Octane reload hook to the managed deployment workflow.

## Troubleshooting

Use **Observe > Logs** for site Nginx and PHP logs, **Commands** for safe one-off Artisan checks, and **Deployments** for complete build output. A failed deployment remains visible across the site until dismissed or superseded by a successful deployment.
