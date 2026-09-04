---
title: Commands
description: Run and review one-off application commands in a site's active directory.
---

# Commands

The Commands page runs a single executable with arguments as the `fluxo` system user in the site's active working directory. It streams output, stores status and output in history, and enforces a two-minute execution limit.

## Examples

Laravel:

```bash
artisan route:list
```

WordPress:

```bash
wp core version
wp plugin list
```

PHP or Composer:

```bash
php -v
composer show
```

Node.js:

```bash
node --version
npm run lint
```

Python:

```bash
.venv/bin/python manage.py check
```

For Python sites, commands begin in the configured application directory inside the active release, so the site's `.venv` and framework files are available directly.

Fluxo resolves supported convenience forms such as Laravel's `artisan` command using the site's active PHP version and path.

## WordPress path handling

For WordPress sites, Fluxo appends `--path=WEB_ROOT` to a `wp` command unless the command already contains a path argument. This lets WP-CLI find installations stored under `/public` without repeating the path.

## History

Command history includes the command, status, timestamp, and captured output. You can inspect, rerun, or delete a historical entry. Removing history does not undo the command's effect.

## Limitations

Commands run non-interactively and are not a full terminal session. They cannot wait for password prompts, editors, or interactive confirmations. Include non-interactive flags where the tool supports them.

Use deployments for repeatable application changes. Use Commands for diagnosis and deliberate one-off administration.

::: warning Production impact
A command runs against the active application directory and can modify production state. Review migrations, cache clears, destructive WP-CLI operations, and database commands before execution.
:::
