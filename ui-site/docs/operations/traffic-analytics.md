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

Open **Site > Observe > Traffic Analytics** and use **Range**. Choices follow the server retention policy: Today, Last 7 days, then 30/60/90/180/365 days up to the saved retention limit. **Keep until manually deleted** also offers **All retained history**. Ranges include today and use UTC calendar boundaries. The displayed from/to timestamps include the year. A retention reduction refreshes the menu and replaces an unavailable selection with the longest permitted range.

Reports use hourly totals up to 30 days, daily totals up to 180 days, then weekly totals. Very long retained history uses wider multiples of a week, keeping each chart and values table at no more than 720 periods. The final period may still be in progress. The historical `hour` field names each period's start; `bucket_seconds` reports its width. Collection gaps remain unavailable rather than becoming quiet zeros. Changing the view range does not change retention or recreate past data.

Reports normally become available within **30–60 seconds**. The page shows the last processed time, delayed collection, pending request data, and incomplete or capped data. Refresh retrieves the current aggregates; opening a report does not rescan historical logs. Visible pages poll while collection is active or a configuration change is pending. Disabled pages can be refreshed manually.

The report has three tabs with one shared date range, collection setting and history:

- **Overview** summarizes all origin requests, estimated page views, API requests and HTTP errors. Requests include assets, bots and APIs; they are not visits or people.
- **Pages** shows estimated page views, popular page paths and referring domains using the same collected data. It does not add browser tracking or count unique visitors.
- **API traffic** shows requests matching the site's configured API paths, HTTP response classes, origin bytes, request-weighted timing and endpoint groups. Its chart compares API requests with HTTP 4xx + 5xx responses.

Switching tabs changes the view only. It does not enable collection, create another collector or change the retention period.

| Metric | Meaning |
|---|---|
| Requests | Valid requests observed by the dedicated analytics stream, including bot and API requests. |
| Estimated page views | Successful or redirected `GET` page requests after excluding known bots, common assets, and API paths. HTML acceptance and page-like paths are heuristics, so the result remains an estimate. |
| Most requested page paths | Page paths counted by the estimate, without query strings. Counts may be grouped into an `(other)` row when dimension limits are reached. |
| Referrer domains | Available referring hostnames, without full referring URLs. Requests without a usable referrer are omitted from this ranking. |
| 404 / other 4xx / 5xx | Nginx response counts for each error group; other 4xx excludes 404. |
| Nginx response bytes | Response body bytes sent by Nginx at this origin, excluding response headers. These are origin bytes, not total CDN delivery. |
| Avg Nginx request time | Request-weighted average Nginx request duration in milliseconds, including upstream processing and response transmission. It does not measure browser rendering or page speed. |

Missing referrers can result from direct navigation, apps, browser privacy settings, or a site's referrer policy. Referrer counts do not identify people or establish a visit's acquisition source with certainty.

## API traffic

The default API prefix is `/api`: it matches `/api` and `/api/...`, including a trailing slash, but not `/apiculture`. Configure additional API prefixes or explicit route patterns from the API traffic tab. A route such as `/api/users/:id` groups one path segment under the literal pattern and remains separated by HTTP method. `/api/users/me` can be an explicit, more specific route. Fluxo does not guess which path segments are IDs, inspect request bodies, or infer application routes from source code.

Explicit route patterns can classify paths outside the prefixes. Paths matching a prefix without a route retain their sanitized path, subject to the endpoint limits. Configured API requests are excluded from the estimated page-view count from that rule's effective time onward. Changing rules does not reclassify stored aggregates. Requests that were queued before a rule change use the rules in effect at their recorded time.

API metrics include all matching methods and bots. **2xx** counts successful HTTP statuses, **3xx** counts redirects, **4xx** counts client errors, **5xx** counts server errors, and **1xx** is reported separately if recorded. The red error line is only 4xx + 5xx; redirects are not HTTP errors. Average request time is the sum of Nginx request durations divided by matching requests, including when results span hours or endpoints. Nginx response bytes are response-body bytes at the origin.

**HTTP 200 can contain an application error.** Nginx status logs cannot detect errors encoded inside JSON or GraphQL responses. A configured `/graphql` path can be counted as API traffic, but operations and application errors are not inspected.

API-specific history starts when this collector supports API aggregation. Older overall traffic summaries cannot reconstruct it. Earlier API intervals remain unavailable, even if the Overview has data. An observed quiet API hour can be zero; an unavailable or disabled interval is a gap. Settings can be saved while collection is off without enabling it, and re-enabling does not backfill disabled periods.

## Gaps, delayed data, and limits

An unavailable period appears as a gap, with no numeric value. It is not zero traffic. A partial period can contain collected requests alongside a disabled or unavailable interval. Use **Hourly values**, **Daily values** or **Weekly values** (according to the range), and **Collection gaps** to inspect coverage.

Disabling collection creates a deliberate gap; re-enabling starts another interval. Backlogs, malformed records, unavailable or rotated logs, and storage pressure can instead make an interval incomplete. The page marks affected data and reports dropped bytes when known. These counts may understate actual requests.

Collection uses bounded incremental batches, a separate SQLite analytics database, bounded page/referrer dimensions, and managed log rotation. Log-size limits are **soft**: files can grow beyond the target between checks, while requests drain, or while rotation is waiting. Sustained traffic beyond the collection budget or storage pressure can discard request data and produce an incomplete report. These bounds do not promise a fixed CPU cost or a hard maximum filesystem footprint.

| V1 control | Setting |
|---|---|
| Effective site selection | At most 32 sites collecting simultaneously. |
| Collection work | A pass processes at most 4 MiB and 32,768 records across sites, with a one-second time budget. Busy sites can show a backlog. |
| Path/referrer dimensions | Up to 64 page paths and 32 referring domains per site/hour, plus an `(other)` bucket. A shared 50,000-row dimension budget can cause grouping earlier. Reports list up to 20 ranked values. |
| API classification | At most 16 literal prefixes and 32 explicit route patterns per site. Pattern matching is bounded and ambiguous patterns are rejected. |
| API endpoint dimensions | Up to 64 method/path pairs per site/hour and 50,000 retained endpoint rows, plus an `(other)` row when capped. Reports show up to 100 ranked groups; request totals still include overflow. |
| API rule history | Up to 128 retained rule intervals and 64 KiB of serialized rule settings per site. Rules apply forward and are kept for delayed records within retention. Further changes are refused at either limit; old rules are not discarded to admit a change. |
| Analytics database | 256 MiB main-database ceiling, enforced using the actual SQLite page size. Ingestion pauses with 8 MiB reserved for control/checkpoint work; unused pages can be reused after expiry or explicit deletion. The WAL is checkpointed around an 8 MiB baseline and can temporarily exceed it during a bounded transaction. |
| Current-log rotation target | 4 MiB, once the previous interval has been collected and Nginx can safely retire its descriptor. |
| Per-site analytics log pressure | 16 MiB across the current and retained rotation; collection for that site is disabled. |
| Server analytics log pressure | 128 MiB across managed analytics logs; server collection is disabled. |
| Available-space cutoff | Below 128 MiB on the analytics filesystem, server collection is disabled. |

Pressure-triggered changes still apply and drain through the normal pending states. Read the displayed reason, address storage or sustained load, then use the same **Enable** switch to collect again. Existing aggregates remain available until retention or explicit history deletion removes them.

## Retention and deletion

Choose one server-wide policy under **Observe > Traffic Analytics > Traffic history retention**: **30 days** (the default), **60 days**, **90 days**, **180 days**, **365 days**, or **Keep until manually deleted**. It applies to all sites and survives panel restarts and updates. Existing installations keep the 30-day default. Turning collection off preserves history under this same policy.

Finite policies expire older hourly aggregates during bounded, hourly cleanup, including while collection is disabled. Shortening retention makes older history eligible for deletion. Lengthening retention cannot recover deleted aggregates, import ordinary access logs, or fill intervals when collection was disabled. The view menu expands with the saved retention policy. Selecting a longer view may reveal retained older records, but periods before collection started or after history was deleted stay unavailable.

**Keep until manually deleted** disables age-based aggregate deletion and the parser's age limit. It does not disable storage, cardinality, log or processing budgets. At analytics storage capacity or disk pressure, collection pauses visibly and drains its dedicated Nginx producer; retained aggregates are never automatically evicted to admit new data. Free space or explicitly delete unneeded site history, then enable collection again. Delayed or discarded unprocessed log data can leave incomplete intervals.

Classification preferences for living sites survive both disablement and history deletion. Preferences for deleted panel sites are reclaimed during analytics reconciliation; retained aggregates and historical rules still follow their retention period. Up to 256 sites can have saved API preferences at once.

Use **Delete analytics history** on the site's analytics page to remove its retained analytics. Fluxo asks for explicit confirmation. This action leaves the collection setting unchanged: an enabled site continues collecting new requests after deletion. Earlier queued requests are excluded so they do not recreate the deleted history.

## Data and privacy

The dedicated analytics stream records request timing, a bounded path without its query string, method, status, response bytes, page/bot classification flags, and a referring domain when available. It does not record IP addresses, cookies, sessions, full referrer URLs, raw user-agent strings, or geographic identifiers. No visitor profile, unique-person count, or GeoIP lookup is created.

These settings govern the dedicated analytics stream. Ordinary Nginx access/error logs remain separately available under **Logs**.

Analytics aggregates and cursors live in `traffic/analytics.db` under the Fluxo data directory, separate from the main `fluxo.db`. In production this is normally `/var/lib/fluxo/traffic/analytics.db`. See [Paths and services](../reference/paths-services) and the [Traffic Analytics API](../reference/api#traffic-analytics).


The dashboard keeps coverage, UTC/freshness, estimates, missing-data and retention consequences next to their metrics/actions. **About these numbers** and **Endpoint details** disclose the full interpretation and bounded-storage/matching notes without duplicating them in every chart. API HTTP status groups describe responses, not application-level success. Navigation puts Traffic Analytics last in Observe; the collection, query, retention and deletion behavior is unchanged.

Both server and site Observe navigation place Logs, Activity, then Traffic Analytics last; the server's existing Monitoring entry is retained. Main Traffic actions use 40px default-md buttons. Range filters and table-row links keep their compact shared sizing.
