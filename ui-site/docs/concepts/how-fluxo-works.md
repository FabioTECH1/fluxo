---
title: How Fluxo Works
description: Understand the Fluxo daemon, dashboard, system user, and managed server resources.
---

# How Fluxo works

Fluxo is installed directly on the server it manages. The installation consists of one Go daemon, an embedded Vue dashboard, a SQLite control database, a systemd service, and the system packages selected during provisioning.

## Request flow

1. Your browser connects directly to the Fluxo daemon on its dashboard port, or through the optional HTTPS panel-domain proxy on port `443`.
2. The embedded dashboard calls the daemon's authenticated REST API.
3. The daemon validates input and updates Fluxo's SQLite records.
4. Privileged services apply the requested Nginx, systemd, UFW, PHP, database, or filesystem change.
5. Long-running operations record activity and stream deployment output over WebSockets.

The dashboard is not a remote SaaS dependency. Fluxo and its control data remain on your server.

## Important identities

### Root

Root installs and upgrades Fluxo, owns the systemd service, and can run account-recovery commands. The web dashboard does not give you an unrestricted root shell.

### The `fluxo` system user

Sites live under `/home/fluxo`. Deploy scripts, site commands, daemons, cron jobs, and Git operations run with the permissions assigned to this user. SSH keys managed by Fluxo are installed for this identity.

### The Fluxo administrator

Fluxo currently uses a single administrator account. On first login, the username you provide claims the bootstrap account. The password begins as a generated bootstrap token and can later be changed in Settings.

## Control data and application data

Fluxo's SQLite database records configuration and history such as sites, deployments, certificate metadata, daemons, scheduled jobs, connected GitHub accounts, and backup plans. It is separate from the databases used by hosted applications.

Application files, database contents, certificate material, and service configuration remain system resources. Back up both the Fluxo data directory and the applications you host.

## Managed and user-owned configuration

Fluxo generates site virtual hosts, the optional panel-domain proxy, PHP-FPM pools, service units, deployment scripts, and certificates through explicit dashboard actions. Avoid editing generated files directly unless the documentation identifies them as user-editable; a later Fluxo action may regenerate managed configuration. Use **Site > Settings > Vhost** for a durable site-specific Nginx override that can also be restored to Fluxo's current generated default.

Deployment scripts and environment files are intentionally editable from the site dashboard. Fluxo supplies defaults, but they remain part of your application's operating configuration.

## Site isolation

Every site has its own record, root directory, domains, deployment history, and optional processes. PHP sites receive an application-specific Nginx virtual host and PHP-FPM configuration. Server-rendered Node.js sites receive an internal application port and a managed service behind Nginx. Python sites add an isolated `.venv`, framework-aware dependency commands, and a managed Gunicorn, Uvicorn, or custom process behind Nginx.

Fluxo uses one shared Linux system user rather than creating a Linux user for every site. Treat anyone with write access to that account as trusted across hosted applications.
