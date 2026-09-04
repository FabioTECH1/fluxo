---
title: Host a Laravel app on Ubuntu with Fluxo
excerpt: A complete path from a clean Ubuntu server to a deployed Laravel application with Nginx, PHP-FPM, a dedicated database user, queues, scheduling, and TLS.
category: Deployments
date: 2026-07-10
image: /blog/laravel-on-ubuntu.webp
imageAlt: Laravel application connected to its server, deployment workflow, and database
---

Hosting Laravel reliably involves more than copying a repository to a VPS. The web server must point at `public`, PHP-FPM must use the right version, production secrets must stay outside Git, background work must survive reboots, and the domain needs a valid certificate.

Fluxo brings those pieces into one application-aware workflow. This guide starts with a clean Ubuntu server and ends with a production verification checklist.

## 1. Choose and prepare the server

Fluxo supports Ubuntu 22.04 or newer on `amd64` and `arm64`. Start with a clean VPS rather than a machine where another control panel already owns Nginx, PHP-FPM, firewall rules, or database configuration.

For a small low-traffic Laravel application and one local database, use at least 1 GB RAM, 20 GB storage, and 1 GB swap. A 2 GB server is a more comfortable production baseline. Choose 4 GB or more when Composer and Node builds are large, Redis is local, or several queue workers and sites share the host.

At the provider level:

- Keep root SSH or console recovery access.
- Allow the effective SSH port.
- Allow ports 80 and 443 for application traffic and Let's Encrypt.
- Allow Fluxo's dashboard port 9595 only from the networks that should reach it when practical.
- Do not expose MySQL, PostgreSQL, or Redis publicly for an ordinary same-server application.

Create the application's DNS `A` record, and create an `AAAA` record only when IPv6 is correctly configured on the server. A stale or incorrect IPv6 record is a common reason certificate validation behaves inconsistently.

## 2. Install Fluxo and the required services

Run the installer as root:

```bash
curl -fsSL https://fluxo.fottify.com/install.sh | sudo bash
```

For an unattended Laravel-oriented installation with MariaDB, Redis, and the Node build toolchain:

```bash
curl -fsSL https://fluxo.fottify.com/install.sh | sudo bash -s -- \
  --db-engine=mysql \
  --redis \
  --node \
  --no-python
```

The Node toolchain is optional if the application has no frontend build. Redis is also optional unless the application uses it for queues, cache, sessions, Horizon, or another service.

The installer provisions Nginx, PHP 8.4 and common extensions, Certbot, Composer, Git, UFW, Fail2Ban, and selected optional components. It creates the dedicated `fluxo` system user and runs deployments without root privileges.

After installation, confirm the service:

```bash
sudo systemctl status fluxo --no-pager
curl -k https://127.0.0.1:9595/api/v1/health
```

A healthy endpoint returns `{"status":"ok"}`.

## 3. Sign in and connect source control

Open `https://SERVER_IP:9595`, accept the initial self-signed certificate warning, and sign in with the generated bootstrap credentials. Store the credentials in a password manager instead of leaving them in terminal history or an unencrypted notes file.

For a private GitHub repository, open **Settings > Source Control** and connect a token with only the repository access Fluxo needs. Fluxo uses the site's deploy key to clone the repository during deployment.

Before creating the site, make sure:

- The production branch is up to date.
- `composer.lock` is committed.
- The frontend lockfile used by the project is committed.
- `.env` is excluded from Git.
- The application does not rely on writable files inside the repository checkout.

## 4. Create a dedicated application database

Laravel can use MariaDB/MySQL or PostgreSQL. Create or select a database during site creation, but always use a dedicated application account.

Do not give the application Fluxo's database control-plane credentials, `root`, or `postgres`. A least-privilege account limits the damage of a leaked application secret and makes later rotation easier to reason about.

When Fluxo creates the database from the site form, it creates and grants the dedicated user. If you select an existing database, provide an application account that already has access. Fluxo verifies the credentials before provisioning and writes them to the site's environment.

Deleting a site does not silently delete its database. Destructive database removal is a separate, explicit decision.

## 5. Create the Laravel site

Open **Sites**, select **Add Site**, and choose **Laravel**.

Configure:

1. The primary hostname without a scheme or path, such as `app.example.com`.
2. An installed PHP version compatible with the project's `composer.json`.
3. The repository and production branch.
4. The application database and dedicated credentials when needed.
5. Zero-downtime deployment for a conventional PHP-FPM application.
6. The normal Laravel web directory, `/public`.

Choose the application type carefully. Fluxo uses it to determine provisioning, Nginx configuration, deployment defaults, logs, and framework features. It does not convert an existing site between Laravel, generic PHP, Node.js, Python, WordPress, or static HTML later.

Zero-downtime deployment requires Git and is the recommended default for conventional Laravel applications. Disable it only when the application must update in place or when you plan to use Laravel Octane.

## 6. Configure the production environment

Open **Site > Settings > Environment**. In zero-downtime mode, Fluxo stores the environment file at the persistent site root and links it into every release.

Review at least:

```dotenv
APP_NAME="Example"
APP_ENV=production
APP_KEY=base64:...
APP_DEBUG=false
APP_URL=https://app.example.com

LOG_CHANNEL=stack
LOG_LEVEL=warning

DB_CONNECTION=mysql
DB_HOST=127.0.0.1
DB_PORT=3306
DB_DATABASE=example
DB_USERNAME=example_app
DB_PASSWORD='a-long-unique-password'

CACHE_STORE=database
SESSION_DRIVER=database
QUEUE_CONNECTION=database
```

The exact variable names can differ by Laravel version. Fluxo generates an application key when one is missing and writes supported database defaults during provisioning, but you remain responsible for mail credentials, object storage, third-party APIs, queue selection, and application-specific configuration.

Keep `APP_DEBUG=false` in production. Debug pages can expose queries, filesystem paths, environment details, and secrets.

Quote database passwords that contain `#`, `$`, spaces, backslashes, or other characters with dotenv meaning. Test the application after any credential rotation.

## 7. Review the managed deployment commands

New Laravel sites receive application-focused defaults. A typical script installs Composer dependencies, builds frontend assets when `package.json` exists, clears optimized caches, creates the storage link, and runs migrations when a database was connected during creation.

```bash
if [ -f composer.json ]; then
  $FLUXO_COMPOSER install --no-dev --no-interaction \
    --prefer-dist --optimize-autoloader
fi

if [ -f package.json ]; then
  (npm ci || npm install)
  npm run --if-present build
fi

if [ -f artisan ]; then
  $FLUXO_PHP artisan optimize:clear
  $FLUXO_PHP artisan storage:link
  $FLUXO_PHP artisan migrate --force
fi
```

Fluxo sets the working directory and supplies version-specific variables. Use `$FLUXO_PHP` and `$FLUXO_COMPOSER` instead of assuming the server's default binaries match the site.

Remove commands your application does not need and add safe application-specific preparation. Do not add `git clone`, symlink activation, old-release deletion, or managed-service restarts; Fluxo owns those lifecycle operations.

Deployment scripts use strict Bash error handling. Avoid interactive prompts, and use conditionals for optional files or commands.

## 8. Make application storage persistent

In zero-downtime mode, each release is disposable. Fluxo shares Laravel's complete `storage` tree and runs `artisan storage:link`, so files written through Laravel's standard local and public disks survive deployments.

If the application writes uploads or generated data somewhere else, move that data into Laravel-managed storage, use object storage, or create an explicit persistent link. Never rely on a custom directory inside one release for irreplaceable files.

Also check file ownership assumptions. Deployments and site commands run as the `fluxo` user, not root. Application code should not require root-owned writable directories.

## 9. Run and observe the first deployment

Start the deployment from the site and keep the output open. The first deployment exercises repository access, dependency installation, asset compilation, the database connection, framework commands, and release activation.

If it fails, start with the first meaningful error rather than the final generic failure line. Common causes include:

- The deploy key cannot read the repository.
- The selected branch does not exist.
- `composer.lock` requires a different PHP version or extension.
- npm runs out of memory during the production build.
- A build-time environment variable is missing.
- Database credentials are incorrect.
- A migration is incompatible with existing data.
- The build exceeds the deployment deadline.

A zero-downtime failure before activation leaves the previous release untouched. On the first deployment there may be no previous application to serve, so resolve the issue before pointing production traffic at the host.

## 10. Configure queues and scheduled work

After a successful release, Fluxo inspects the active `composer.lock` and exposes Laravel-specific controls.

Enable **Scheduler** to create a once-per-minute cron entry for `artisan schedule:run`. Define the real schedule inside the Laravel application; Fluxo's job only wakes Laravel every minute.

For queues, choose one operating model:

- Use Fluxo's managed **Queue Worker** for standard `queue:work` consumers. Configure the connection, queue priority, process count, retries, timeout, backoff, memory, and maximum process lifetime.
- Use **Horizon** when `laravel/horizon` is installed and the application has a working Redis queue connection.

Queue Worker and Horizon are mutually exclusive. Successful deployments and rollbacks restart workers through Laravel's graceful lifecycle so in-flight jobs can finish before new code is loaded.

Do not run an untracked custom queue daemon against the same queues unless the extra concurrency is intentional.

## 11. Add the domain and TLS certificate

Confirm DNS resolves to the server before requesting a certificate. For a root domain, decide whether traffic should redirect from `www`, redirect to `www`, or use no redirect. A WWW redirect needs DNS and certificate coverage for both names.

Fluxo shows the exact hostname set before a Let's Encrypt request. Port 80 must reach the server for validation. Incorrect DNS, blocked HTTP, stale IPv6, CDN routing, and certificate-authority rate limits are common failure sources.

If the site is proxied by Cloudflare, use **Full (strict)** with a valid origin certificate. Flexible mode leaves the origin connection unencrypted and can cause redirect loops.

After activation, test both the canonical hostname and every redirecting hostname. Confirm the path and query string survive the redirect.

## 12. Verify the production application

Do not stop at a successful deployment badge. Check the system from the user's perspective:

- The homepage and a database-backed route return successfully.
- Login, session storage, and CSRF-protected forms work over HTTPS.
- Compiled CSS, JavaScript, and uploaded files return without 404 errors.
- `APP_URL` matches the canonical HTTPS hostname.
- The scheduler is enabled and the server timezone is understood.
- Queue workers or Horizon are running and processing a safe test job.
- Nginx, PHP-FPM, Laravel, and process logs contain no new recurring errors.
- An external uptime monitor can reach the application.
- A backup plan includes both the persistent site files and attached database.

Use **Observe > Logs** for Nginx and application output, **Commands** for a safe one-off Artisan check, and **Deployments** for complete build history.

## 13. Operate and update safely

For later releases, keep schema changes backward-compatible, review deployment output, and verify the same small set of critical application paths after activation.

Fluxo can rebuild a previous successful commit as a new rollback deployment. That restores code, not the database. A destructive migration still needs an application-specific reversal or a verified database restore.

Maintain off-server backups, apply operating-system updates, review Fluxo release notes before panel upgrades, remove unused access keys, and rotate credentials after suspected exposure.

With those habits in place, Fluxo handles the repetitive server plumbing while the application retains a clear and testable production lifecycle.
