---
title: Domains and SSL
description: Manage primary domains, aliases, Let's Encrypt, custom certificates, and safe certificate cloning.
---

# Domains and SSL

Every site has one primary domain and can have additional aliases. Certificates can be assigned to the primary hostname or to an individual domain.

## Add a domain

1. Create the DNS record with your DNS provider.
2. Open **Site > Domains > Domains**.
3. Select **Add domain** and enter the hostname.
4. Confirm the hostname resolves to the server.
5. Issue or assign a certificate after HTTP is working.

You can promote an alias to become the primary domain. Fluxo updates its domain records and managed Nginx configuration; review application-level canonical URL settings separately.

## Let's Encrypt

Choose **Let's Encrypt** to request a free certificate through Certbot. Port 80 must be reachable and every requested hostname must resolve to this server.

Let's Encrypt certificates are stored in Certbot-managed lineages and renewed by the system's Certbot automation. Fluxo records the lineage and activates it through Nginx.

Common issuance failures are incorrect DNS, a proxied record that does not reach this server, blocked port 80, an invalid IPv6 record, or certificate-authority rate limits.

## Custom certificate

Choose **Custom certificate** to install certificate and private-key PEM content you already own. Fluxo validates that:

- The certificate parses and is currently usable.
- The private key matches the certificate.
- The certificate covers the selected hostname.

Include the appropriate intermediate chain in the certificate content when required by the issuer. Fluxo stores a managed copy with restricted private-key permissions.

This option is suitable for a Cloudflare Origin CA certificate or another externally issued certificate. A Cloudflare Origin certificate is trusted between Cloudflare and your origin, not by browsers connecting directly to the server.

## Clone a certificate

Choose **Clone certificate** when a compatible custom certificate is already installed on another Fluxo site. This is useful for wildcard certificates shared across subdomains.

Fluxo lists only source certificates that:

- Belong to a different site.
- Were installed as custom certificates.
- Have readable certificate and key files.
- Have a matching key pair.
- Cryptographically cover the exact selected hostname, including valid wildcard coverage.

Cloning creates an independent target-owned copy. If the target has no active certificate, Fluxo activates the clone; otherwise it installs the clone without unexpectedly replacing the current certificate.

Let's Encrypt certificates are not offered as clone sources because their lifecycle remains owned by Certbot.

## Existing certificate versus clone

Use **Custom certificate** when importing PEM material from outside Fluxo. Use **Clone certificate** when Fluxo already manages the source custom certificate and you want to avoid handling its private key again.

## Activate, deactivate, and delete

Activation regenerates and validates Nginx configuration before reloading. A certificate can serve multiple matching aliases through hostname bindings.

Deactivation returns the affected hostname to HTTP unless another compatible certificate remains assigned. Deleting a managed custom or cloned certificate removes its unreferenced private copy. Fluxo avoids deleting shared or Certbot-owned material while another record still depends on it.

::: warning CDN SSL mode
When using Cloudflare, prefer Full (strict) mode with a valid origin certificate. Flexible mode can create redirect loops and does not encrypt traffic from Cloudflare to Fluxo.
:::
