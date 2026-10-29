---
title: REST API
description: Authenticate with and automate the Fluxo REST and WebSocket APIs.
---

# REST API

The dashboard uses Fluxo's JSON API under `/api/v1`. The API is currently versioned by path but does not promise compatibility beyond documented release behavior; test automation against the target Fluxo release.

## Base URL

```text
https://YOUR_SERVER_IP:9595/api/v1
```

When a panel domain is active, the equivalent base URL is:

```text
https://admin.example.com/api/v1
```

For the initial self-signed certificate, development clients may need an explicit trust exception. Do not disable TLS verification in production automation.

## Authentication

Sign in:

```http
POST /api/v1/auth/login
Content-Type: application/json

{
  "username": "admin",
  "password": "your-password"
}
```

Use the returned JWT on protected requests:

```http
Authorization: Bearer YOUR_JWT
```

Public endpoints are limited to login, bootstrap status, health, version, the signed GitHub webhook receiver, and WebSocket authentication. The dashboard stores its session token in browser local storage.

## Version and update awareness

`GET /version` is unauthenticated and returns the version of the installed Fluxo binary:

```json
{"version":"0.4.39-hotfix.4"}
```

Authenticated clients can call `GET /update-status`. Fluxo compares the installed version with the validated public manifest at `https://fluxo.fabiotech.dev/api/v1/releases/latest` and returns `current_version`, `latest_version`, `update_available`, `release_url`, and check metadata. Successful checks are cached for six hours; temporary failures are cached briefly and return `check_available: false` so update awareness never blocks normal dashboard use.

These endpoints are informational. They cannot download, install, or activate a release. Administrators must review the release notes and perform upgrades from the server terminal.

## Main resource groups

| Resource | Representative endpoints |
|---|---|
| Authentication | `POST /auth/login`, `GET /auth/bootstrap` |
| Sites | `GET/POST /sites`, `GET/PUT/DELETE /sites/{id}` |
| Deployments | `POST /sites/{id}/deploy`, list, rollback, dismiss failure |
| Configuration | Site environment, WordPress config, deployment settings, and Nginx vhost |
| Files | List, read, write, create, move, upload, download, delete |
| Domains and SSL | Domain CRUD, Let's Encrypt, custom, clone, activate, deactivate |
| Site processes | Daemons and scheduled jobs, actions, and logs |
| Commands | Execute, stream, list, inspect, and delete history |
| Laravel features | Scheduler, Queue Worker, Nightwatch, Horizon, Octane, maintenance |
| Databases | Databases, users, grants, password rotation, sizes, exports |
| Backups | Destinations, plans, runs, artifacts, downloads |
| Runtimes | PHP, Node.js, Python, Nginx, database engines, service actions |
| Observation | Metrics, logs, downloads, clearing, activity |
| Settings | General settings, panel domain and SSL, GitHub accounts, SSH keys and effective SSH security policy, firewall |

## Scheduler status and configuration

`GET /api/v1/scheduler/status` requires `Authorization: Bearer YOUR_JWT`. It accepts no body, path parameters or query parameters. It observes the installed scheduler and existing coordination state; it does not install a provider, publish cron files, stop jobs or advance migration. Operational observations share a 30-second cache. Maintenance authorization uses fresh independent checks.

Example `200` response:

```json
{
  "backend": {
    "engine": "cronie", "state": "active", "unit": "cronie.service",
    "package": "cronie", "version": "1.7.1-1", "architecture": "amd64",
    "enabled": "enabled", "qualification": "qualified", "observed_at": 1792612800
  },
  "branches": [{"version": "8.4", "state": "ready"}]
}
```

`backend.engine` is `cron`, `cronie`, `custom`, `none` or `unknown`. Operational `state` is `active`, `inactive`, `masked`, `missing`, `ambiguous`, `unqualified` or `unknown`; `qualification` is `qualified` or `unqualified`. Optional `code` and `message` explain unavailable or unsupported observations. `observed_at` is a Unix timestamp in seconds. Package, architecture, unit and enabled state are omitted when they cannot be identified. An active engine does not establish that application jobs succeeded.

`branches` contains branch/version identifiers and maintenance states: `ready`, `pending`, `blocked` or `paused`, with optional `code`/`message`. Examples include `waiting_for_reload`, `waiting_for_legacy_jobs`, `backend_identity_changed`, `legacy_origin_unproven`, `reconciliation_failed`, `panel_proof_required` and `maintenance_active`. A missing/inactive/unqualified engine is a successful observation (`200`), rather than an HTTP failure:

```json
{"backend":{"engine":"none","state":"missing","qualification":"unqualified","code":"backend_missing","message":"No scheduling engine was found. Scheduled jobs cannot be verified; configure a scheduler before PHP maintenance.","observed_at":1792612800},"branches":[]}
```

An unreadable coordination journal produces a `200` observation with `state_error` describing the failure and no inferred readiness. Missing/invalid authentication returns `401 Unauthorized`.

`GET /api/v1/crons` and `GET /api/v1/sites/{id}/crons` retain their existing array responses. The site path takes an integer site ID; neither request has a body or query parameters. Both require the same Bearer token. Each row adds optional `configuration_state`: `configured`, `missing`, `modified` or `unknown`, derived from the actual managed file. This field describes configuration, not successful execution:

```json
[{"id":4,"site_id":2,"name":"Laravel Scheduler","expression":"* * * * *","command":"php8.4 /home/fluxo/example.com/current/artisan schedule:run","user":"fluxo","created_at":"2026-10-21T12:00:00Z","configuration_state":"configured"}]
```

The global list also includes `site_domain` when available. Missing/invalid authentication returns `401`; database query failure returns `500`. Unknown configuration is represented in the row rather than removing that row.

`DELETE /api/v1/crons/{cron_id}` requires a Bearer token and an integer cron ID, with no body or query parameters. Success returns `204` with no body. If scheduler coordination or removal fails, it returns `409` with a plain-text explanation such as `Scheduler is held for PHP maintenance`; the database record is retained. Database deletion failure returns `500`. An already absent file does not itself prevent record deletion. Missing/invalid authentication returns `401`.

### Guarded scheduled jobs

`POST /api/v1/crons/{cron_id}/run` and `POST /api/v1/sites/{id}/crons/{cron_id}/run` require the existing Bearer token and integer path IDs, with no body or query parameters. A supported guarded job returns `200` with `{"output":"Scheduled system task started. Output is available in its cron log."}` after submission; completion is asynchronous and output remains available from the existing cron-log endpoint. A durable maintenance/recovery hold returns `409` with its existing plain-text explanation before launch. Unreadable/changed registration or unconfirmed earlier effects also return `409`; there is no fallback to executing the stored command directly. Missing jobs return `404`, missing/invalid authentication returns `401`. Readiness/configuration observations do not imply that the job succeeded.

Node and PHP review responses optionally include `impact.scheduled_tasks`. Each server-generated receipt binds `name`, optional `domain`, `contract`, `identity` (registration UUID), `database` (absolute registration database), `helper` (retained executable digest), and `profile` (dispatcher protocol identifier). `contract` contains `id`, `kind` (`application-job`, `package-cleanup`, `certificate-renewal`, `composer-update`), `expression`, `command`, `site_id`, `user` and `directory`. Clients display these values; they are not caller-supplied execution overrides. For example:

```json
{"scheduled_tasks":[{"name":"Daily report","domain":"example.com","contract":{"id":12,"kind":"application-job","expression":"0 2 * * *","command":"php8.4 artisan reports:generate","site_id":8,"user":"fluxo","directory":"/home/fluxo/example.com/current"},"identity":"c5f71faa-d3bb-42c9-8fef-3ed8043eb9b8","database":"/var/lib/fluxo/fluxo.db","helper":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","profile":"fixed-dispatch-v1"}]}
```

A reviewed command/schedule/site-layout change requires a new review. Existing immutable recovery plans are retained; a new recovery review explicitly approves current scheduled-job contracts. Deletion revokes future authority before removing the trigger; an already admitted invocation drains naturally and its unresolved journal can still block maintenance after the row is deleted.

## PHP version list

`GET /api/v1/server/php/versions/available` requires `Authorization: Bearer YOUR_JWT` and accepts no request body, path parameters or query parameters. It returns the supported PHP branches, whether each is installed, its FPM status, and its installed FPM package's upstream patch when available:

```json
[
  {"version":"8.5","full_version":"","installed":false,"status":"not_installed"},
  {"version":"8.4","full_version":"8.4.26","installed":true,"status":"running"}
]
```

`version` remains the branch identifier for all actions and selectors. `installed` reflects a configured or incomplete FPM package, or an existing unqualified FPM executable. A removed package in `config-files` state with only a retained `/etc/php/<branch>` directory does not count. `full_version` is display-only; it is empty when the configured package's upstream patch cannot be identified. Status is `running`, `stopped`, `not_installed`, or `unavailable`. `unavailable` means the package is incomplete, its executable is missing/unusable, or an FPM executable exists outside a configured package; the UI shows **Needs attention** and prevents replacement installation from the selector. Package-inventory failure returns `503` with `Could not read installed PHP packages`, rather than claiming nothing is installed. A missing or invalid JWT returns `401 Unauthorized`. The displayed patch is not a security assessment: distro backports can change package revisions without changing this value, and it does not independently verify that an already-running process adopted newly installed packages.

`GET /api/v1/server/php` has the same authentication and no parameters. It returns a JSON array of installed branch identifiers with usable FPM and CLI executables, for example `["8.3","8.4"]`, or `[]` when none qualify. It never invents an installed default branch. Inventory failure returns `503` as above.

## PHP patch maintenance

These endpoints use the base `/api/v1/server/php` and require `Authorization: Bearer YOUR_JWT`. Send `Content-Type: application/json` for requests with a body. Responses use `Cache-Control: no-store`; JSON responses also use `Content-Type: application/json`. There are no query parameters. Branch identifiers match `^[0-9]{1,2}\.[0-9]{1,2}$`, such as `8.4`, rather than an upstream patch number. Revisions use Debian ordering, including epochs and backports.

Checks only discover package updates: they do not inspect application runtimes, stop services or assess service restart eligibility. Selecting Update requests a separate installation review. Package installation requires explicit confirmation of a reviewed update or a reviewed Repair/Rollback plan. Neither page loads, daily checks, panel closure nor process restart authorize installation or automatic rollback.

| Method and path | Request and validation | Success |
|---|---|---|
| `GET /updates` | No body. Reads persisted state without refreshing APT. | `200` with `branches`, nullable `active_operation`, `failures`, and `recoveries`. |
| `POST /updates/check` | `{"version":"8.4"}`. Required branch; a configured FPM package and usable executable must be installed. | `202` Operation. Concurrent identical checks, including a running daily batch, coalesce. A check refreshes repositories once and inventories installed branches. |
| `GET /updates/plans/{plan_id}` | Required UUID path parameter. | `200` immutable Plan. |
| `POST /updates/plans/{plan_id}/review` | Required UUID of the current discovery plan; JSON `{}` only. | `200` new immutable Plan with `reviewed: true` and `parent_plan_id` referencing discovery. Read-only runtime preflight; no package install or service start/stop. Installation blockers are returned in `impact.blocked_reasons`, not as failed package checks. |
| `POST /updates/apply` | Required `version`, `plan_id` UUID and `request_id` UUID; optional boolean `acknowledge_no_rollback`, default `false`. See example below. | `202` Operation, kind `apply`. Requires a reviewed plan less than 15 minutes old, without package/runtime blockers. Its parent must still be the current eligible discovery snapshot, less than 24 hours old. Discovery-only and legacy unreviewed plans are rejected. |
| `GET /updates/operations/{operation_id}` | Required UUID path parameter. | `200` Operation, including durable progress and bounded diagnostics. |
| `POST /updates/operations/{operation_id}/recovery-plan` | Required current-head operation UUID in the path; JSON `{"kind":"repair"}` or `{"kind":"rollback"}`. No other kind is accepted. | `200` immutable RecoveryPlan. This reviews state and records a plan; it does not install packages or start/stop services. |
| `POST /updates/recovery/apply` | Required `plan_id` and `request_id`, both UUIDs. `plan_id` is the **recovery** plan ID. | `202` Operation with the reviewed `repair` or `rollback` kind. Recovery plans expire after 15 minutes. |
| `POST /updates/operations/{operation_id}/verify` | Required operation UUID. No body required. **Legacy operations without a recovery generation only.** | `202` legacy `verify` Operation. New maintenance generations return `422 unsupported_configuration`; use reviewed Repair instead. |
| `POST /updates/notices/dismiss` | `{"version":"8.4","candidate_id":"<64-character candidate fingerprint>"}`. Both required; fingerprint must identify a stored candidate. | `204`, no body. Dismissal applies to that candidate, not the check timestamp. |

Check, installation-review, apply, recovery-plan, recovery/apply and dismissal JSON bodies are limited to 4096 bytes. Unknown fields, malformed JSON, extra JSON values, invalid branches and malformed UUIDs are rejected. No maintenance request accepts caller-selected package names, package paths, commands, unit names, APT options, archive URLs or native process IDs. Commands and paths in responses are server-derived evidence, not executable request inputs.

A check for a removed or absent FPM package returns `422` with `{"code":"runtime_not_installed","message":"PHP-FPM is not installed: PHP 8.5 has no installed FPM package. Use Install version in Runtime > PHP before checking for updates; retained configuration files are not an installed runtime"}` and launches no worker. An installed package with a missing or unusable executable returns `422 unsupported_configuration` with a package-repair explanation. These new-check prerequisites do not prevent reviewed recovery of an already recorded partial package transaction.

### Confirmation and idempotency

First review the discovered package plan:

```http
POST /api/v1/server/php/updates/plans/7c8401be-4337-4cf4-86bf-02f11ba33990/review
Authorization: Bearer YOUR_JWT
Content-Type: application/json

{}
```

The `200` response has the Plan schema below, with a **new** `id`, `parent_plan_id` equal to the discovery ID and `reviewed: true`. A completed review may contain blockers; show those explanations and disable installation. For example, `impact.blocked_reasons` may contain `Scheduler setup is waiting for earlier cron jobs to finish. PHP has not been changed.` Package discovery and its availability notice remain intact. Close and repeat review after resolving the issue.

Use the returned reviewed `id`, rather than the discovery ID, for an update request with rollback available:

```http
POST /api/v1/server/php/updates/apply
Authorization: Bearer YOUR_JWT
Content-Type: application/json

{"version":"8.4","plan_id":"061ce8b3-94d7-4d70-8aa1-021c01a4630f","request_id":"f5f34f6b-9d65-4f2a-bbd5-46b4fd0d7637","acknowledge_no_rollback":false}
```

If `plan.rollback.available` is false, the client must present its reason and obtain explicit acknowledgement before sending `acknowledge_no_rollback: true`. Otherwise the API returns `422 rollback_acknowledgement_required`. The server retains and verifies the complete target package family before draining consumers. Previous revisions must form a complete authenticated archive set for rollback; installed files or an arbitrary cached `.deb` are not proof. Discovery-time availability does not guarantee that later downloading succeeds. Without acknowledgement, failure to retain the old set aborts before Scheduler holds, consumer drainage or package changes. A subsequent installation review revalidates that retention failure using a fresh temporary archive probe; recovered availability restores the normal confirmation. Persistent unavailability requires a fresh unchecked exceptional acknowledgement and a new request UUID. The original approval is never amended automatically.

Keep a client-generated `request_id` across transport retries. Reusing an apply UUID for the same kind, branch and plan returns its original operation, even after completion. A different kind/branch/plan returns `409 plan_changed`. Repeating a UUID does not amend the original saved acknowledgement; a changed confirmation requires a new request UUID. Recovery UUIDs are bound to the exact recovery plan; reusing one for another plan returns `409`.

Repair continues the generation's current target: `after` for the intended update, or `before` after rollback was selected. It can install/reinstall the exact retained family if package state requires it, or only finish activation/verification when revisions already match. Rollback requires a separate review and confirmation, and restores the complete retained previous package family. It does **not** reverse application code, configuration edits, database changes or completed jobs. There is no automatic rollback.

```http
POST /api/v1/server/php/updates/operations/0a1b7fe7-18c5-4a53-a1a6-d344ae73b42a/recovery-plan
Authorization: Bearer YOUR_JWT
Content-Type: application/json

{"kind":"rollback"}
```

Example `200` recovery-plan response excerpt (the full response also includes `observation`, `packages` and `impact` as described below):

```json
{"id":"72311db4-90a5-44ee-86fc-9e786699c24d","generation_id":"0a1b7fe7-18c5-4a53-a1a6-d344ae73b42a","operation_id":"0a1b7fe7-18c5-4a53-a1a6-d344ae73b42a","version":"8.4","kind":"rollback","goal":"before","created_at":1792573500,"retry_fpm_reload":false}
```

After reviewing the returned target packages, impact and limitations, submit its `id`:

```http
POST /api/v1/server/php/updates/recovery/apply
Authorization: Bearer YOUR_JWT
Content-Type: application/json

{"plan_id":"72311db4-90a5-44ee-86fc-9e786699c24d","request_id":"fba15cb6-a854-4b74-bb1a-fa1c85d23d6f"}
```

A recovery plan binds the generation, its current operation head, package observation and affected applications. A newer attempt supersedes that head. Changed packages, marks, repository policy, service definitions or application/native-runtime evidence can require another review or manual reconciliation. Enqueue-time checks do not replace worker-side verification. Legacy `/verify` never installs packages: it rechecks approved revisions/marks and retries runtime activation. Repeating a legacy parent returns its existing verification; a superseded parent without an existing verification returns `409`.

### Snapshot and plan responses

An empty successful snapshot is:

```json
{"branches":[],"active_operation":null,"failures":[],"recoveries":[]}
```

`branches` contains Snapshots with all these fields:

- `version`; `state`: `unknown`, `current`, `available`, `blocked`, `stale`, or `check_failed`.
- `checked_at`, `attempted_at`: Unix seconds for successful check/latest attempt.
- Optional `error` and `plan`; `blocked_reasons`: array of explanations.
- `update_available`: a fresh, eligible plan is available. A failed refresh does not authorize a cached candidate.
- `notice`: the known candidate has an undismissed notice. A prior notice can remain visible while apply is disabled.

`failures` contains latest unresolved terminal maintenance reports per branch. A queued retry does not hide an earlier failure. `recoveries` contains RecoverySummary objects:

```json
{
  "generation_id":"0a1b7fe7-18c5-4a53-a1a6-d344ae73b42a",
  "operation_id":"0a1b7fe7-18c5-4a53-a1a6-d344ae73b42a",
  "version":"8.4","state":"recovery_pending",
  "can_repair":true,"can_rollback":true
}
```

`operation_id` is the latest head to use for review. `rollback_reason` is an optional string explaining unavailable rollback. Capability flags are discovery hints, not permission to skip review. The persistent internal `Pending` flag is exposed as `state: "recovery_pending"`, not a separate response boolean; it keeps conflicting mutations gated even after the worker exits or an operation reports failure. Successful resolution exposes `succeeded` or `rolled_back`. An idle worker alone does not imply maintenance is resolved.

A Plan has `id`, `reviewed` (boolean), optional `parent_plan_id` (discovery UUID for installation reviews), `version`, `candidate_id`, `created_at` (Unix seconds), `inventory`, `impact`, and `rollback: {"available":true}` or `{"available":false,"reason":"..."}`. The candidate fingerprint identifies selected package identities and target revisions rather than a check timestamp. Discovery plans have `reviewed: false` and empty runtime impact. Installation reviews expire after 15 minutes and cannot outlive or replace their parent discovery result. A failed or newer Check invalidates earlier reviews. GET remains a pure fetch and never converts discovery into installation authorization.

`inventory` contains `version`, `packages` (the complete installed branch family), `changes` (proposed upgrades), `blocked_reasons`, and `policy_hash` (repository/pinning fingerprint). Each package contains:

- `name`, `architecture`, `installed`, `candidate`: binary identity and full Debian revisions.
- `source`, `source_version`, `candidate_source`, `candidate_source_version`: source/build identity; independently packaged extensions retain their own numbering.
- `sha256`, `size`: candidate archive digest and byte size.
- `held`, `automatic`, `configured`: hold, automatic-installation and fully-configured flags.
- `origins`: records with `origin`, `label`, `archive`, `component`, `site`, `trusted`. These are repository evidence, not proof of historical installed provenance.
- Optional `installed_archive`: authenticated repository metadata for the installed revision, using the PackageArchive schema below. Omission means the old archive has not been established as available.

A PackageArchive has `name`, `architecture`, `version`, `sha256`, `size`, `origins` (same schema), and `control`, a string-to-string map containing `Package`, `Version`, `Architecture`, `Source`, `Depends`, `Pre-Depends`, `Conflicts`, `Breaks`, `Replaces`, `Provides`, `Multi-Arch`, and `Essential` (missing control values are empty strings). Recovery validates archive bytes, identity and control metadata against retained authenticated evidence; it uses a private offline resolver constrained to that family and preserves installation marks/holds.

`impact` contains `sites`, `workers`, optional `schedulers`, `config_hash`, `unit_hash`, `phpmyadmin` (boolean), `blocked_reasons`, and optional `notes` (strings explaining runtime limitations such as native grace deadlines). Each site has `id`, `domain`, `path`, `strategy`, `app_type`, `port`. Each worker has `id`, `site_id`, `name`, `kind`, `command`, `directory`, `instances`, `stop_seconds`; nonzero `port` and an `octane` descriptor are optional.

Supported custom queue workers retain `kind: ""` and additionally return
`protocol: "laravel_queue"`. This is a server-qualified maintenance protocol, not
feature ownership or a caller-supplied override. For example, a custom worker can
appear as `{"id":1,"site_id":8,"name":"Queue Worker","kind":"","protocol":"laravel_queue","command":"php8.4 artisan queue:work --sleep=3 --tries=3 --timeout=90","directory":"/home/fluxo/hotel","instances":1,"stop_seconds":15}`
alongside a separate managed worker with `kind: "laravel_queue"`. Both retain
their own IDs, commands, settings and running/stopped state. No daemon or feature
record is adopted, merged or removed. Review and execution verify the explicit
interpreter, supported argv, current site directory, effective unit definition
and initialized Laravel signal handling for active queues. Unsupported launchers
remain in `blocked_reasons`; stopped status alone does not authorize them.

Other qualified direct custom Artisan commands return `kind: ""` with
`protocol: "artisan_natural"`, for example
`{"id":12,"site_id":8,"name":"Report Worker","kind":"","protocol":"artisan_natural","command":"php8.4 artisan reports:generate --format=json","directory":"/home/fluxo/hotel","instances":1,"stop_seconds":15}`.
This protocol holds restarts and waits for natural exit of the full execution
domain; it never sends a guessed stop signal. Long-running commands need their
application's own graceful shutdown. After package verification, originally
running instances are resumed once; successful short-lived completion is
retained so recovery cannot replay it. Unconfirmed, failed or rebooted
activation requires operator review. Originally stopped instances and all
records/settings/enabled state are preserved. Literal arguments, fixed
interpreter/site binding and the existing unit/process checks are required;
this is not permission to run arbitrary HTTP-supplied commands. See
[custom Artisan maintenance](/operations/runtimes#other-custom-artisan-commands).

A scheduler entry has `id` (cron ID), `site_id`, `identity` (registration UUID), `version` (the actual command interpreter branch), `directory`, `command`, `database` (root-owned registration database path), `enabled`, `ready`, and optional `legacy_boot` (migration boot UUID) and `migration_nonce` (server-generated reload-receipt UUID). These fields are server-generated evidence, not caller-supplied executable overrides. Readiness is checked during installation review, never package discovery. Supported entries use the same admission/cgroup path for scheduled and manual execution. Their ticks are skipped during unresolved PHP maintenance; current tasks and descendants drain naturally, and successful activation restores future ticks. Custom commands/schedules remain unsupported by this automatic workflow. Qualified legacy entries wait for authenticated cron reload receipts, verified old panel admission and natural legacy-work drainage without a VPS reboot; newly registered managed entries are ready immediately. Missing/invalid established state blocks admission rather than clearing recovery evidence.

An OctaneDescriptor contains `backend`, `directory`, `storage`, `vendor`, `app_name`, `fingerprint`, `grace`, `rr_binary`, `rr_worker`, `rr_sha256`, and nullable `extension`. `extension`, when present, contains `name`, `version`, `file`, `sha256`. Backend-specific strings may be empty. These bind the approved application/native runtime and are not caller-supplied overrides. Managed, verified Swoole/RoadRunner consumers can be drained through their native master process; unsupported/custom runtimes and active FrankenPHP controllers are blocked. FrankenPHP’s embedded serving PHP is outside this APT transaction and remains unchanged. An update that changes the running Swoole native extension requires separate qualification and is blocked. Fluxo does not force-kill workers or shorten native deadlines; the application's existing request/task limits remain in effect.

A RecoveryPlan contains `id`, `generation_id`, `operation_id` (reviewed head), `version`, `kind` (`repair` or `rollback`), `goal` (`before` or `after`), `created_at`, `observation`, `packages` (complete target PackageArchive array), `impact` (same schema), and `retry_fpm_reload` (boolean). The latter identifies a reviewed FPM reload retry; it is not an extra request field. `observation` contains `version`, `policy_hash`, `journal_dirty`, `unrelated_pending` (package names), and `packages`: each with `name`, `architecture`, `installed`, `state` (dpkg current-state integer; `6` means fully installed), `automatic`, `held`.

### Operation progress and recovery limits

An accepted apply response:

```json
{
  "id":"0a1b7fe7-18c5-4a53-a1a6-d344ae73b42a",
  "kind":"apply","version":"8.4",
  "plan_id":"061ce8b3-94d7-4d70-8aa1-021c01a4630f",
  "status":"queued","phase":"queued","message":"","output":"",
  "username":"admin","ip_address":"192.0.2.10",
  "created_at":1792573200,"updated_at":1792573200,
  "started_at":0,"finished_at":0,"checkpoint":"","result":null
}
```

All operations expose those fields. `kind` is `check`, `apply`, `repair`, `rollback`, or legacy `verify`; `version` may be empty for a scheduled all-branch check. `plan_id` remains the original package Plan ID for recovery operations. Timestamps are Unix seconds, with zero before the corresponding event. `output` is bounded to the last 64 KiB of diagnostics. Active statuses are `queued` and `running`; terminal statuses are `succeeded`, `failed`, `partial`, `restart_required`, `health_failed`, and `interrupted`.

Progress phases include `queued`, `waiting_for_lock`, `refreshing`, `revalidating`, `preparing_recovery`, `draining`, `applying`, `packages_updated`, `activating`, and `verifying`; legacy work may report `downloading`. Terminal `phase` equals terminal `status`. `checkpoint` is initially empty and can record `package_transaction` or `packages_updated`; historical records may carry earlier checkpoint names. It is evidence, not authorization to replay a transaction.

On subsequent reads an uninitialized `result` is `{}`. Captured runtime evidence can include `units` (`name`, `active`, `pids` mapping PID to boot/process identity, `cgroup`, `main_pid`, `restarts`, `state`, `sub_state`, optional `octane` with `backend`, `master_pid`, `manager_pid`), `sockets`, `default_cli`, and optional `signalled`, `signal_delivery`, `signal_attempts`, `reload_approved_head`. Clients should display progress and poll the operation instead of interpreting these journals as instructions to issue signals.

Managed queue/Horizon and supported Octane consumers stop accepting new work and drain before package mutation. Persistent barriers preserve stopped services and prevent premature restart across interruption/reboot. FPM configuration, process replacement and request health are checked before success. Shared-cache Laravel restart broadcasts are not used. Missing or contradictory process identities, changed native bindings, unconfirmed signal delivery, corrupt archives or journals, dirty dpkg state, unrelated pending packages and changed repository policy can require operator intervention. Repair is scoped recovery, not unrestricted `dpkg --configure -a` or `apt --fix-broken`.

Terminal response excerpts:

```json
{"status":"succeeded","phase":"succeeded","message":"PHP 8.4 previous package revisions restored and affected services verified. Application files, data and jobs were not rolled back"}
```

```json
{"status":"partial","phase":"partial","message":"The package transaction did not verify completely. Review Repair or Roll back PHP packages; no automatic rollback was attempted. Operator repair may still be required."}
```

HTTP `202` means accepted, not installed or healthy. Poll through completion and consult `recoveries` for the latest reviewed action. A package transaction may complete while runtime health still fails. Neither Repair nor Rollback guarantees that every interrupted system is recoverable without manual work.

### Errors

Maintenance errors use `{ "code": "...", "message": "..." }`:

| HTTP | Code | Meaning |
|---|---|---|
| `400` | `invalid_request`, `invalid_version`, `invalid_id` | Invalid body, required field, kind, branch or UUID. |
| `401` | Existing authentication response | Missing/invalid JWT. |
| `404` | `not_found` | Missing plan, operation or recovery resource. |
| `409` | `maintenance_busy` | Another operation or persistent maintenance gate blocks the request. |
| `409` | `review_changed` | Runtime readiness changed or the 15-minute installation review expired. Choose Update to review again; successful package discovery remains available. |
| `409` | `plan_changed` | Stale/expired plan, superseded head, or reused UUID with another intent. |
| `409` | `recovery_required` | Resolve previous package/runtime maintenance before starting another update. |
| `422` | `rollback_acknowledgement_required` | Explicit acknowledgement is missing for an update without retained rollback. |
| `422` | `unsupported_configuration` | Unsupported action/configuration, including legacy verification requested for a recovery generation. |
| `503` | `supervisor_unavailable` | Installed root-owned Fluxo/systemd supervision is unavailable. |
| `500` | `maintenance_unavailable` | Other errors, including database failures and some operator-repair/recovery-validation failures. Inspect `message`; a generic error does not authorize broad repair or bypassing a hold. |

```json
{"code":"rollback_acknowledgement_required","message":"explicit acknowledgement is required to update without verified package rollback"}
```

```json
{"code":"maintenance_unavailable","message":"The package journal or unrelated packages require operator repair before this scoped recovery"}
```

```json
{"code":"plan_changed","message":"PHP packages or affected applications changed; check for updates again"}
```

## Managed Laravel Octane enablement

`GET /api/v1/sites/{id}/features` includes `octane_installed`, `octane_version`, `octane_available`, and `octane_enabled`. Enable with `POST /api/v1/sites/{id}/features/octane/enable`; disable with `POST /api/v1/sites/{id}/features/octane/disable`. Both require the JWT header, an existing numeric site ID, and no request body or query fields. The server chooses the configured PHP branch, active application directory, managed command and available local port; callers do not supply a launcher or native PID.

```http
POST /api/v1/sites/42/features/octane/enable
Authorization: Bearer YOUR_JWT
```

A new, verified enable returns `201` with an empty body; reconciliation of an already enabled feature returns `204`. Disable returns `204`. New enablement checks the installed Composer capability, runtime compatibility and startup readiness before routing traffic to Octane. Pending enablement claims and running deployments exclude one another. Zero-downtime deployment is incompatible (`400`); missing sites return `404`; unsupported configuration, custom Nginx overrides, missing Octane capability or failed readiness return `422`. Other startup/cleanup/persistence failures can return `500`.

If startup or cancellation leaves an uncertain live runtime, protected cleanup retains its daemon record and reserved port until native drainage can be proved. Re-enabling first retries cleanup and then performs fresh compatibility/startup verification; unresolved cleanup returns `409`. Pending statuses are `enable_pending`, `enable_publish_pending`, `enable_fallback_pending`, and `enable_cleanup_pending`. They are exposed in daemon listings but do not count as `octane_enabled`; ordinary `failed`/`stopped` states of a previously enabled runtime retain enabled semantics. Pending site edits/deletion and generic daemon start/stop/restart return `409` to preserve recovery identity.

For interrupted publication, cleanup restores, reloads, verifies and durably records safe routing before backend removal. Failures before publication do not publish Octane routing. Custom Nginx overrides remain unsupported for enablement. Native cleanup does not force-kill in-flight work; unsupported/uncertain runtimes remain held for retry or operator action. Octane endpoint errors are plain text, unlike the PHP-maintenance JSON error envelope. For example:

```http
HTTP/1.1 409 Conflict
Content-Type: text/plain; charset=utf-8

Octane enablement cleanup remains pending; routing was not changed: Octane service changed during failed-enable cleanup
```

## Environment save preference

`GET /sites/{id}/env` returns `content` and the boolean `cache_config_after_save` preference. `POST /sites/{id}/env` accepts JSON such as `{"content":"APP_ENV=production\n","cache_config_after_save":true}`. The optional boolean defaults to false for new and existing sites; omitting it preserves and uses the saved preference. Enabling it on a non-Laravel site returns `400`. Missing sites return `404`; file or preference persistence failures return `500`.

After saving the file and preference, the endpoint automatically rebuilds Laravel's configuration cache when enabled, using the site's PHP version and active application directory with a two-minute timeout. It returns HTTP `200` with `status: "saved"` and `cache_status: "success"`, `"failed"`, or `"skipped"`. Failure also includes `cache_error` and does not roll back the environment save. No separate command request is needed, and automatic rebuilding does not create command-history entries. Explicit commands submitted through `/commands` retain their normal history behavior.

## Managed Laravel Queue Worker

`GET /sites/{id}/features` returns `queue_worker_enabled`, `queue_worker_available`, the saved `queue_worker_config`, the current `queue_connection`, and `custom_queue_workers`. Enable or reconfigure the managed worker with:

```http
POST /api/v1/sites/42/features/queue-worker/enable
Content-Type: application/json

{
  "connection": "redis",
  "queues": "high,default",
  "processes": 2,
  "sleep_seconds": 3,
  "tries": 3,
  "timeout_seconds": 60,
  "backoff_seconds": 5,
  "memory_mb": 128,
  "max_time_seconds": 3600,
  "force": false
}
```

Connections and queue names must be shell-safe Laravel configuration keys; `sync` and `null` are rejected. Process count is `1–16`, sleep is `0–60`, tries is `0–100`, timeout is `1–86400`, backoff and maximum runtime are `0–86400`, and memory is `32–4096` MB. A successful enable returns `201 Created`; reconfiguration replaces the existing managed worker only after preserving its prior settings. Horizon conflicts return `409 Conflict`.

`POST /api/v1/sites/{id}/features/queue-worker/disable` stops and removes the managed processes and deploy hook while preserving the saved settings, returning `204 No Content`. It does not remove custom daemons that happen to run `artisan queue:work`.

## Site mutation contract

`POST /sites` requires an `app_type` of `laravel`, `php`, `wordpress`, `node`, `python`, or `html`. Choose it as a provisioning contract rather than an editable label. After creation, `PUT /sites/{id}` may omit `app_type` or repeat the current value for compatibility with older clients, but a different value returns `409 Conflict`. The deployment strategy follows the same immutable-after-creation rule. Other compatible settings, such as PHP version, repository, branch, web root, and runtime-specific options, remain editable.

Laravel, PHP, and Python creation requests may omit database fields. WordPress creation requires MySQL/MariaDB. Whenever `database_name` is supplied, `database_user` and `database_password` are also required, the user must not be a `fluxo`, `root`, or `postgres` control-plane identity, and Fluxo verifies that the supplied account can access the selected database before provisioning. For generated `.env` portability, the database password must not contain a single quote.

Python creation accepts `python_preset` (`django`, `flask`, `fastapi`, or `generic`), `python_entrypoint`, a relative `app_directory`, `package_manager` (`pip` or `uv`), `build_command`, `start_command`, and `app_port`. Generic repository-backed applications require an explicit start command. The Python runtime endpoints are `GET /server/python/info`, `POST /server/python/install`, `POST /server/python/restart`, and `POST /server/python/remove`.

Domain creation accepts `www_redirect` as `from_www`, `to_www`, or `none`. When the field is omitted, an ICANN registrable root domain defaults to `from_www`; subdomains, explicit `www.` hostnames, private suffixes, and unknown suffixes default to `none`. `PUT /sites/{id}/domains/{domain_id}` updates that behavior, where `domain_id` `0` identifies the primary domain. The update returns `409 Conflict` when the generated hostname is already owned or when the domain's active certificate does not cover every hostname required by the proposed behavior. Deactivate the domain certificate, apply the behavior change, and then issue or assign a compatible certificate.

Panel-domain endpoints are grouped under `/settings/panel-domain`: `GET` returns status, `POST /letsencrypt`, `POST /custom`, and `POST /clone` activate a hostname with the selected certificate workflow, `GET /cloneable` lists compatible custom certificates, and `DELETE` removes the managed proxy. Activating a panel domain does not disable direct access on the configured dashboard port.

### Site vhost

`GET /sites/{id}/vhost` returns the effective stored or generated configuration:

```json
{
  "config": "server {\n    listen 80;\n}\n",
  "customized": false,
  "revision": "SHA256_REVISION",
  "path": "/etc/nginx/sites-available/example.com"
}
```

Save a complete replacement with `PUT /sites/{id}/vhost`:

```json
{
  "config": "server {\n    listen 80;\n    server_name example.com;\n}\n",
  "expected_revision": "SHA256_REVISION"
}
```

Restore the current Fluxo-generated default with `POST /sites/{id}/vhost/restore` and an `expected_revision` field. Both mutations validate the full Nginx configuration and reload it transactionally before changing the stored override. Payloads are limited to 256 KiB, stale revisions return `409 Conflict`, and configurations that fail validation or activation return `422 Unprocessable Entity` without replacing the last working vhost.

Firewall list responses include `managed_by` and an `active` value verified against UFW's persisted rule state. Deleting an installer-managed baseline rule returns `409 Conflict`; change protected SSH, HTTP, HTTPS, or dashboard access deliberately over SSH instead. Creating a rule that already exists directly in UFW also returns `409` so Fluxo does not silently adopt and later delete an externally managed rule.

## SSH access security

`GET /ssh/security` evaluates the effective OpenSSH policy for the `fluxo` user and bypasses response caching. Its response includes:

```json
{
  "available": true,
  "password_authentication": "yes",
  "keyboard_interactive_authentication": "no",
  "public_key_authentication": "yes",
  "permit_root_login": "prohibit-password",
  "password_login_enabled": true,
  "hardened": false,
  "managed": false,
  "authorized_key_count": 1,
  "authorized_keys_valid": true,
  "can_harden": true
}
```

Activate Fluxo's validated key-only policy with:

```http
POST /api/v1/ssh/security/harden
Content-Type: application/json

{
  "key_access_confirmed": true,
  "recovery_access_confirmed": true
}
```

Both acknowledgement fields are required. The server also requires at least one usable bare RSA, Ed25519, or ECDSA key in the `fluxo` user's securely owned `authorized_keys` file. Fluxo validates the staged syntax and effective `fluxo` and `root` policies for remote IPv4, remote IPv6, and local IPv4/IPv6 contexts before installation, validates again after installation, reloads OpenSSH, and rolls back and reloads the prior live state on failure. An unmanaged file at Fluxo's policy path returns `409 Conflict` rather than being overwritten.

`DELETE /ssh/security/hardening` removes only the Fluxo-managed policy and returns the newly effective status. It may still report key-only access when a VPS-provider or administrator policy independently disables passwords. Hardening transitions and key mutations are serialized. `DELETE /ssh-keys/{id}` returns `409 Conflict` when removal would leave no usable key while effective password login is disabled. Privileged human-key and deploy-key filesystem operations fail closed if `.ssh` is a symlink or changes during the operation.

## Asynchronous operations

Site deployment returns HTTP `202 Accepted` after creating a pending deployment. Poll deployment history or connect to the WebSocket stream rather than assuming the HTTP request represents completion.

Runtime installation, backup execution, and other long-running operations may also continue beyond an initiating UI transition. Read the resource status returned by the relevant endpoint.

## WebSocket

Deployment and command logs are delivered through:

```text
wss://YOUR_SERVER_IP:9595/api/v1/ws?site_id=SITE_ID&token=YOUR_JWT
```

With a panel domain, use `wss://admin.example.com/api/v1/ws?...` instead. The managed proxy forwards WebSocket upgrades and the server requires the Origin hostname to match the request hostname.

The server validates the token and WebSocket origin. Keep the token out of logs and avoid sharing connection URLs.

## Errors

Fluxo uses standard HTTP status families:

- `400` for invalid input
- `401` for missing or invalid authentication
- `404` for missing resources
- `409` for lifecycle conflicts
- `422` for valid requests that cannot be applied to the current app/runtime state
- `500` for unexpected or system-operation failures

Some error responses are plain text because the Vue client surfaces the response body directly. API clients should handle both text and JSON errors.

## Mutation safety

The API has no separate read-only token scope. A valid administrator JWT can perform privileged dashboard operations. Store it only in a trusted automation environment and prefer short-lived login sessions over embedding credentials in scripts.


## PHP repository migration

These routes use the `/api/v1` prefix and require `Authorization: Bearer <token>`. All POST bodies use `Content-Type: application/json`, reject unknown fields and trailing JSON, and are limited to 4 KiB. They never accept repository URLs, keys, filenames or commands from the client.

| Method and path | Request | Result |
| --- | --- | --- |
| `GET /server/php/updates/repository` | No parameters or body | `200` local configuration status; no network refresh or mutation |
| `POST /server/php/updates/repository/review` | Required `request_id` UUID; no `plan_id` | `202` supervised `repository_review` operation |
| `POST /server/php/updates/repository/apply` | Required `plan_id` UUID from review and `request_id` UUID | `202` supervised `repository_apply` operation |

Use `GET /server/php/updates/operations/{operation_id}` to poll progress and read the final operation. Request IDs are idempotent for the same operation intent; reusing one with different intent returns `409`. A successful review operation’s `result` is a public repository review. The stored plan’s source-file contents stay private. Review expiry is 15 minutes. A pending interrupted migration can resume its original plan after expiry; failed or consumed plans require a new review.

Example review request:

```json
{"request_id":"b1a43128-9fa2-4dc5-9a57-8581041a8791"}
```

Example successful review `result`:

```json
{
  "id":"cdd3b690-3702-4342-b4d8-f6cb411c905a",
  "created_at":1792598400,
  "codename":"noble",
  "architecture":"amd64",
  "files":["/etc/apt/sources.list.d/ondrej-php.sources","/etc/apt/sources.list.d/fluxo-sury-php.sources"],
  "disabled_ppa_entries":1,
  "key_fingerprint":"15058500A0235D97F5D10063B188E2B695BD4743",
  "rollback_unavailable":[],
  "package_blockers":{},
  "retained_extensions":[],
  "migration_blockers":[]
}
```

`files` lists source and managed branch-priority files changed by setup; the scoped signing key is also provisioned. `rollback_unavailable` names current packages whose complete verified rollback archive was unavailable during review. `package_blockers` maps installed PHP branches to installation issues under the proposed repository policy. `retained_extensions` names independent extensions preserved at verified installed revisions. `migration_blockers` identifies unsupported preservation or configuration conditions that prevent the switch itself. Review performs no package installation or active-source changes.

Example apply request:

```json
{"plan_id":"cdd3b690-3702-4342-b4d8-f6cb411c905a","request_id":"f0585a5c-91a6-4ab3-91cd-4a7775887989"}
```

Status responses contain `state` (`current`, `available`, `blocked`, `interrupted`), `message`, and `can_review`. An interrupted response also includes `resume_plan_id` and its public `review`. Status reads inspect local files only. Successful apply invalidates previous PHP discovery; clients must request a new check before reviewing package installation. It never installs PHP or restarts applications.

Malformed requests return `400 invalid_request`; unauthenticated requests return `401`; a missing plan returns `404 not_found`; concurrent maintenance returns `409 maintenance_busy`; expired/consumed reviews return `409 review_changed`; unresolved recovery or repository publication returns `409 recovery_required`; unsupported configuration returns `422 unsupported_configuration`; an unavailable installed supervisor returns `503 supervisor_unavailable`. Worker-time repository/network, inventory-drift or conflict failures are reported through the accepted operation’s terminal status and message, for example:

```json
{"status":"failed","kind":"repository_apply","message":"Installed PHP packages or marks changed since repository review; review again"}
```

Source publication is journaled across individual durable file replacements. Preimage restoration refuses concurrent operator edits. Resuming repository setup reconciles its journal; it is not a broad APT repair. PHP recovery policy authorizations bind exact repository transitions and immutable retained manifests; unknown source or pin edits remain invalid.

## Node.js runtime updates

All routes below use the `/api/v1/server/node/updates` prefix and require `Authorization: Bearer <token>`. Send mutation bodies as `Content-Type: application/json`. There are no query parameters or caller-supplied versions, URLs, hashes, paths, commands, services or process IDs. Responses are JSON with `Cache-Control: no-store`, except successful dismissal (`204`, empty). GET requests read stored evidence only and never perform network discovery or install a runtime.

| Method and suffix | Path/body fields and validation | Success |
|---|---|---|
| `GET /` (omit trailing slash) | No parameters or body. | `200` Status. |
| `POST /check` | Required `request_id`: UUID identifying this intent. | `202` Operation, kind `check`. Daily checks use the same supervised discovery path. |
| `POST /plans/{id}/review` | `id`: current snapshot's `discovery_id` UUID. Body `{}`. | `200` immutable Plan, kind `apply` for a managed runtime or `migrate` for a supported legacy source; installation blockers appear in `impact.blocked_reasons`. Review may stage/validate the inactive candidate but never activates it or stops apps. |
| `POST /apply` | Required `plan_id` and `request_id` UUIDs. | `202` Operation. Requires an unblocked review less than 15 minutes old, the same successful discovery less than 24 hours old, and unchanged runtime/application evidence. |
| `POST /migrate` | Required `plan_id` and `request_id` UUIDs; exactly the same JSON validation as `/apply`. | `202` Operation, kind `migrate`. Requires a fresh, explicitly reviewed migration plan and unchanged legacy source/discovery. Ordinary `/apply` cannot approve a migration, and `/migrate` cannot approve an ordinary update. |
| `GET /operations` | No parameters or body. | `200` array of the latest 30 Operations, newest first. |
| `GET /operations/{id}` | `id`: operation UUID. | `200` Operation, including current or terminal progress. |
| `POST /operations/{id}/recovery-plan` | `id`: latest recovery generation's current **head operation** UUID. Required `kind`: `repair` or `rollback`. | `200` immutable recovery Plan, including freshly authenticated `retained.before`/`retained.after` Releases. Only the newest generation can be recovered; both exact retained releases and consumers must still qualify. Review downloads/verifies both official archives against current bundled trust; it never stops apps or changes the active runtime. |
| `POST /recovery/apply` | Required `plan_id` and `request_id` UUIDs. | `202` Operation for the reviewed `repair` or `rollback`. Review expires after 15 minutes and must still name the current generation head. |
| `POST /notices/dismiss` | Required nonempty `candidate_id`: exact snapshot candidate fingerprint. | `204`. Dismissal applies to that candidate; a different candidate can notify again. |

Mutation JSON is limited to 4096 bytes. Unknown fields, malformed/trailing JSON, invalid UUIDs and unsupported recovery kinds return `400 invalid_request`. Replaying a request ID with the same operation kind and plan returns the original operation before busy/expiry admission; reusing it for a different intent returns `409 review_changed`. A new check has its own request ID. Accepted work may still fail worker-side revalidation; `202` does not mean installed or healthy.

Legacy discovery uses `snapshot.state: "migration_available"`, `migration_available: true`, a server-derived `legacy_source` and the authenticated official `candidate`. It does not set `update_available` or create an automatic installation notice. Expired evidence disables migration. A migration Plan has `kind: "migrate"`, `migration` holding the approved legacy source and `after` holding the staged official Release; its empty `before` is not an official proof of the APT runtime. For example, explicitly approve the returned migration plan with:

```http
POST /api/v1/server/node/updates/migrate
Authorization: Bearer YOUR_JWT
Content-Type: application/json

{"plan_id":"061ce8b3-94d7-4d70-8aa1-021c01a4630f","request_id":"f5f34f6b-9d65-4f2a-bbd5-46b4fd0d7637"}
```

The `202` response uses the existing Operation schema with `kind: "migrate"`. The `migration`/`legacy_source` receipt has `schema` (currently 1), `kind` (`nodesource-apt-local-baseline`), `version`, `npm`, `package_version`, `architecture`, `node` (exact `path`, `sha256`, device/inode/mode), `native` (dependency file receipts), `npm_tree_sha256`, `package_files_sha256`, `links` (original `node`/`npm`/`npx` presence and symlink targets), `state` (base64 prior toolchain receipt), `state_mode`, `manager_tree_sha256`, `manager_links` (exact command symlinks) and `managers` (versions). These are server-derived read-only evidence; requests never accept them. They record a local baseline, not official archive authentication.

Migration generations use `plan.migration`, separately refreshed `migration_target` (official Release) and `migration_step` (last journaled command-link/receipt mutation intent). They do not fabricate `retained.before` as an official Release. The existing recovery routes return a fresh Plan with `migration` and `after` equal to the reauthenticated official target; `kind: "rollback"` selects the original APT source instead. Repair selects the official destination. Both verify the unchanged legacy baseline, exact original/target link states and original consumer intent. Changed APT files, foreign command links or unavailable package locks fail closed. Native package locks disappear after interruption; API recovery never overwrites changed APT files to make rollback pass.

Example check and installation review:

```http
POST /api/v1/server/node/updates/check
Authorization: Bearer <token>
Content-Type: application/json

{"request_id":"8f9e6390-a0c9-4c83-92c5-f93f4aa8166b"}
```

```json
{"id":"7e75f822-4900-43dc-a5d7-29d03227235b","kind":"check","plan_id":"","status":"queued","phase":"queued","message":"","created_at":1792695600,"updated_at":1792695600,"username":"admin","ip_address":"192.0.2.10"}
```

After the check succeeds, read `GET /api/v1/server/node/updates`, then review its discovery ID:

```http
POST /api/v1/server/node/updates/plans/b38ab807-96c7-465c-8183-729a531d3952/review
Authorization: Bearer <token>
Content-Type: application/json

{}
```

Review returns an immutable plan with the fields described below. For example, its version/impact portion can contain:

```json
{"kind":"apply","before":{"release":{"version":"24.19.0","npm":"11.17.0"}},"after":{"version":"24.20.0","npm":"11.18.0"},"impact":{"sites":[{"id":1,"domain":"app.example.com","mode":"server","app_type":"node"}],"units":[{"id":3,"site_id":1,"domain":"app.example.com","name":"fluxo-daemon-3.service","active":true,"bun_only":false}],"blocked_reasons":[]}}
```

The example above is an excerpt: the real response also includes plan IDs/timestamps and complete release, runtime and unit evidence. Version numbers are illustrative. Submit the **returned plan ID**, not the discovery ID:

```http
POST /api/v1/server/node/updates/apply
Authorization: Bearer <token>
Content-Type: application/json

{"plan_id":"29ef141c-e76b-47f2-9ad3-d11c75d06542","request_id":"f7114422-0cd5-4454-8ad9-eb4d995c74b7"}
```

Success returns `202` with an Operation, initially `queued`. Poll its operation ID. After an interruption or for retained rollback, use the current `recoveries[].head`:

```http
POST /api/v1/server/node/updates/operations/05959774-bb58-49a4-8b8b-3a34524b7817/recovery-plan
Authorization: Bearer <token>
Content-Type: application/json

{"kind":"rollback"}
```

```http
POST /api/v1/server/node/updates/recovery/apply
Authorization: Bearer <token>
Content-Type: application/json

{"plan_id":"bb7bb88d-141d-4c1a-9d10-59b2c0111977","request_id":"d23e349a-376b-4fef-8c32-1b7aa527207d"}
```

A Status contains `snapshot`, nullable `active_operation`, `operations` (latest 30), `recoveries` (newest generation, at most one), and boolean `locked`. An initial response is:

```json
{"snapshot":{"candidate_id":"74234e98afe7498fb5daf1f36ac2d78acc339464f950703b8c019892f982b90b","state":"unchecked","checked_at":0,"attempted_at":0,"discovery_id":"","installed":{"release":{"version":"","npm":"","architecture":"","filename":"","sha256":"","manifest_sha256":"","signer":"","trust_revision":""},"root":"","managers":null},"candidate":null,"update_available":false,"notice":false},"active_operation":null,"operations":[],"recoveries":[],"locked":false}
```

Snapshot fields: `state` is `unchecked`, `available`, `current`, `stale` or `check_failed`; `checked_at` is last successful discovery time; `attempted_at` is last attempted discovery; optional `error` explains failure; `discovery_id` names the successful discovery; `installed` is the authenticated observed runtime; nullable `candidate` is the exact newer same-major release; `candidate_id` is its fingerprint; `update_available` requires a fresh successful check; `notice` additionally respects dismissal. Times are Unix seconds. A failed check retains previous evidence for inspection, disables its eligibility and schedules a later retry. Historical `lts` flags do not establish current support: a reviewed support policy excludes EOL/non-LTS majors and itself expires.

A Release contains `version` and bundled `npm` (strict version strings), `architecture` (`x64` or `arm64`), server-derived official `filename`, archive `sha256`, signed manifest `manifest_sha256`, verified public-key `signer` fingerprint, bundled keyring `trust_revision`, and optional `tree_sha256` after staging/authentication. Candidate discovery does not yet have a tree digest. An OwnedRuntime contains `release`, absolute `root`, and `managers` (Corepack/pnpm/Yarn/Bun observed versions). These are evidence, not request overrides.

A Plan contains server-derived `consumer_policy` (currently `3`), `id`, `kind`, `discovery_id` (normal apply), `created_at`, optional recovery `generation_id` and `head`, `before` (OwnedRuntime), `after` (Release), and `impact`. Impact contains `sites`, `units`, `blocked_reasons`, `php_policy` (currently `1`), optional `php_consumers` and optional `php_resumes`. Each site has `id`, `domain`, `mode` and `app_type`; this is build-impact context, not a claim that every listed site executes Node. Each unit has `id`, `site_id`, `domain`, `name`, generated `command`, `directory`, `port`, `active`, `enabled`, `definition` fingerprint, `main_pid`, `pids` (PID-to-boot/start-identity map), `bun_only`, and optional observed systemd `state` and `sub_state`. Recovery also reports `listener_present` (boolean); `active` retains the **original generation's running/stopped intent**, even if the current service has failed. `bun_only` describes the recorded runtime observation and NEVER exempts a unit from holds, full-cgroup drainage, restart or verification. A Bun application also includes `bun: {path, version, sha256}`, binding the existing root-managed Bun executable, including for stopped Bun launchers. Any Node children must use the selected exact Node executable; Bun must remain unchanged. All originally active generated applications, including Bun, restart; originally stopped units remain stopped. Runtime-only fields are server-derived and never authorize client-selected signals or commands. Recovery authenticates the saved service configuration, barriers, retained runtimes, cgroup/process identities and any existing listener ownership. Failed, transitional and missing-listener states can be reviewed for recovery; health is verified after activation. Naturally exiting processes do not invalidate a recovery review; changed configuration, unknown runtimes and foreign listeners still block it.

Each `php_consumers` entry contains `branch` (exact CLI branch, independent of the site's FPM setting), `domain`, `consumer` (`id`, `site_id`, `name`, unchanged `kind`, optional `protocol`, exact `command`, `directory`, `instances`, `stop_seconds`, optional `port`/`octane` descriptor), `php: {path, version, sha256}` for the unchanged package-owned CLI, and `units`. Each PHP unit contains original `enabled`, exact normalized effective `definition`, and `runtime: {name, active, pids, cgroup, main_pid, restarts, state, sub_state, natural?, octane?}`. `active` is retained intent; state/PIDs are current recovery observations. Every instance must be present. Protocol `artisan_natural` sends no signal, waits for full natural exit, and permits at most one journaled resume. Optional `php_resumes` maps unit name to `{boot, after_monotonic_usec, invocation?, completed?}`. A successful completion is not replayed; missing/failed/rebooted/replaced execution evidence keeps recovery pending. These are server-derived observations, never client overrides. PHP-FPM and PHP packages are not changed by a Node operation.

Recovery Plans additionally contain `retained: {before: Release, after: Release}` with newly authenticated proofs for both original exact targets. `after` must equal the chosen retained target. Reauthentication can update signature/manifest/trust evidence only after current-key signature verification and canonical archive/tree validation; version, architecture, filename, npm, archive checksum and tree remain fixed. The worker repeats this preflight and persists the proofs before installing holds or stopping apps. Network/trust/persistence failures reject without runtime mutation; the queued operation alone closes admission during this preflight. Revoked/removed/expired signers fail closed. Fresh official signatures from currently trusted keys can authenticate the same recorded archive, but cannot replace its payload.

All daemon records are inventoried during normal/recovery review and worker revalidation, including global/orphan/duplicate records. Only positively inspected supported Node unit identities or qualified PHP consumer identities qualify; nonempty `managed_kind` is not an exemption. Unqualified Python/PHP/custom/unknown workers produce `impact.blocked_reasons` identifying the process name and site domain, even when stopped. For example: `Background process "Bridge" on app.example.com requires manual Node.js maintenance.` Global or orphaned records with no domain are explicitly marked `no site domain available`; they remain blocked. A new/changed unsupported record invalidates execution before proof staging or service holds. No request field or acknowledgement bypasses this policy. Cron inventory likewise includes every global/site/orphan record and requires exact managed Scheduler qualification plus verified admission. Review notes and blockers use domains and process/task names; numeric IDs remain in structured API fields for routing and identity checks.

An Operation contains `id`, `kind` (`check`, `apply`, `repair`, `rollback`), `plan_id`, `status`, `phase`, `message`, `created_at`, `updated_at`, initiating `username`, `ip_address`, and optional recovery `generation_id`. Statuses include `queued`, `running`, `succeeded`, `failed`, `interrupted`, `recovery_required`, `rolled_back`. Phases include `queued`, `waiting_for_lock`, `staging`, `reauthenticating`, `stopping`, `activating`, `verifying`, `rolling_back`, `complete`, `failed`, `launch_failed`, `interrupted` and `recovery_required`. Display message/status and poll; phase names do not grant permission to replay work. A verified automatic rollback reports `rolled_back` with the original failure, rather than update success.

Reviews with missing/older `consumer_policy` are rejected with `409 review_changed`, including already queued work. Fresh recovery reviews can coordinate historical `bun_only` generations: they authenticate current managed Bun receipts, preserve the original service identity and active/stopped intent, and persist fresh `coordination` separately before holds. For historical generations without PHP coordination, a fresh review may explicitly add currently qualified PHP consumers; their current intent is captured separately and then bound across recovery attempts. Receipt/configuration drift after review rejects preflight without mutation; immutable historical plans are not rewritten.

A recovery Generation contains `id`, current operation `head`, `state`, original immutable `plan`, captured `boot` UUID, activation `goal` (`after` or `before`), optional `retained` (same before/after Release proof schema as recovery Plans), optional `coordination` (Impact with fresh runtime receipts used for execution), `activation_attempted` (durable Node activation intent), and optional `php_state`. `php_state` contains schema `1`, immutable `original` consumer set, current `attempt` unit observations, native shutdown `stops` receipts, durable natural-command `resumes`, and optional server-owned `evidence: {owner, generation, units, state}`. Evidence owner is `node`; units are exact natural service names, and state is `preparing`, `held`, `releasing` or `released`. This independent systemd reference retains execution results across worker exit until they are journaled; it grants no dispatch or replay permission. Older generations acquire a reference on reviewed recovery without changing their original plan. Unknown schemas or inconsistent protocols fail closed. Node-owned PHP hold rows independently keep admission closed through partial cleanup. `coordination` covers every original supported unit, including historically exempted Bun units; absence requires a new recovery review before execution. States include `prepared`, `stopping`, `activating`, `starting`, `rolling_back`, `verified`, `recovery_required`, `succeeded`, `rolled_back`. Refreshed proofs never overwrite the original plan. A completed generation stays completed during non-mutating recovery preflight; its queued/running operation supplies admission until proofs and mutation intent are durable. Open generations and leftover service barriers keep admission closed regardless of a worker's terminal status. Success/rollback retains the newest recovery point; later updates supersede it. Recovery is manual approval; the worker only attempts automatic rollback after its own apply failure. Worker loss/reboot never authorizes an automatic new installation.

| HTTP | Code | Meaning |
|---|---|---|
| `400` | `invalid_request` | Invalid bounded JSON, unknown fields, UUID or recovery intent. |
| `401` | Plain text `Unauthorized` | Missing or invalid Bearer token. |
| `404` | `not_found` | Unknown plan/operation or no matching current recovery head. |
| `409` | `maintenance_busy` | Active operation, installer, PHP maintenance or unresolved Node recovery/hold. |
| `409` | `review_changed` | Expired/stale/superseded review, discovery/runtime/consumer drift or conflicting request-ID intent. |
| `422` | `unsupported_configuration` | Unsupported operation/recovery kind or configuration. Runtime blockers are normally returned in a successful review's `impact.blocked_reasons`. |
| `503` | `supervisor_unavailable` | Requires production/root/systemd supervisor. |
| `500` | `maintenance_unavailable` | Other trust, filesystem, database, staging or runtime inspection failures. The message explains the reason; it does not authorize broad repair. |

Example errors:

```json
{"code":"review_changed","message":"The Node.js update review changed or expired; check and review again"}
```

```json
{"code":"maintenance_busy","message":"Runtime maintenance is in progress or requires recovery"}
```

```json
{"code":"maintenance_unavailable","message":"Node.js release signature is untrusted or invalid; update Fluxo trust data before retrying"}
```

No update API performs a major migration, changes independent package-manager versions, rebuilds an application, or restores application data. Verification proves managed process/TCP/runtime adoption, not every application route. See [Node.js runtime updates](/operations/runtimes#node-js-updates) for supported consumer rules, cooperation boundaries, downtime and recovery limits.
