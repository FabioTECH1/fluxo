# Fluxo

Web server control panel inspired by Laravel Forge. Go daemon with embedded Vue 3 frontend.

## Stack

- **Backend**: Go 1.26.3 (single binary, CGo-free SQLite via `modernc.org/sqlite`)
- **Frontend**: Vue 3 + TypeScript + Vite + Tailwind CSS v4
- **Auth**: JWT (HS256), admin token printed to stdout on first start (day-zero auth)
- **DB**: SQLite, schema + migrations auto-applied on startup in `database/db.go`

## Development

```sh
# build frontend first (required before Go)
cd ui && npm install && npm run build && cd ..

# build Go binary
go build -o fluxo main.go

# run (dev, port 8080, local db)
./fluxo
# FLUXO_ENV=prod  → /var/lib/fluxo/fluxo.db
# FLUXO_PORT=     → default 8080
# FLUXO_DATA_DIR= → default .  (prod: /var/lib/fluxo)
```

## Architecture

```
main.go              → entrypoint (init DB, admin token, pprof, server)
config/config.go     → reads FLUXO_ENV, FLUXO_PORT, FLUXO_DATA_DIR
database/            → SQLite, models (Site, Deployment, Daemon, Cron, etc.)
syscmd/runner.go     → only way to run external commands (no shell strings)
server/              → REST handlers + WebSocket + JWT auth + middleware
services/            → wrappers for: nginx, php, git, ssl, firewall, mysql, postgres, cron, daemon, deploy, system
ui/embed.go          → //go:embed dist/* serves SPA with History API fallback
```

## Commands & shortcuts

- **Full build**: `cd ui && npm run build && cd .. && go build -o fluxo .`
- **UI dev server**: `cd ui && npm run dev` (Vite standalone, no Go backend)
- **Frontend typecheck**: `cd ui && npx vue-tsc -b --noEmit`

## Key details

- **syscmd.Run** requires executable name + explicit args — no shell strings
- **syscmd.RunAsUser** drops privileges to target user (e.g. www-data)
- WebSocket at `/api/v1/ws` **bypasses auth** (skipped in middleware)
- pprof always active at `127.0.0.1:6060`
- Deploy scripts always run as `www-data`
- Node.js app ports must be unique (`app_port` check in `handleCreateSite`)
- Deploy strategies in `services/deploy/strategies.go` (standard, zero-downtime, octane)

## Main APIs

All under `/api/v1/` with JWT Bearer (except login and ws):
- `POST /api/v1/auth/login` — credentials: `{username, password}` (password = admin token)
- `GET/POST /api/v1/sites` — create/delete sites (nginx + php-fpm pool + dir)
- `POST /api/v1/sites/{id}/deploy` — triggers async deploy (returns 202)
- `GET /api/v1/ws?site_id=N` — WebSocket for real-time deploy logs
- Other CRUDs: env, daemons, crons, ssl, databases, ssh-keys, firewall

## Constraints

- **No tests** in the repository
- **No CI/CD** configured
- Deploy requires system dependencies (nginx, php-fpm, systemd)
- `install.sh` provisions: nginx, php8.4-fpm, certbot, mariadb-server, ufw
