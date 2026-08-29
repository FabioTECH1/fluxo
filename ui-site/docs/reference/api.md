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
{"version":"0.4.25"}
```

Authenticated clients can call `GET /update-status`. Fluxo compares the installed version with the validated public manifest at `https://fluxo.fottify.com/api/v1/releases/latest` and returns `current_version`, `latest_version`, `update_available`, `release_url`, and check metadata. Successful checks are cached for six hours; temporary failures are cached briefly and return `check_available: false` so update awareness never blocks normal dashboard use.

These endpoints are informational. They cannot download, install, or activate a release. Administrators must review the release notes and perform upgrades from the server terminal.

## Main resource groups

| Resource | Representative endpoints |
|---|---|
| Authentication | `POST /auth/login`, `GET /auth/bootstrap` |
| Sites | `GET/POST /sites`, `GET/PUT/DELETE /sites/{id}` |
| Deployments | `POST /sites/{id}/deploy`, list, rollback, dismiss failure |
| Configuration | Site environment, WordPress config, and deployment settings |
| Files | List, read, write, create, move, upload, download, delete |
| Domains and SSL | Domain CRUD, Let's Encrypt, custom, clone, activate, deactivate |
| Site processes | Daemons and scheduled jobs, actions, and logs |
| Commands | Execute, stream, list, inspect, and delete history |
| Laravel features | Scheduler, Queue Worker, Nightwatch, Horizon, Octane, maintenance |
| Databases | Databases, users, grants, password rotation, sizes |
| Backups | Destinations, plans, runs, artifacts, downloads |
| Runtimes | PHP, Node.js, Nginx, database engines, service actions |
| Observation | Metrics, logs, downloads, clearing, activity |
| Settings | General settings, panel domain and SSL, GitHub accounts, SSH keys and effective SSH security policy, firewall |

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

`POST /sites` requires an `app_type` of `laravel`, `php`, `wordpress`, `node`, or `html`. Choose it as a provisioning contract rather than an editable label. After creation, `PUT /sites/{id}` may omit `app_type` or repeat the current value for compatibility with older clients, but a different value returns `409 Conflict`. The deployment strategy follows the same immutable-after-creation rule. Other compatible settings, such as PHP version, repository, branch, web root, and runtime-specific options, remain editable.

Laravel and PHP creation requests may omit database fields. WordPress creation requires MySQL/MariaDB. Whenever `database_name` is supplied, `database_user` and `database_password` are also required, the user must not be a `fluxo`, `root`, or `postgres` control-plane identity, and Fluxo verifies that the supplied account can access the selected database before provisioning. For Laravel and PHP `.env` portability, the database password must not contain a single quote.

Domain creation accepts `www_redirect` as `from_www`, `to_www`, or `none`; new domains default to `from_www`. `PUT /sites/{id}/domains/{domain_id}` updates that behavior, where `domain_id` `0` identifies the primary domain. The update returns `409 Conflict` when the generated hostname is already owned or when the domain's active certificate does not cover every hostname required by the proposed behavior. Deactivate the domain certificate, apply the behavior change, and then issue or assign a compatible certificate.

Panel-domain endpoints are grouped under `/settings/panel-domain`: `GET` returns status, `POST /letsencrypt`, `POST /custom`, and `POST /clone` activate a hostname with the selected certificate workflow, `GET /cloneable` lists compatible custom certificates, and `DELETE` removes the managed proxy. Activating a panel domain does not disable direct access on the configured dashboard port.

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
