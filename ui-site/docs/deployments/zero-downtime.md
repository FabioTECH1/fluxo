---
title: Zero-Downtime Deployments
description: Understand release directories, shared state, activation, cleanup, and limitations.
---

# Zero-downtime deployments

Zero-downtime mode prepares a complete release away from live traffic and atomically changes the `current` symlink only after application commands succeed.

## Directory layout

```text
/home/fluxo/example.com/
  .env
  current -> releases/20260804121000-42
  releases/
    20260804115500-41/
    20260804121000-42/
  storage/                 # Laravel shared storage
```

Nginx and managed services resolve the active application through `current`.

## Release sequence

1. Create a unique directory under `releases`.
2. Clone the configured branch or rollback commit.
3. Link the persistent site `.env` when it exists.
4. For Laravel, seed and link the complete shared `storage` tree.
5. Run application commands in the release.
6. Create a temporary symlink and atomically move it to `current`.
7. Run required runtime hooks.
8. Keep the five newest successful releases.

If application commands fail before activation, Fluxo removes the failed release and leaves `current` unchanged. If a critical managed runtime hook fails after activation, Fluxo attempts to restore the previous release.

## Benefits

- Failed builds do not overwrite the live checkout.
- Activation is an atomic filesystem operation.
- Previous successful commits can be rebuilt and activated through rollback.
- Concurrent requests do not see a partially installed dependency tree.
- Release cleanup prevents unlimited storage growth.

## Persistent data

Never store irreplaceable runtime data only inside a release directory. Each future deployment can replace it, and old-release cleanup will eventually delete it.

Laravel's full `storage` tree and the root `.env` are shared automatically. Add explicit shared directories or external storage for other framework-generated uploads.

## Node.js limitation

Server-rendered Node applications still run as a managed process. After activation, Fluxo restarts the process and waits for the internal TCP port. The release switch is atomic and failures are recoverable, but a single process restart can briefly interrupt open connections.

For stricter connection-level zero downtime, an architecture with multiple workers and health-aware handoff would be required.

## Python limitation

Python applications use the same release-safe model: each release receives its own virtual environment, the `current` symlink changes atomically, and Fluxo restarts the managed Gunicorn, Uvicorn, or custom process before checking its internal port. A single process restart can briefly interrupt long-lived connections even though a failed build never replaces the active release.

## Laravel Octane limitation

Fluxo does not offer Octane while zero-downtime deployment is enabled. Octane keeps application code in long-lived worker memory, which can retain stale application state and does not align with the current symlink lifecycle. Use standard deployment when enabling Octane.

## Requirements

Zero-downtime mode requires a configured Git repository. WordPress sites use standard in-place hosting and do not expose this mode.
