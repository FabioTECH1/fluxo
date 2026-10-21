---
title: "PHP patch updates, directly from Fluxo"
excerpt: PHP maintenance now has a place in the dashboard. Check for updates, see which sites are affected, and patch your installed PHP branch when you're ready.
category: Releases
date: 2026-10-21
image: /blog/php-patch-updates.webp
imageAlt: PHP runtime beside a server, package updates, and blue refresh arrows
featured: true
---

When a PHP update comes out, there are a few questions to answer before installing it. Is it available for your server yet? Which sites use that PHP version? And what happens to the workers already running in the background?

PHP package updates arrived in Fluxo 0.4.34. Version 0.4.35 adds automatic repository setup, handles managed Nightwatch agents, and keeps package checks separate from installation readiness. Version 0.4.36 adds automatic handling for managed Scheduler entries and a shorter update confirmation. Open **Runtime > PHP**, check the branch your applications use, and review the affected sites and services before applying the update.

Fluxo checks for updates each day. You choose when to install them.

## Keep PHP current without changing branches

If your application runs on PHP 8.4, updating its packages keeps it on PHP 8.4. Moving to PHP 8.5 is a separate decision, with its own compatibility testing.

That's useful on a server hosting several applications. You can maintain an existing branch while deciding when each application is ready to move on. The update leaves your site selections and the server's site and CLI defaults as they are.

Fluxo checks the installed PHP packages together, including FPM, CLI, and applicable extensions. An extension may have a different version number from PHP itself; what matters is that the package set works together.

## Where to find it

The update controls live in the **Versions** table on **Runtime > PHP**, inside the three-dot menu beside each installed branch. **Restart FPM** stays outside the menu for quick access while the service is running.

To update a branch:

1. Choose **Check for updates** if you want a fresh check. A toast tells you whether an update is available.
2. Select **Update** when the branch has an eligible package update. Fluxo then checks installation readiness separately.
3. Review the affected sites and services in the confirmation.
4. Confirm when you're ready for maintenance. Fluxo runs the update in the background and reports progress through toasts.

**Update** stays disabled when there is no eligible package update. A package check does not inspect or stop your background services. If installation review finds a service problem, the confirmation explains what to resolve and keeps installation disabled; the available packages remain visible. If a repository check fails, you'll see that failure rather than a misleading “no updates” message.

You can close the page after starting an update. The server-side job continues, including across a panel restart.

## You don't have to keep checking the page

On installed production servers, Fluxo checks all installed PHP branches once a day using one repository refresh. Opening the runtime page shows the stored result, so every visit doesn't trigger more package-manager work.

When an update is available, a dashboard notice points you back to PHP. You can dismiss that notice, and a later package revision can notify you again.

The manual check is still there when you need it: perhaps you've just read a package announcement, fixed a repository issue, or updated PHP over SSH and want Fluxo to refresh its view.

Checking uses some server resources while it runs. It doesn't require a separate checking process to stay running all day, and it doesn't install anything automatically.

## What happens to your sites and workers?

Allow for downtime when planning an update. Background jobs and requests may need time to finish, and your sites can be unavailable while services stop and restart.

Fluxo handles the affected FPM service, managed queue workers, Horizon, Fluxo-generated Nightwatch agents, and supported Octane runtimes for the branch you're updating. An update to PHP 8.4 leaves PHP 8.3 services and unrelated applications alone. Services that were stopped stay stopped.

Queue workers and Horizon begin a graceful shutdown: consumers stop taking work and finish the jobs they have accepted. The queue can keep receiving jobs during that process. Fluxo waits for the affected processes to exit, rather than waiting for the whole queue to become empty; the restarted consumers can pick up the remaining backlog.

For supported Swoole and RoadRunner setups, Fluxo also lets Octane finish its current work before patching and starts it again afterward. Support is deliberately limited to tested versions and configurations. Existing Octane instances need the maintenance integration installed, followed by a controlled full restart; a worker reload is not enough. The [runtime documentation](/docs/operations/runtimes#php) lists the supported versions and configurations.

Nightwatch monitoring pauses while its agent stops and restarts. Buffered monitoring events may be lost; checking the local agent's listener does not verify delivery to Nightwatch.

Fluxo also pauses supported managed Laravel Scheduler entries while PHP is updated. Current tasks, including background children, finish first. New scheduled ticks are skipped until verification completes; Fluxo doesn't replay them afterward. For supported existing entries, Fluxo checks that cron has loaded the new launcher and lets earlier jobs finish naturally. You don't need to restart cron or reboot the VPS. Custom cron or PAM setups may still need operator review before automatic maintenance is available. Automatic Scheduler handling requires systemd 250 or newer with cgroup v2, normally available on Ubuntu 24.04 and newer.

Your existing job and native runtime timeouts still apply. Fluxo doesn't add a forced kill or shorten those deadlines to make an update finish. If work cannot drain safely, maintenance stops with an attention notice and keeps the restart hold in place.

Custom background commands, unsupported Octane configurations, and alternative interpreters still need a manual plan. Pause or coordinate PHP jobs started through cron, SSH, or another process manager too: they are outside Fluxo's managed service holds.

Before installing anything, Fluxo checks the approved plan again. A change to packages or affected applications means checking again. Package holds, repository pinning, locks, or unavailable repositories can also prevent the update. Fluxo respects those constraints, and package changes requiring removals, downgrades, or new dependencies need manual attention.

## If an update needs attention

Installing packages and getting every service healthy are two different outcomes. Fluxo checks package revisions, FPM configuration, and whether supported running services have picked up the update. It also checks FPM requests and service stability.

Before stopping consumers, Fluxo normally downloads and verifies both the target packages and the currently installed package set. Older revisions are not always available from repositories. If the previous packages can't be retained, Fluxo asks separately whether you want to continue without package rollback. Routine updates don't need an extra checkbox. Without that explicit approval, Fluxo stops before pausing services or installing packages.

If something fails, the dashboard keeps an attention notice visible. **View report** shows what happened, with separate recovery actions to review:

- **Repair** resumes the recorded maintenance attempt. It can finish the recorded package transaction and then activate and verify affected services. It keeps your current configuration, so an invalid FPM setting still needs correcting before services can start.
- **Roll back PHP packages** restores the verified previous package set retained for that update. It is a separate confirmation, and it is available only when Fluxo can safely use those files. Older packages may bring back a security vulnerability, so treat rollback as a temporary recovery measure.

Neither action restores application files, databases, Redis data, or jobs. An interrupted package journal, unrelated package damage, or an unsupported service can still require repair over SSH. Fluxo keeps an unresolved operation visible instead of reporting success or automatically trying a broad system repair.

Package rollback is manual. Keep a tested server recovery path available, and check your application's important pages and background jobs after maintenance. Site backups don't restore operating-system packages, and a successful runtime check doesn't test every application route.

The [PHP runtime documentation](/docs/operations/runtimes#php) and [troubleshooting guide](/docs/reference/troubleshooting) cover the detailed checks and recovery steps.

## A note about version numbers

PHP's displayed version isn't enough to judge whether a security fix is installed. Ubuntu can backport a fix into an existing release, changing the package revision while keeping the upstream version. Its [security updates documentation](https://documentation.ubuntu.com/security/security-updates/) explains how that works.

Fluxo compares installed revisions with packages available from your configured repositories. Third-party PHP repositories have their own maintenance and publication schedules, so an upstream announcement won't always mean a package is immediately available on your VPS.

“No updates available” describes what the successful package check found. Keep following the advisories for your package source and maintaining the rest of the operating system too.

## One smaller improvement to Fluxo updates

We've also changed what happens when you rerun Fluxo's installer with the current release already installed. If the selected version is present and the panel passes its health checks, it now says **Fluxo is already up to date** and exits before package changes or service restarts.

Use `--reinstall` when you want to reinstall the selected release. Pin `FLUXO_VERSION` if you're repairing a specific version; the [upgrade guide](/docs/getting-started/upgrade) has the commands.

To try the PHP workflow, head to **Runtime > PHP** and choose **Check for updates** for an installed branch. You'll see what's available before deciding when to apply it.


## Moving to the new PHP repository

Ondřej Surý is moving Ubuntu PHP packages from Launchpad to `packages.sury.org/php`. When you upgrade Fluxo with the installer, it makes that change for you, backing up and disabling recognized old PPA entries. You do not need to switch repositories yourself. Switching sources does not update PHP or restart your applications. Once it finishes, check for updates and review the package installation separately.

This matters when Launchpad is unavailable or the two repositories publish builds at different times. Some installed extensions can also have higher revisions than the new repository. Fluxo keeps those extensions unchanged only when it can retain and verify their original packages; it does not silently downgrade them. If that evidence is missing or your repository setup conflicts, Fluxo stops and explains what needs attention.

If repository setup is interrupted, you can resume it from the PHP page. A later PHP package rollback keeps the new repository configuration in place, so recovering PHP does not quietly bring back the old source.
