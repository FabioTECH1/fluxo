---
title: Installation
description: Install Fluxo interactively or with unattended provisioning flags.
---

# Installation

Run the installer on the server as root:

```bash
curl -fsSL https://fluxo.fottify.com/install.sh | sudo bash
```

The script installs the required web stack, lets you choose optional components, downloads the matching Fluxo release for the server architecture, verifies its SHA256 checksum, and starts the `fluxo` systemd service.

## What the installer changes

The installer:

1. Installs Nginx, PHP 8.4 and common extensions, Certbot, Composer, WP-CLI, Git, UFW, and Fail2Ban.
2. Optionally installs the Node.js toolchain, MariaDB/MySQL, PostgreSQL, and Redis.
3. Allows ports 22, 80, 443, and 9595 through UFW.
4. Creates the `fluxo` system user and configures key-only SSH access.
5. Installs the Fluxo binary at `/usr/local/bin/fluxo`.
6. Creates and starts `fluxo.service`.
7. Generates initial credentials and waits for the health endpoint.

Composer and WP-CLI are installed globally and verified before installation. They are command-line programs and consume storage but no idle RAM.

## Unattended installation

Pass flags after `bash -s --` to avoid interactive component prompts:

```bash
curl -fsSL https://fluxo.fottify.com/install.sh | sudo bash -s -- \
  --db-engine=mysql \
  --redis \
  --node
```

| Flag | Meaning |
|---|---|
| `--db-engine=mysql` | Install MariaDB/MySQL |
| `--db-engine=postgres` | Install PostgreSQL |
| `--db-engine=both` | Install both database engines |
| `--db-engine=none` | Do not install a database engine |
| `--redis` | Install Redis |
| `--no-redis` | Skip Redis |
| `--node` | Install Node.js, npm, pnpm, Yarn, Corepack, and Bun |
| `--no-node` | Skip the Node.js toolchain |

If a component is skipped, install it later from the Runtime section of the dashboard. Creating a Node.js site remains disabled until the full Node.js toolchain is ready. WordPress site creation requires MariaDB/MySQL.

## Install a specific release

Set `FLUXO_VERSION` to a published tag:

```bash
curl -fsSL https://fluxo.fottify.com/install.sh | \
  FLUXO_VERSION=v0.4.10 sudo -E bash
```

Advanced installers can override `FLUXO_GITHUB_REPO`, `FLUXO_BINARY_URL`, and `FLUXO_BINARY_SHA256_URL`. A custom binary URL must be accompanied by a checksum URL.

## Open the dashboard

After installation, visit:

```text
https://YOUR_SERVER_IP:9595
```

Accept the initial self-signed certificate warning, then follow [First login](./first-login.md).

## Verify the service

```bash
sudo systemctl status fluxo --no-pager
sudo journalctl -u fluxo -n 100 --no-pager
curl -k https://127.0.0.1:9595/api/v1/health
```

A healthy API returns a response containing `ok`.

::: warning Keep SSH open
Confirm a second SSH session works before closing the first installation session. Fluxo enables UFW and key-only SSH authentication; losing the only working session can require provider-console recovery.
:::

