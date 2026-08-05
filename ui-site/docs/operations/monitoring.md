---
title: Monitoring and Logs
description: Read system metrics, service logs, site logs, deployment history, and activity events.
---

# Monitoring and logs

Fluxo combines lightweight system metrics with structured activity and service-specific logs. It is an operational view, not a long-term metrics warehouse.

## Server monitoring

**Observe > Monitoring** displays CPU information, Linux load averages, memory, swap, disk use, uptime, operating-system information, and related host metrics.

### CPU Load (1m, 5m, 15m)

These values are Linux load averages, not percentages. They represent the average number of runnable or uninterruptible tasks over one, five, and fifteen minutes.

Compare load with available CPU cores:

- Load near `1.00` on a one-core server means roughly one core is continuously occupied.
- Load near `4.00` on a four-core server means the machine is near full scheduling capacity.
- A sustained load far above the core count indicates queueing or blocked I/O.

An idle server can correctly show `0.00 0.00 0.00`. Refresh metrics after creating actual CPU or I/O activity before treating zero as a collection failure.

## Server logs

**Observe > Logs** discovers supported Nginx, PHP-FPM, MariaDB/MySQL, PostgreSQL, Redis, Fluxo daemon, managed process, and cron log files. You can refresh, download, and clear allowed logs.

Log availability depends on which services and files exist on the server.

## Site logs

Each site exposes its Nginx access and error logs plus relevant PHP or application process logs. Start with:

- Nginx error log for routing, upstream, and filesystem failures
- PHP-FPM log for PHP startup and worker errors
- Managed daemon log for Node, Horizon, Octane, Nightwatch, or custom processes
- Deployment output for build and activation failures

## Activity

The activity feed records administrative operations such as provisioning, configuration changes, deployments, SSL actions, file changes, and warnings. Entries can include the site, administrator identity, and client address where applicable.

Activity history explains what Fluxo was asked to do. It does not replace operating-system audit logging or external security monitoring.

## External monitoring

For production availability, add an independent uptime monitor and provider-level alerts. External checks can detect a server, network, DNS, or certificate failure even when the Fluxo dashboard is unreachable.
