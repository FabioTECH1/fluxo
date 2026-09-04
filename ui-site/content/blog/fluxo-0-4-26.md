---
title: Fluxo 0.4.26: safer WWW redirects and clearer SSL setup
excerpt: Fluxo 0.4.26 makes smarter root-domain defaults, shows the exact DNS names required for TLS, and improves SSH hardening guidance without changing existing domains.
category: Releases
date: 2026-09-01
image: /blog/release-0-4-26.webp
imageAlt: Fluxo logo and version 0.4.26 release artwork
---

Fluxo 0.4.26 is a focused domain and certificate release. It improves how new hostnames choose a default WWW behavior, makes certificate requirements more explicit, and removes ambiguity from the command shown during SSH password-login hardening.

The release does not rewrite existing domain configuration and does not require a manual database migration. Its changes are intended to make the safe choice clearer when creating a new site or alias.

## Smarter defaults for root domains and subdomains

A domain can use one of three WWW behaviors in Fluxo:

- **Redirect from www** sends `www.example.com` to `example.com`.
- **Redirect to www** sends `example.com` to `www.example.com`.
- **No redirect** serves only the exact configured hostname.

Earlier behavior treated new domains too uniformly. That can be convenient for a registrable root such as `example.com`, but it is surprising for a subdomain. Enabling a WWW redirect for `app.example.com` introduces another hostname—`www.app.example.com`—which usually has no DNS record and no reason to exist.

Version 0.4.26 classifies the hostname before selecting a default:

| Hostname type | New default |
|---|---|
| ICANN registrable root such as `example.com` | Redirect from www |
| Multi-label public root such as `example.co.uk` | Redirect from www |
| Subdomain such as `app.example.com` | No redirect |
| Explicit `www.` hostname | No redirect |
| Private or unknown suffix | No redirect |

This preserves the familiar canonical root-domain behavior without inventing nested WWW hostnames for applications, dashboards, staging sites, or private naming schemes.

## Public-suffix-aware classification matters

Determining whether a hostname is a root domain is not as simple as counting dots. `example.co.uk` is a registrable root even though it contains three labels, while `app.example.com` is a subdomain with the same number.

Fluxo's backend uses public-suffix-aware classification, and 0.4.26 aligns the frontend with that result. The site-creation and domain interfaces no longer mistake multi-label roots for subdomains while waiting for classification.

Domain configuration actions remain disabled until classification completes. The reusable domain modal also refuses to save behavior for an empty hostname. Those small safeguards prevent a transient or missing value from becoming a persisted Nginx decision.

## The site-creation form stays focused

For a new root domain, Fluxo defaults to **Redirect from www** but keeps the choice available during creation. For a subdomain, the form avoids presenting an unnecessary WWW decision and selects **No redirect**.

Nothing is locked in permanently. Every primary domain and alias remains editable later from **Site > Domains**.

The practical outcomes are:

- `example.com` naturally canonicalizes `www.example.com` to the shorter hostname.
- `www.example.com` stays exactly as entered unless you deliberately choose another behavior.
- `app.example.com` does not unexpectedly require `www.app.example.com`.
- `example.co.uk` is recognized as a root and receives the root-domain default.

## SSL now explains the DNS requirement directly

HTTPS negotiation happens before an HTTP redirect. A site that redirects `www.example.com` to `example.com` still needs a certificate valid for both names, because the browser establishes TLS with `www.example.com` before Nginx can return the redirect.

Fluxo 0.4.26 shows the exact additional hostname required by the selected behavior during the SSL workflow. The Let's Encrypt request includes only the names implied by the configuration.

Examples:

- `example.com` with **Redirect from www** needs DNS and certificate coverage for `example.com` and `www.example.com`.
- `www.example.com` with **No redirect** needs only `www.example.com`.
- `app.example.com` with the new default needs only `app.example.com`.
- `app.example.com` with an explicitly enabled WWW redirect needs both `app.example.com` and `www.app.example.com`.

This makes certificate failures easier to prevent. Before issuance, create every displayed `A` or `AAAA` record and confirm it resolves to the Fluxo server. Port 80 must be reachable for Let's Encrypt HTTP validation.

## Safer behavior with Cloudflare Full (strict)

The hostname set is especially important when Cloudflare proxies the site in **Full (strict)** mode. Cloudflare verifies the certificate presented by the origin for the requested hostname. If the WWW behavior adds a name that the active origin certificate does not cover, the request can fail with error 526 before Nginx has a chance to redirect it.

Fluxo already blocks a domain behavior change when the active certificate is incompatible with the proposed hostname set. In 0.4.26, the certificate-cloning flow adds a direct way to remove the incompatible WWW redirect and retry using the exact hostname.

That gives the operator two clear options:

1. Add DNS and install a certificate covering both the configured and generated WWW names.
2. Use **No redirect** and request or clone a certificate for only the exact hostname.

For proxied sites, continue to use Cloudflare **Full (strict)** with a valid origin certificate. Flexible mode does not encrypt Cloudflare-to-origin traffic and can produce redirect loops.

## Existing domains are not changed

Upgrading to 0.4.26 does not enable redirects on existing primary domains or aliases. Their stored behavior remains unchanged.

The new default applies only when a newly created site's primary domain or a new alias does not already have an explicit WWW choice. That avoids surprising live traffic, DNS requirements, canonical URLs, or certificates during an upgrade.

If you want to adopt a redirect on an existing domain:

1. Open **Site > Domains**.
2. Select the domain's configuration action.
3. Choose the canonical WWW behavior.
4. Create DNS for every required hostname.
5. Ensure the active certificate covers the resulting set.
6. Save and test HTTP and HTTPS for both forms.

Fluxo preserves the request path and query string during managed WWW redirects.

## Clearer SSH hardening confirmation

The release also improves the confirmation step used when disabling SSH password login. When Fluxo can detect the server address, the interface shows it in the command the administrator should use for a second login test.

IPv6 addresses receive correct bracket formatting. When an address cannot be determined safely, the flow can still fall back to an explicit value rather than pretending a placeholder is a verified target.

The safety model remains the same:

- Confirm a working key-authenticated session.
- Retain provider-console recovery access.
- Test a second session before applying the hardened policy.
- Keep the original session open until the new connection succeeds.

Version 0.4.26 does not disable password login automatically. SSH hardening remains an explicit operator action.

## Upgrade to 0.4.26

Review current backups and recovery access first, then rerun the installer:

```bash
curl -fsSL https://fluxo.fottify.com/install.sh | sudo bash
```

To request this exact version:

```bash
curl -fsSL https://fluxo.fottify.com/install.sh | \
  FLUXO_VERSION=v0.4.26 sudo -E bash
```

The installer validates the release and its signed build provenance, snapshots Fluxo-owned state, stops the service cleanly, installs the candidate, and requires exact health and version responses. If the candidate fails those checks, the installer attempts to restore the previous application snapshot and verifies the recovered service.

Version 0.4.26 has no manual migration or service-configuration step. Existing domain behavior remains unchanged.

## Verify after upgrading

From the server, check:

```bash
fluxo --version
sudo systemctl status fluxo --no-pager
curl -k https://127.0.0.1:9595/api/v1/health
```

Then use the dashboard to verify:

1. Existing primary domains and aliases retain their current behavior.
2. Creating a root domain offers **Redirect from www** by default.
3. Creating a subdomain uses **No redirect** by default.
4. The SSL dialog lists the exact names required by the selection.
5. Any panel domain and important application domain still pass an external HTTPS check.

If a domain is behind Cloudflare, test through the public hostname with **Full (strict)** enabled rather than relying only on direct origin access.

## Why this release is small but useful

Domain defaults are configuration, and configuration becomes production behavior. A surprising `www.app.example.com` requirement can create unnecessary DNS, certificate, and CDN work; a misclassified `example.co.uk` can create the opposite problem.

Fluxo 0.4.26 narrows that ambiguity. Root domains get the familiar canonical redirect, subdomains stay exact by default, certificate requirements are visible before issuance, and existing sites remain untouched. It is a focused change aimed at making the safe path the obvious one.
