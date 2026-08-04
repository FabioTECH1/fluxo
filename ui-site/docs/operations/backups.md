---
title: Backups
description: Configure encrypted S3 or R2 destinations, site backup plans, retention, downloads, and recovery.
---

# Backups

Fluxo creates off-server backups of selected site files and attached databases. Backups are organized into reusable destinations, per-site plans, and immutable run history.

## Add a destination

Open **Storage > Backups** and select **Add Destination**.

### Cloudflare R2

Provide a name, bucket, account ID, jurisdiction, object prefix, access-key ID, and secret. Use an R2 token with Object Read & Write restricted to the backup bucket.

### Amazon S3

Provide a name, bucket, region, and prefix. Authenticate with dedicated access keys or the AWS credential chain available to the server, such as an instance role.

Fluxo tests a destination by writing, reading, and deleting a temporary object. Stored access credentials are encrypted and never returned through the API.

Use a private bucket and least-privilege policy limited to the intended prefix. Changing a prefix affects future backups only; retain old-prefix access until previous recovery points expire.

## Create a backup plan

A plan selects:

- One site
- One destination
- Site files, attached databases, or both
- Every 6 hours, every 12 hours, daily, weekly, or manual-only schedule
- Start hour in the server timezone
- Minimal, recommended, or extended retention
- Enabled or paused state

File backups include persistent application and configuration content while excluding Git metadata, Node dependencies, caches, logs, and old release directories. Database artifacts are created separately for every selected database.

## Retention profiles

| Profile | Retention |
|---|---|
| Minimal | 7 most recent and 7 daily recovery points |
| Recommended | Recent runs plus 14 daily, 8 weekly, and 6 monthly points |
| Extended | Recent runs plus 30 daily, 12 weekly, and 12 monthly points |

Completed runs use unique object paths and are never overwritten. Failed run records are kept for 30 days.

## Run and monitor a backup

Choose **Back up now** from a plan to queue a manual run. History shows queued, running, completed, and failed states, total artifact size, trigger, and errors.

Do not delete a destination or site while relying on a running backup. Fluxo coordinates active operations, but a final recovery point should be allowed to complete and be verified.

## Download and restore

Completed artifacts can generate a short-lived download URL. Download the file archive and each required database dump to a secure recovery workstation or target server.

Fluxo currently provides artifact download rather than a one-click in-place restore. Restore files and databases deliberately so you can inspect the target path, ownership, application downtime, and schema compatibility.

Test restoration periodically. A successful upload is evidence that a backup ran, not proof that the application can be recovered.

## Delete backup history

Deleting a completed run removes its remote artifacts and Fluxo history. Deleting a failed run removes the failure record. This cannot be undone.

::: warning Separate failure domains
Keep backups in an account or bucket that cannot be destroyed with the server's root credentials alone. Protect the storage account with MFA and independent recovery access.
:::

