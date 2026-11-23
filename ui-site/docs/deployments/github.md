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

Read the confirmation dialog before saving a repository change on a production site. A repository change prepares a new deploy key for the new repository. After saving that association, Fluxo attempts to remove the previous repository's key registration. Review any cleanup warning in the site activity log. Replacing an account token alone keeps the site's existing private key.

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

## Token expiry and connection recovery

**Settings > Source Control** shows each account's token expiry date, remaining days, linked sites and last deployment-access result. Tokens within 14 days of expiry show **Expires soon**. Once the date has passed, the status says **Expired**. When GitHub does not supply a date, Fluxo shows **Unavailable**; this does not mean the token lasts forever. A token can also be revoked before its expiry date.

Select **Check access** to validate the saved token and test repository access as the `fluxo` system user using each site's SSH deploy key. The compact report separates token status from each site’s SSH result. It identifies an unavailable private key or a timed-out check when known. A generic SSH failure leaves registration, repository permissions and connectivity unconfirmed. The check does not register keys or start deployments. SSH checks run independently of token validation, so a working deploy key can still be verified when the API token has expired. Previously saved access results are cleared when a site's repository or account membership changes.

Use **Retry check** in the report to repeat the observational check. Healthy accounts show **Update token** on their account card; their access report has no repair prompt. **Update token & repair** appears only for a reported key-registration failure. SSH failures also offer **Update token** without assuming their cause; timeouts and unavailable private keys do not prompt token replacement. Invalid or expired credentials show **Reconnect**. The new token must belong to the same numeric GitHub user ID, including after a username change. Fluxo encrypts and saves it, preserves the account's site links, clears cached repository options, and checks deployment access. Fluxo reconciles the existing public deploy key's GitHub registration and its saved registration ID, then verifies SSH access again. A missing registration can be restored without generating a different private key. The token needs permission to manage deploy keys (`repo` for a classic token accessing private repositories, or repository **Administration: write** for a fine-grained token). Missing, unreadable or invalid server private keys need administrator recovery; reconnecting cannot restore them and Fluxo does not silently rotate them.

A successful token replacement does not guarantee every site can deploy. Review any site marked **Needs attention**. If GitHub registered a key but Fluxo could not save its registration, fix the reported storage problem and use **Update token** again to reconcile that same key. **Check access** remains observational and does not repair registration tracking. Expired/revoked personal access tokens require manual replacement; Fluxo cannot renew them automatically.

## Disconnect an account

**Disconnect** is in the account's three-dot menu. Confirming removes the saved credential and unlinks its sites; existing applications keep running. Existing SSH keys and GitHub webhooks are not revoked by Disconnect. Deployments using previously granted access may continue; disable **Push to Deploy** first if you also want to pause that automation. Account-based repository listing and webhook management become unavailable, and Fluxo does not borrow another connected account's token for the disconnected site.

Use **Update token** for credential rotation. If you do disconnect, Fluxo records the verified numeric GitHub user ID and restores those exact site associations when that same account is connected again. Sites disconnected before this recovery metadata existed are not automatically assigned to a new connection. After reconnecting, return to the site's **Settings > General** page; Git options use the restored account connection. A disconnected site shows no options from other accounts.

The site's saved repository and branch remain visible when GitHub options cannot be loaded. A missing account link is shown separately from a repository-list or branch-list error; an empty remote list does not erase the saved values. **Refresh Git options** reloads the current site account association and then its options, without resetting unsaved form fields. Repository owners can be organizations: one GitHub login can access its own repositories and company repositories, subject to that account and token's permissions.

For a legacy connection, **Check access** can verify the account ID with its saved credential. If that credential expired before verification, ordinary replacement and disconnect preserve the site links and refuse to guess ownership from a username.

### Recover an unverified legacy connection

Available from v0.4.50; v0.4.51 adds a compact stored/new account review.

Enter the new token and select **Continue**. If GitHub rejects the saved legacy
token, Fluxo shows the **Stored connection** and the account **verified** by the
new token, including its numeric GitHub ID. Review these accounts and the sites
that will stay linked, enter your Fluxo password, then select **Approve & reconnect**.
Changing the token requires a fresh review. This authorizes that
verified GitHub identity to manage the connection from now on; it does not prove
which account owned the expired token historically. Your local account ID, site
links, repositories and private keys remain intact. Fluxo records the recovery in
Activity and checks deployment access using the existing key-repair workflow.

The recovery requires a current signed-in session and password, an unchanged
reviewed repository list, and a new GitHub identity that is not already connected.
It is available only while the legacy connection has no verified numeric ID and
GitHub returns `401` for the saved token. Temporary network, rate-limit and
permission failures require a retry rather than recovery. Once a numeric ID is
known, ordinary replacement remains restricted to that identity. In the demo,
use `demo-password` rather than a real password for a simulated recovery.

Username-only records from previous disconnects still cannot establish ownership;
this recovery does not claim old orphaned sites or unlink an account.

The simulated access report includes an SSH failure, an unavailable private key and a timed-out check. Retrying observes them again; token replacement demonstrates registration repair while preserving the unavailable-key warning. No real keys or credentials are changed.

In the simulated demo, enter `demo-secondary` to connect a second identity or `demo-renamed` to exercise a username change on the first identity. Use dummy tokens in the demo.
