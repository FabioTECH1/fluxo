---
title: Requirements
description: Minimum RAM, storage, operating system, network, and access requirements for Fluxo.
---

# Requirements

Install Fluxo on a fresh VPS or dedicated server that you control.

## Server requirements

| Requirement | Supported value |
|---|---|
| Operating system | Ubuntu 22.04 or newer |
| Architecture | `amd64` or `arm64` |
| Memory | 1 GB RAM minimum; 2 GB or more recommended |
| Storage | 20 GB minimum; 40 GB or more recommended |
| Init system | systemd |
| Package manager | APT |
| Access | Root SSH access or a sudo-capable account |
| Network | Public outbound HTTPS; inbound HTTP, HTTPS, the server's effective SSH port, and dashboard access as required |

A clean Ubuntu LTS server is the most predictable starting point. Do not install Fluxo over another hosting control panel that already owns Nginx, PHP-FPM, firewall rules, or database configuration.

The 1 GB minimum is intended for Fluxo, Nginx/PHP-FPM, one small low-traffic site, and one local database engine such as MariaDB/MySQL or PostgreSQL. Configure at least 1 GB of swap on a 1 GB server and avoid installing both database engines or other optional services unless the server has more memory.

Use 2 GB or more for a more comfortable small production server, including a low-traffic WordPress site with its database. Use at least 4 GB for Node.js builds, larger Python dependency builds, Redis, multiple databases, several active sites, or applications with larger worker pools. Actual application traffic and build requirements may require more.

The storage figures include the operating system and Fluxo-managed software. Site releases, database data, logs, local archives, uploaded media, Node.js dependencies, and Python virtual environments consume additional space. Production servers should be sized for their application data and should keep off-server backups in S3 or R2.

## DNS

You can install Fluxo before pointing a domain at the server. Before issuing a Let's Encrypt certificate or serving a production site, create the required `A` and, when appropriate, `AAAA` records with your DNS provider.

The primary domain, every alias included in a certificate, and any panel domain using Let's Encrypt must resolve to the server and be reachable over ports 80 and 443.

## Cloud firewall

When UFW is inactive, the installer configures it inside the server. If UFW is already active, Fluxo preserves the existing policy instead of adding broader rules. Many providers also have a network firewall or security group. Allow:

| Port | Purpose |
|---|---|
| Effective SSH port | SSH administration; Fluxo detects this with `sshd -T` |
| `80/tcp` | HTTP sites and Let's Encrypt validation |
| `443/tcp` | HTTPS sites and an optional panel domain |
| `9595/tcp` | Fluxo dashboard |

Restrict port `9595` to trusted source addresses at the provider firewall when possible, or pass `--management-cidr` when Fluxo creates a new UFW policy. Do not expose database ports publicly unless you have a deliberate, separately secured requirement.

## Existing software

The installer can detect existing Node.js, MariaDB/MySQL, and PostgreSQL installations. Existing databases are not erased. Even so, take a server snapshot before adopting an existing machine because Fluxo will install packages and manage service configuration.

## Browser

Use a current browser with JavaScript, WebSocket, and Web Crypto support. The dashboard initially uses a self-signed certificate. After first login, **Settings > General > Panel Domain** can connect a hostname using Let's Encrypt, an uploaded certificate, or a compatible cloned custom certificate.
