---
title: Daemons and Scheduler
description: Run persistent background processes and scheduled commands for a site.
---

# Daemons and scheduler

Fluxo manages persistent processes with systemd and scheduled jobs through system cron configuration. Site-scoped entries run as the `fluxo` user in the site's active directory. Python entries use the configured application directory and receive the site's persistent `.env` values.

The add buttons on the site overview open the same complete forms as **Site > Processes**, so process and scheduled-job behavior is consistent from either entry point.

## Background processes

Open **Site > Processes > Daemons** and add a process with:

- A display name
- The executable command and arguments
- The generated site working directory
- One or more process instances
- Start and stop grace periods
- A stop signal
- Optional restart-after-deployment behavior

systemd starts every configured process instance, restarts it when configured to do so, and starts it again after a server reboot. Each instance has its own systemd unit and shares the process group's Fluxo log. New custom groups are limited to 64 processes. The dashboard reports a group as degraded when only some instances are active and provides start, stop, restart, logs, and delete actions.

For zero-downtime sites, new daemons use the `current` path so the same unit follows release activation. Enable **Restart after deployments** when the process must load new code.

For a Python executable inside the virtual environment, use an explicit command such as `/usr/bin/env .venv/bin/python worker.py` or `/usr/bin/env .venv/bin/celery -A app worker`. The `/usr/bin/env` prefix lets systemd launch a relative virtual-environment executable from the managed working directory.

Fluxo-managed Node.js, Python, Queue Worker, Horizon, Octane, and Nightwatch processes own their deployment policy and do not expose the generic restart toggle. Managed Queue Workers always display their fixed graceful deployment restart behavior. Use the Queue Worker row's **Configure worker** action to change its connection or process settings, and disable managed processes from their owning site or Laravel feature control rather than deleting them from the daemon list.

## Scheduled jobs

Open **Site > Processes > Scheduler** to create a named cron expression and command. Fluxo can also run the job immediately, display its logs, and remove it.

Common schedules:

```text
* * * * *       Every minute
0 * * * *       Every hour
0 2 * * *       Daily at 02:00 server time
0 2 * * 0       Sundays at 02:00 server time
```

Cron uses the server's timezone. Confirm the server clock and timezone before relying on wall-clock schedules.

## Choosing the right mechanism

| Workload | Use |
|---|---|
| HTTP Node server | Managed Node.js site process |
| Gunicorn, Uvicorn, or another Python web server | Managed Python site process |
| Laravel queue consumer | Queue Worker feature, or Horizon for Redis queues |
| Custom queue consumer or websocket server | Daemon |
| Laravel scheduled tasks | Laravel Scheduler feature |
| Periodic backup or cleanup command | Scheduled job |
| One-time diagnostic | Commands page |

## Logs

Daemon and scheduled-job logs are available from their action menus. For lower-level diagnosis, inspect the corresponding systemd journal as root over SSH.

Process logs live in a root-owned directory and are created as single-link regular files for the configured service user. On upgrade, Fluxo detects legacy symlinked or hardlinked logs, disables the affected job or service, replaces the unsafe entry, regenerates its configuration, and restores its previous enabled/running state only after repair succeeds.
