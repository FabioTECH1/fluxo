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
{"version":"0.4.20"}
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
| Laravel features | Scheduler, Nightwatch, Horizon, Octane, maintenance |
| Databases | Databases, users, grants, password rotation, sizes |
| Backups | Destinations, plans, runs, artifacts, downloads |
| Runtimes | PHP, Node.js, Nginx, database engines, service actions |
| Observation | Metrics, logs, downloads, clearing, activity |
| Settings | General settings, panel domain and SSL, GitHub accounts, SSH keys, firewall |

## Site mutation contract

`POST /sites` requires an `app_type` of `laravel`, `php`, `wordpress`, `node`, or `html`. Choose it as a provisioning contract rather than an editable label. After creation, `PUT /sites/{id}` may omit `app_type` or repeat the current value for compatibility with older clients, but a different value returns `409 Conflict`. The deployment strategy follows the same immutable-after-creation rule. Other compatible settings, such as PHP version, repository, branch, web root, and runtime-specific options, remain editable.

Laravel and PHP creation requests may omit database fields. WordPress creation requires MySQL/MariaDB. Whenever `database_name` is supplied, `database_user` and `database_password` are also required, the user must not be a `fluxo`, `root`, or `postgres` control-plane identity, and Fluxo verifies that the supplied account can access the selected database before provisioning. For Laravel and PHP `.env` portability, the database password must not contain a single quote.

Panel-domain endpoints are grouped under `/settings/panel-domain`: `GET` returns status, `POST /letsencrypt`, `POST /custom`, and `POST /clone` activate a hostname with the selected certificate workflow, `GET /cloneable` lists compatible custom certificates, and `DELETE` removes the managed proxy. Activating a panel domain does not disable direct access on the configured dashboard port.

Firewall list responses include `managed_by` and an `active` value verified against UFW's persisted rule state. Deleting an installer-managed baseline rule returns `409 Conflict`; change protected SSH, HTTP, HTTPS, or dashboard access deliberately over SSH instead. Creating a rule that already exists directly in UFW also returns `409` so Fluxo does not silently adopt and later delete an externally managed rule.

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
