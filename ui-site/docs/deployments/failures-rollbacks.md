---
title: Failures and Rollbacks
description: Diagnose deployment failures, dismiss persistent alerts, and roll back successful commits.
---

# Failures and rollbacks

Fluxo keeps failed deployment information visible across every page of the affected site until it is dismissed or a newer deployment succeeds.

## Persistent failure alert

The site-level alert shows that deployment failed and offers **View issue**. The issue dialog contains the recorded failure reason and full command output.

- **Close** hides the dialog but leaves the alert unresolved.
- **Dismiss** acknowledges that specific failed deployment and removes the alert.
- A newer successful deployment automatically clears the alert.

The failed record and its output remain in deployment history in every case.

## Diagnose a failure

Read output from the first error upward and identify the phase:

| Phase | Common causes |
|---|---|
| Git preparation | Repository access, missing branch, deploy key, network |
| Dependency install | Lockfile conflict, unsupported runtime, package registry |
| Build | Missing environment variable, TypeScript/build error, memory exhaustion |
| Framework commands | Migration, cache, filesystem permissions, database access |
| Activation hook | Node process start, occupied port, Octane/Horizon reload |
| Timeout | Build or command exceeded the ten-minute deployment deadline |

Avoid dismissing an unresolved production failure only to hide the alert. Correct the cause and run a new deployment whenever possible.

## Roll back

Open a successful deployment with a recorded commit hash and select **Rollback**. Fluxo creates a new deployment targeted at that commit; history remains append-only.

In zero-downtime mode, the target commit is cloned and built as a new release before activation. In standard mode, the live checkout moves to the target commit and reruns application commands.

Rollback does not reverse database migrations automatically. If the newer release made an incompatible schema change, use the application's documented rollback procedure and a verified database backup.

## Failed runtime hook after activation

For managed zero-downtime deployments, Fluxo records the previous `current` target. If a required Node, Horizon, or Octane-related hook fails after activation, Fluxo attempts to restore the prior release and includes the outcome in deployment output.
