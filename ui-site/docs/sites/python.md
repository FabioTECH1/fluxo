---
title: Python
description: Deploy Django, Flask, FastAPI, and generic Python web applications with isolated environments and managed processes.
---

# Python sites

Fluxo hosts Python web applications as managed processes behind Nginx. Each site receives its own virtual environment, internal port, environment file, deployment settings, and systemd service. Python itself is not a network service and does not consume RAM while idle; the hosted application process does.

## Install application support

Open **Runtime > Python** and select **Install support** before creating a Python site. Fluxo requires Python 3.10 or newer, the standard `venv` and `ensurepip` modules, and the release-pinned `uv` binary.

The installer can prepare the same components during server provisioning:

```bash
curl -fsSL https://fluxo.fottify.com/install.sh | sudo bash -s -- \
  --db-engine=postgres \
  --no-redis \
  --no-node \
  --python
```

Fluxo uses Ubuntu's supported system Python and does not replace it with a custom global interpreter. Removing Fluxo-managed Python tools removes only Fluxo's verified `uv` installation. It does not remove system Python, site code, or existing virtual environments.

## Presets

| Preset | Default entrypoint | Managed server |
|---|---|---|
| Django | `config.wsgi:application` | Gunicorn |
| Flask | `app:app` | Gunicorn |
| FastAPI | `main:app` | Uvicorn |
| Generic | Supplied by the application | Custom command or Gunicorn |

The entrypoint is a Python module and callable separated by a colon. Change it when the project uses a different module layout, for example `src.web:app` or `project.wsgi:application`.

For a repository-backed Generic site, a start command is required. Framework presets receive a safe default, but the command remains editable later in **Site > Settings > General**.

## Application directory

The application directory is relative to the checked-out repository root. Use `.` when dependency files and the entrypoint are at the root. For a monorepo, enter a path such as `services/api`.

Fluxo creates the virtual environment and runs dependency, build, and application commands from this directory. Absolute paths and paths that leave the site root are rejected.

## Dependencies

Choose **pip** or **uv** during site creation.

With pip, Fluxo creates `.venv` and then:

- Installs `requirements.txt` when present.
- Otherwise installs the current project when `pyproject.toml` is present.
- Leaves an empty virtual environment when neither file exists.

With uv, Fluxo:

- Runs `uv sync --frozen` when both `uv.lock` and `pyproject.toml` exist.
- Installs `requirements.txt` into `.venv` when present.
- Runs `uv sync` for an unlocked `pyproject.toml` project.
- Creates an empty `.venv` when no dependency manifest exists.

Commit the lock file when using uv so deployments reproduce the dependency graph already reviewed by your team.

## Build and start commands

The build command runs after dependencies are installed. Django defaults to `collectstatic`, and Fluxo serves the resulting `staticfiles/` directory at `/static/` through Nginx. Migrations are not run automatically because database migrations need an application-specific release policy. Add an explicit migration command under **Site > Settings > Deployments** when appropriate.

The start command is stored separately from the deployment script and belongs to the managed systemd process. Fluxo replaces `$FLUXO_APP_PORT` with the assigned internal port before generating the service. Applications must listen on `127.0.0.1` so public traffic continues through Nginx.

Do not start Gunicorn or Uvicorn in the deployment script. Fluxo restarts the managed application after activating code and waits for the configured port to accept connections.

## Environment and databases

Fluxo stores the editable environment at `/home/fluxo/DOMAIN/.env` and supplies it to the Python systemd unit. For a nested application directory, an application-level `.env` symlink points back to this persistent file.

New sites include `PYTHONUNBUFFERED`, `HOST`, and `PORT`. Django starter sites also receive `ALLOWED_HOSTS`. When a database is attached, Fluxo writes dedicated `DB_CONNECTION`, `DB_HOST`, `DB_PORT`, `DB_DATABASE`, `DB_USERNAME`, and `DB_PASSWORD` values.

Python sites can use MySQL/MariaDB or PostgreSQL. Fluxo verifies the selected dedicated database account before provisioning. Your application remains responsible for using those variables or translating them into its own configuration model.

## Process and logs

Fluxo creates a protected **Python** process under **Site > Processes**. Systemd starts it after boot and restarts it after an unexpected exit. Deployments and runtime controls can restart the process, but it cannot be deleted like a user-created daemon while the site exists.

Nginx access and error logs are available in **Site > Observe**. Application stdout and stderr are written to the managed daemon log under `/var/log/fluxo/`.

## Zero downtime

Python sites support standard and zero-downtime deployment. Zero-downtime mode builds a fresh release, installs that release's `.venv`, runs the build command, atomically switches `current`, and then restarts the Python process. If the new process does not accept connections, Fluxo attempts to restore the previous release.

The release switch is atomic, but one managed application process is restarted. Existing long-lived connections can briefly disconnect. Multiple health-aware workers with overlapping generations would be required for connection-level zero downtime.

Keep uploads and other irreplaceable runtime data outside release directories or link them from persistent storage. Fluxo automatically persists the root `.env`; it cannot infer every framework-specific upload directory.

## Starter applications

When no Git repository is selected, Fluxo creates a minimal starter for the chosen preset so the site can be verified immediately. These starters are intentionally small foundations, not production templates. Review security, middleware, database migrations, static files, observability, and worker needs before building on them.
