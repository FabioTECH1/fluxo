---
title: First Login
description: Claim the Fluxo administrator account and recover account access.
---

# First login

Fluxo creates a one-time bootstrap token during installation. Use that token with a username of your choice to claim the administrator account.

## Bootstrap credentials

The installer displays the bootstrap token once after the first provisioning completes. Store it securely. Fluxo also keeps a recovery copy in `/var/lib/fluxo/.fluxo_credentials`, which is owned by root and uses mode `0600`.

## Claim the account

1. Open `https://YOUR_SERVER_IP:9595`.
2. Enter the username you want to keep.
3. Enter the bootstrap token as the password.
4. Sign in and download the complete one-time credentials export when prompted.
5. Store that export in a secure password manager and acknowledge it in Fluxo.

After the account is claimed, the username is permanent unless it is changed outside the supported dashboard workflow. Change the password from **Settings > General**.

After signing in, the **Panel Domain** card on the same page can replace the initial browser-warning URL with a trusted HTTPS hostname. Let's Encrypt requires the administrator email; uploaded or compatible cloned certificates do not. Keep the direct server IP and dashboard port recorded for recovery.

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

The command displays the administrator username and newly generated token, replaces the stored password hash, invalidates previously issued JWT sessions, and updates the root-only recovery copy. A root-only pending marker makes the file-and-database handoff recoverable if the process or machine stops mid-reset; Fluxo completes it on the next start before accepting logins. Use the generated token to sign in, then set a new password in Settings.

::: danger Protect recovery access
Anyone with root access can reset the Fluxo administrator token and can already control the hosted server. Restrict root SSH access and protect the provider console.
:::
