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
| Settings | General settings, GitHub accounts, SSH keys, firewall |

Firewall list responses include `managed_by` and an `active` value verified against UFW's persisted rule state. Deleting an installer-managed baseline rule returns `409 Conflict`; change protected SSH, HTTP, HTTPS, or dashboard access deliberately over SSH instead. Creating a rule that already exists directly in UFW also returns `409` so Fluxo does not silently adopt and later delete an externally managed rule.

## Asynchronous operations

Site deployment returns HTTP `202 Accepted` after creating a pending deployment. Poll deployment history or connect to the WebSocket stream rather than assuming the HTTP request represents completion.

Runtime installation, backup execution, and other long-running operations may also continue beyond an initiating UI transition. Read the resource status returned by the relevant endpoint.

## WebSocket

Deployment and command logs are delivered through:

```text
wss://YOUR_SERVER_IP:9595/api/v1/ws?site_id=SITE_ID&token=YOUR_JWT
```

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
