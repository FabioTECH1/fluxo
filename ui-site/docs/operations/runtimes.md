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

**Install toolchain** installs a compatible Node.js LTS release and the package-manager tools. **Repair toolchain** fills missing or incompatible components for servers that already had Node.js. Legacy Fluxo installations can use Repair to add pnpm, Yarn, Corepack, and Bun without recreating sites. Fluxo checks the required Ubuntu packages first and avoids an APT refresh when they are already installed; when packages are missing, installation is noninteractive and uses bounded lock and command waits.

Release builds bind architecture-specific Node.js and Bun hashes plus Corepack, pnpm, and Yarn package integrity values into the Fluxo binary. Runtime installation verifies those values before activation and durably snapshots the previous Fluxo-managed binaries, links, state, and Corepack selection. Ordinary failures restore immediately; after a process or machine interruption, Fluxo restores the snapshot on startup before serving the dashboard.

Fluxo reserves `/opt/fluxo` for root-owned runtime files. Its directories and executables remain readable and executable by the `fluxo` account but must not be recursively assigned to that account. User-owned package-manager state belongs under `/home/fluxo`. Installation verifies both ownership boundaries and runs every advertised tool as `fluxo` before activation.

**Restart applications** restarts Node.js and Bun application processes managed by Fluxo; it does not restart a single global Node daemon because Node itself is an executable, not a server service.

**Remove toolchain** is available only when Fluxo owns the installation. It refuses removal while Node.js sites still depend on it and does not delete externally managed installations, site files, or package caches.

Fluxo does not install or operate PM2, Forever, or Nodemon. Applications using an external process manager do not participate in Fluxo's restart, health-check, or upgrade rollback workflow.

Installed package managers consume disk space but no idle RAM. RAM is used when their commands or hosted applications are running.

## Python application support

The Python page groups the components required to host Python sites:

- Ubuntu's system `python3` and its version
- `venv` and `ensurepip` support for isolated site environments
- pip availability
- Fluxo's release-pinned and checksum-verified uv binary

Fluxo requires Python 3.10 or newer. **Install support** installs supported Ubuntu packages and Fluxo's verified uv release. **Repair support** fills missing components. Existing system Python packages are not replaced with an independently managed interpreter.

Every Python site has its own `.venv`; packages are not installed globally. **Restart applications** restarts all Fluxo-managed Python site processes. **Remove managed tools** is refused while Python sites exist and removes only Fluxo-managed uv files. Ubuntu's system Python, application files, and site virtual environments remain untouched.

The Python interpreter and command-line tools use storage but no idle RAM. Each running Gunicorn, Uvicorn, or custom application process uses RAM according to the application and worker configuration. See [Python sites](../sites/python.md) for presets, dependency handling, and deployments.

## Nginx

The Nginx runtime page shows version, service status, configuration paths, and restart controls. Fluxo validates generated configuration before reloading it during site, domain, or SSL changes.

If Nginx fails to restart, inspect its error log and run `sudo nginx -t` over SSH before changing more configuration.

## Database engines

The database runtime page reports MariaDB/MySQL, PostgreSQL, and Redis status and versions. Missing supported engines can be installed from the dashboard. Installed services can be started, stopped, or restarted.

Stopping an engine immediately affects every application using it. Database engine removal is intentionally different from deleting an individual application database.
