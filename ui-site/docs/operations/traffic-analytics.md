---
title: Traffic Analytics
description: Enable per-site origin request analytics, understand collection gaps and estimates, and manage retained history.
---

# Traffic analytics

Traffic Analytics V1 summarizes requests reaching each site's Nginx origin. It is disabled by default, with a server switch and a separate selection for each site.

Based on requests reaching this server. Visits served entirely from a CDN or browser cache are not included.

## Enable collection

1. Open **Observe > Traffic** and enable traffic collection on the server.
2. Select the sites to collect in the same page, or open **Site > Observe > Traffic Analytics** and enable that site.
3. Wait for the collection status to finish applying. **Applying change** and **Draining** mean the operation is still in progress.

Both switches must be enabled for a site to collect. Site selections are preserved when the server switch is off. V1 supports up to **32 effectively enabled sites** at once.

Enabling starts a new collection interval. It does not import earlier requests from ordinary access logs. After a configuration failure, Fluxo reports the error and displays the saved state; refresh and review that state before trying the same switch again.

Disabling applies the Nginx configuration and drains the previous producer before completing the collection change. A pending disable must not be treated as already stopped. Use the same **Enable** switch to start collection again; there is no separate resume action.

## Read a report

Open **Site > Observe > Traffic Analytics** and choose **Today**, **7 days**, or **30 days**. All boundaries and hourly buckets use **UTC**. The longer ranges include today and the preceding 6 or 29 calendar days; the current hour is still in progress.

Reports normally become available within **30–60 seconds**. The page shows the last processed time, delayed collection, pending request data, and incomplete or capped data. Refresh retrieves the current aggregates; opening a report does not rescan historical logs. Visible pages poll while collection is active or a configuration change is pending. Disabled pages can be refreshed manually.

| Metric | Meaning |
|---|---|
| Requests | Valid requests observed by the dedicated analytics stream, including bot and API requests. |
| Estimated page views | Successful or redirected `GET`/`HEAD` page requests after excluding known bots, common assets, and API paths. HTML acceptance and page-like paths are heuristics, so the result remains an estimate. |
| Most requested page paths | Page paths counted by the estimate, without query strings. Counts may be grouped into an `(other)` row when dimension limits are reached. |
| Referrer domains | Available referring hostnames, without full referring URLs. Requests without a usable referrer are omitted from this ranking. |
| 404 / other 4xx / 5xx | Nginx response counts for each error group; other 4xx excludes 404. |
| Nginx response bytes | Response body bytes sent by Nginx at this origin, excluding response headers. These are origin bytes, not total CDN delivery. |
| Avg Nginx request time | Request-weighted average Nginx request duration in milliseconds, including upstream processing and response transmission. It does not measure browser rendering or page speed. |

Missing referrers can result from direct navigation, apps, browser privacy settings, or a site's referrer policy. Referrer counts do not identify people or establish a visit's acquisition source with certainty.

## Gaps, delayed data, and limits

An unavailable hour appears as a gap, with no numeric value. It is not zero traffic. A partial hour can contain collected requests alongside a disabled or unavailable interval. Use **Hourly values** and **Collection gaps** to inspect coverage.

Disabling collection creates a deliberate gap; re-enabling starts another interval. Backlogs, malformed records, unavailable or rotated logs, and storage pressure can instead make an interval incomplete. The page marks affected data and reports dropped bytes when known. These counts may understate actual requests.

Collection uses bounded incremental batches, a separate SQLite analytics database, bounded page/referrer dimensions, and managed log rotation. Log-size limits are **soft**: files can grow beyond the target between checks, while requests drain, or while rotation is waiting. Sustained traffic beyond the collection budget or storage pressure can discard request data and produce an incomplete report. These bounds do not promise a fixed CPU cost or a hard maximum filesystem footprint.

| V1 control | Setting |
|---|---|
| Effective site selection | At most 32 sites collecting simultaneously. |
| Current-log rotation target | 4 MiB, once the previous interval has been collected and Nginx can safely retire its descriptor. |
| Per-site analytics log pressure | 16 MiB across the current and retained rotation; collection for that site is disabled. |
| Server analytics log pressure | 128 MiB across managed analytics logs; server collection is disabled. |
| Available-space cutoff | Below 128 MiB on the analytics filesystem, server collection is disabled. |

Pressure-triggered changes still apply and drain through the normal pending states. Read the displayed reason, address storage or sustained load, then use the same **Enable** switch to collect again. Existing aggregates remain available until retention or explicit history deletion removes them.

## Retention and deletion

Aggregates are retained for **30 days**, including while collection is disabled. Turning a switch off does not delete history.

Use **Delete analytics history** on the site's analytics page to remove its retained analytics. Fluxo asks for explicit confirmation. This action leaves the collection setting unchanged: an enabled site continues collecting new requests after deletion. Earlier queued requests are excluded so they do not recreate the deleted history.

## Data and privacy

The dedicated analytics stream records request timing, a bounded path without its query string, method, status, response bytes, page/bot classification flags, and a referring domain when available. It does not record IP addresses, cookies, sessions, full referrer URLs, raw user-agent strings, or geographic identifiers. No visitor profile, unique-person count, or GeoIP lookup is created.

These settings govern the dedicated analytics stream. Ordinary Nginx access/error logs remain separately available under **Logs**.

Analytics aggregates and cursors live in `traffic/analytics.db` under the Fluxo data directory, separate from the main `fluxo.db`. In production this is normally `/var/lib/fluxo/traffic/analytics.db`. See [Paths and services](../reference/paths-services) and the [Traffic Analytics API](../reference/api#traffic-analytics).
