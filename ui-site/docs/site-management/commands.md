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

## Interactive Laravel Tinker

For Laravel sites, **Commands** contains a sidebar with **Commands** and **Tinker**. Tinker opens a real, persistent `php artisan tinker` session using the site's selected PHP CLI and active release directory. For example, assign `$user = App\Models\User::find(1);`, then inspect `$user` in a later input. Variables survive until that session ends.

Click **Connect** and verify your current Fluxo password. The page requires HTTPS, the installed systemd-managed Fluxo service, and `laravel/tinker` in the application's production dependencies (`require` in `composer.json`). Fluxo does not install packages automatically. Missing dependencies or application boot errors appear in the console.

Use Enter, multiline PHP, arrow keys, Tab, Ctrl-C and Ctrl-D as in a local Tinker session. **Disconnect** ends the process and its managed descendants. Reconnect requires password verification and starts fresh. There is no background reattachment or automatic reconnection. Leaving the page, signing out, closing the connection, authentication expiry/revocation, five minutes without input, or fifteen minutes total ends the session. Browser refresh also ends the session. Network failures are detected through heartbeat timeouts.

A maximum of four pending/active sessions is allowed per server, with one per administrator/site. Connection attempts are limited to five per account and IP in fifteen minutes, including successful connections. Disconnect before changing the site or deploying. Runtime/package maintenance waits for live consoles to end.

Fluxo records session start/end metadata in daemon logs, not code or terminal output. Browser terminal scrollback is bounded and removed on disconnect. Application code can still deliberately write files or application logs.

::: warning Administrative access to live applications
Tinker executes arbitrary PHP as the shared `fluxo` account. It can modify production data, access secrets and files available to that account, and start subprocesses. It is not a per-site sandbox. Tinker processes have privilege elevation disabled, but existing applications and processes sharing the account are not isolated from one another. Disconnecting or interrupting does not undo committed data changes, dispatched jobs, emails or other side effects.
:::
