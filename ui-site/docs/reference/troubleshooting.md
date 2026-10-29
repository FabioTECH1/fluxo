---
title: Troubleshooting
description: Diagnose common Fluxo access, site, deployment, SSL, database, runtime, and process problems.
---

# Troubleshooting

## PHP patch updates

Daily and manual checks refresh APT metadata but never install packages or inspect application-service readiness. A successful check reports package availability independently of installation blockers. Selecting Update performs the separate runtime review; its confirmation explains blockers without stopping anything. Failed checks keep prior candidate information as unavailable/stale; use **Check for updates** after repairing repository access. Package holds, pinning, repository transitions and new dependencies can block package eligibility. Custom PHP extensions and unsupported process wrappers can block installation review. Active deployments can block installation. Fluxo-generated Nightwatch agents now stop/restart automatically; monitoring pauses and buffered telemetry may be lost. For an unsupported/custom process, merely stopping it does not remove the blocker: disable/remove its Fluxo entry before reviewing again and keep it disabled until maintenance finishes. Supported Fluxo-managed Laravel Scheduler entries are held and drained automatically. A supported legacy-entry migration waits for actual cron reload receipts and natural completion of earlier jobs; it does not require a VPS or cron restart. Review again after cron ticks and current work finish. If the manual-panel boundary is unverified, start the compatible panel and rerun the installer with `--reinstall`. Unknown cron/PAM configurations need operator review and manual PHP maintenance over SSH; do not delete the journal to force readiness. Custom or unsupported Scheduler configurations still require operator attention.

PHP updates run as `fluxo-php-update-<operation UUID>.service`, independently of the panel service. Closing a page or restarting Fluxo does not cancel the job. Use **Runtime > PHP > View report** for durable progress and error output. A temporarily disconnected browser is not proof that installation failed.

If an operation is interrupted or reports a partial installation, confirm that no APT/dpkg process is still working before attempting recovery. Inspect the recorded output, the operation's systemd journal, and `sudo dpkg --audit`. A stopped supervisor does not prove its child processes have exited. Do not delete package locks or maintenance hold files, kill a working package manager, or repeatedly send signals to an uncertain runtime.

New PHP operations retain a recovery generation. Its consumer holds and block on conflicting Fluxo mutations survive panel crashes and reboots; an interrupted operation can therefore remain blocked after its worker exits. Open the latest report and review **Repair** after resolving the reported cause. Repair can resume draining/activation or replay the exact authenticated recorded package set if needed. It is not a general package repair: dirty or unknown dpkg journal state, unrelated broken packages, changed repository policy or missing/corrupt recovery evidence can require SSH intervention. Fluxo does not run broad `fix-broken` or `dpkg --configure -a` recovery.

For **draining** timeouts, allow in-flight jobs, requests, or scheduled tasks and their background children to finish and investigate processes still present in the unit's recursive cgroup. New Redis jobs may keep arriving: queue depth does not need to reach zero. Horizon's master may have exited while `fast_termination` descendants still run. Fluxo does not force-stop them, although configured application/native deadlines remain effective. Pause external schedulers, SSH-launched work and other managers yourself; Fluxo cannot prevent them from starting new PHP processes.

**Restart required** or **Health failed** means package and runtime outcomes need separate attention. Check FPM configuration/socket responses and the affected application service. Reviewed Repair can retry an unconfirmed FPM reload only for the recorded original master that still uses the old runtime; it does not re-signal a healthy replacement. A running systemd unit or successful transport probe does not establish that every application route works. Originally stopped services must remain stopped throughout recovery.

**Roll back PHP packages** is a separate reviewed action, available only with complete verified previous archives. It can reintroduce security defects and does not undo application changes, migrations, database writes, Redis state or completed jobs. Current configuration is preserved. Configuration copies at `php-updates/<operation UUID>/configuration.tar` support diagnosis and deliberate selective recovery; Fluxo does not automatically restore them. Neither Repair nor rollback is a server snapshot restore.

Before stopping consumers, Fluxo normally retains complete old/new package archives. Missing previous packages require acknowledgement before updating without package rollback; even an acknowledged update can fail during archive fetching. Retained unique archives are capped at 2 GiB per operation, with additional free-space/inode checks. Older recorded package payloads are cleaned only after a newer verified forward update; latest and protected recovery data remain. Do not delete evidence belonging to unresolved recovery; local retention cannot replace off-server backups.

**Retry service verification** applies only to legacy reports. It requires clean package state and the approved configured revisions, and never installs packages. For new generations, use the latest reviewed **Repair** action instead of creating another update or retrying an obsolete report.

### Retained PHP package storage

Fluxo automatically reclaims recorded package payloads from superseded, completed updates after a newer forward update on that branch is fully verified. The latest sets, unfinished recovery, retained installer snapshot dependencies and an exceptional prior complete rollback set remain protected. Manifests, configuration backups and reports stay available.

If older package files remain, check unresolved maintenance and the service log for `PHP package archive cleanup deferred`. Busy maintenance, changed or corrupt evidence, unknown files and unsafe filesystem layouts defer cleanup. Retries run after startup and hourly. Do not delete a generation or its manifest manually to bypass a recovery hold; review [retained package behavior](../operations/runtimes#recovery-and-retained-packages) first.

## Octane maintenance and failed enablement

Check the [qualified backend matrix](../operations/runtimes#octane-qualification). Unsupported native builds, OpenSwoole, custom launch/configuration options and active FrankenPHP consumers block automatic maintenance. Swoole package-version/rebuild changes are outside this PHP patch workflow. Legacy Octane requires the managed integration and a controlled full restart after traffic drains; worker reload does not migrate its running controller.

If native signal delivery is uncertain, keep the hold and inspect the recorded process identities. RoadRunner in particular must not receive a blind repeated termination signal. Complete a controlled native shutdown using the backend's operational procedure, wait for all descendants to exit, then retry cleanup or reviewed Repair. The observation timeout is not permission to force-kill an in-flight request.

A failed **Enable** can leave an enablement, publication, fallback or cleanup step pending. Fluxo keeps the daemon record and port reserved, preserves the intended routing transition, and blocks conflicting site edits/deployments. Retry through the Octane action after correcting the cause. Cleanup uses durable holds and native process proof; it may remain pending across restart/reboot until safe removal is possible. Do not delete the record, drop-in, hold or cleanup journal to bypass this protection.

Start with the narrowest failing layer: DNS, Nginx, application runtime, deployment, database, or Fluxo itself. Preserve the first meaningful error before restarting services.

## Dashboard is unavailable

```bash
sudo systemctl status fluxo --no-pager
sudo journalctl -u fluxo -n 150 --no-pager
sudo ss -lntp | grep 9595
curl -k https://127.0.0.1:9595/api/v1/health
sudo ufw status verbose
```

If the local health check works but a browser cannot connect, inspect the provider firewall, routing, and source-IP restrictions. If using a reverse proxy, confirm WebSocket forwarding and upstream TLS settings.

For a Fluxo-managed panel domain, also check DNS, the certificate, and the dedicated proxy:

```bash
sudo nginx -t
sudo grep -n "server_name" /etc/nginx/sites-available/fluxo-panel
curl --resolve admin.example.com:443:127.0.0.1 \
  https://admin.example.com/api/v1/health
```

Replace `admin.example.com` with the configured hostname. If the panel-domain proxy is broken but the daemon's local health check succeeds, use the direct server IP and dashboard port to repair or remove the panel domain from **Settings > General**.

An upgrade stops if the configured dashboard port or `127.0.0.1:6060` remains occupied after `systemctl stop fluxo`. Use the PID and command printed by the installer to identify the owning service. Do not kill an unknown process or start `/usr/local/bin/fluxo` manually alongside its systemd service.

## Installer reports a missing SSH privilege-separation directory

Some minimal Ubuntu VPS images expose the OpenSSH server binary before creating its volatile `/run/sshd` runtime directory. Older Fluxo installers then stop safely during preflight with:

```text
ERROR: Unable to evaluate the current SSH server configuration:
Missing privilege separation directory: /run/sshd
```

No Fluxo services or firewall rules have been installed at this point. Confirm that the path is not a symlink, restore it with the ownership and permissions OpenSSH expects, and validate SSH before retrying:

```bash
if sudo test -L /run/sshd; then
  echo "Refusing to replace symlinked /run/sshd" >&2
  exit 1
fi
sudo install -d -o root -g root -m 0755 /run/sshd
sudo sshd -t
```

`sshd -t` produces no output when validation succeeds. Re-run the installer only after that command passes. Current Fluxo installers check this prerequisite first, perform the same safe repair when the directory is absent, and then evaluate the SSH configuration. Other SSH configuration errors still stop installation and must be corrected manually.

## A domain opens the wrong site

1. Confirm the domain's `A` and `AAAA` records point only to this server.
2. Check both HTTP and HTTPS; they may be reaching different proxies or addresses.
3. Confirm Fluxo lists the hostname under the intended site's Domains page.
4. Validate and inspect Nginx:

```bash
sudo nginx -t
sudo nginx -T | less
```

Search for duplicate `server_name` entries. Fluxo installs an unknown-host guard, but a stale manually created Nginx virtual host can still conflict. Remove conflicts through their owning system and reload Nginx only after validation.

Sites created before a hostname-routing fix may still contain old generated configuration. Saving the site's settings or recreating the managed Nginx configuration can apply current templates without deleting application data.

## Deployment failed

Open the persistent failure alert, then read the complete deployment output. Check repository access, branch, runtime versions, lockfiles, environment values, database connectivity, filesystem permissions, and the first command that exited non-zero.

The deployment limit is ten minutes. Build processes killed without a normal application error may indicate memory exhaustion; inspect `journalctl` and kernel logs.

## Node.js site creation is disabled

Open **Runtime > Node.js** in the new tab offered by the form. Install or repair Node.js, npm, pnpm, Yarn, Corepack, and Bun. Return to the existing form and select **Check again**.

If runtime status stays incomplete, inspect the missing-component list and the Fluxo service journal. Node.js must be at least `22.13.0`.

## Python site creation is disabled

Open **Runtime > Python** in the new tab offered by the form. Install or repair Python application support, then return to the existing form and select **Check again**. Fluxo requires Python 3.10 or newer, working `venv` and `ensurepip` modules, and uv.

If the runtime remains incomplete, inspect the missing-component list and `sudo journalctl -u fluxo -n 100 --no-pager`. Fluxo uses Ubuntu's system Python, so a server with an unsupported distribution-provided version must be upgraded to a supported Ubuntu release rather than replacing `/usr/bin/python3` manually.

## Python application returns 502

Open **Site > Processes** and inspect the protected Python process and its logs. Confirm the configured entrypoint exists in the application directory, dependencies were installed into `.venv`, and the process binds to `127.0.0.1:$FLUXO_APP_PORT`. Review the most recent deployment output before restarting the process.

## Node.js installer rejects the SSH working directory

In v0.4.39, an upgrade started from a user's home containing an unrelated
`fluxo.db` can stop with `unsafe ownership or permissions: .`. The Node.js helper
mistakenly inspects the working directory as an additional panel database. Do
not change ownership of the home directory or delete that database to bypass the
error. Version v0.4.39-hotfix.1 explicitly targets the production database and rejects
relative custom data directories before installation.

The same failed-installer cleanup can report that existing Node.js snapshot
paths could not be restored even when their copy and rename succeeded. A Bash
exit-status bug causes those false failures; v0.4.39-hotfix.1 fixes them while retaining
snapshots on genuine filesystem failures. Preserve the first failure and retained
snapshots, and check current state before retrying:

```bash
sudo /usr/local/bin/fluxo --version
sudo systemctl is-active fluxo
command -v node
node --version
```

When the previous panel is active and Node.js retains its previous selection,
retry with the corrected installer and exact release. This example selects Node
and leaves Python unselected; retain any other original component options:

```bash
curl -fsSL https://raw.githubusercontent.com/FabioTECH1/fluxo/v0.4.43/install.sh | \
  sudo env FLUXO_VERSION=v0.4.43 bash -s -- --node --no-python
```

Keep the earlier snapshots until the upgrade and hosted applications are
verified. If the panel is stopped, Node.js changed unexpectedly, or a genuine
restore error remains, inspect the retained snapshot and service logs before
retrying; do not remove maintenance holds or installer markers.

## Fluxo upgrade blocked by paused Node.js recovery

Use the usual SSH installer. A recovery-aware release verifies and stages a
compatible worker without restarting the running dashboard or clearing runtime
holds. Open **Runtime > Node.js > Review repair**, review and approve the repair,
and leave the installer running: it continues after recovery succeeds.

If repair fails or the installer reaches its waiting deadline, read the runtime
error in Fluxo. No worker is killed or retried automatically. Once recovery has
finished, rerun the same installer command to complete the panel upgrade. Do not
delete database rows, holds or history to get past admission. Running workers,
unsupported recovery formats and pending PHP recovery remain explicit blockers.

## Node.js toolchain installation appears stalled

The installer reports the active phase and shows percentage checkpoints for large Node.js and Bun downloads. A healthy installation commonly takes one to seven minutes depending on the package mirror, network, disk, and server size; 15 minutes is the overall safety limit, not the expected duration.

Fluxo `v0.4.23` and earlier can appear to stop after `Checking Node.js system prerequisites...` while a silent `apt-get install` waits on package configuration or a package-manager lock. Current releases first inspect `ca-certificates`, `xz-utils`, and `unzip`, skip APT when all three are ready, and print a heartbeat every 15 seconds during required package operations. Required APT commands run noninteractively and stop after a finite lock and command timeout.

On an older release, open a second SSH session and check whether Ubuntu is still running package maintenance. Do not kill an active `apt`, `dpkg`, `unattended-upgrade`, or provider initialization process:

```bash
ps -ef | grep -E '[a]pt|[d]pkg|[u]nattended-upgrade|[c]loud-init'
sudo systemctl status apt-daily.service apt-daily-upgrade.service --no-pager
```

If no package operation is active, finish any interrupted package configuration and install the prerequisites noninteractively before retrying Fluxo:

```bash
sudo dpkg --audit
sudo env DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a dpkg --configure -a
sudo env DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a \
  APT_LISTCHANGES_FRONTEND=none \
  apt-get install -y ca-certificates xz-utils unzip
```

Use `--no-node` to finish provisioning without the optional Node.js toolchain when the server package manager cannot be repaired immediately. Install the toolchain later from **Runtime > Node.js** after resolving the package-manager problem.

Fluxo retries temporary download and npm registry failures up to three times. Direct downloads fail after 90 seconds without receiving data, and the final error distinguishes DNS, connection timeout, TLS, HTTP, interrupted transfer, and local file-write failures. Do not start a second installer while the first is active. If a phase fails, preserve its error message; Fluxo restores the previous managed Node.js toolchain before exiting.

## Node application returns 502

Check the site's managed Node process, its logs, and internal port. The application must bind to `127.0.0.1` on the configured port and remain running.

Confirm no second site uses that port and that the start command matches the built output. A successful static build should use static mode instead of a server process.

Fluxo-managed Node sites run as `fluxo-daemon-<id>.service`; they do not use PM2. If `pm2`, `forever`, or `nodemon` appears in the process tree or daemon command, treat it as an external setup that Fluxo cannot restart or roll back. Keep PM2 state under `/home/fluxo` and owned by `fluxo`; do not recursively change `/opt/fluxo` away from root ownership.

## Let's Encrypt failed

Check DNS from an external resolver, inbound port 80, IPv6, CDN proxy behavior, and Certbot output. Every requested hostname must resolve to and reach this server.

For custom or cloned certificates, confirm the SAN list covers the exact hostname and the private key matches. Fluxo intentionally omits incompatible certificates from the clone list.

## WordPress installer cannot connect to the database

Confirm MariaDB/MySQL is running, the attached database still exists, and `wp-config.php` contains the expected database, user, password, and `127.0.0.1` host. Test with a non-destructive WP-CLI command from the site's Commands page.

Do not replace WordPress commands with Laravel `php artisan` commands.

## CPU load remains 0.00

Linux load averages can legitimately remain zero on an idle server. Generate real work and refresh. If memory, disk, uptime, and all other metrics also fail, inspect the Fluxo API response and daemon journal.

## Site deletion is interrupted

The site remains visible with a deletion stage and stored intent. Correct the reported external failure, such as an unavailable database engine, then select **Retry deletion**. Do not manually delete the site row or managed files between retries.

## Database or backup action failed

Verify the engine service, local socket, credentials, available disk, and active backup state. For S3/R2, run the destination connection test and verify read, write, delete, bucket, region/account, and prefix permissions.

## Ask for help

Open a [GitHub issue](https://github.com/FabioTECH1/fluxo/issues) with:

- Fluxo version and server OS
- Application type and deployment mode
- Exact steps and expected behavior
- Sanitized error output
- Relevant service status

Remove domains when necessary and always remove tokens, passwords, private keys, `.env`, `wp-config.php`, and database credentials.


## PHP repository failures and migration

A `503 Service Unavailable` from `ppa.launchpadcontent.net` is a failed upstream repository request. Fluxo rejects the refresh instead of accepting cached indexes as current. Automatic failed checks retry after roughly one hour; manual refresh remains available. This error does not mean PHP was installed or restarted.

Normal installer upgrades automatically migrate recognized legacy Ondřej PHP PPA entries. If setup needs attention, open **Runtime > PHP > Review repair**. The internally reviewed migration uses `packages.sury.org/php`, backs up and disables recognized PPA entries, and preserves unrelated sources and pins. Leaving a failing PPA enabled alongside the new repository will still fail strict refresh.

If setup is interrupted, use **Resume repository setup**. A configuration conflict means an operator changed a journaled file or its permissions; Fluxo will not overwrite it. Reconcile the named file before resuming. A failed setup whose preimages were restored requires a fresh review. Missing extension provenance, incompatible dependencies or unexpected custom source layouts can require manual maintenance; do not force extension downgrades to bypass the report.


### Scheduler state needs reconciliation

Check the engine summary in **Processes > Scheduler**. A configured file and an active Cronie/cron service are separate observations. For qualified Ubuntu cron and Cronie configurations, an initial failed legacy migration is retried in the background without restarting the engine or VPS. Allow genuine minute ticks and earlier tasks to finish, then review PHP installation again. A changed backend identity or custom configuration requires investigation; checking for PHP updates remains available.

For support, collect a read-only sanitized diagnostic artifact:

```sh
sudo env FLUXO_ENV=prod /usr/local/bin/fluxo scheduler-diagnostics > scheduler-diagnostics.json
```

This reports package/service identity, configuration metadata and hashes, registration bindings, migration receipt state and recovery holds. It does not export application commands, environments, nonce values or logs, and does not initialize/repair state or signal services. Unknown fields include their read errors. Review the artifact before sharing it; it still contains server paths and registration identifiers.

Consistency checks cover the boot, process start, service properties, cgroup inode, file metadata and available regular-file hashes across collection. Symlinks include their link target; target content is deliberately not followed or hashed. Branch recovery counts distinguish outstanding submissions and durable launch authorization, which does not establish that application code actually started or succeeded.

A missing, malformed or conflicting Scheduler journal is not treated as an empty task list. New launches and PHP maintenance fail closed. A normal PHP package Repair cannot safely recreate lost invocation history: an operator must restore verified state or reconcile it over SSH. Keep the journal and its initialization anchor together when backing up the server; deleting maintenance markers does not constitute a repair.

If an installer lease remains after an interrupted panel upgrade, retry the installer. It verifies that the previous helper has exited before replacing its durable marker. A live installer or unresolved PHP generation continues to block conflicting work.
