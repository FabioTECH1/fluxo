---
title: Deployment Workflow
description: Understand Fluxo deployment triggers, queues, output, and managed runtime hooks.
---

# Deployment workflow

Fluxo records every deployment and processes deployments for the same site sequentially. A deployment can be triggered manually, by a GitHub webhook, by rollback, or by a repository synchronization event.

## Before the first deployment

Confirm:

- The repository and branch are correct.
- Private-repository access is configured.
- The environment file contains production values.
- The deployment script matches the application.
- The selected PHP or Node runtime is installed.
- The site web directory or static output directory matches the build.

## Start a deployment

Select **Deploy** from the site header and confirm the action. Fluxo navigates to the site's Deployments page, creates a pending record, and streams output as the queue starts it.

Deployments for one site run one at a time in creation order. Separate sites can deploy independently.

## Managed deployment phases

For current managed scripts, Fluxo owns the surrounding lifecycle:

1. Prepare the repository or a new release directory.
2. Link shared state required by the strategy.
3. Run the editable application commands.
4. Activate the release when using zero downtime.
5. Run managed hooks, such as process restarts or package-driven Laravel reloads.
6. Verify a server-rendered Node application's internal port.
7. Record commit metadata, status, output, and activity.
8. Clean older successful releases.

The deployment deadline is ten minutes. Long builds should be optimized or moved to an external build pipeline that produces deployable artifacts.

## Statuses

| Status | Meaning |
|---|---|
| Pending | Waiting for the site's deployment worker |
| Running | Script or managed runtime hooks are executing |
| Success | Application commands and required hooks completed |
| Failed | A command, timeout, activation, restart, or health check failed |

## Deployment history

History stores the branch, commit hash, message, author, trigger source, timestamps, and complete output. The list is paginated and remains the audit record after a failure alert has been dismissed or cleared by a later successful deployment.

## Environment exposure

The site's `.env` remains a file used by the application. Fluxo does not export every environment entry into deployment-process variables unless **Expose environment variables to deployment script** is enabled.

Reserved `FLUXO_` variables are provided by Fluxo and cannot be overridden by the site environment.

