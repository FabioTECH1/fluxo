---
title: Deployment Scripts
description: Customize application deployment commands without breaking the Fluxo lifecycle.
---

# Deployment scripts

Open **Site > Settings > Deployments** to edit the commands that prepare the application. New sites use a managed script mode: Fluxo owns repository synchronization, release activation, service restarts, and cleanup, while the editor contains application-specific commands.

## Managed script model

Write commands as though the current directory is the code being deployed. Fluxo sets that directory before executing the editor content.

For example, Laravel defaults include:

```bash
if [ -f composer.json ]; then
  $FLUXO_COMPOSER install --no-dev --no-interaction --prefer-dist --optimize-autoloader
fi

if [ -f package.json ]; then
  (npm ci || npm install)
  npm run --if-present build
fi

if [ -f artisan ]; then
  $FLUXO_PHP artisan optimize:clear
  $FLUXO_PHP artisan storage:link
  # Included by Fluxo only when a database was connected during site creation.
  $FLUXO_PHP artisan migrate --force
fi
```

Do not clone the repository, switch the `current` symlink, restart the managed Node service, or delete old releases in this editor. Those operations belong to the platform lifecycle.

## Available variables

| Variable | Meaning |
|---|---|
| `FLUXO_DOMAIN` | Primary site domain |
| `FLUXO_SITE_PATH` | Persistent site root |
| `FLUXO_ACTIVE_SITE_PATH` | Currently active application path |
| `FLUXO_DEPLOY_PATH` | Directory whose code is being prepared |
| `FLUXO_RELEASE_DIRECTORY` | New release path during managed zero downtime |
| `FLUXO_WEB_ROOT` | Resolved web directory for this deployment |
| `FLUXO_REPO` | SSH repository URL |
| `FLUXO_BRANCH` | Configured branch |
| `FLUXO_PHP_VERSION` | Site PHP version |
| `FLUXO_PHP` | Version-specific PHP executable |
| `FLUXO_COMPOSER` | Composer invoked with the site PHP version |
| `FLUXO_APP_TYPE` | `laravel`, `php`, `wordpress`, `node`, or `html` |
| `FLUXO_APP_PORT` | Internal Node/Octane port when applicable |

Node sites additionally receive package-manager, install, build, start, preset, mode, and static-output variables.

## Shell behavior

Managed scripts execute with Bash strict error handling. A failed command stops the application phase and marks the deployment failed. Use explicit conditionals for optional files and commands.

Prefer Fluxo-provided PHP and Composer variables over bare `php` or `composer` so the command uses the site's configured PHP version.

## Reset to managed defaults

Imported or older sites may show a complete legacy script. Fluxo preserves it unchanged to avoid rewriting user-owned behavior. The deployment settings page offers a reset action that replaces it with current application-specific defaults and re-enables the managed lifecycle.

Review custom persistence, build, and framework commands before resetting. Fluxo cannot infer every modification from a full legacy script.

## WordPress

WordPress starts with an empty deployment script because it is installed and managed in place. Add a script only if you have a deliberate deployment process. A WordPress deploy action is rejected while the script is empty.
