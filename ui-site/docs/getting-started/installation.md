---
title: Installation
description: Install Fluxo interactively or with unattended provisioning flags.
---

# Installation

Run the installer on the server as root:

```bash
curl -fsSL https://fluxo.fottify.com/install.sh | sudo bash
```

Before changing the host, the script verifies the operating system, architecture, release artifact and provenance, installation mode, existing Fluxo service and SQLite schema, `fluxo` account, effective SSH port, and effective UFW state. Ambiguous legacy installations or inconsistent UFW state stop the installer without firewall changes.

## What the installer changes

The installer:

1. Installs Nginx, PHP 8.4 and common extensions, Certbot, Composer, WP-CLI, Git, UFW, and Fail2Ban.
2. Optionally installs the Node.js toolchain, MariaDB/MySQL, PostgreSQL, and Redis.
3. On a fresh server with inactive UFW, allows the detected SSH port, 80, 443, and 9595 before enabling it. The exact rules created by the installer are imported as Fluxo-managed rules. An existing installation's active or inactive UFW policy is preserved unchanged and is not claimed by Fluxo.
4. Creates and validates the `fluxo` system user. SSH hardening is enabled only when explicitly requested or when maintaining an existing Fluxo hardening file.
5. Installs the Fluxo binary at `/usr/local/bin/fluxo`.
6. Creates and starts `fluxo.service`.
7. Generates initial credentials and waits for exact healthy and matching-version API responses.

Composer and WP-CLI are installed globally. Their baseline versions are embedded in each Fluxo release, and the installer verifies their downloaded contents and reported versions before activating them. Node.js and Bun architecture hashes and npm integrity values for Corepack, pnpm, and Yarn are embedded in the same signed release. Fluxo then schedules a weekly update to the latest stable Composer 2 release; WP-CLI remains at the release-selected version until a Fluxo upgrade. These command-line tools consume storage but no idle RAM.

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
| `--harden-ssh` | Disable root password authentication after validating an authorized key and the staged effective policy |
| `--no-harden-ssh` | Do not change SSH authentication policy |
| `--management-cidr=<cidr>` | Restrict a newly created port `9595` rule, for example `203.0.113.4/32` |
| `--skip-release-attestation` | Explicitly trust a custom `FLUXO_BINARY_URL` that cannot carry Fluxo release provenance |

If a component is skipped, install it later from the Runtime section of the dashboard. Creating a Node.js site remains disabled until the full Node.js toolchain is ready. WordPress site creation requires MariaDB/MySQL.

## Install a specific release

Set `FLUXO_VERSION` to a published tag:

```bash
curl -fsSL https://fluxo.fottify.com/install.sh | \
  FLUXO_VERSION=v0.4.10 sudo -E bash
```

Advanced installers can override `FLUXO_GITHUB_REPO`, `FLUXO_BINARY_URL`, and `FLUXO_BINARY_SHA256_URL`. A custom binary URL must be accompanied by a checksum URL and the explicit `--skip-release-attestation` acknowledgement. A local binary selected with `--local-binary` is treated as locally trusted.

For published releases from `v0.4.10` onward, the installer downloads the release's public attestation bundle and enforces GitHub's cryptographically signed build provenance before executing the binary. It constrains the repository, tag, signing workflow, hosted runner, artifact digest, and transparency-log proof. No GitHub account or token is required. The temporary GitHub CLI verifier is not installed on the server; its exact version and Linux hashes are pinned in the installer. Older published releases remain available with checksum verification and a compatibility warning because no historical attestation exists.

After downloading a release asset manually, verify its provenance with the GitHub CLI:

```bash
curl -fsSLO https://github.com/FabioTECH1/fluxo/releases/download/v0.4.10/fluxo-release-attestation.json
gh attestation verify fluxo-linux-amd64 \
  --repo FabioTECH1/fluxo \
  --bundle fluxo-release-attestation.json \
  --signer-workflow FabioTECH1/fluxo/.github/workflows/release.yml \
  --source-ref refs/tags/v0.4.10 \
  --deny-self-hosted-runners
```

## Open the dashboard

After installation, visit:

```text
https://YOUR_SERVER_IP:9595
```

Accept the initial self-signed certificate warning, then follow [First login](./first-login.md).

The default newly created UFW rule allows dashboard access from any source so you can use Fluxo across your devices. Authentication, TLS, application rate limiting, and Fail2Ban still apply. To restrict a fresh server to one trusted network instead, pass `--management-cidr`, for example:

```bash
curl -fsSL https://fluxo.fottify.com/install.sh | sudo bash -s -- \
  --management-cidr=203.0.113.4/32
```

This option applies only when the installer creates a new UFW policy. Fluxo refuses to rewrite an existing active policy.

## Verify the service

```bash
sudo systemctl status fluxo --no-pager
sudo journalctl -u fluxo -n 100 --no-pager
curl -k https://127.0.0.1:9595/api/v1/health
```

A healthy API returns exactly `{"status":"ok"}`. The installer also requires `/api/v1/version` to report the candidate release before committing an upgrade.

::: warning Keep SSH open
When using `--harden-ssh`, confirm a second key-authenticated root session works before running the installer and keep the first session open until installation completes. Without that flag, a fresh installation does not change SSH authentication. Fluxo always allows the effective SSH port before enabling a new UFW policy.
:::
