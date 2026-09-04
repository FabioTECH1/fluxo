---
title: "Fluxo 0.4.27: deploy Python apps and customize Nginx safely"
excerpt: Fluxo 0.4.27 adds first-class Django, Flask, FastAPI, and generic Python hosting, plus a guarded Nginx vhost editor that validates every configuration before activation.
category: Releases
date: 2026-09-04
image: /blog/fluxo-0-4-27-python-vhosts.webp
imageAlt: Python application runtime connected to a server control panel and a protected Nginx configuration
featured: true
---

Fluxo 0.4.27 expands the kinds of applications you can operate from a Fluxo server. Python applications are now first-class sites alongside Laravel, WordPress, PHP, Node.js, and static applications. The release also adds a guarded editor for the complete Nginx virtual host of each site when the generated configuration is not enough.

This is more than a new option in the site-creation form. Python sites receive isolated dependencies, a managed application process, Nginx routing, environment management, database credentials, deployment automation, logs, commands, cron jobs, and runtime health controls. The vhost editor complements that managed workflow with an explicit escape hatch for advanced routing requirements—without turning every edit into an unsafe direct change on the server.

Fluxo 0.4.27 is available from the [GitHub release](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.27). This article explains what changed, how the pieces work together, and what existing installations should expect during an upgrade.

## What is included in 0.4.27

The release has two headline additions and several supporting improvements:

| Area | What is new |
|---|---|
| Python sites | Presets for Django, Flask, FastAPI, and generic WSGI or ASGI applications |
| Dependencies | Per-site virtual environments with pip or the release-pinned uv toolchain |
| Processes | A protected systemd service for each hosted Python application |
| Deployments | Standard and zero-downtime strategies with configurable build and start commands |
| Operations | Environment variables, databases, commands, cron jobs, logs, and runtime health controls |
| Nginx | Complete per-site vhost editing with validation, revision checks, recovery, and restore-to-default |
| Compatibility | Application-aware lifecycle behavior that keeps PHP, Node.js, and Python processes separate |

The goal is consistent operations across runtimes. A Python application should not require you to abandon the dashboard and assemble an unrelated deployment system simply because its long-running process is Gunicorn or Uvicorn instead of PHP-FPM or Node.js.

## Python is now a first-class application type

When creating a site, you can select **Python** and choose the preset that matches the application:

- **Django** uses `config.wsgi:application` with Gunicorn by default.
- **Flask** uses `app:app` with Gunicorn by default.
- **FastAPI** uses `main:app` with Uvicorn by default.
- **Generic** accepts the entrypoint and start command needed by another WSGI, ASGI, or custom Python web application.

These are editable defaults, not assumptions permanently baked into the server. If a project uses `src.web:app`, `project.wsgi:application`, or another module layout, you can provide that entrypoint during creation and update the stored application settings later.

Fluxo also supports an application directory relative to the repository root. This matters for monorepos where the deployable service lives under a path such as `services/api`. Dependency installation, build commands, the application process, static-file handling, and health checks all use that resolved directory. Absolute paths and paths that escape the site root are rejected.

## Isolated dependencies with pip or uv

Each Python site receives its own `.venv`. Fluxo does not install application packages into Ubuntu's global Python environment and does not replace the distribution-provided interpreter.

You can choose between pip and uv:

- With **pip**, Fluxo installs `requirements.txt` when it exists, installs the current project when only `pyproject.toml` is present, or leaves a clean environment when neither file exists.
- With **uv**, Fluxo uses `uv sync --frozen` when a lock file is committed, installs a requirements file when appropriate, or synchronizes an unlocked `pyproject.toml` project.

The uv binary is selected by the release, downloaded for the server architecture, and verified using the checksums pinned into Fluxo's release process. That keeps runtime provisioning reproducible and prevents a fresh installation from silently selecting an unrelated tool version.

For production deployments, commit the dependency lock file used by the project. A locked environment gives Fluxo—and your team—a reviewed dependency graph to reproduce in each release rather than resolving new package versions during every deployment.

## Managed Gunicorn and Uvicorn processes

A Python web application needs a long-running process after its code and dependencies have been prepared. Fluxo creates a protected systemd unit for that process and manages it as part of the site.

The service:

- starts after the server boots;
- restarts after an unexpected exit;
- receives the site's persistent environment file;
- runs from the configured application directory;
- binds to the internal port assigned to the site;
- sends stdout and stderr to the Fluxo-managed daemon log;
- can be restarted through deployment and runtime controls.

Nginx remains the public entry point. The Python process should listen on `127.0.0.1` using the assigned `$FLUXO_APP_PORT`, while Nginx handles domains, certificates, and external HTTP traffic.

The managed Python process is protected from accidental deletion in the same way that other application-owned infrastructure is protected. It belongs to the site lifecycle rather than behaving like an unrelated user-created daemon.

## Starter applications and repository deployments

You can connect a Git repository during site creation, or create a Python site without one. When there is no repository, Fluxo creates a small starter for the selected preset so the runtime, service, Nginx path, and domain can be verified immediately.

The starter is deliberately minimal. It proves that the hosting path works; it is not a production framework template. Before building on it, review application security, middleware, static files, database migrations, observability, workers, and framework-specific production settings.

For a repository-backed site, Fluxo keeps dependency preparation separate from the managed process command:

1. The deployment checks out or prepares the candidate code.
2. Fluxo creates the release's isolated Python environment.
3. Dependencies are installed with the selected package workflow.
4. The optional build command runs from the application directory.
5. The release is activated.
6. Fluxo restarts the managed application and waits for its internal port.

Django sites default to collecting static files during the build. Database migrations are not inserted automatically because migration policy is application-specific. If the application should migrate during deployment, add the exact migration command to its deployment script and decide how it should behave during rollback.

## Environment variables and databases

Python sites use the same persistent environment-management flow as other Fluxo applications. The editable file lives outside disposable release directories at:

```text
/home/fluxo/DOMAIN/.env
```

When the application lives in a nested directory, Fluxo links the application-level `.env` back to that persistent file. New Python sites receive baseline values including `PYTHONUNBUFFERED`, `HOST`, and `PORT`; Django starters also receive `ALLOWED_HOSTS`.

Attaching MySQL, MariaDB, or PostgreSQL adds dedicated database connection values. Fluxo verifies the selected database account before provisioning, but the application is still responsible for reading those variables in its own settings model.

This separation is intentional: Fluxo safely provisions the infrastructure and credentials without pretending that every framework or project uses the same configuration library.

## Zero-downtime deployments for Python sites

Python sites support both standard and zero-downtime deployment strategies. In zero-downtime mode, Fluxo prepares a fresh release directory instead of modifying the live code in place.

The workflow builds a new virtual environment, installs the candidate dependencies, runs the build command, and atomically switches the site's `current` symlink. Fluxo then restarts the managed Python process and waits for the assigned port to accept connections. If the new process does not become reachable, Fluxo attempts to restore the previous release.

The filesystem switch is atomic, but this release still manages one application service generation at a time. Existing long-lived connections may briefly disconnect during the process restart. Connection-level zero downtime would require overlapping, health-aware process generations rather than only an atomic release switch.

Uploads and other irreplaceable runtime data should remain outside release directories or be linked from persistent storage. Fluxo persists the root environment automatically, but it cannot infer every framework-specific upload, media, or generated-data path.

## A safe escape hatch for custom Nginx requirements

Fluxo normally generates a complete Nginx configuration from the site's application type, domains, certificates, runtime, and directory settings. That managed mode is the best fit for most sites because dashboard changes and upgrades can regenerate the correct configuration consistently.

Some applications need directives that a general-purpose form cannot reasonably model: unusual proxy headers, a framework-specific cache policy, a protected internal location, custom rewrites, or routing to another upstream. Version 0.4.27 adds **Site > Settings > Vhost** for those cases.

The editor exposes the complete virtual host Fluxo expects for the site. Saving a custom version follows a guarded activation sequence:

1. Fluxo stages the replacement at the site's existing infrastructure path.
2. It validates the complete Nginx configuration with `nginx -t`.
3. It reloads Nginx only after validation succeeds.
4. It persists the override only after successful activation.
5. If validation or activation fails, it restores the previous working file and reloads that configuration.

This is not merely a text box writing directly into `/etc/nginx`. The configuration has to pass the server-level validation gate before it can become active.

## Revision checks prevent stale-tab overwrites

The vhost editor tracks a configuration revision. If two browser tabs or operators load the same configuration and one saves a newer version, the older view cannot silently overwrite it. Fluxo returns a conflict and requires the stale editor to reload the current state.

The page also warns about unsaved changes, supports the familiar save shortcut, and lets you discard edits without touching the active server configuration.

Custom vhosts are limited to 256 KiB. This is large enough for a practical site configuration while keeping the editor and persistence boundary focused on Nginx configuration rather than arbitrary server files.

## Managed mode versus customized mode

After the first successful custom save, the site is marked **Customized**. Fluxo stores the exact override and reapplies it when another site action would normally regenerate Nginx.

That persistence is important, but it also establishes a clear ownership boundary. While a custom vhost is active, later dashboard changes to domains, certificates, paths, PHP, Octane, or the application runtime are recorded without rewriting the custom text. You must add the equivalent directive changes yourself or restore managed mode.

The **Restore Fluxo Default** action generates a fresh configuration from the site's current settings. It does not restore an old snapshot from the day customization began. Fluxo validates and activates the generated configuration before removing the stored override; if that process fails, the custom version remains active or is reactivated.

When using Let's Encrypt, keep the `/.well-known/acme-challenge/` location reachable. A custom vhost that blocks the HTTP-01 challenge path can prevent certificate issuance or renewal even if the rest of the syntax is valid.

## Runtime-aware compatibility safeguards

Adding another application runtime affects more than creation screens. Lifecycle operations must know which sites depend on PHP-FPM, which use a Node.js service, and which now use a Python service.

Version 0.4.27 makes those capabilities explicit. PHP lifecycle operations remain limited to PHP-based applications, while Node.js and Python sites use their own runtime and process handling. Commands, cron jobs, deploy output, site settings, Nginx generation, and service recovery now follow the site's application capabilities instead of assuming every site behaves like PHP.

The release also fixes the Nginx and Let's Encrypt challenge root for Django applications in subdirectories and zero-downtime release layouts. Customized Python build and start commands are preserved when the settings page hydrates its framework fields.

## Upgrade behavior

The database changes in 0.4.27 are additive and applied automatically when Fluxo starts. Existing Laravel, WordPress, PHP, Node.js, and static sites retain their stored application type, deployment settings, Nginx behavior, and process lifecycle.

Python support is opt-in. You can prepare it during installation with `--python`, or install support later from **Runtime > Python**. Fluxo requires Python 3.10 or newer together with the standard virtual-environment tooling.

To upgrade to the latest release:

```bash
curl -fsSL https://fluxo.fottify.com/install.sh | sudo bash
```

To request exactly version 0.4.27:

```bash
curl -fsSL https://fluxo.fottify.com/install.sh | \
  FLUXO_VERSION=v0.4.27 sudo -E bash
```

For a fresh non-interactive installation with Python support:

```bash
curl -fsSL https://fluxo.fottify.com/install.sh | sudo bash -s -- \
  --db-engine=postgres \
  --no-redis \
  --no-node \
  --python
```

Existing sites do not receive a custom vhost automatically. They remain in Fluxo-managed mode until an operator explicitly saves an override.

## What to verify after upgrading

Start with the installed version and service health:

```bash
fluxo --version
sudo systemctl status fluxo --no-pager
curl -k https://127.0.0.1:9595/api/v1/health
```

Then check the dashboard:

1. Confirm existing sites still report the correct application types and deployment strategies.
2. Open **Runtime > Python** and install support if this server will host Python applications.
3. Create a small starter site for the intended framework and verify its process becomes healthy.
4. Review its Nginx and application logs before connecting production traffic.
5. Open **Site > Settings > Vhost** on a test site and confirm managed mode displays the generated configuration.
6. If you need an override, retain SSH or provider-console recovery access, save a small controlled change, and test both the application and certificate challenge path.

For production applications, also test a real repository deployment, dependency locking, database connectivity, persistent uploads, process recovery, and rollback behavior before migrating traffic.

## A broader foundation for self-hosted applications

Fluxo began with a strong focus on PHP and Laravel operations. Version 0.4.27 broadens that foundation without hiding the operational differences between runtimes.

Python applications now have a managed path from source code to an isolated environment, an application service, Nginx, TLS, deployments, logs, and recovery. Advanced users can customize the resulting Nginx virtual host through a workflow that validates changes and preserves a route back to the generated default.

Read the complete [Python hosting guide](/docs/sites/python) and [Nginx vhost editor guide](/docs/site-management/vhost) before moving a production workload. The full code and release artifacts are available in [Fluxo 0.4.27 on GitHub](https://github.com/FabioTECH1/fluxo/releases/tag/v0.4.27).
