---
title: CLI Commands
description: Fluxo service, version, and root-only account-recovery commands.
---

# CLI commands

The production binary is installed at `/usr/local/bin/fluxo`. Most server management belongs in the dashboard; the public CLI focuses on service startup, diagnostics, and account recovery.

## Version

```bash
fluxo --version
```

Example output:

```text
fluxo version 0.4.27
```

## Show the administrator username

```bash
sudo fluxo --show-admin-username
```

This root-only command opens the installed Fluxo SQLite database read-only and reports the claimed username. It does not change account state or invalidate sessions.

## Reset the administrator token

```bash
sudo fluxo --reset-token
```

This root-only command:

1. Reports the configured username or first-login state.
2. Generates a new secure token.
3. Displays the new token once in the command output.
4. Replaces the stored password hash.
5. Invalidates existing JWT sessions.
6. Writes the current recovery details to `/var/lib/fluxo/.fluxo_credentials` with root-only permissions.

The reset is journaled before either the recovery file or database hash becomes authoritative. If the command is interrupted, normal Fluxo startup completes the same reset token instead of leaving an invalid recovery copy.

Use the token to sign in, then change the password from Settings.

Do not combine `--reset-token` and `--show-admin-username`; Fluxo exits with status 2 rather than choosing one unexpectedly.

## Service commands

```bash
sudo systemctl status fluxo --no-pager
sudo systemctl restart fluxo
sudo systemctl stop fluxo
sudo systemctl start fluxo
sudo journalctl -u fluxo -f
```

Restarting the Fluxo daemon temporarily interrupts dashboard and API access. It does not restart hosted Nginx, PHP-FPM, database, or application services unless startup recovery explicitly reconciles their state.

## Health endpoint

```bash
curl -k https://127.0.0.1:9595/api/v1/health
```

The health endpoint is public and suitable for local service verification. Do not treat it as proof that every hosted site and dependency is healthy.

When a panel domain is active, `https://PANEL_DOMAIN/api/v1/health` checks the Nginx proxy path as well. Fluxo's installer verifies both the local endpoint and any active panel domain during upgrades.

## Runtime environment

| Variable | Default | Purpose |
|---|---|---|
| `FLUXO_ENV` | `dev` | Use `prod` for installed-server data defaults |
| `FLUXO_PORT` | `9595` | Dashboard/API listen port; port `6060` is reserved for loopback diagnostics |
| `FLUXO_DATA_DIR` | Current directory, or `/var/lib/fluxo` in prod | Persistent control data |
| `FLUXO_USE_HTTP` | unset | Set to `1` only behind a trusted local reverse proxy |

The installed systemd service sets production configuration. Override it only with a deliberate systemd drop-in and retain a recovery path.
