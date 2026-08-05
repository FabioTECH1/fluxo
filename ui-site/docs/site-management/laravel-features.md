---
title: Laravel Features
description: Enable Scheduler, Maintenance, Horizon, Nightwatch, and Octane when installed packages are detected.
---

# Laravel features

Fluxo inspects the active `composer.lock` rather than assuming packages from the selected site label. Laravel feature controls appear only when the framework and relevant packages are actually installed in the active application.

After adding a Composer package, deploy successfully and refresh the site Overview so Fluxo can inspect the new lockfile.

## Scheduler

The Scheduler control becomes available when Laravel is detected. Enabling it creates a once-per-minute cron entry that runs the version-specific PHP executable against the active `artisan schedule:run` command.

Define actual schedules inside the Laravel application. Fluxo's cron only wakes Laravel's scheduler every minute.

## Maintenance mode

Maintenance mode uses the active Laravel application's `artisan down` and `artisan up` behavior. Use it for changes that cannot safely run while requests are served.

Maintenance mode is application state, not a substitute for a failed deployment rollback. Confirm privileged bypass or retry behavior according to the Laravel version in use.

## Horizon

Horizon appears when `laravel/horizon` is present in the active lockfile. Enabling it creates a managed Horizon process. Fluxo terminates Horizon after successful managed deployments so Laravel's process monitor starts workers on the new code.

Configure queues, balancing, timeouts, and supervisors in the application before enabling the service.

## Nightwatch

Nightwatch appears when the Laravel Nightwatch package is detected. Enabling it creates the required background agent and assigns an available internal port where needed.

Package configuration and credentials remain the application's responsibility.

## Octane

Octane appears when `laravel/octane` is detected and the site uses standard deployment. Enabling it creates the Octane server process and Nginx proxy configuration on a unique internal port.

Fluxo reloads Octane after successful application deployment. Disable Octane before switching the site to zero-downtime deployment.

::: warning Long-lived workers
Horizon and Octane keep application state in memory. Ensure services restart or reload after code and configuration changes, and design application code to release request-specific resources.
:::

## Feature does not appear

Check that:

1. The active release contains `composer.lock`.
2. The package was installed into that release.
3. The package name and version are recorded in the lockfile.
4. The latest deployment succeeded.
5. Octane is not hidden by zero-downtime mode.
