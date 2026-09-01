---
title: Domains and SSL
description: Manage primary domains, aliases, Let's Encrypt, custom certificates, and safe certificate cloning.
---

# Domains and SSL

Every site has one primary domain and can have additional aliases. Certificates can be assigned to the primary hostname or to an individual domain.

This page covers application hostnames. To connect a hostname for the Fluxo dashboard itself, use **Settings > General > Panel Domain** and see [Settings and access](../operations/settings.md).

## Add a domain

1. Create the DNS record with your DNS provider.
2. Open **Site > Domains > Domains**.
3. Enter the hostname and select **Add domain**.
4. Configure how Fluxo should handle the corresponding `www.` hostname.
5. Confirm every hostname used by that configuration resolves to the server.
6. Issue or assign a certificate after HTTP is working.

New registrable root domains default to **Redirect from www**, which permanently redirects `www.example.com` to `example.com`. Subdomains such as `app.example.com`, explicit `www.` hostnames, private suffixes, and hostnames with unknown suffixes default to **No redirect** so Fluxo does not unexpectedly require a nested hostname such as `www.app.example.com`. You can instead choose **Redirect to www** or **No redirect** for a root domain during site creation. Use the domain action menu to change any domain's behavior later. Fluxo preserves the request path and query string when redirecting.

Existing domains created before this feature remain on **No redirect** until you explicitly change them. A generated `www.` hostname is reserved for that domain, so it cannot also be attached as a separate alias.

If a domain already has an active certificate, Fluxo accepts a WWW behavior change only when that certificate covers every hostname the new behavior requires. Otherwise, deactivate SSL for the domain, change the WWW behavior, and then issue or assign a certificate covering both names. This prevents a configuration change from silently replacing a working origin certificate with the fallback certificate and causing Cloudflare Full (strict) error 526.

You can promote an alias to become the primary domain. Fluxo updates its domain records and managed Nginx configuration; review application-level canonical URL settings separately.

## Let's Encrypt

Choose **Let's Encrypt** to request a free certificate through Certbot. Port 80 must be reachable and every requested hostname must resolve to this server.

Fluxo shows the exact hostnames required before requesting the certificate. A root domain using **Redirect from www** or **Redirect to www** needs DNS records for both `example.com` and `www.example.com`. A subdomain using the default **No redirect** needs only its exact hostname, such as `app.example.com`. If you later enable a WWW redirect for that subdomain, create `www.app.example.com` in DNS and ensure the certificate covers both hostnames before issuing or activating it.

Let's Encrypt certificates are stored in Certbot-managed lineages and renewed by the system's Certbot automation. Fluxo records the lineage and activates it through Nginx.

Common issuance failures are incorrect DNS, a proxied record that does not reach this server, blocked port 80, an invalid IPv6 record, or certificate-authority rate limits.

## Custom certificate

Choose **Custom certificate** to install certificate and private-key PEM content you already own. Fluxo validates that:

- The certificate parses and is currently usable.
- The private key matches the certificate.
- The certificate covers every hostname required by the selected WWW behavior.

Include the appropriate intermediate chain in the certificate content when required by the issuer. Fluxo stores a managed copy with restricted private-key permissions.

This option is suitable for a Cloudflare Origin CA certificate or another externally issued certificate. A Cloudflare Origin certificate is trusted between Cloudflare and your origin, not by browsers connecting directly to the server.

HTTPS negotiation happens before an HTTP redirect. When a domain redirects from or to `www.`, its certificate must therefore cover both the configured hostname and its `www.` variant. With Cloudflare Full (strict), missing origin coverage can surface as error 526 before Nginx can return the redirect.

## Clone a certificate

Choose **Clone certificate** when a compatible custom certificate is already installed on another Fluxo site. This is useful for wildcard certificates shared across subdomains.

Fluxo lists only source certificates that:

- Belong to a different site.
- Were installed as custom certificates.
- Have readable certificate and key files.
- Have a matching key pair.
- Cryptographically cover every hostname required by the selected domain configuration, including valid wildcard coverage.

Cloning creates an independent target-owned copy. If the target has no active certificate, Fluxo activates the clone; otherwise it installs the clone without unexpectedly replacing the current certificate.

Let's Encrypt certificates are not offered as clone sources because their lifecycle remains owned by Certbot.

## Existing certificate versus clone

Use **Custom certificate** when importing PEM material from outside Fluxo. Use **Clone certificate** when Fluxo already manages the source custom certificate and you want to avoid handling its private key again.

## Activate, deactivate, and delete

Activation regenerates and validates Nginx configuration before reloading. A certificate can serve multiple matching aliases through hostname bindings.

Deactivation removes the trusted certificate assignment unless another compatible certificate remains. Fluxo keeps a fallback 443 listener for safe unknown-host handling, so direct HTTPS clients can see a certificate mismatch until a matching certificate is assigned; use HTTP while reconfiguring, or complete the replacement promptly. Deleting a managed custom or cloned certificate removes its unreferenced private copy. Fluxo avoids deleting shared or Certbot-owned material while another record still depends on it.

::: warning CDN SSL mode
When using Cloudflare, prefer Full (strict) mode with a valid origin certificate. Flexible mode can create redirect loops and does not encrypt traffic from Cloudflare to Fluxo.
:::
