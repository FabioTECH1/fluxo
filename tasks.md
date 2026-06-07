# Fluxo Development Phases

This document breaks down the Fluxo MVP specification into logical, sequential implementation phases. It is structured to allow an AI editor or developer to tackle features one by one, ensuring the foundational OS and Go daemon layers are stable before building the Vue frontend or advanced deployment mechanics.

## Phase 1: Foundation & Installation Skeleton
*Focus: Setting up the Go daemon, SQLite state, and the basic Vue SPA structure. Establishing the secure boundaries for system execution.*

- [ ] **1.1 Go Daemon Initialization**
  - Set up Go project with SQLite driver.
  - Implement basic HTTP REST server skeleton.
  - Implement system command execution wrapper (strict isolation, no raw shell strings).
- [ ] **1.2 Database & State**
  - Define SQLite schema (Sites, Deployments, Environment, Processes).
  - Write basic CRUD operations in Go for state management.
- [ ] **1.3 Vue 3 + Vite Scaffold**
  - Scaffold Vue 3 SPA.
  - Set up routing (Dashboard, Sites, Settings).
  - Create API client for HTTP communication with Go daemon.
- [ ] **1.4 Installation Script (V1)**
  - Draft `install.sh` to install Go binary as a `systemd` service.
  - Initialize empty SQLite DB on installation.

## Phase 2: Core Server OS & Site Management
*Focus: Nginx and PHP integration. The engine that routes requests to directories.*

- [ ] **2.1 Nginx Config Generator (Go)**
  - Create Go templates for Nginx server blocks.
  - Implement safe reload/test logic (`nginx -t && systemctl reload nginx`).
- [ ] **2.2 PHP Manager**
  - Script installation of PHP 8.1, 8.2, 8.3 via OS package manager.
  - Generate and manage PHP-FPM pool configs per site.
  - Implement PHP version switching logic (relinking FPM socket in Nginx config).
- [ ] **2.3 Site API & UI**
  - Go API: `POST /sites` (creates directory, writes Nginx config, sets up FPM pool).
  - Go API: `DELETE /sites` (cleans up configs and directories).
  - Vue UI: Sites list and site creation modal.

## Phase 3: The Deployment Engine & WebSockets
*Focus: Replicating the "Forge" deployment experience. This is the most complex phase.*

- [ ] **3.1 Git Integration**
  - Go function to generate deploy keys (SSH) per site.
  - Go function to clone repositories and checkout branches.
- [ ] **3.2 Deployment Script Runner**
  - Execute custom bash scripts (e.g., `composer install`, `php artisan migrate`).
  - Enforce execution strictly within the mapped project directory.
- [ ] **3.3 Real-time Log Streaming**
  - Implement WebSocket server in Go.
  - Capture `stdout`/`stderr` from deployment scripts and broadcast via WS.
  - Implement log tailing for application logs (e.g., `storage/logs/laravel.log`).
- [ ] **3.4 Deployment UI**
  - Vue UI: Deployment history list, script editor.
  - Vue UI: Live terminal component for WebSocket streaming.

## Phase 4: Environment & Application Runtimes
*Focus: Managing `.env` files, Node.js, and Laravel Queues securely.*

- [ ] **4.1 Environment Editor**
  - Go API: Read `.env`, backup `.env`, write `.env` (atomic writes).
  - Vue UI: Key-value editor and raw text fallback.
- [ ] **4.2 Laravel Queue Manager**
  - Go logic to generate `systemd` services for `php artisan queue:work`.
  - API to start/stop/restart individual queue workers.
  - Vue UI: Processes tab to monitor and control queues.
- [ ] **4.3 Node.js App Manager**
  - Go logic to generate `systemd` services for Node apps (e.g., `npm run start`).
  - Port assignment and local proxy logic (updating Nginx config to proxy to Node port).

## Phase 5: Automation (Scheduler & SSL)
*Focus: Routine maintenance and security.*

- [ ] **5.1 Scheduler Manager**
  - Abstract system cron into a readable Go struct.
  - Read/write user-level crontab or drop files in `/etc/cron.d/`.
  - Vue UI: Cron builder interface.
- [ ] **5.2 SSL Automation (Let's Encrypt)**
  - Integrate Certbot or ACME client in Go.
  - Implement domain validation, certificate issuance, and Nginx config update.
  - Set up auto-renewal via system cron.
  - Vue UI: One-click SSL toggle per site.

## Phase 6: Database Module (Optional MVP)
*Focus: Basic DB provisioning without the overhead of a full GUI.*

- [ ] **6.1 DB Installation Scripts**
  - Add logic to `install.sh` or Go daemon to install MySQL/MariaDB, PostgreSQL, and Redis.
- [ ] **6.2 DB Provisioning API**
  - Go API to execute SQL commands: `CREATE DATABASE`, `CREATE USER`, `GRANT PRIVILEGES`.
  - Vue UI: Simple form to generate a database and user credentials.

## Phase 7: Polish & Security Audit
*Focus: Preparing the single binary for production use.*

- [ ] **7.1 Security Hardening**
  - Audit all file path concatenations for directory traversal risks.
  - Ensure Go daemon runs with least required privileges (or drops privileges when executing app-level code).
- [ ] **7.2 Performance Profiling**
  - Verify Go daemon memory footprint stays within the 20-80MB target.
  - Optimize WebSocket connection handling.
- [ ] **7.3 UI/UX Polish**
  - Refine Vue components for a clean, pragmatic, developer-first layout.
  - Add toast notifications, error handling, and loading states.