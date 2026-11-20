---
title: Installation
description: Install Fluxo interactively or with unattended provisioning flags.
---

# Installation

Public latest remains **v0.4.51**. The recommended final bridge for future clean-base entry is **[v0.4.52](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.52)**, published immutably off latest; exact-public native installer acceptance remains pending. The future base described below is under development and has no approved production release or promotion. The opt-in private `v0.4.52-base-test.2` trial is a separate base prerelease with its original policy.

Run the installer on the server as root:

```bash
curl -fsSL https://fluxo.fabiotech.dev/install.sh | sudo bash
```

Before changing the host, the script verifies the operating system, architecture, release artifact and provenance, installation mode, existing Fluxo service and SQLite schema, `fluxo` account, effective SSH port, and effective UFW state. Ambiguous legacy installations or inconsistent UFW state stop the installer without firewall changes. The installer validates the SSH configuration before provisioning. See [Troubleshooting](../reference/troubleshooting) if this check refuses your server.

## Clean base installation and bridge handoff

The candidate base's normal installer first distinguishes a genuinely new server, a completed supported bridge and an existing base installation. This check runs before package, repository, configuration, service or database changes. Partial data, service, account or execution remnants count as an existing installation; missing principal files never imply a new server.

A new server initializes the current database and execution schemas directly. An older installation must first complete this exact published bridge installer:

```bash
curl -fsSL https://github.com/FabioTECH1/fluxo/releases/download/v0.4.52/install.sh | sudo env FLUXO_VERSION=v0.4.52 bash
```

Finish any recovery it reports, then rerun the base release's normal installer. The base verifies the exact v0.4.52 image, completed bridge and preserved current state automatically; no `baseline-bridge certify` or internal Scheduler command is required. Pending operations retain their original exact-image recovery requirements. Later base updates, including already-admitted base-test installations, recognize the saved base installation without returning to the bridge based on version ordering. The bridge attempts historical site-account link restoration automatically and without blocking installation; deployment-key repair remains in Source Control. A refused upgrade preserves existing state and prints these bridge/retry instructions. See [Upgrade Fluxo](./upgrade.md) for interruption handling and [Baseline bridge](../operations/baseline-bridge) for the retained evidence contracts.

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
| `--node` | Add Node.js, npm, pnpm, Yarn, Corepack, and Bun; candidate base updates verify and preserve an existing managed toolchain |
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
curl -fsSL https://fluxo.fabiotech.dev/install.sh | sudo bash -s -- --reinstall --node --no-python
```

Keep any release selection and other options from your original command. `--reinstall` alone selects the latest release; use an explicit `FLUXO_VERSION` to repair a specific published version. These flags repeat your choices; they do not indicate that the selected components were installed successfully. Do not run two installers simultaneously.

A resumed run performs the final daemon, configured panel-domain, and previously active managed Node.js application checks before reporting success. Missing upgrade snapshots or optional cleanup failures do not suppress the final summary. If first login is still pending, the installer displays the saved bootstrap token; if you already claimed the administrator account, it directs you to your existing login. If credentials cannot be read safely, the summary includes recovery guidance.

## Install a specific release

Set `FLUXO_VERSION` to a published tag:

```bash
curl -fsSL https://fluxo.fabiotech.dev/install.sh | \
  FLUXO_VERSION=v0.4.51 sudo -E bash
```

Advanced installers can override `FLUXO_GITHUB_REPO`, `FLUXO_BINARY_URL`, and `FLUXO_BINARY_SHA256_URL`. A custom binary URL must be accompanied by a checksum URL and the explicit `--skip-release-attestation` acknowledgement. A local binary selected with `--local-binary` is treated as locally trusted. For the base installer, also supply its `FLUXO_LOCAL_BINARY_SHA256`; the installer checks it against the protected candidate before privileged inspection.

For published releases from `v0.4.10` onward, the installer downloads the release's public attestation bundle and enforces GitHub's cryptographically signed build provenance before executing the binary. It constrains the repository, tag, signing workflow, hosted runner, artifact digest, and transparency-log proof. No GitHub account or token is required. The temporary GitHub CLI verifier is not installed on the server; its exact version and Linux hashes are pinned in the installer. Older published releases remain available with checksum verification and a compatibility warning because no historical attestation exists.

After downloading a release asset manually, verify its provenance with the GitHub CLI:

```bash
curl -fsSLO https://github.com/FabioTECH1/fluxo/releases/download/v0.4.51/fluxo-release-attestation.json
gh attestation verify fluxo-linux-amd64 \
  --repo FabioTECH1/fluxo \
  --bundle fluxo-release-attestation.json \
  --signer-workflow FabioTECH1/fluxo/.github/workflows/release.yml \
  --source-ref refs/tags/v0.4.51 \
  --deny-self-hosted-runners
```

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

A healthy API returns exactly `{"status":"ok"}`. The installer also requires `/api/v1/version` to report the candidate release before committing an upgrade.

::: warning Keep SSH open
When using `--harden-ssh`, confirm a second key-authenticated root session works before running the installer and keep the first session open until installation completes. Without that flag, a fresh installation does not change SSH authentication. Fluxo always allows the effective SSH port before enabling a new UFW policy.
:::


### PHP repository selection

Current releases automatically prepare the official Ubuntu PHP repository at `packages.sury.org/php` on fresh installs and ordinary installer upgrades, using a release-pinned signing key scoped to that source. Recognized legacy PHP PPA entries are backed up and disabled. Ubuntu distribution sources and unrelated custom package priorities remain intact; overlapping native-PHP pins require reconciliation; existing native PHP branches keep Ubuntu, ESM and ESMApps security candidates.

The internal review, staged refresh, locks, recovery authorization and resumable publication run without a separate user confirmation. Existing installations preserve installed PHP package versions and CLI selection: installer dependency transactions reject PHP archives before dpkg starts and disallow removals. PHP patches use **Runtime > PHP** separately. Fresh installations still provision PHP normally.

`--migrate-php-repository` retries repository setup when the selected Fluxo release is already installed; it is no longer required for ordinary upgrades. Normal healthy same-version runs remain a no-op. **Runtime > PHP > Review repair** and **Resume repository setup** handle exceptional or interrupted setups. Selecting an older Fluxo binary retains that release's legacy repository contract.

Unsupported source layouts or missing authenticated extension archives require manual reconciliation. Do not replace Ubuntu package sources with Debian suites or disable signature verification.
