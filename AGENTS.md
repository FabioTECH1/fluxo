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
ui/src/components/   → reusable UI components (BaseModal, SidebarNav, Card, DataTable, etc.)
ui/src/composables/  → shared composition functions (useTheme, useToast, useConfirm)
```

## Commands & shortcuts

- **Full build**: `cd ui && npm run build && cd .. && go build -o fluxo .`
- **UI dev server**: `cd ui && npm run dev` (Vite standalone, no Go backend)
- **Frontend typecheck**: `cd ui && npx vue-tsc -b --noEmit`
- **Post-build verify**: `multipass exec fluxo-dev -- sudo systemctl status fluxo --no-pager && multipass exec fluxo-dev -- ls -la /usr/local/bin/fluxo`

## Key details

- **syscmd.Run** requires executable name + explicit args — no shell strings
- **syscmd.RunAsUser** drops privileges to target user (e.g. www-data)
- WebSocket at `/api/v1/ws` **bypasses auth** (skipped in middleware)
- pprof always active at `127.0.0.1:6060`
- Deploy scripts always run as `www-data`
- Node.js app ports must be unique (`app_port` check in `handleCreateSite`)
- Deploy strategies in `services/deploy/strategies.go` (standard, zero-downtime, octane)
- Single `fluxo` system user for SSH, daemons, crons, DB access
- SSH key-only auth (`PasswordAuthentication no`); sudo password for `sudo` commands

## Main APIs

All under `/api/v1/` with JWT Bearer (except login and ws):
- `POST /api/v1/auth/login` — credentials: `{username, password}` (password = admin token)
- `GET/POST /api/v1/sites` — create/list sites (nginx + php-fpm pool + dir)
- `PUT /api/v1/sites/{id}` — update site settings (app_type, php_version, web_root, repo, branch, etc.)
- `DELETE /api/v1/sites/{id}` — delete site
- `POST /api/v1/sites/{id}/deploy` — triggers async deploy (returns 202)
- `GET /api/v1/ws?site_id=N` — WebSocket for real-time deploy logs
- `GET/POST/PUT/DELETE /api/v1/sites/{id}/env` — .env management
- `GET/POST/DELETE /api/v1/sites/{id}/domains` — domain alias CRUD
- `POST /api/v1/sites/{id}/ssl/letsencrypt` — install LE cert
- `POST /api/v1/sites/{id}/ssl/activate` — activate installed SSL
- `POST /api/v1/sites/{id}/ssl/deactivate` — deactivate SSL
- `POST /api/v1/sites/{id}/commands` — run command via web terminal
- `GET /api/v1/sites/{id}/commands` — command history
- `GET/POST/DELETE /api/v1/daemons` — systemd daemon management
- `GET/POST/DELETE /api/v1/crons` — cron job management
- `GET/POST/DELETE /api/v1/ssh-keys` — SSH key management
- `GET/POST/DELETE /api/v1/firewall` — UFW firewall rule management
- `GET/POST/DELETE /api/v1/databases` — database (MySQL/PostgreSQL) management
- `GET/POST/DELETE /api/v1/databases/users` — database user management
- `GET /api/v1/databases/sizes` — database sizes
- `GET/POST /api/v1/databases/users/grants` — user grants
- `GET /api/v1/settings` — server settings
- `PUT /api/v1/settings` — update settings (admin email, GitHub PAT, default PHP)
- `PUT /api/v1/auth/password` — change admin password
- `GET /api/v1/github/repos` — list GitHub repos (requires PAT)
- `GET /api/v1/github/branches?repo=user/repo` — list repo branches
- `GET /api/v1/server/php` — list installed PHP versions
- `GET /api/v1/server/metrics` — system metrics (CPU, memory, disk)
- `GET /api/v1/server/logs?type=nginx|php|mysql|postgres|redis` — tail log files
- `GET /api/v1/system/activity` — recent activity feed

## Frontend Component Library

### Reusable UI Components (`ui/src/components/`)

All components have dark mode baked in as a single source of truth:

| Component | Purpose | Key Props |
|-----------|---------|-----------|
| `BaseModal.vue` | Modal shell with header, body, footer slots, Escape/click-outside close | `v-model`, `title`, `loading`, `maxWidth` |
| `SidebarNav.vue` | Sidebar navigation card with `border-l-4` active state | `items: [{to, label, icon, match?}]` |
| `Card.vue` | Consistent card container | `padding?` (default true) |
| `AppButton.vue` | Button with primary/secondary/danger variants + loading | `variant`, `size`, `loading`, `to`, `type` |
| `FormGroup.vue` | Label + hint + input slot + error message | `label`, `forAttr`, `error?`, `hint?` |
| `DataTable.vue` | Table with thead/tbody + scoped slots + empty state | `columns`, `items`, `emptyText` |
| `EmptyState.vue` | "No items found" for table or standalone | `message`, `mode`, `colSpan` |
| `PageHeader.vue` | Page title with optional subtitle | `title`, `subtitle?` |
| `ErrorAlert.vue` | Red error banner | `message` |
| `StatusBadge.vue` | Color-coded status chip | `label`, `variant` (green/red/blue/yellow/gray) |

### Dark Mode

- Theme toggle (☀ Light / ☽ Dark / ☿ System) in the top nav bar
- Persisted to `localStorage` as `fluxo_theme`
- Class-based via `@variant dark (&:where(.dark, .dark *))` in Tailwind
- All `dark:` variants live in reusable components — single source of truth
- Theme composable at `ui/src/composables/useTheme.ts`

### Tailwind v4 Conventions

- Only standard color tokens (50–950 scale, no 150/250/450/650/750)
- Dark mode: `@variant dark (&:where(.dark, .dark *))` in `style.css`
- Nav active detection: `route.path` from `useRoute()` with full ternary in `:class`
- Sidebar active: `border-blue-600 bg-blue-50 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300`
- Sidebar inactive: `border-transparent text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-800`

## Constraints

- **No tests** in the repository
- **No CI/CD** configured
- Deploy requires system dependencies (nginx, php-fpm, systemd)
- `install.sh` provisions: nginx, php8.4-fpm, certbot, mariadb-server, ufw
