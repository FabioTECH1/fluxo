---
title: Daemons and Scheduler
description: Run persistent background processes and scheduled commands for a site.
---

# Daemons and scheduler

Fluxo manages persistent processes with systemd and scheduled jobs through system cron configuration. Site-scoped entries run as the `fluxo` user in the site's active directory.

## Background processes

Open **Site > Processes > Daemons** and add a process with:

- A display name
- The executable command and arguments
- The generated site working directory
- One or more process instances
- Start and stop grace periods
- A stop signal
- Optional restart-after-deployment behavior

systemd starts the process, restarts it when configured to do so, and starts it again after a server reboot. The dashboard provides start, stop, restart, logs, and delete actions.

For zero-downtime sites, new daemons use the `current` path so the same unit follows release activation. Enable **Restart after deployments** when the process must load new code.

Fluxo-managed Node.js, Horizon, Octane, and Nightwatch processes own their deployment policy and do not expose the generic restart toggle.

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
| Queue consumer or websocket server | Daemon |
| Laravel scheduled tasks | Laravel Scheduler feature |
| Periodic backup or cleanup command | Scheduled job |
| One-time diagnostic | Commands page |

## Logs

Daemon and scheduled-job logs are available from their action menus. For lower-level diagnosis, inspect the corresponding systemd journal as root over SSH.

