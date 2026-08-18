---
title: PHP Sites
description: Host frameworks and custom PHP applications without Laravel-specific assumptions.
---

# PHP sites

Use the PHP site type for Symfony, CodeIgniter, Slim, custom PHP, and other PHP-FPM applications. It shares the PHP and Composer infrastructure used by Laravel but does not blindly run Artisan commands.

## Create the site

1. Select **PHP**.
2. Choose an installed PHP version.
3. Set the web directory expected by the application.
4. Optionally attach a database with a dedicated application username and password.
5. Select a repository and branch when deploying from Git.
6. Choose standard or zero-downtime deployment.

The default web directory is `/`. Change it to `/public` or another subdirectory when the application's entrypoint lives there.

## Composer and frontend assets

The managed deployment script runs Composer only when `composer.json` is present. It runs npm installation and the build script only when `package.json` is present. PHP deployments do not execute `php artisan` unless the site is actually detected as Laravel and a Laravel feature explicitly requires it.

You can edit the deployment script for framework-specific cache warmup, migrations, asset commands, or worker reloads.

## Laravel packages in a PHP site

Fluxo detects Laravel from the active `composer.lock`, not only from the selected app-type label. If a PHP site later becomes a Laravel application, compatible Laravel feature controls can appear after a successful dependency installation.

This detection does not change the site's application type: it remains a **PHP** site, and its provisioning and deployment defaults are not converted automatically. This lets imported or legacy projects keep the PHP site type while still avoiding false Laravel commands before the framework is present. Create a new Laravel site and migrate the application if you need Laravel's full site model.

## Standard versus zero downtime

Standard deployment updates the site directory in place. Zero downtime clones a fresh release, links shared state, completes the build, and switches `current` only after success.

For framework applications, add any writable persistent directories to the zero-downtime script before relying on release activation.
