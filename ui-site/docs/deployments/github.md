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

During site creation, select the account, optionally filter by organization, then choose a repository and branch. Use the refresh button beside **Source Control Account** to reload connected accounts and the selected account’s Git options.

After creation, repository and branch settings are available in **Site > Settings > General**. Use the refresh button in the **Git** section to reload options for the site’s source control account without resetting unsaved form edits.

Fluxo uses an SSH-form repository URL during deployment. The site deploy key grants server access without placing the personal access token in Git commands.

## Repository changes

Changing a repository or branch can affect the active checkout:

- Standard deployments can synchronize the in-place repository immediately.
- Zero-downtime sites record the change and apply it on the next release deployment.

Read the confirmation dialog before saving a repository change on a production site.

## Push to deploy

When enabled, Fluxo maintains one GitHub webhook for the repository pointing at:

```text
https://YOUR_FLUXO_HOST/api/v1/github/webhook
```

GitHub signs the request with the shared webhook secret. Fluxo validates the signature, matches the repository and branch, creates a deployment with the `github_webhook` trigger source, and places it in the site's queue.

Fluxo remembers the callback it manages instead of rebuilding it from whichever dashboard hostname is currently open. Saving deployment commands or other settings therefore does not register another webhook. Disabling Push to Deploy removes the tracked callback unless another site on the same Fluxo server still uses that repository.

During an upgrade, Fluxo checks enabled repositories in the background. It treats a callback as locally owned only when its ID or URL was already stored, it exactly matches the configured panel endpoint or the server's listening address and port, or Fluxo has independently matched a signed delivery to that hook through GitHub's API. This proof prevents one Fluxo server from changing another server's callback when both deploy the same repository or address.

When an older installation has both a verified working public-IP callback and a callback for the configured panel domain, Fluxo retains the public-IP callback and removes the panel-domain duplicate. Verified legacy callbacks using HTTP or non-public IP addresses are removed because they are not valid public HTTPS endpoints for this server. An unverified callback is left untouched even if it looks obsolete; after its first correctly signed delivery, Fluxo can reconcile it automatically.

The Fluxo API must be publicly reachable by GitHub over HTTPS. A direct public-IP callback may use Fluxo's self-signed dashboard certificate because Fluxo registers that callback with GitHub certificate verification disabled; a panel-domain callback uses its configured certificate. Private IP addresses such as `10.x.x.x`, `172.16.x.x`–`172.31.x.x`, and `192.168.x.x` cannot receive GitHub's public delivery.

Fluxo also ignores an immediate repeated delivery for the same site and commit. This is a safety net during upgrades or provider retries; normal later pushes still create deployments.

## Disconnect an account

Disconnecting removes Fluxo's account record and prevents future repository listing and webhook management through that account. Existing sites can lose automated webhook capability. Plan replacement credentials before disconnecting an account used by production sites.
