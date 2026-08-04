---
title: First Login
description: Claim the Fluxo administrator account and recover account access.
---

# First login

Fluxo creates a one-time bootstrap token during installation. Use that token with a username of your choice to claim the administrator account.

## Retrieve the bootstrap credentials

The installer prints the credentials at completion. Before acknowledging the credentials screen in the dashboard, root can also read:

```bash
sudo cat /var/lib/fluxo/.fluxo_credentials
```

Look for the `Fluxo bootstrap token` entry. The credentials file is owned by root and uses mode `0600`.

## Claim the account

1. Open `https://YOUR_SERVER_IP:9595`.
2. Enter the username you want to keep.
3. Enter the bootstrap token as the password.
4. Sign in and download the complete one-time credentials export when prompted.
5. Store that export in a secure password manager and acknowledge it in Fluxo.

After the account is claimed, the username is permanent unless it is changed outside the supported dashboard workflow. Change the password from **Settings > General**.

## Credential export

The one-time export includes credentials generated while provisioning Fluxo and optional services. Fluxo stores sensitive values encrypted in its database; the export is the intended plaintext handoff.

Once you acknowledge the export, the bootstrap credentials endpoint no longer exposes it and obsolete bootstrap material is removed from the root-only file.

## Forgotten username

Run the read-only recovery command as root:

```bash
sudo fluxo --show-admin-username
```

This reports the configured administrator username without changing the password or invalidating existing sessions. Before the initial account has been claimed, it reports that a username must be chosen during first login.

## Forgotten password

Generate a replacement token:

```bash
sudo fluxo --reset-token
```

The command reports the administrator username, replaces the stored password hash, invalidates previously issued JWT sessions, and writes the current token to the root-only credentials file. Use the generated token to sign in, then set a new password in Settings.

::: danger Protect recovery access
Anyone with root access can reset the Fluxo administrator token and can already control the hosted server. Restrict root SSH access and protect the provider console.
:::

