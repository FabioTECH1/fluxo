---
title: "Fluxo 0.4.56: Traffic Analytics and Site Network Controls"
excerpt: See which pages and API endpoints reach your server, protect private sites, add redirects, and route wildcard subdomains from Fluxo.
category: Releases
date: 2026-11-23
image: /blog/fluxo-0-4-56-traffic-network.webp
imageAlt: A traffic analytics dashboard connected to protected website routes and wildcard subdomains
featured: true
---

Which pages are getting requests? Which API endpoints are returning errors? How do you keep a staging site private or send an old URL to its replacement?

Fluxo 0.4.56 brings those tasks into the dashboard with **Traffic Analytics** and **Site Network Controls**. You can read traffic reports for each site, choose how long to retain history, and manage password protection, redirects and wildcard routing alongside your existing site settings.

Here is how to put them to use.

## Understand the traffic reaching your server

Traffic Analytics summarizes requests reaching your site's Nginx server. Open **Observe > Traffic Analytics**, enable server collection, and choose the sites you want to include. Each selected site then has its own reports under **Site > Observe > Traffic Analytics**.

There is no tracking script to add to your application. Collection starts when you enable it, with reports usually appearing within 30–60 seconds.

### Three views of the same traffic

The reports are organized into three tabs:

| Report | What you can see |
|---|---|
| **Overview** | Requests, estimated page views, API requests and HTTP errors over time. |
| **Pages** | Popular page paths, estimated page views and referring domains. |
| **API traffic** | Requests by endpoint and method, HTTP status groups, response bytes and average Nginx request time. |

For example, **Pages** can help you find a frequently requested landing page. **Overview** can show a rise in 404s after a deployment. **API traffic** can help you identify which endpoint groups account for errors or longer request times.

Requests and page views mean different things. Request totals include assets, bots and APIs. Estimated page views exclude known bots, common assets and configured API paths, so they remain an estimate rather than a count of individual visitors.

These reports cover your origin server. Requests served entirely by a CDN or browser cache do not reach it and are not included. The analytics data does not store IP addresses, cookies or visitor profiles.

### Group API requests into useful endpoints

The default API prefix is `/api`. Use **Configure API** to add prefixes or patterns that match your application.

A pattern such as `/api/orders/:id` groups requests for individual orders into one endpoint, while keeping HTTP methods separate. This makes it easier to compare activity without reading a separate row for every order URL.

Rule changes apply to new traffic; they do not reclassify old reports. HTTP errors are based on response status codes, so an application returning an error inside a `200` response will still appear as HTTP success.

### Keep the history you need

Choose **30, 60, 90, 180 or 365 days**, or keep history until you delete it manually. The report's range selector follows that retention setting, including **All retained history** for manual retention.

Longer ranges use broader chart intervals to keep reports manageable. Dates use UTC, and the report shows the interval you are viewing.

Increasing retention keeps more future history; it cannot restore deleted records or periods when collection was off. Missing coverage appears as gaps rather than invented zero traffic. Storage limits still apply, including with manual retention.

The [Traffic Analytics guide](/docs/operations/traffic-analytics) covers collection, metrics, API rules and retention in detail.

## Control site access and routing

Some site changes should be straightforward: put shared credentials in front of a private preview, redirect a retired page, or let one application serve unmatched subdomains.

Version 0.4.56 adds password protection and path redirects under **Site > Settings > Network**, with wildcard routing in the existing domain configuration. These controls work with Fluxo-managed Nginx configurations.

### Put a password in front of a private site

Enable **Protect this site** and enter a username and password. Visitors must then provide those shared credentials before accessing the site's content. It is useful for a staging environment, client preview or temporary private site.

You can generate a password or reveal the value you are entering. Saved passwords cannot be retrieved; **Change credentials** lets you replace them. Use HTTPS to protect credentials in transit.

This is a shared access gate. Your application's own login and permissions continue to serve their usual purpose. Certificate-validation paths remain available for renewal, and a redirect destination needs its own protection.

### Give old URLs a new destination

In the **Redirects** card, add a source path, a destination and a status:

- **301 Permanent** for a lasting move, such as `/old-pricing` to `/pricing`.
- **302 Temporary** for a change you expect to reverse.

Destinations can be another path on the site or an HTTP/HTTPS URL. Rules match exact paths: `/old-pricing` does not also match `/old-pricing/` or everything beneath it.

Browsers may cache permanent redirects. Use a temporary redirect while testing a destination or running a short-lived campaign. These rules are separate from your domain's WWW redirect setting.

### Serve unmatched subdomains with one site

Open a domain's three-dot menu, choose **Configure domain**, and enable **Allow wildcard subdomains**. You can also choose it while adding a domain.

For `example.com`, wildcard routing lets unmatched subdomains such as `alice.example.com` and `bob.example.com` reach that site. Your application decides what to serve for each hostname; the toggle does not create separate applications or tenants.

**Add wildcard DNS at your DNS provider too.** For example, create an A record for `*.example.com` pointing to your VPS's IPv4 address. Fluxo configures server routing but does not create DNS records. Sites already assigned an exact hostname keep priority over the wildcard.

HTTPS is configured separately under **Domains > SSL**. An active certificate for `example.com` alone does not cover its subdomains. A certificate for `*.example.com` covers one subdomain level, but not the root name or deeper names such as `a.b.example.com`.

Import a suitable certificate covering the names you need, or clone a compatible custom certificate. Fluxo's built-in Let's Encrypt HTTP validation cannot issue wildcard certificates, and imported certificates need their own renewal arrangement.

### Keep managed settings and custom configuration clear

Fluxo validates generated Nginx changes before reloading configuration. If you use a custom vhost, restore **Fluxo Default** to use these managed controls. Active protection, redirects and wildcards must be explicitly disabled before saving manual vhost edits.

The [Domains and SSL guide](/docs/site-management/domains-ssl) explains the Network controls, DNS, certificate coverage and custom-vhost requirements.

Fluxo 0.4.56 is available now. Follow the [installation and update guide](/docs/getting-started/installation), then enable Traffic collection or open a site's Network settings to try the features you need.
