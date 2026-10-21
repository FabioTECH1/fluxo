---
title: Development
description: Build Fluxo, run its frontends, and contribute documentation or code.
---

# Development

Fluxo is a Go daemon with an embedded Vue dashboard. The public landing page, documentation, and interactive demo live in `ui-site`.

## Prerequisites

- Go 1.26.3 or newer
- Node.js 20.19 or newer, or Node.js 22.12 or newer, for frontend development
- npm

## Build the dashboard and daemon

Build the embedded dashboard before compiling Go:

```bash
cd ui
npm install
npm run build
cd ..
go build -o fluxo ./cmd/fluxo
```

## Run the dashboard frontend

```bash
cd ui
npm install
npm run dev
```

The standalone Vite server needs a reachable Fluxo API for real operations.

## Run the landing page and demo

```bash
cd ui-site
npm install
npm run dev
```

The demo uses a mock API and does not need a Fluxo server.

## Reuse the local Ubuntu test VM

The existing Multipass instance `fluxo-php-patch-test` is the reusable local demo and integration environment. It has Ubuntu 24.04, PHP, Nginx and Redis. Keep the separate `fluxo-dev` instance unchanged unless selecting it deliberately.

```bash
multipass start fluxo-php-patch-test
multipass info fluxo-php-patch-test
```

Open `http://<VM-IP>:5177/demo/runtime/php` using the address reported by `info`. Nginx serves the built simulated demo from `/opt/fluxo-demo/current`, with configuration at `/etc/nginx/conf.d/fluxo-demo.conf`. It remains available after closing the host terminal and starts with the VM. The demo uses mock data; real backend/package/service tests must run separately inside the VM.

Build updated demo assets with `cd ui-site && npm run build`, transfer them into a new release directory under `/opt/fluxo-demo/releases`, then switch the `current` symlink. Validate any Nginx configuration edits with `sudo nginx -t` before reloading.

For real backend testing, build the dashboard first, then compile the daemon with `CGO_ENABLED=0 go build -o fluxo ./cmd/fluxo`. Transfer binaries using `multipass transfer` from the workspace. Large stdin transfers may truncate; do not use them for compiled binaries.

PHP supervisor and deferred-mutation acceptance tests are guarded by the VM hostname, root privileges and `FLUXO_PHP_UPDATE_VM_TEST=1`; ordinary `go test ./...` skips these VM-only cases. Keep package fixtures and test data isolated from real servers, and do not recreate or wipe the VM for ordinary tests.

### PHP recovery acceptance

Run selected acceptance cases only as root inside `fluxo-php-patch-test`. Build and transfer the current CGo-free daemon and test binary first; tests that launch independent workers expect `/usr/local/bin/fluxo-php-recovery-test`. Application fixtures live at `/home/fluxo/php-recovery-acceptance`. Preserve the fixture repository, databases, applications and recovery reports between runs. The frontend demo cannot establish backend safety.

The following cases use `FLUXO_PHP_UPDATE_VM_TEST=1`. Names refer to `internal/services/phpupdate` unless a different package is shown.

| Acceptance tests | What they exercise |
| --- | --- |
| `TestVMBarrierWaitsForDescendantWithoutForceKillAndPreservesStoppedUnit` | A surviving child process, bounded observation, durable admission and original stopped-state preservation. |
| `TestVMJournaledBarrierCrashBoundariesResume` | Injected journal/file/reload boundaries and their recovery; these are not machine power cuts. |
| `TestVMReconcileInterruptedSupervisorWithoutReplay`, `TestVMReconcileKeepsGateWhileDescendantDeactivates` | Reconciliation without automatic package replay and retained admission while descendants remain. |
| `TestVMPHPUpdateRollbackAndRepeatedRepairWithHorizon` | Actual package update/rollback/repair, Horizon drainage with continued queue arrivals and repeated activation recovery. Also requires `FLUXO_PHP_UPDATE_PACKAGE_VM_TEST=1`. |
| `TestVMOctaneConfigurationInspectionDoesNotBootProviders` | Configuration inspection without booting application providers. |
| `TestVMNativeOctaneDrainAndRepeatedRepair` | Qualified Swoole/RoadRunner request admission, native drain and repeated recovery boundaries. |
| `TestVMSwooleQueuedTasksAndExistingRequestDeadline` | Queued task handling and preservation of existing request deadlines. |
| `TestVMFPMIntentAndUnrecordedDeliveryRecoverSafely` | Injected FPM signal-intent/delivery interruptions; `TestVMFPMDeliveryCrashChild` is its internally guarded subprocess helper. |
| `TestVMOctaneEnablePreflightAndFailedReadinessKeepRouting` (`internal/server`) | Enablement preflight, readiness failure, cancellation and retained cleanup/routing state. |
| `TestVMOctaneCleanupRetriesAbsentEntryNamespaceSync` | Injected directory-sync failures after unit/enable-link removal, then recovery when those entries are already absent. |
| `TestVMBarrierRequiresDurableDropInParentBeforeJournalAndOnResume` | Drop-in parent-directory durability before initial and resumed holds. |
| `TestVMPreparingBarrierRetriesPublishedOverrideDirectorySync` | Existing marker/override publication retry, failed directory sync preventing drainage, and successful recovery. |

The fsync tests inject errors at real filesystem boundaries. They check ordering and retry behavior, not storage-controller guarantees or arbitrary power-loss survival. Native tests establish the checked process/transport contracts; they do not certify every application route or unqualified framework/native version.

### Real reboot acceptance

Reboot tests additionally require `FLUXO_PHP_UPDATE_REBOOT_VM_TEST=1` and explicit authorization to reboot this isolated VM. Run one exact Stage test, wait for the VM to return, then run its matching Resume test. Do not run all Stage tests together or start another pair while a retained spec remains unfinished. Resume verifies a changed kernel boot ID before asserting recovery.

| Stage / Resume pair | Boundary and retained spec |
| --- | --- |
| `TestVMStageRebootDuringPackageTransaction` / `TestVMResumeAfterRealPackageReboot` | Reboot during actual dpkg work; `/var/lib/fluxo-php-recovery-reboot.json`. Resume may correctly refuse scoped Repair when the journal or unrelated package state needs operator intervention. |
| `TestVMStageRebootDuringPartialActivationRelease` / `TestVMResumeAfterRealPartialActivationReboot` | Real reboot after an injected post-package partial-release checkpoint, using observed current packages without changing them; `/var/lib/fluxo-php-recovery-activation-reboot.json`. |
| `TestVMStageRebootDuringOctaneCleanupAutoRestart` / `TestVMResumeAfterRealOctaneCleanupReboot` | Real failed startup/auto-restart and verified durable cleanup hold, followed by reboot and resumed removal; `/var/lib/fluxo-octane-cleanup-reboot.json`. |

These tests request an orderly systemd reboot. A real reboot is distinct from the injected checkpoint used by the activation pair, and neither demonstrates sudden power-loss behavior. Stage intentionally preserves evidence and owned fixtures; Resume performs exact cleanup after assertions. Failed tests retain evidence for investigation rather than performing broad removal or package repair.

`TestVMResumeFailedOctaneEnablementAcceptance` is a separate, narrowly scoped cleanup utility for the historical failed acceptance unit `fluxo-daemon-9108001.service`. It additionally requires `FLUXO_PHP_UPDATE_CLEANUP_VM_TEST=1` and verifies the exact application directory, command and port before native-safe cleanup. It is not a general service-removal tool or part of routine test runs.

```bash
# Stop it when no one is reviewing or testing. Its disk and setup are retained.
multipass stop fluxo-php-patch-test
```

## Run documentation locally

```bash
cd ui-site
npm run docs:dev
```

The documentation is served under `/docs/`. Source Markdown lives in `ui-site/docs`.

## Production website build

```bash
cd ui-site
npm run build
```

This command synchronizes the installer, builds the Vue client, prerenders the landing page and Markdown-backed blog routes as static HTML, writes a separate `noindex` shell for the demo SPA, and builds VitePress into `dist/docs`. Cloudflare Pages publishes `ui-site/dist`.

Blog source Markdown lives in `ui-site/content/blog`. Each article is discovered during the build, rendered through the shared Vue article template, added to `sitemap-pages.xml`, and emitted as an extensionless-compatible HTML route under `dist/blog`.

Create a post by adding one Markdown file whose filename becomes its URL slug:

```md
---
title: Deploy a Laravel application with Fluxo
excerpt: A short description used on the blog listing and by search engines.
category: Deployments
date: 2026-09-04
image: /blog/deploy-laravel.webp
imageAlt: A concise description of the cover image
featured: true
---

## Start with a production-ready server

Write the article in Markdown here.
```

Supported categories are `Releases`, `Deployments`, `Operations`, and `Security`. `readTime` is optional and is calculated from the article when omitted. `slug` is also optional and overrides the filename-derived URL when present. Put cover images in `ui-site/public/blog`.

## Verification

```bash
cd ui
npx vue-tsc -b --noEmit
npm run build
cd ../ui-site
npm run build
cd ..
go test ./...
go vet ./...
```

System integration features require Nginx, PHP-FPM, systemd, and related packages and are best verified on a disposable VM.

## Contributing

Read the repository's [contribution guide](https://github.com/FabioTECH1/fluxo/blob/main/CONTRIBUTING.md). Keep documentation behavior aligned with the code and update the relevant guide whenever a user-facing workflow changes.
