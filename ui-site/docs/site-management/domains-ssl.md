---
title: Domains and SSL
description: Configure site domains, password protection, redirects, wildcard routing and certificates.
---

# Domains and SSL

Every site has a primary domain and can have aliases, each with a certificate
assignment. For the Fluxo dashboard hostname, use **Settings > General > Panel
Domain** instead; see [Settings and access](../operations/settings).

## Add a domain

1. Point its DNS records to the server.
2. Open **Site > Domains > Domains**, enter the hostname and select **Add domain**.
3. Choose the WWW behavior and confirm every required hostname resolves here.
4. Issue or assign a certificate after HTTP works.

Registrable root domains default to **Redirect from www**. Subdomains, explicit
`www.` names and unknown/private suffixes default to **No redirect**. Older
domains keep their saved choice. Change it from **Configure domain** in the
three-dot menu. Redirects preserve the request path and query string.

WWW redirects require both the configured hostname and its `www.` variant in
DNS and on the certificate. HTTPS negotiation happens before redirects; missing
coverage can cause Cloudflare Full (strict) error 526. If the active certificate
cannot cover a new WWW choice, deactivate it, change the choice, then assign a
compatible certificate. A generated WWW hostname cannot also be a separate alias.

An alias can be promoted to primary. Review the application's canonical URL
settings separately.

## Network: password protection and redirects

Open **Site > Settings > Network**. Both features require **Fluxo managed** Nginx
mode. Dashboard domain, SSL and Network changes keep that mode; saving manual
vhost text creates custom mode. See [Nginx vhost editor](./vhost).

### Password protection

Enable **Protect this site** and enter a username and new password. The password
field can generate or reveal the draft; saved passwords cannot be retrieved.
Fluxo stores a protected bcrypt hash. Use HTTPS for shared Basic authentication.
**Change credentials** replaces them; disabling protection asks for confirmation.

Protection covers application, static, PHP and proxy content. ACME challenge
paths remain public for renewal. WWW/HTTPS and path redirects can occur before
authentication; a redirect destination needs its own protection.

### Exact-path redirects

Add a source path, destination and **301 Permanent** or **302 Temporary** status.
Browsers may cache 301 responses; use 302 for temporary changes.

- Sources match the exact original path, ignoring its query string.
  `/old-pricing` does not match `/old-pricing/` or nested paths.
- Use ASCII letters, digits, `/`, `_`, `.`, `~`, `+` and `-`; no encoding,
  dot segments, query strings or regex.
- Destinations can be local absolute paths or HTTP/HTTPS URLs without credentials.
- ACME challenge sources, duplicate sources and direct local self-loops are
  rejected. A site can have at most 100 rules.

These rules are separate from domain WWW redirects.

### Failed changes and custom vhosts

Fluxo tests Nginx before reload and restores the previous configuration after a
failed activation. If restoration cannot be confirmed, use **Restore last
confirmed settings** in Network. Other site changes and deployments remain
blocked until recovery completes. Wait for queued/running deployments before
editing Network settings; keep operation records and credential files.

Before saving a custom vhost, explicitly disable protection, remove redirects
and disable wildcards. Custom mode makes managed controls unavailable. Restoring
**Fluxo Default** restores the controls without reactivating explicitly disabled
features. Custom text may contain its own authentication or routing rules.

## Wildcard subdomains

In **Configure domain** (or while adding one), enable **Allow wildcard
subdomains**. Use the parent `example.com`; entering `*.example.com` also turns
on the toggle. Ordinary domains default to wildcard off.

Configure DNS at your provider; Fluxo does not create records:

| Name | Points to |
|---|---|
| `example.com` | This server's IPv4 address (A record). |
| `*.example.com` | This server's IPv4 address (wildcard A record). |

Use AAAA records only when the server is reachable over the specified IPv6
address. Without wildcard DNS, an unmatched subdomain can return `NXDOMAIN`
before reaching Fluxo.

### Routing priority

- Exact-host sites such as `shop.example.com` keep priority over a wildcard.
  Removing that exact host exposes the wildcard fallback if DNS still points here.
- Duplicate/overlapping wildcard owners and conflicting custom patterns are refused.
- A covered panel hostname keeps priority only with its enabled, Fluxo-managed
  exact route. Restore that route before enabling the wildcard. Disable a
  covering wildcard before moving/removing the panel hostname.
- Root routing stays separate; IPv4 and IPv6 use the same ownership rules.

### Wildcard HTTPS

Routing and certificates are separate settings. Configure certificates in
**Domains > SSL**; enabling wildcard routing does not replace root SSL.

| Certificate name | Covers | Does not cover |
|---|---|---|
| `*.example.com` | `shop.example.com` | `example.com`, `deep.shop.example.com` |
| `example.com` plus `*.example.com` | Apex and one-label subdomains | Deeper subdomains |

Include WWW coverage when required. Import or clone a compatible custom
certificate covering the entire target. Root certificate replacement preserves
inherited alias coverage and refuses to drop an active primary wildcard scope.

**Root SSL active** does not mean wildcard SSL is ready. Without coverage,
matching wildcard TLS handshakes are rejected while root HTTPS stays active.
HTTP wildcard matching can reach deeper names, but TLS serves only covered
one-label names; deeper handshakes are rejected. Nginx must support
`ssl_reject_handshake`; an unsupported configuration fails validation and rolls back.

## Let's Encrypt and renewal

Select **Let's Encrypt** to issue through Certbot's webroot HTTP-01 validation.
Port 80 and every displayed hostname must reach this server. A WWW redirect needs
both names; a normal `app.example.com` domain needs only that exact name unless
you enable its WWW redirect.

Fluxo's scheduled root job runs `certbot renew --quiet` every **12 hours** for
Certbot-managed lineages. Due certificates renew when validation succeeds, and
Nginx reloads after renewal. Incorrect DNS/IPv6, blocked port 80, challenge
routing or CA rate limits can prevent issuance or renewal.

Built-in HTTP-01 issuance cannot issue wildcards. An externally issued Let's
Encrypt wildcard still needs DNS validation. Importing or cloning it does not
configure automatic renewal. A `manual` lineage without authentication hooks
needs manual renewal; PHP/Node maintenance shows a separate notice and keeps
that SSL configuration unchanged. Do not change `manual` to `webroot` to clear it.

Custom executable hooks, DNS plugins and unfinished Certbot rollback operations
can require compatibility review. Read the reported error; do not delete
lineages, hooks or renewal files to make maintenance proceed.

## Import or clone a certificate

**Custom certificate** imports a PEM certificate/chain and matching private key.
It must be currently valid and cover all target apex, WWW and wildcard names.
Include intermediate certificates required by the issuer. Private keys are
stored with restricted permissions.

**Clone certificate** copies a compatible custom certificate from another Fluxo
site. Fluxo lists only readable, matching key pairs with full target coverage.
The copy is independent. It activates automatically only if the target has no
active certificate; otherwise the current certificate remains until activation.
Certbot-managed certificates are not clone sources.

Imported/cloned certificates do **not** renew automatically. Cloudflare Origin
CA certificates are trusted by Cloudflare's proxy, not browsers connecting
directly to the origin.

## Activate, deactivate or delete

Activation validates Nginx before reload. Deactivation removes the trusted
assignment unless another compatible certificate remains; direct HTTPS may show
a certificate mismatch until a replacement is assigned. Deleting custom/cloned
material removes only unreferenced copies, preserving shared and Certbot-owned files.

With Cloudflare, use **Full (strict)** and valid origin coverage. Flexible mode
can cause redirect loops and leaves the Cloudflare-to-origin connection unencrypted.
