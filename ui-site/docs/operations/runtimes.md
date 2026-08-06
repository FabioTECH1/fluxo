---
title: Runtimes
description: Install, inspect, configure, restart, and remove Fluxo server runtimes.
---

# Runtimes

The Runtime section controls server-wide software used by hosted sites. Runtime changes can affect every application using that service, so schedule restarts and removals carefully.

## PHP

The PHP page lists installed PHP-FPM versions and service state. You can:

- Install an available PHP version
- Set the default PHP version used for new sites
- Set the server CLI default
- Start, stop, or restart a version's PHP-FPM service
- Edit supported `php.ini` limits
- Remove an unused version

Existing sites keep their selected PHP version when the default changes. PHP removal is a server-wide package operation, so manually migrate and test every dependent site before removing a version.

Site commands and managed deployment scripts use version-specific PHP and Composer variables, avoiding accidental use of the wrong CLI version.

## Node.js toolchain

The Node.js page presents one grouped toolchain, not six unrelated services. It reports:

- Node.js and its binary path
- npm
- pnpm
- Yarn
- Corepack
- Bun
- Missing components and the minimum supported Node.js version

Fluxo requires Node.js `22.13.0` or newer and all listed package managers before Node site creation is enabled.

**Install toolchain** installs a compatible Node.js LTS release and the package-manager tools. **Repair toolchain** fills missing or incompatible components for servers that already had Node.js. Legacy Fluxo installations can use Repair to add pnpm, Yarn, Corepack, and Bun without recreating sites.

Release builds bind architecture-specific Node.js and Bun hashes plus Corepack, pnpm, and Yarn package integrity values into the Fluxo binary. Runtime installation verifies those values before activation and durably snapshots the previous Fluxo-managed binaries, links, state, and Corepack selection. Ordinary failures restore immediately; after a process or machine interruption, Fluxo restores the snapshot on startup before serving the dashboard.

Fluxo reserves `/opt/fluxo` for root-owned runtime files. Its directories and executables remain readable and executable by the `fluxo` account but must not be recursively assigned to that account. User-owned package-manager state belongs under `/home/fluxo`. Installation verifies both ownership boundaries and runs every advertised tool as `fluxo` before activation.

**Restart applications** restarts Node.js and Bun application processes managed by Fluxo; it does not restart a single global Node daemon because Node itself is an executable, not a server service.

**Remove toolchain** is available only when Fluxo owns the installation. It refuses removal while Node.js sites still depend on it and does not delete externally managed installations, site files, or package caches.

Fluxo does not install or operate PM2, Forever, or Nodemon. Applications using an external process manager do not participate in Fluxo's restart, health-check, or upgrade rollback workflow.

Installed package managers consume disk space but no idle RAM. RAM is used when their commands or hosted applications are running.

## Nginx

The Nginx runtime page shows version, service status, configuration paths, and restart controls. Fluxo validates generated configuration before reloading it during site, domain, or SSL changes.

If Nginx fails to restart, inspect its error log and run `sudo nginx -t` over SSH before changing more configuration.

## Database engines

The database runtime page reports MariaDB/MySQL, PostgreSQL, and Redis status and versions. Missing supported engines can be installed from the dashboard. Installed services can be started, stopped, or restarted.

Stopping an engine immediately affects every application using it. Database engine removal is intentionally different from deleting an individual application database.
