---
title: Monitor server health from one place
excerpt: Use CPU, load, memory, disk, service logs, deployment history, and activity events as a repeatable first-response workflow when a site slows down or fails.
category: Operations
date: 2026-08-14
image: /blog/server-monitoring.webp
imageAlt: Server rack beside a health and performance dashboard
---

When a production site becomes slow, the fastest response is rarely random restarting. A useful investigation starts broad, narrows toward the affected service, and preserves enough evidence to explain what happened.

Fluxo combines current server metrics, discovered service logs, site-specific logs, deployment output, managed-process state, and administrative activity. It is designed for operational triage, not as a long-term metrics warehouse. This guide shows how to use that view without misreading the signals.

## Begin with the symptom and timeframe

Write down what is actually failing before looking at charts:

- Is the whole site unavailable or only one route?
- Are requests slow, returning 5xx responses, or timing out?
- Did the problem begin suddenly or degrade gradually?
- Does it affect every user, one location, or one account?
- Did it begin after a deployment, configuration change, traffic event, or scheduled task?

A precise timeframe lets you correlate logs and activity. “The checkout began returning 502 responses at 14:08, immediately after deployment 42” is much more actionable than “the server is broken.”

Avoid changing several things at once. Every restart, cache clear, or configuration edit can remove evidence and make the eventual cause harder to establish.

## Step 1: Check whether the host is under pressure

Open **Observe > Monitoring** and start with CPU information, Linux load averages, memory, swap, disk use, and uptime.

These answer the first branching question: is the issue likely isolated to one application or is the whole machine struggling?

### Interpret load averages correctly

The one-, five-, and fifteen-minute load values are not CPU percentages. They represent the average number of runnable tasks or tasks waiting in uninterruptible I/O.

Compare load with the number of CPU cores:

- A load near `1.00` on a one-core server means roughly one task continuously occupies the available execution capacity.
- A load near `4.00` on a four-core server means the host is close to full scheduling capacity.
- Sustained load well above the core count means work is queueing or blocked.

The trend matters. If the one-minute load is high while the five- and fifteen-minute values are lower, pressure may have just started. If all three are high, the condition is sustained.

High load does not always mean CPU saturation. Disk or network storage waits can add tasks to load while CPU usage remains modest. That is why metrics should lead to logs and process inspection rather than a conclusion by themselves.

An idle host can legitimately show `0.00 0.00 0.00`. Refresh after creating actual work before treating zero as a collection failure.

### Read memory and swap together

Linux uses otherwise idle memory for filesystem cache, so “used” memory alone can be misleading. Focus on available memory, swap activity, application behavior, and whether processes are being killed.

Warning patterns include:

- Available memory approaching zero during builds or traffic peaks
- Swap filling and request latency rising sharply
- PHP-FPM, database, Node, Python, or queue processes restarting unexpectedly
- Kernel or service logs indicating out-of-memory termination

A small amount of used swap is not automatically an incident. Constant memory pressure and active swapping are the more important signs.

If a build repeatedly exhausts a small VPS, restarting the site is not a durable fix. Reduce build concurrency, move builds elsewhere, add swap as a short-term safeguard, or resize the server according to the workload.

### Treat disk space as an availability metric

A full filesystem can break deployments, database writes, logs, sessions, package installation, and certificate renewal at the same time.

Check disk use early, especially after:

- Several large releases or dependency installations
- Rapid log growth
- Local uploads or media processing
- Database growth
- Temporary backup creation
- A failed build that left large artifacts elsewhere

Fluxo retains the five newest successful zero-downtime releases, but application data, database files, logs, caches, and virtual environments can still outgrow the server. Keep enough headroom for one additional release and its build process, not only the currently running application.

Do not delete unfamiliar files during an incident. Identify ownership and recovery implications first.

## Step 2: Check uptime and scope

Server uptime can reveal an unexpected reboot. If uptime is short and multiple services are unavailable, investigate provider events, kernel updates, power loss, or startup failures before treating each application independently.

Compare more than one site when the server hosts several applications:

- If every site fails, inspect Nginx, the host, networking, and shared services.
- If PHP sites fail but static sites work, inspect PHP-FPM.
- If one site fails, prioritize its deployment, Nginx error log, environment, and managed processes.
- If database-backed routes fail while static pages work, inspect the database service and credentials.

This simple comparison narrows the search faster than opening every log at once.

## Step 3: Read the log closest to the symptom

Open **Observe > Logs** for server-wide logs or the affected site's logs. Fluxo discovers supported Nginx, PHP-FPM, MariaDB/MySQL, PostgreSQL, Redis, Fluxo daemon, managed-process, and cron files that actually exist on the server.

Use the request path to choose where to start.

### Nginx access and error logs

The access log establishes response codes, request frequency, client patterns, and whether traffic reaches the server. The error log explains routing, upstream, permission, certificate, and filesystem failures.

Common patterns:

- `502 Bad Gateway`: the upstream PHP, Node, or Python process is unavailable or misconfigured.
- `504 Gateway Timeout`: the upstream accepted the connection but did not respond within the configured time.
- Repeated `404` for compiled assets: a build or web-root mismatch.
- Permission denied: ownership, mode, or path traversal prevents Nginx from reading the target.
- Certificate or hostname errors: the active certificate does not cover the requested name or the Nginx configuration did not activate as expected.

### PHP-FPM and Laravel logs

PHP-FPM logs reveal worker startup, pool, socket, and process failures. Laravel logs are better for exceptions, failed jobs, external API errors, and application-specific context.

Correlate timestamps between Nginx and Laravel. A 500 in the access log at the same second as a database exception in Laravel gives you a much stronger causal link than either line alone.

Never paste logs containing environment values, access tokens, session identifiers, or personal data into a public issue without redaction.

### Managed process logs

Node.js, Python, Queue Worker, Horizon, Octane, Nightwatch, and custom daemons have process state and logs. Check whether the service is running, stopped, restarting, or degraded because only some configured instances are active.

A process that exits repeatedly usually needs its first startup error, not another restart. Look for a missing environment variable, unavailable database, occupied port, invalid command, or runtime mismatch.

For queue systems, distinguish web health from background health. The website can return 200 responses while jobs accumulate because workers are stopped or failing.

### Database and Redis logs

Inspect database logs when requests report connection refusal, authentication errors, deadlocks, crashes, storage exhaustion, or long-running work. Redis logs matter when Laravel cache, sessions, queues, Horizon, or application state depend on it.

Do not expose a local database port to the internet as a troubleshooting shortcut. Diagnose through the server or a deliberately secured administrative path.

## Step 4: Correlate deployments

Open the site's deployment history when the incident follows a release. Fluxo retains the command output and keeps a failed deployment alert visible until it is dismissed or superseded by a success.

Identify the phase that failed:

| Phase | Typical causes |
|---|---|
| Git preparation | Repository access, deploy key, branch, or network |
| Dependency installation | Lockfile conflict, package registry, missing runtime |
| Build | Missing environment, compilation error, memory pressure |
| Framework commands | Migration, cache, permissions, database access |
| Activation hook | Managed process start, occupied port, worker restart |
| Timeout | Command exceeded the deployment deadline |

For a successful deployment followed by bad application behavior, compare the commit, migration, environment changes, and service restarts with the incident start time.

In zero-downtime mode, a failure before activation leaves the previous release active. A rollback rebuilds a selected previous commit as a new deployment, but it does not reverse database migrations automatically.

## Step 5: Review administrative activity

The activity feed records operations Fluxo was asked to perform, including provisioning, deployments, SSL actions, file changes, configuration edits, and warnings. Entries can identify the affected site, administrator, and client address where applicable.

Use activity to answer:

- Was the PHP version changed?
- Was an environment file edited?
- Did someone rotate a database password?
- Was a domain or certificate changed?
- Was a daemon stopped or deleted?
- Did a deployment begin immediately before the symptom?

Activity is an application-level operational record. It does not replace operating-system audit logs, provider events, identity-provider logs, or external security monitoring.

## A repeatable ten-minute triage sequence

When time is limited, use the same order every time:

1. Confirm the symptom from an external network or monitor.
2. Record the first observed time and affected routes.
3. Check server uptime, CPU/load, memory/swap, and disk.
4. Compare another site or a static path to determine scope.
5. Read the affected site's Nginx error log.
6. Read the relevant PHP, application, or managed-process log.
7. Check process state and local database or Redis health.
8. Correlate the most recent deployment and activity entries.
9. Apply the smallest reversible mitigation.
10. Verify from outside the dashboard and keep watching.

If the dashboard itself is unavailable, switch to provider-console or SSH diagnostics. That is also why external monitoring and recovery access matter.

## Common symptom-to-signal mappings

### The entire server feels slow

Check sustained load relative to core count, memory pressure, swap, disk saturation, and free storage. Look for a build, backup, database query, or runaway process that began at the same time.

### The site returns 502

Start with Nginx error output, then inspect PHP-FPM or the managed Node/Python process. Confirm the expected socket or internal port is available and the service is not in a restart loop.

### The site works, but jobs are delayed

Inspect Queue Worker or Horizon state, queue connection settings, Redis or database health, and worker logs. Confirm the deployment restart hook completed.

### A deployment hangs or times out

Check live deployment output, available memory and disk, package-manager activity, registry reachability, and long-running framework commands. Fluxo's deployment deadline prevents work from running forever, but the first slow command explains the cause.

### Errors began after a configuration edit

Use activity to identify the change, then compare the environment, domain, runtime, or process settings. Restore one known value at a time and verify the result.

## Build a baseline before the incident

Metrics are easier to interpret when you know what normal looks like. Record or export a lightweight baseline for each production server:

- Typical one-, five-, and fifteen-minute load
- Normal memory and swap use
- Disk growth per month
- Expected process count
- Usual backup duration and artifact size
- Normal deployment duration
- Peak traffic periods

Fluxo is not a long-term time-series platform, so use external monitoring when you need historical graphs, percentile latency, alerting, or cross-server comparison.

## Add monitoring outside the server

An internal dashboard cannot report that the whole server, network, DNS, or TLS path is unreachable. Add at least:

- External HTTP uptime checks for critical routes
- Certificate-expiry monitoring
- Provider CPU, disk, and network alerts where available
- Error reporting inside the application
- A notification path that does not depend on the affected server

For important applications, monitor a lightweight database-backed health route rather than only the homepage. Keep that route safe: it should not expose secrets or perform expensive checks on every request.

## Turn the investigation into prevention

After service is stable, write a short incident record:

- User-visible impact and duration
- Timeline of signals and changes
- Root cause and contributing conditions
- Mitigation used
- Evidence that recovery succeeded
- Follow-up owner and deadline

The follow-up might be a disk alert, a safer migration, more memory, a process limit, a documented rollback, or a code fix. Monitoring is valuable when it shortens both the current incident and the next one.

Fluxo gives you a focused operational view. A consistent triage order turns that view into a dependable first response instead of a collection of unrelated numbers and logs.
