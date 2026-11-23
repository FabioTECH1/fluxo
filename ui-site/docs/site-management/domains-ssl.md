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

## Network: password protection and redirects

Open **Site > Settings > Network** for whole-site shared credentials and exact-path redirects. These controls require **Fluxo managed** Nginx mode. Settings changed through Domains, Network or SSL remain managed; saving manual vhost text is what creates custom mode.

Enable **Protect this site** and enter a username and a new password. The existing password field can generate or reveal your current draft; saved passwords cannot be retrieved. Fluxo stores a bcrypt hash in protected credential storage. Use HTTPS for Basic authentication. **Change credentials** replaces the shared credentials; disabling asks for confirmation. Protection covers application, static, PHP and proxy content. ACME challenges remain public so HTTP-01 renewal can work. WWW/HTTPS redirects and configured path redirect responses may occur before authentication; their destinations are not protected unless separately configured.

Redirects match an **exact original path**, ignoring its query string. `/old-pricing` does not match `/old-pricing/`, nested paths or encoded alternatives. Use canonical ASCII paths (letters, numbers, `/`, `_`, `.`, `~`, `+`, `-`), without dot segments, encoding, query strings or regex. Destinations can be local absolute paths or HTTP/HTTPS URLs without credentials. ACME challenge sources, duplicate sources and direct local self-loops are rejected. There are at most 100 redirects per site. **301** may be cached by browsers; use **302** for temporary changes. These rules are separate from domain WWW behavior.

Network activation validates the complete Nginx configuration before reload. Failed activation restores the previous configuration; failure to confirm restoration leaves a visible interrupted state. **Restore last confirmed settings** in Network explicitly completes that recovery. Other site mutations and deployment execution remain blocked while it is pending. Wait for queued or running deployments before changing network settings. Do not delete operation records or credential files to bypass recovery: retained configurations may reference old hash files.

## Wildcard subdomains

In **Domains**, use the domain menu's **Configure domain**, or configure a domain while adding it. Keep the WWW choice, then enable **Allow wildcard subdomains**. Enter the ordinary parent `example.com`; entering `*.example.com` selects wildcard routing and normalizes the stored parent. Existing and new ordinary domains default to wildcard off.

Create wildcard DNS yourself. Fluxo serves unmatched subdomains through that site. An explicitly configured exact host such as `shop.example.com` on another site keeps priority. Removing that exact host makes requests fall back to the wildcard if DNS still reaches this server. Duplicate or overlapping wildcard scopes and conflicting custom hostname patterns are refused. A wildcard can include the panel hostname only while the panel has its enabled, Fluxo-managed exact Nginx route; the exact panel route and its certificate keep priority on IPv4 and IPv6. Restore a missing or unmanaged panel route before enabling such a wildcard. Disable a covering wildcard before moving or removing the panel hostname, so the old hostname cannot silently fall through to a site. Connecting an initial panel hostname within an existing wildcard is allowed, with the usual exact ownership, certificate and activation checks. Root-domain routing remains separate. IPv4 and IPv6 listeners follow the same rules.

HTTP wildcard matching can include deeper names. TLS wildcard certificates cover only **one label**: `*.example.com` covers `shop.example.com`, not `example.com` or `deep.shop.example.com`. Import a combined apex-plus-wildcard certificate in **Domains > SSL** when securing both. Include `www.example.com` coverage when WWW behavior requires it. Compatible custom-certificate cloning uses the same complete target coverage check. The domain dialog does not install or replace certificates. Root certificate rotation preserves inherited alias wildcard coverage; replacing active primary wildcard SSL requires a compatible certificate or explicit wildcard disablement first.

Wildcard SSL status is separate from root-domain SSL. Without a valid assigned wildcard certificate, matching wildcard TLS handshakes are rejected; the existing root-domain certificate stays active. With coverage, only the one-label TLS scope is served; deeper wildcard TLS handshakes are rejected. Exact-host configurations keep priority. This requires Nginx support for `ssl_reject_handshake`; unsupported configuration fails validation and rolls back rather than using an unrelated certificate.

Built-in HTTP-01 issuance remains available for ordinary hostnames; it cannot issue the wildcard scope. Import externally issued PEM material or clone a compatible custom certificate instead. Imported/cloned certificates do **not** gain automatic renewal. Cloudflare Origin CA certificates require Cloudflare proxy trust and are not browser-trusted for direct connections.

These controls are part of the unreleased base candidate. Native routing, authentication and TLS acceptance is a separate qualification gate; the public demo simulates them.

## Let's Encrypt

Choose **Let's Encrypt** to request a free certificate through Certbot. Port 80 must be reachable and every requested hostname must resolve to this server.

Fluxo shows the exact hostnames required before requesting the certificate. A root domain using **Redirect from www** or **Redirect to www** needs DNS records for both `example.com` and `www.example.com`. A subdomain using the default **No redirect** needs only its exact hostname, such as `app.example.com`. If you later enable a WWW redirect for that subdomain, create `www.app.example.com` in DNS and ensure the certificate covers both hostnames before issuing or activating it.

Let's Encrypt certificates are stored in Certbot-managed lineages and renewed by the system's Certbot automation. Fluxo records the lineage and activates it through Nginx.

### Existing wildcard and manual certificates

Fluxo's built-in issuance uses webroot validation for specific hostnames. It does not issue wildcard certificates through DNS validation. An existing Let’s Encrypt wildcard is still a Let’s Encrypt certificate, but it needs DNS validation; installing or cloning an existing certificate does not configure that automation.

Current runtime coordination can handle a protected manual-renewal lineage with no executable authentication or cleanup hooks, while preserving its configuration. PHP/Node review shows a separate notice: the certificate cannot renew automatically as configured. Runtime-update success is not evidence of certificate renewal. Arrange manual DNS renewal or separately configure a supported automated DNS solution; do not change `manual` to `webroot` just to clear a maintenance message.

Executable manual hooks, additional DNS plugins and unfinished Certbot rollback checkpoints remain subject to compatibility review. Fluxo preserves them; it does not delete certificate files, disable renewal or add DNS credentials during an update. Older retained helpers require the new optional profile capability through the normal archived installer refresh and genuine cron acknowledgement.

Common issuance failures are incorrect DNS, a proxied record that does not reach this server, blocked port 80, an invalid IPv6 record, or certificate-authority rate limits.

## Custom certificate

Choose **Custom certificate** to install certificate and private-key PEM content you already own. Fluxo validates that:

- The certificate parses and is currently usable.
- The private key matches the certificate.
- The certificate covers every hostname required by the selected WWW behavior and any enabled wildcard scope.

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

Older `/sites/{id}/network` bookmarks redirect to `/sites/{id}/settings/network`, preserving query and fragment. Domains and SSL remain in their existing sections.
