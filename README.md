# Fluxo

Fluxo is a self-hosted web server control panel inspired by Laravel Forge. It helps you provision servers, manage PHP/Node.js sites, databases, SSL certificates, cron jobs, daemons, and firewall rules — all from a clean web dashboard.

---

## Requirements

| | |
|---|---|
| **OS** | Ubuntu 22.04+, Debian 12+ |
| **Architecture** | `amd64` or `arm64` |
| **Server** | A fresh VPS with root SSH access |

---

## Installation

Run the following command on your server as **root**:

```bash
curl -fsSL https://raw.githubusercontent.com/FabioTECH1/fluxo/main/install.sh -o install.sh && sudo bash install.sh
```

The script will:

1. Install Nginx, PHP 8.4, Certbot, UFW, Fail2Ban, Git
2. Prompt for Node.js, MariaDB, PostgreSQL, and Redis (optional)
3. Open ports 22, 80, 443, and 9595 in the firewall
4. Create the `fluxo` system user and harden SSH (key-only auth)
5. Download the Fluxo binary and verify its SHA256 checksum
6. Install and start the `fluxo` systemd service

After installation, access the dashboard at:

```
https://<your-server-ip>:9595
```

The dashboard uses a self-signed TLS certificate — accept the browser warning to proceed.

---

## First Login

Fluxo generates a unique admin token on first boot. The installer displays it at the end — you can also retrieve it anytime from the credentials file:

```bash
sudo cat /home/fluxo/.fluxo_credentials
```

Look for the `Fluxo bootstrap token` entry — use it with any username to log in. That username becomes the permanent admin username going forward.

> The credentials file is root-only (`chmod 0600`) and also contains the database and sudo passwords.

### Password Reset

Change your password from the dashboard (Settings → Change Password). If you're locked out, generate a new token via the CLI:

```bash
sudo fluxo --reset-token
```

---

## Upgrade

Re-run the installer to upgrade to the latest version. It stops the service, replaces the binary, and restarts — nothing else is touched:

```bash
curl -fsSL https://raw.githubusercontent.com/FabioTECH1/fluxo/main/install.sh -o install.sh && sudo bash install.sh
```

To pin a specific version:

```bash
curl -fsSL https://raw.githubusercontent.com/FabioTECH1/fluxo/main/install.sh -o install.sh && FLUXO_VERSION=v0.2.0 sudo -E bash install.sh
```

---

## What's Next

- **Create a site** — PHP (Nginx + PHP-FPM) or Node.js (Nginx proxy + systemd)
- **Deploy** — Git-based deployments with zero-downtime or Octane strategies
- **SSL** — Free Let's Encrypt certificates with one click
- **Databases** — Manage MySQL/MariaDB and PostgreSQL databases and users
- **Daemons** — Run persistent processes via systemd
- **Cron jobs** — Schedule automated tasks
- **Firewall** — Manage UFW rules from the dashboard

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
