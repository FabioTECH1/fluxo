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
{"version":"0.4.34"}
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

## PHP patch maintenance

These endpoints use the base `/api/v1/server/php` and require `Authorization: Bearer YOUR_JWT`. Send `Content-Type: application/json` for requests with a body. Responses use `Cache-Control: no-store`; JSON responses also use `Content-Type: application/json`. There are no query parameters. Branch identifiers match `^[0-9]{1,2}\.[0-9]{1,2}$`, such as `8.4`, rather than an upstream patch number. Revisions use Debian ordering, including epochs and backports.

Checks only discover updates. Package installation requires explicit confirmation of an update or a reviewed Repair/Rollback plan. Neither page loads, daily checks, panel closure nor process restart authorize installation or automatic rollback.

| Method and path | Request and validation | Success |
|---|---|---|
| `GET /updates` | No body. Reads persisted state without refreshing APT. | `200` with `branches`, nullable `active_operation`, `failures`, and `recoveries`. |
| `POST /updates/check` | `{"version":"8.4"}`. Required branch; the FPM executable must be installed. | `202` Operation. Concurrent identical checks, including a running daily batch, coalesce. A check refreshes repositories once and inventories installed branches. |
| `GET /updates/plans/{plan_id}` | Required UUID path parameter. | `200` immutable Plan. |
| `POST /updates/apply` | Required `version`, `plan_id` UUID and `request_id` UUID; optional boolean `acknowledge_no_rollback`, default `false`. See example below. | `202` Operation, kind `apply`. The current eligible snapshot must reference the plan, which must be less than 24 hours old. |
| `GET /updates/operations/{operation_id}` | Required UUID path parameter. | `200` Operation, including durable progress and bounded diagnostics. |
| `POST /updates/operations/{operation_id}/recovery-plan` | Required current-head operation UUID in the path; JSON `{"kind":"repair"}` or `{"kind":"rollback"}`. No other kind is accepted. | `200` immutable RecoveryPlan. This reviews state and records a plan; it does not install packages or start/stop services. |
| `POST /updates/recovery/apply` | Required `plan_id` and `request_id`, both UUIDs. `plan_id` is the **recovery** plan ID. | `202` Operation with the reviewed `repair` or `rollback` kind. Recovery plans expire after 15 minutes. |
| `POST /updates/operations/{operation_id}/verify` | Required operation UUID. No body required. **Legacy operations without a recovery generation only.** | `202` legacy `verify` Operation. New maintenance generations return `422 unsupported_configuration`; use reviewed Repair instead. |
| `POST /updates/notices/dismiss` | `{"version":"8.4","candidate_id":"<64-character candidate fingerprint>"}`. Both required; fingerprint must identify a stored candidate. | `204`, no body. Dismissal applies to that candidate, not the check timestamp. |

Check, apply, recovery-plan, recovery/apply and dismissal JSON bodies are limited to 4096 bytes. Unknown fields, malformed JSON, extra JSON values, invalid branches and malformed UUIDs are rejected. No maintenance request accepts caller-selected package names, package paths, commands, unit names, APT options, archive URLs or native process IDs. Commands and paths in responses are server-derived evidence, not executable request inputs.

### Confirmation and idempotency

An update request with rollback available:

```http
POST /api/v1/server/php/updates/apply
Authorization: Bearer YOUR_JWT
Content-Type: application/json

{"version":"8.4","plan_id":"061ce8b3-94d7-4d70-8aa1-021c01a4630f","request_id":"f5f34f6b-9d65-4f2a-bbd5-46b4fd0d7637","acknowledge_no_rollback":false}
```

If `plan.rollback.available` is false, the client must present its reason and obtain explicit acknowledgement before sending `acknowledge_no_rollback: true`. Otherwise the API returns `422 rollback_acknowledgement_required`. The server retains and verifies the complete target package family before draining consumers. Previous revisions must form a complete authenticated archive set for rollback; installed files or an arbitrary cached `.deb` are not proof. Discovery-time availability does not guarantee that later downloading succeeds. Without acknowledgement, failure to retain the old set aborts before package changes.

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

A Plan has `id`, `version`, `candidate_id`, `created_at` (Unix seconds), `inventory`, `impact`, and `rollback: {"available":true}` or `{"available":false,"reason":"..."}`. The candidate fingerprint identifies selected package identities and target revisions rather than a check timestamp.

`inventory` contains `version`, `packages` (the complete installed branch family), `changes` (proposed upgrades), `blocked_reasons`, and `policy_hash` (repository/pinning fingerprint). Each package contains:

- `name`, `architecture`, `installed`, `candidate`: binary identity and full Debian revisions.
- `source`, `source_version`, `candidate_source`, `candidate_source_version`: source/build identity; independently packaged extensions retain their own numbering.
- `sha256`, `size`: candidate archive digest and byte size.
- `held`, `automatic`, `configured`: hold, automatic-installation and fully-configured flags.
- `origins`: records with `origin`, `label`, `archive`, `component`, `site`, `trusted`. These are repository evidence, not proof of historical installed provenance.
- Optional `installed_archive`: authenticated repository metadata for the installed revision, using the PackageArchive schema below. Omission means the old archive has not been established as available.

A PackageArchive has `name`, `architecture`, `version`, `sha256`, `size`, `origins` (same schema), and `control`, a string-to-string map containing `Package`, `Version`, `Architecture`, `Source`, `Depends`, `Pre-Depends`, `Conflicts`, `Breaks`, `Replaces`, `Provides`, `Multi-Arch`, and `Essential` (missing control values are empty strings). Recovery validates archive bytes, identity and control metadata against retained authenticated evidence; it uses a private offline resolver constrained to that family and preserves installation marks/holds.

`impact` contains `sites`, `workers`, `config_hash`, `unit_hash`, `phpmyadmin` (boolean), `blocked_reasons`, and optional `notes` (strings explaining runtime limitations such as native grace deadlines). Each site has `id`, `domain`, `path`, `strategy`, `app_type`, `port`. Each worker has `id`, `site_id`, `name`, `kind`, `command`, `directory`, `instances`, `stop_seconds`; nonzero `port` and an `octane` descriptor are optional.

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
