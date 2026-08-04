---
title: Settings and Access
description: Configure Fluxo administrator settings, source control accounts, and SSH keys.
---

# Settings and access

The Settings area controls administrator identity, source-control integrations, server SSH keys, and network policy.

## General settings

Use **Settings > General** to maintain server-level defaults and change the Fluxo administrator password. Use a unique password stored in a password manager.

Changing the administrator password updates the signing secret used for sessions, invalidating previously issued authentication tokens. Sign in again on trusted devices.

Account recovery is intentionally available only from a root shell. See [First login and recovery](../getting-started/first-login.md).

## Source control

**Settings > Source Control** connects one or more GitHub accounts using classic personal access tokens. Fluxo uses them for repository discovery and lifecycle operations such as deploy keys and webhooks.

Label accounts by owner or purpose so operators can select the correct identity during site creation. Rotate credentials when an owner leaves or a token may have been exposed.

## SSH keys

**Settings > SSH Keys** manages public keys authorized for the `fluxo` system user. Add a descriptive name and a valid public key.

Fluxo's SSH hardening disables password authentication. Before removing a key, confirm another key or provider-console recovery path works.

Site deploy keys used for repository access are separate from human login keys and are managed through the source-control workflow.

## Dashboard access

Fluxo listens on port `9595` and uses HTTPS with a self-signed certificate by default. Restrict the port using the provider firewall, a VPN, or a trusted reverse proxy. If proxying the dashboard, preserve secure forwarding headers and WebSocket support.

Do not expose the dashboard through a CDN configuration that weakens origin authentication.

