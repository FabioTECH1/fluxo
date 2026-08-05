---
title: Delete a Site
description: Understand Fluxo's resumable site deletion and optional database cleanup.
---

# Delete a site

Site deletion removes application files and Fluxo-managed resources. It is deliberately separate from database-user deletion.

## Before deleting

1. Create and verify a final backup.
2. Record any DNS, certificate, environment, and external-service configuration you may need later.
3. Confirm whether attached databases should survive.
4. Stop sending production traffic to the site when appropriate.

## Delete from the dashboard

Open **Site > Settings > General**, find the Danger section, and select **Delete site**. Type the primary domain exactly to confirm.

When attached databases exist, Fluxo offers a checkbox to drop them. The choice is explicit:

- Unchecked: the site is removed and attached databases remain.
- Checked: only the listed attached databases are dropped as part of deletion.
- Database users and PostgreSQL roles are preserved in both cases.

The selected database intent is locked once deletion starts so a retry cannot silently change what will be destroyed.

## Resumable cleanup

Deletion proceeds through recorded stages. Fluxo removes or updates processes, scheduled jobs, Nginx configuration, certificates, files, selected databases, source-control resources, and control records in a recoverable order.

If an external service is temporarily unavailable, the site remains visible with its current deletion stage and error. Select **Retry deletion** after correcting the problem; Fluxo resumes the stored intent instead of beginning an unrelated deletion.

::: danger No recycle bin
Application files and selected databases cannot be restored from Fluxo after deletion. Restore them from an independent backup.
:::
