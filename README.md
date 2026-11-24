# Fluxo

An open-source server control panel licensed under [GNU AGPLv3](LICENSE).

Fluxo is a self-hosted web server control panel inspired by Laravel Forge. It helps you provision servers, manage Laravel, WordPress, PHP, Python, static HTML, and Node.js sites, databases, SSL certificates, cron jobs, daemons, and firewall rules — all from a clean web dashboard.

Try the live demo at **[fluxo.fabiotech.dev](https://fluxo.fabiotech.dev)** — no sign-up required.

Read the complete installation and operations handbook at **[fluxo.fabiotech.dev/docs](https://fluxo.fabiotech.dev/docs/)**.

See the [release notes](CHANGELOG.md) for version-specific fixes and upgrade guidance.

**v0.4.57 adds optional WordPress Cron to the clean base.** New servers install it directly, completed v0.4.55 bridges enter automatically, and existing admitted bases—including base-test installations—update directly.

- **[Traffic Analytics](ui-site/docs/operations/traffic-analytics.md)** — origin request and API reports, collection controls, configurable retention, and bounded history ranges through 365 days or all retained history.
- **[Site Network and wildcard routing](ui-site/docs/site-management/domains-ssl.md)** — managed whole-site password protection, exact-path redirects and wildcard subdomains with separate DNS and certificate requirements.
- **[Nginx update awareness](ui-site/docs/operations/runtimes.md#nginx)** — daily and manual checks against configured APT repositories, with SSH instructions. Package installation remains an operator action.
- **[System settings](ui-site/docs/operations/settings.md#system)** — supported Ubuntu automatic security-update controls, the running kernel, restart-required notices and confirmed manual server restarts with service-status reporting.

**Older installations first complete [v0.4.55](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.55), then install v0.4.57.** New servers and existing bases skip that intermediate step. See [Upgrade an older installation](ui-site/docs/operations/baseline-bridge.md), including interrupted-installation guidance.

Release artifacts are signed and immutable. New artifacts remain off latest until exact-public acceptance and promotion; earlier bridge and base-test tags and assets are retained unchanged.

---

## License

Copyright © 2026 Fottify Software Solutions. Fluxo is licensed under the GNU Affero General Public License v3.0 only (`AGPL-3.0-only`). Commercial use is permitted. Distributed modified versions must remain under AGPLv3, and modified versions used over a network must offer corresponding source to their users. See [LICENSE](LICENSE) for the complete terms. Fluxo is provided without warranty.

Independent applications hosted or managed by Fluxo do not become AGPL-licensed merely by using the panel. Third-party dependencies retain their own licenses. Previously published releases retain their original license terms.

Contributions use the same AGPLv3 license. See [CONTRIBUTING.md](CONTRIBUTING.md).

## Requirements

| | |
|---|---|
| **OS** | Ubuntu 22.04, 24.04 or 26.04 LTS |
| **Architecture** | `amd64` and `arm64`; reviewed Ubuntu profiles |
| **Memory** | 1 GB RAM minimum; 2 GB+ recommended |
| **Storage** | 20 GB minimum; 40 GB+ recommended |
| **Server** | A fresh VPS with root SSH access |

The 1 GB minimum supports Fluxo, one small low-traffic site, and one local database engine when at least 1 GB of swap is configured. Use 2 GB or more for a more comfortable small production server and at least 4 GB for Node.js builds, Redis, multiple databases, or several active sites. Site releases, databases, logs, uploads, and backups require capacity beyond the baseline; see the [complete requirements guide](https://fluxo.fabiotech.dev/docs/getting-started/requirements) for sizing details.

Release-specific platform checks and limits are recorded in the [release evidence](scripts/ci/evidence/base-0.4.57-release.md).

---

## Installation

Run the following command on your server as **root**:

```bash
curl -fsSL https://fluxo.fabiotech.dev/install.sh | sudo bash
```

The script will:

1. Validate Ubuntu, the server architecture, SSH, UFW, and any existing Fluxo installation
2. Collect optional Node.js, Python, database, and Redis choices before changing the host
3. Download the Fluxo binary, verify its SHA256 checksum, and verify signed GitHub build provenance for attested releases
4. Install Nginx, PHP 8.4, Certbot, Composer, WP-CLI, UFW, Fail2Ban, and Git
5. Configure safe firewall defaults and the validated `fluxo` account; optionally enable key-only root SSH with `--harden-ssh`
6. Install and start the `fluxo` systemd service

By default, the installer runs interactively and prompts you for additional software. You can pass flags to skip prompts and automate the installation:

```bash
# Example: Install MySQL and Redis, and skip Node.js and Python application support
curl -fsSL https://fluxo.fabiotech.dev/install.sh | sudo bash -s -- --db-engine=mysql --redis --no-node --no-python
```

### Available Flags
| Flag | Description |
|---|---|
| `--db-engine=<engine>` | `mysql`, `postgres`, `both`, or `none` |
| `--redis` / `--no-redis` | Install or skip Redis |
| `--node` / `--no-node` | Install or skip Node.js, npm, pnpm, Yarn, Corepack, and Bun |
| `--python` / `--no-python` | Install or skip Python application support, venv tooling, pip, and uv |
| `--harden-ssh` / `--no-harden-ssh` | Explicitly enable or skip validated key-only root SSH configuration |
| `--management-cidr=<cidr>` | Restrict port `9595` when the installer creates a new UFW policy |
| `--skip-release-attestation` | Explicitly trust a custom binary URL that cannot use Fluxo release provenance |

*If you skip a component (for example `--no-node` or `--no-python`), you can install it later from Runtime in the Fluxo dashboard.*

The installer remembers Node.js, Python, database-engine and Redis choices in root-owned `/var/lib/fluxo-installer/preferences.json`, including choices to skip them. Later upgrades reuse those choices without prompting; explicit component flags override them. Choices are saved before provisioning, so they also apply when retrying an interrupted installation and do not certify that installation completed. For older installations without saved choices, upgrades preserve installed components and skip absent optional components. Installing a skipped component from Runtime does not remove or disable it on the next upgrade. An already-current installation still exits without changing files; use `--reinstall` to apply new installer choices.

The base verifies and preserves an existing managed Node.js toolchain during panel updates. A saved Node choice, `--node`, or `--reinstall` does not request runtime repair or another Node major. Healthy older payloads can receive a non-mutating authenticated assessment; later verified reuse avoids registry checks and package preparation. Defects receive explicit recovery or repair guidance. See the [upgrade guide](ui-site/docs/getting-started/upgrade.md#existing-nodejs-toolchains).

Published binaries from `v0.4.10` onward include an attached GitHub build-provenance bundle. The installer verifies the expected repository, release tag, signing workflow, hosted runner, artifact digest, and transparency-log signature before executing the candidate. The bundle is fetched as a public release asset, so installation does not require a GitHub account or token. Its temporary GitHub CLI verifier is version-pinned with hashes embedded in the installer. The release workflow also pins every third-party Action and embeds the checked-in Node.js and Bun architecture hashes, npm integrity values for Corepack, pnpm, and Yarn, and exact Composer and WP-CLI baselines into each Fluxo release. Fluxo keeps Composer on the stable Composer 2 channel with a weekly maintenance job.

After installation, access the dashboard at:

```
https://<your-server-ip>:9595
```

The dashboard uses a self-signed TLS certificate — accept the browser warning to proceed.

After signing in, you can connect a trusted hostname from **Settings → General → Panel Domain** using Let's Encrypt, an existing certificate, or a compatible cloned custom certificate. The direct IP address and port remain available as a recovery path.

When a newer Fluxo release is published, the dashboard displays an informational banner with the installed and latest versions. Install panel releases with the installer from the server terminal after reviewing the release notes. PHP and Node.js runtime updates have separate reviewed workflows in the dashboard.

---

## First Login

Fluxo generates a unique admin token on first boot and displays it once when installation completes. Use it with your chosen username to log in; that username becomes the permanent administrator username.

> Store the displayed token securely. A recovery copy remains in the root-only credentials file (`chmod 0600`) until you download and acknowledge the complete credentials TXT in the dashboard. Database and sudo passwords are generated by the daemon and stored encrypted; their only plaintext export is that one-time TXT.

### Account Recovery

If you forget the administrator username, retrieve it without changing the account or invalidating sessions:

```bash
sudo fluxo --show-admin-username
```

Change your password from the dashboard (Settings → Change Password). If you're locked out, generate a new token via the CLI:

```bash
sudo fluxo --reset-token
```

The reset command displays both the administrator username and the new token, and retains a root-only recovery copy. Before the first account is claimed, both commands report that you must choose a username during the initial login.

---

## Upgrade

For a legacy installation, complete the supported bridge first, then use the base installer shown above. An already-admitted base, including an earlier base trial, updates directly and must not be replaced by the bridge merely because of version ordering:

```bash
curl -fsSL https://fluxo.fabiotech.dev/releases/v0.4.55/install.sh | sudo env FLUXO_VERSION=v0.4.55 bash
```

The base checks the exact v0.4.55 image, bridge completion, retained execution archives, current schema and installation identity. A version string or manual `baseline-bridge certify` receipt does not replace these checks. Subsequent base updates, including already-admitted base-test installations, recognize their existing base installation and use the normal installer regardless of version ordering. The bridge attempts historical site-account link restoration automatically and without blocking installation. GitHub health and its optional recovery report do not gate base admission; deployment-key repair remains in Source Control.

PHP and Node.js runtime maintenance use separate reviewed dashboard workflows. Finish pending maintenance before a panel upgrade, and preserve its approvals and recovery history. See the [runtime guide](https://fluxo.fabiotech.dev/docs/operations/runtimes) for supported consumers and recovery requirements.

Re-run the normal installer to update to the latest stable release. Before replacing the binary, the installer stops Fluxo cleanly, confirms its dashboard and diagnostic ports were released, and snapshots its binary, SQLite database (including WAL state), service file, Composer and WP-CLI executables, managed cron files, and Fluxo-owned sudoers, SSH, and Fail2Ban policy. A verified unchanged Node toolchain is preserved without repair or snapshot work. Deliberately selected Node installation or repair retains separate snapshots of Fluxo-managed runtimes, package-manager files, state and links, and verifies previously active managed applications. A failed managed application check restores the previous release and toolchain. PM2 and other external process managers are detected and reported but remain outside Fluxo's lifecycle. Existing HTTP/TLS service mode, panel-domain setting, UFW state, and UFW rules are preserved during upgrades. Direct loopback access must pass every upgrade; a panel domain that was healthy before the upgrade must also remain healthy, while an already-unhealthy panel proxy is preserved without blocking recovery through the direct address. After startup, Fluxo also reconciles enabled GitHub webhooks in the background, retaining one working callback and removing locally owned duplicates without delaying the health check. Retained application snapshots live under `/var/lib/fluxo/upgrades/`.

```bash
curl -fsSL https://fluxo.fabiotech.dev/install.sh | sudo bash
```

To pin a specific version:

```bash
curl -fsSL https://fluxo.fabiotech.dev/releases/v0.4.57/install.sh | sudo env FLUXO_VERSION=v0.4.57 bash
```

---

## What's Next

Detailed guides for every workflow below are available in the **[Fluxo documentation](https://fluxo.fabiotech.dev/docs/)**.

- **Create a site** — Laravel, WordPress, PHP, Python apps such as Django, Flask, and FastAPI, static HTML, or Node.js apps such as Next.js and Nuxt
- **WordPress management** — WP-CLI, hardened Nginx defaults, browser-based admin setup, and an editable `wp-config.php`
- **Deploy** — Git-based deployments with zero-downtime release symlinks enabled by default, persistent failure alerts, full error output, and one-click rollback
- **Laravel features** — Scheduler, managed Queue Workers, Nightwatch, Horizon, maintenance mode, and optional Octane worker/proxy support for standard deployments
- **SSL and panel domain** — Connect the panel or application domains using Let's Encrypt, custom certificates, or compatible certificate cloning
- **Nginx vhosts** — Inspect and safely customize each site's complete virtual host with validation, rollback, and one-click restoration to Fluxo defaults
- **Site Network** — Whole-site shared credentials, exact-path redirects and wildcard routing on managed vhosts
- **Traffic Analytics** — Origin request, page and API reports with collection controls, retention and longer history ranges
- **System controls** — Ubuntu security-update settings, restart-required status, the running kernel and explicitly confirmed manual restarts
- **Nginx update awareness** — Daily/manual APT candidate discovery and SSH instructions; package installation stays under operator control
- **Databases** — Manage MySQL/MariaDB and PostgreSQL databases and dedicated application users, with optional phpMyAdmin access for MySQL/MariaDB
- **Off-server backups** — Schedule site-file and database backups to private Amazon S3 or Cloudflare R2 destinations
- **Files** — Browse, upload, download, create, rename, and safely edit small text files inside each site's root
- **Daemons** — Run persistent processes via systemd
- **Cron jobs** — Schedule automated tasks
- **Firewall** — Manage and verify Fluxo-owned UFW rules from the dashboard, with installer baseline rules protected from accidental deletion

---

## Contributing

Pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

---

## Development

### Prerequisites

- Go >= 1.26.3
- Node.js >= 20

### Build

```bash
cd ui && npm install && npm run build && cd .. && go build -o fluxo ./cmd/fluxo
```

### Frontend Dev Server

```bash
cd ui && npm install && npm run dev
```

### Frontend Typecheck

```bash
cd ui && npx vue-tsc -b --noEmit
```

### Release Tool Pins

Before tagging a release that updates managed tooling or the temporary attestation verifier, change the exact versions and authenticated Composer hash in [`.github/release-tool-versions.conf`](.github/release-tool-versions.conf). The release workflow verifies every pinned artifact before building and publishes the selected versions and hash in `TOOLCHAIN-VERSIONS`.

### Project Structure

```
cmd/fluxo/main.go            Entrypoint
internal/
  config/                    Configuration
  database/                  SQLite, models, migrations
  server/                    HTTP handlers, WebSocket, JWT
  services/                  Nginx, PHP, Git, SSL, firewall, databases, etc.
  syscmd/                    Secure command runner
ui/
  src/
    components/              Vue components
    composables/             Shared composables
```

---

## Tech Stack

| Component | Tech |
|-----------|------|
| Backend | Go 1.26.3 |
| Database | SQLite (`modernc.org/sqlite`, CGo-free) |
| Frontend | Vue 3, TypeScript, Vite, Tailwind v4 |
| Real-time | WebSockets (`gorilla/websocket`) |
| Auth | JWT (HS256) |
| Profiling | pprof at `127.0.0.1:6060` |
