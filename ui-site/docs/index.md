---
title: Fluxo Documentation
description: The complete guide to installing, configuring, and operating Fluxo.
---

# Fluxo documentation

Fluxo is a self-hosted web server control panel for provisioning and operating Laravel, PHP, WordPress, Python, Node.js, and static sites. It manages Nginx, PHP-FPM, deployments, certificates, databases, processes, backups, server logs, and firewall rules from one dashboard.

This handbook documents Fluxo `v0.4.27`. Use it to install a new server, understand the deployment model, or troubleshoot an existing installation.

![Fluxo sites dashboard showing managed applications and deployment status](/images/dashboard-sites.png)

<div class="quick-links">
  <a href="./getting-started/installation">Install Fluxo<small>Prepare a server and run the verified installer.</small></a>
  <a href="./sites/">Create a site<small>Choose the correct application type and configuration.</small></a>
  <a href="./deployments/">Deploy an application<small>Configure Git, scripts, zero downtime, and rollbacks.</small></a>
  <a href="./reference/troubleshooting">Troubleshoot<small>Diagnose access, deployment, SSL, runtime, and database problems.</small></a>
</div>

## What Fluxo manages

- Application sites and their Nginx configuration
- Validated per-site Nginx vhost overrides with restoration to generated defaults
- An optional trusted HTTPS panel domain while retaining direct recovery access
- PHP versions, PHP-FPM pools, and PHP settings
- The Node.js toolchain, including npm, pnpm, Yarn, Corepack, and Bun
- Python application support, including isolated virtual environments, pip, and uv
- GitHub accounts, repositories, branches, deploy keys, and webhooks
- Standard and release-based zero-downtime deployments
- Let's Encrypt, custom, existing, and cloned certificates
- MariaDB/MySQL and PostgreSQL databases, users, and grants
- Site daemons, scheduled jobs, commands, files, and logs
- Amazon S3 and Cloudflare R2 backup destinations and plans
- UFW firewall rules, SSH keys, system services, and activity history

## Choose a starting point

| Goal | Read |
|---|---|
| Install a new server | [Installation](./getting-started/installation.md) |
| Sign in for the first time | [First login](./getting-started/first-login.md) |
| Host a Laravel or PHP application | [Laravel](./sites/laravel.md) or [PHP](./sites/php.md) |
| Install WordPress | [WordPress](./sites/wordpress.md) |
| Deploy Next.js, Nuxt, or another Node app | [Node.js](./sites/nodejs.md) |
| Deploy Django, Flask, FastAPI, or another Python app | [Python](./sites/python.md) |
| Understand release directories | [Zero-downtime deployments](./deployments/zero-downtime.md) |
| Configure HTTPS | [Domains and SSL](./site-management/domains-ssl.md) |
| Customize a site's Nginx vhost | [Nginx vhost editor](./site-management/vhost.md) |
| Connect a panel hostname | [Settings and access](./operations/settings.md) |
| Protect site files and databases | [Backups](./operations/backups.md) |

## Product boundaries

Fluxo operates one server per installation. It expects root access during installation and performs privileged server administration through its daemon. Application deployment commands run as the dedicated `fluxo` system user.

Fluxo is not a DNS provider, source control host, email server, container orchestrator, or managed cloud. Configure DNS with your DNS provider, keep source code with GitHub or another Git remote, and retain an independent recovery path to the server.

::: tip Live demo
Explore the dashboard without changing a server at [fluxo.fottify.com/demo/sites](https://fluxo.fottify.com/demo/sites). Destructive and privileged actions are simulated in demo mode.
:::
