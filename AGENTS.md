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
go build -o fluxo ./cmd/fluxo

# run (dev, HTTPS with self-signed cert on port 9595)
./fluxo
# FLUXO_USE_HTTP=1 → plain HTTP for development/reverse-proxy
# FLUXO_ENV=prod    → /var/lib/fluxo/fluxo.db
# FLUXO_PORT=       → default 9595
# FLUXO_DATA_DIR=   → default .  (prod: /var/lib/fluxo)
```

## Architecture

```
cmd/fluxo/main.go                → entrypoint (init DB, TLS, admin token, pprof, server)
internal/config/config.go        → reads FLUXO_ENV, FLUXO_PORT, FLUXO_DATA_DIR
internal/database/               → SQLite, models (Site, Deployment, Daemon, Cron, etc.)
internal/syscmd/runner.go        → only way to run external commands (no shell strings)
internal/server/                 → REST handlers + WebSocket + JWT auth + middleware
internal/services/               → wrappers for: nginx, php, git, ssl, firewall, mysql, postgres, phpMyAdmin, cron, daemon, deploy, system
ui/embed.go                      → //go:embed dist/* serves SPA with History API fallback
ui/src/components/               → reusable UI components (BaseModal, SidebarNav, Card, DataTable, etc.)
ui/src/composables/              → shared composition functions (useTheme, useToast, useConfirm)
ui-site/                         → prerendered landing/blog + live demo (deployed to Cloudflare Pages)
ui-site/content/blog/            → Markdown blog articles discovered and prerendered at build time
ui-site/scripts/prerender.mjs    → emits static public HTML, SEO metadata, sitemap, and demo shell
ui-site/src/api/mock.ts          → mock API client for the demo (intercepts /api/v1/*)
ui-site/src/views/Landing.vue    → product landing page
```

## Commands & shortcuts

- **Full build**: `cd ui && npm run build && cd .. && go build -o fluxo ./cmd/fluxo`
- **UI dev server**: `cd ui && npm run dev` (Vite standalone, no Go backend)
- **Site dev server**: `cd ui-site && npm run dev` (landing page + demo, no backend needed)
- **Frontend typecheck**: `cd ui && npx vue-tsc -b --noEmit`
- **Post-build verify**: `multipass exec fluxo-dev -- sudo systemctl status fluxo --no-pager && multipass exec fluxo-dev -- ls -la /usr/local/bin/fluxo`

## Key details

- **syscmd.Run** requires executable name + explicit args — no shell strings
- **syscmd.RunAsUser** drops privileges to target user (e.g. www-data)
- WebSocket at `/api/v1/ws` authenticates via token query param
- pprof always active at `127.0.0.1:6060`
- Deploy scripts always run as `fluxo`
- Node.js app ports must be unique (`app_port` check in `handleCreateSite`)
- Deploy strategies in `internal/services/deploy/strategies.go` (standard, zero-downtime, octane)
- Single `fluxo` system user for SSH, deployments, daemons, and crons; hosted applications use dedicated least-privilege database users
- SSH key-only auth (`PasswordAuthentication no`); sudo password for `sudo` commands

## Main APIs

All are under `/api/v1/`; protected endpoints require a JWT Bearer token unless noted:
- `POST /api/v1/auth/login` — credentials: `{username, password}` (password = admin token)
- `GET /api/v1/version` — unauthenticated installed binary version
- `GET /api/v1/update-status` — authenticated informational comparison with the latest published release; never installs updates
- `GET/POST /api/v1/sites` — create/list sites (nginx + php-fpm pool + dir)
- `PUT /api/v1/sites/{id}` — update mutable site settings (PHP version, web root, repo, branch, etc.); `app_type` and deployment strategy are immutable after creation
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
- `GET /api/v1/tools/phpmyadmin` — optional phpMyAdmin installation status
- `POST /api/v1/tools/phpmyadmin/install` — download, verify, install, and enable phpMyAdmin
- `POST /api/v1/tools/phpmyadmin/enable` — enable an installed phpMyAdmin release
- `POST /api/v1/tools/phpmyadmin/disable` — disable phpMyAdmin without removing it
- `POST /api/v1/tools/phpmyadmin/access` — create a one-time phpMyAdmin access link
- `DELETE /api/v1/tools/phpmyadmin` — remove Fluxo-managed phpMyAdmin files and configuration
- `GET/POST /api/v1/backups/destinations` — list/connect reusable S3 and R2 destinations
- `PUT /api/v1/backups/destinations/{id}` — test and rotate destination credentials
- `POST /api/v1/backups/destinations/{id}/test` — verify destination read/write/delete access
- `DELETE /api/v1/backups/destinations/{id}` — remove an unused destination
- `GET/POST /api/v1/backups/plans` — list/create per-site backup plans
- `PUT/DELETE /api/v1/backups/plans/{id}` — update/delete a backup plan
- `POST /api/v1/backups/plans/{id}/run` — queue a manual backup
- `GET /api/v1/backups/runs` — backup history and artifacts
- `POST /api/v1/backups/runs/{id}/artifacts/{artifact_id}/download` — create a short-lived download URL
- `DELETE /api/v1/backups/runs/{id}` — delete remote backup artifacts and history
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
| `DataTable.vue` | Responsive, horizontally scrollable table with scoped slots and sticky actions | `columns`, `items`, `emptyText`, `ariaLabel?` |
| `TableActionMenu.vue` | Teleported row-action menu that escapes table overflow and repositions on scroll | `items`, `ariaLabel?`, `width?`, `loading?` |
| `TablePagination.vue` | Compact result count and previous/next controls for client-paginated tables | `v-model:page`, `totalItems`, `pageSize?` |
| `EmptyState.vue` | "No items found" for table or standalone | `message`, `mode`, `colSpan` |
| `PageHeader.vue` | Page title with optional subtitle | `title`, `subtitle?` |
| `ErrorAlert.vue` | Red error banner | `message` |
| `StatusBadge.vue` | Color-coded status chip | `label`, `variant` (green/red/blue/yellow/gray) |
| `ToggleSwitch.vue` | Accessible, dark-mode-aware boolean switch | `v-model`, `label`, `description?`, `disabled?`, `labelPosition?` |

Use `ToggleSwitch` for boolean settings and feature states. Do not hand-build switch buttons in page views. Keep native checkboxes for acknowledgement fields and multi-select lists where each checkbox represents a selection rather than an independent enabled/disabled setting.

Use `TableActionMenu` for three-dot actions inside `DataTable`. Do not place an absolutely positioned dropdown directly inside a scrollable table because it will be clipped by the overflow container.

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
- Boolean settings: use `ToggleSwitch.vue`; use `labelPosition="left"` for full-width settings rows and the default switch-first layout for compact forms

## Constraints

- **No tests** in the repository
- **CI**: GitHub Actions builds and publishes releases on tag push (`v*`)
- Deploy requires system dependencies (nginx, php-fpm, systemd)
- `install.sh` provisions: nginx, php8.4-fpm, certbot, mariadb-server, ufw

## Releases

Pushing a `v*` tag triggers `.github/workflows/release.yml`:

```sh
git tag v0.1.0
git push origin v0.1.0
```

This builds the frontend, cross-compiles Go for `linux/amd64` and `linux/arm64`,
generates SHA256SUMS, and creates a GitHub Release with all artifacts attached.

The `install.sh` script auto-detects the architecture and fetches the matching
binary from the latest GitHub Release. To point install.sh at your own fork, set
`FLUXO_GITHUB_REPO` in the script or as an environment variable.

```sh
# One-liner install
curl -fsSL https://fluxo.fottify.com/install.sh | sudo bash

# Or with env var for custom repos
curl -fsSL https://raw.githubusercontent.com/FabioTECH1/fluxo/main/install.sh -o install.sh && FLUXO_GITHUB_REPO=myorg/fluxo sudo -E bash install.sh
```

## Cloudflare Pages Deployment

The `ui-site/` project is deployed to Cloudflare Pages. Build settings:

| Setting | Value |
|---|---|
| Framework preset | **None** (manual) |
| Production branch | `main` |
| Build command | `cd ui && npm install && cd ../ui-site && npm install && npm run build` |
| Build output directory | `ui-site/dist` |
| Root directory | *(leave blank)* |
| Custom domain | `fluxo.fottify.com` |

The public latest-release manifest is served by the Pages Function at `functions/api/v1/releases/latest.ts`. It validates and caches GitHub's latest published release metadata; keep the Pages project root blank so Cloudflare discovers the repository-root `functions/` directory.

The same build prerenders `/`, `/blog`, and every Markdown article into static HTML. `/demo/*` remains a Vue SPA served through `demo/index.html`, and `_routes.json` keeps Pages Functions limited to `/api/v1/releases/*`.

The `ui/src/tsconfig.json` file allows Oxc (Vite 8's transformer) to find tsconfig for `@fluxo` aliased imports. No changes needed between pushes — Cloudflare auto-deploys on every `git push main`.
