---
title: GitHub Integration
description: Connect GitHub accounts, deploy private repositories, and enable push-to-deploy webhooks.
---

# GitHub integration

Fluxo can connect multiple GitHub accounts. A connected account lets the dashboard list repositories and branches, add SSH deploy keys, and register signed push webhooks.

## Connect an account

1. Open **Settings > Source Control**.
2. Select **Add Account**.
3. Optionally enter a recognizable label.
4. Enter a classic GitHub personal access token with `repo` and `admin:public_key` scopes.
5. Select **Connect**.

Fluxo verifies the token and defaults the label to the authenticated GitHub username when no label is provided.

::: warning Token scope
Use an account and token dedicated to the minimum repositories the server needs. A classic token with `repo` can access every private repository available to that token owner.
:::

## Select a repository

During site creation or in **Site > Settings > General**, select the account, optionally filter by organization, then choose a repository and branch.

Fluxo uses an SSH-form repository URL during deployment. The site deploy key grants server access without placing the personal access token in Git commands.

## Repository changes

Changing a repository or branch can affect the active checkout:

- Standard deployments can synchronize the in-place repository immediately.
- Zero-downtime sites record the change and apply it on the next release deployment.

Read the confirmation dialog before saving a repository change on a production site.

## Push to deploy

When enabled, Fluxo registers a GitHub webhook pointing at:

```text
https://YOUR_FLUXO_HOST/api/v1/github/webhook
```

GitHub signs the request with the shared webhook secret. Fluxo validates the signature, matches the repository and branch, creates a deployment with the `github_webhook` trigger source, and places it in the site's queue.

The Fluxo API must be reachable by GitHub over trusted HTTPS for webhooks to work. A dashboard accessed only by private IP cannot receive GitHub's public webhook delivery without a secure ingress path.

## Disconnect an account

Disconnecting removes Fluxo's account record and prevents future repository listing and webhook management through that account. Existing sites can lose automated webhook capability. Plan replacement credentials before disconnecting an account used by production sites.

