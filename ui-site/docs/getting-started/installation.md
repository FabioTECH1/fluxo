---
title: Installation
description: Install Fluxo and select the runtimes, databases and network settings for your server.
---

# Installation

Install the latest stable Fluxo release on a fresh server using root or a
sudo-capable SSH account:

```bash
curl -fsSL https://fluxo.fabiotech.dev/install.sh | sudo bash
```

Already running Fluxo? Use [Upgrade Fluxo](./upgrade). Installations that have
not entered the base must first follow [Upgrade an older installation](../operations/baseline-bridge).
The installer checks existing state before provisioning and reports any recovery
that must finish first.

Start with at least 1 GB of RAM and 20 GB of storage. This minimum supports one small low-traffic site and one local database engine when at least 1 GB of swap is configured. Use 2 GB or more for a more comfortable small production server and at least 4 GB for Node.js builds, Redis, multiple databases, or several active sites. See [Requirements](./requirements.md) for detailed sizing guidance.

## What the installer changes

The installer:

1. Installs Nginx, PHP 8.4 and common extensions, Certbot, Composer, WP-CLI, Git, UFW, and Fail2Ban.
2. Optionally installs the Node.js toolchain, Python application support, MariaDB/MySQL, PostgreSQL, and Redis.
3. On a fresh server with inactive UFW, allows the detected SSH port, 80, 443, and 9595 before enabling it. The exact rules created by the installer are imported as Fluxo-managed rules. An existing installation's active or inactive UFW policy is preserved unchanged and is not claimed by Fluxo.
4. Creates and validates the `fluxo` system user. SSH hardening is enabled only when explicitly requested or when maintaining an existing Fluxo hardening file.
5. Installs the Fluxo binary at `/usr/local/bin/fluxo`.
6. Creates and starts `fluxo.service`.
7. Generates initial credentials and waits for exact healthy and matching-version API responses.

Composer and WP-CLI are installed globally. Their baseline versions are embedded in each Fluxo release, and the installer verifies their downloaded contents and reported versions before activating them. Node.js and Bun architecture hashes, npm integrity values for Corepack, pnpm, and Yarn, and architecture-specific uv hashes are embedded in the same signed release. Optional runtime package operations use noninteractive settings and finite command waits. Fluxo then schedules a weekly update to the latest stable Composer 2 release; WP-CLI and uv remain at their release-selected versions until a Fluxo upgrade. These command-line tools consume storage but no idle RAM.

## Unattended installation

Pass flags after `bash -s --` to avoid interactive component prompts:

```bash
curl -fsSL https://fluxo.fabiotech.dev/install.sh | sudo bash -s -- \
  --db-engine=mysql \
  --redis \
  --node \
  --python
```

| Flag | Meaning |
|---|---|
| `--db-engine=mysql` | Install MariaDB/MySQL |
| `--db-engine=postgres` | Install PostgreSQL |
| `--db-engine=both` | Install both database engines |
| `--db-engine=none` | Do not install a database engine |
| `--redis` | Install Redis |
| `--no-redis` | Skip Redis |
| `--node` | Add Node.js, npm, pnpm, Yarn, Corepack, and Bun; base updates verify and preserve an existing managed toolchain |
| `--no-node` | Skip the Node.js toolchain |
| `--python` | Install Python, venv and build prerequisites, pip support, and verified uv |
| `--no-python` | Skip Python application support |
| `--harden-ssh` | Disable root password authentication after validating an authorized key and the staged effective policy |
| `--no-harden-ssh` | Do not change SSH authentication policy |
| `--management-cidr=<cidr>` | Restrict a newly created port `9595` rule, for example `203.0.113.4/32` |
| `--skip-release-attestation` | Explicitly trust a custom `FLUXO_BINARY_URL` that cannot carry Fluxo release provenance |
| `--reinstall` | Reinstall the selected release, including optional component or configuration changes; pin `FLUXO_VERSION` to repair a specific release |

If a component is skipped, install it later from the Runtime section of the dashboard. Creating a Node.js or Python site remains disabled until its runtime page reports ready. WordPress site creation requires MariaDB/MySQL.

## Retry an interrupted installation

The installer saves optional component choices before provisioning. A retry reuses them; explicit flags override them. Selected components are installed after their prerequisites have been verified. Saving a choice does not prove that component setup completed.

Rerun the installer with `--reinstall` after resolving the reported error, so an already installed Fluxo binary does not skip unfinished component/configuration work. A normal run exits early when the selected release is already installed and the dashboard is healthy; see [Upgrade Fluxo](./upgrade.md). The installer prepares the official Sury PHP repository with validation and retained recovery evidence. Dependency download failures identify the URL without its credentials/query parameters, the curl exit code, and the HTTP status. Signature and checksum checks still apply.

The failure output shows flags for reusing your Node.js and Python choices. For example, to select Node.js and skip Python without repeating those prompts:

```bash
sudo curl -fsSL https://fluxo.fabiotech.dev/releases/v0.4.58/install.sh -o /root/fluxo-install-0.4.58.sh &&
sudo env FLUXO_VERSION=v0.4.58 bash /root/fluxo-install-0.4.58.sh --reinstall --node --no-python
```

Keep any release selection and other options from your original command. `--reinstall` alone selects the latest release; use an explicit `FLUXO_VERSION` to repair a specific published version. These flags repeat your choices; they do not indicate that the selected components were installed successfully. Do not run two installers simultaneously.

A resumed run performs the final daemon, configured panel-domain, and previously active managed Node.js application checks before reporting success. Missing upgrade snapshots or optional cleanup failures do not suppress the final summary. If first login is still pending, the installer displays the saved bootstrap token; if you already claimed the administrator account, it directs you to your existing login. If credentials cannot be read safely, the summary includes recovery guidance.

## Install a specific release

Use the matching tagged installer and `FLUXO_VERSION`. To choose another
release, change the tag in both the download URL and environment value:

```bash
sudo curl -fsSL https://fluxo.fabiotech.dev/releases/v0.4.58/install.sh -o /root/fluxo-install-0.4.58.sh &&
sudo env FLUXO_VERSION=v0.4.58 bash /root/fluxo-install-0.4.58.sh
```

Versioned URLs use `/releases/TAG/install.sh`, with a `v`-prefixed stable or
prerelease tag (up to 100 ASCII characters). `GET` and `HEAD` return a `302`
redirect to that tag's original GitHub installer; query parameters are not
forwarded. Invalid tag syntax returns `404`, unsupported methods `405`, and an
unpublished tag fails at GitHub. There is no fallback to the latest script.

Advanced installers can override `FLUXO_GITHUB_REPO`, `FLUXO_BINARY_URL`, and `FLUXO_BINARY_SHA256_URL`. A custom binary URL must be accompanied by a checksum URL and the explicit `--skip-release-attestation` acknowledgement. A local binary selected with `--local-binary` is treated as locally trusted. For the base installer, also supply its `FLUXO_LOCAL_BINARY_SHA256`; the installer checks it against the protected candidate before privileged inspection.

The installer verifies the release checksum and signed GitHub build provenance
before running the binary. No GitHub account or token is required. For a manual
download, verify the binary against the same release's attestation bundle:

```bash
curl -fsSLO https://github.com/FabioTECH1/fluxo/releases/download/v0.4.58/fluxo-release-attestation.json
gh attestation verify fluxo-linux-amd64 \
  --repo FabioTECH1/fluxo \
  --bundle fluxo-release-attestation.json \
  --signer-workflow FabioTECH1/fluxo/.github/workflows/release.yml \
  --source-ref refs/tags/v0.4.58 \
  --deny-self-hosted-runners
```

Use `fluxo-linux-arm64` instead when verifying an ARM64 binary.

## Open the dashboard

After installation, visit:

```text
https://YOUR_SERVER_IP:9595
```

Accept the initial self-signed certificate warning, then follow [First login](./first-login.md).

After signing in, you can connect a trusted HTTPS hostname from **Settings > General > Panel Domain**. Direct access through the server IP and existing dashboard port remains available as a recovery path.

The default newly created UFW rule allows dashboard access from any source so you can use Fluxo across your devices. Authentication, TLS, application rate limiting, and Fail2Ban still apply. To restrict a fresh server to one trusted network instead, pass `--management-cidr`, for example:

```bash
curl -fsSL https://fluxo.fabiotech.dev/install.sh | sudo bash -s -- \
  --management-cidr=203.0.113.4/32
```

This option applies only when the installer creates a new UFW policy. Fluxo refuses to rewrite an existing active policy.

## Verify the service

```bash
sudo systemctl status fluxo --no-pager
sudo journalctl -u fluxo -n 100 --no-pager
curl -k https://127.0.0.1:9595/api/v1/health
```

A healthy API returns exactly `{"status":"ok"}`. The installer also requires `/api/v1/version` to report the selected release before committing an upgrade.

::: warning Keep SSH open
When using `--harden-ssh`, confirm a second key-authenticated root session works before running the installer and keep the first session open until installation completes. Without that flag, a fresh installation does not change SSH authentication. Fluxo always allows the effective SSH port before enabling a new UFW policy.
:::


## PHP repository selection

Current releases automatically prepare the official Ubuntu PHP repository at `packages.sury.org/php` on fresh installs and ordinary installer upgrades, using a release-pinned signing key scoped to that source. Recognized legacy PHP PPA entries are backed up and disabled. Ubuntu distribution sources and unrelated custom package priorities remain intact; overlapping native-PHP pins require reconciliation; existing native PHP branches keep Ubuntu, ESM and ESMApps security candidates.

Existing installations keep their PHP package versions and CLI selection. Apply PHP patches separately through **Runtime > PHP**. Fresh installations provision PHP normally.

`--migrate-php-repository` retries repository setup when the selected Fluxo release is already installed; it is no longer required for ordinary upgrades. Normal healthy same-version runs remain a no-op. **Runtime > PHP > Review repair** and **Resume repository setup** handle exceptional or interrupted setups. Selecting an older Fluxo binary retains that release's legacy repository contract.

Unsupported source layouts or missing authenticated extension archives require manual reconciliation. Do not replace Ubuntu package sources with Debian suites or disable signature verification.
